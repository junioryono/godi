package reflection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// Fuzz targets for the reflection layer. `go test` runs only the seed corpus
// (f.Add plus testdata/fuzz); run a target with
//
//	go test -run '^$' -fuzz '^FuzzAnalyzeGeneratedConstructor$' -fuzztime 30s ./internal/reflection

// ---------------------------------------------------------------------------
// Struct tags
// ---------------------------------------------------------------------------

// referenceTagInfo is an independent statement of the tag rules: optional is
// on only for "true", name and group are taken verbatim when present, and
// inject:"-" excludes the field.
func referenceTagInfo(tag reflect.StructTag) parsedTags {
	var info parsedTags
	if v, ok := tag.Lookup("optional"); ok {
		info.Optional = v == "true"
	}
	info.Name, _ = tag.Lookup("name")
	info.Group, _ = tag.Lookup("group")
	if v, ok := tag.Lookup("inject"); ok && v == "-" {
		info.Ignore = true
	}
	return info
}

func FuzzParseFieldTags(f *testing.F) {
	for _, seed := range []string{
		``,
		`optional:"true"`,
		`optional:"false"`,
		`optional:"TRUE"`,
		`name:"primary"`,
		`name:""`,
		`group:"routes"`,
		`name:"a" group:"b"`,
		`inject:"-"`,
		`inject:"-" name:"x"`,
		`optional:"true" name:"cache" json:"c"`,
		`name:"a" name:"b"`,
		`name:"unterminated`,
		`name: "space"`,
		`:"empty key"`,
		`name:"esc\"aped" group:"g\\"`,
		"name:\"tab\tinside\"",
		`json:"x,omitempty"`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		a := New()
		tag := reflect.StructTag(raw)

		got := a.parseFieldTags(tag)
		if want := referenceTagInfo(tag); got != want {
			t.Fatalf("parseFieldTags(%q) = %+v, want %+v", raw, got, want)
		}

		// The same tag on an In field must be analyzed consistently.
		fieldType := reflect.TypeFor[*fuzzA]()
		inStruct := reflect.StructOf([]reflect.StructField{
			{Name: "In", Type: inType, Anonymous: true},
			{Name: "Field", Type: fieldType, Tag: tag},
		})
		fn := makeFunc(reflect.FuncOf([]reflect.Type{inStruct}, []reflect.Type{fieldType}, false))
		info, err := a.Analyze(fn)
		if err != nil {
			t.Fatalf("Analyze(In with tag %q): %v", raw, err)
		}
		if !info.IsParamObject {
			t.Fatalf("In struct with tag %q not recognized as a parameter object", raw)
		}
		checkParamObject(t, info, inStruct)

		// And on an Out field.
		outStruct := reflect.StructOf([]reflect.StructField{
			{Name: "Out", Type: outType, Anonymous: true},
			{Name: "Field", Type: fieldType, Tag: tag},
		})
		fn = makeFunc(reflect.FuncOf(nil, []reflect.Type{outStruct}, false))
		info, err = a.Analyze(fn)
		if err != nil {
			t.Fatalf("Analyze(Out with tag %q): %v", raw, err)
		}
		if !info.IsResultObject {
			t.Fatalf("Out struct with tag %q not recognized as a result object", raw)
		}
		checkResultObject(t, info, outStruct)
	})
}

// ---------------------------------------------------------------------------
// Generated constructor shapes
// ---------------------------------------------------------------------------

type (
	fuzzA      struct{ ID int }
	fuzzB      struct{ Name string }
	fuzzLogger interface{ Log(string) }
	// fuzzStructError is an error whose values can never be nil.
	fuzzStructError struct{ msg string }
)

func (e fuzzStructError) Error() string { return e.msg }
func (*fuzzA) Close() error             { return nil }
func (e *fuzzB) String() string         { return e.Name }

// typeChoice is one entry of a pool of types a generated signature draws from.
// Struct entries are built from further fuzz bytes.
type typeChoice int

const (
	choicePtrA typeChoice = iota
	choicePtrB
	choiceLogger
	choiceSliceA
	choiceChan
	choiceUnsafe
	choiceError
	choiceContext
	choiceFunc
	choiceStructErr
	choicePtrStructErr
	choiceInt
	choiceString
	choiceMap
	choiceInStruct
	choiceInStructPtr
	choiceOutStruct
	choiceOutStructPtr
	numTypeChoices
)

var simpleTypes = map[typeChoice]reflect.Type{
	choicePtrA:         reflect.TypeFor[*fuzzA](),
	choicePtrB:         reflect.TypeFor[*fuzzB](),
	choiceLogger:       reflect.TypeFor[fuzzLogger](),
	choiceSliceA:       reflect.TypeFor[[]*fuzzA](),
	choiceChan:         reflect.TypeFor[chan int](),
	choiceUnsafe:       reflect.TypeFor[unsafe.Pointer](),
	choiceError:        reflect.TypeFor[error](),
	choiceContext:      reflect.TypeFor[context.Context](),
	choiceFunc:         reflect.TypeFor[func() int](),
	choiceStructErr:    reflect.TypeFor[fuzzStructError](),
	choicePtrStructErr: reflect.TypeFor[*fuzzStructError](),
	choiceInt:          reflect.TypeFor[int](),
	choiceString:       reflect.TypeFor[string](),
	choiceMap:          reflect.TypeFor[map[string]int](),
}

// fieldTypes and fieldTags are the pools In/Out struct fields draw from.
var (
	fieldTypes = []reflect.Type{
		reflect.TypeFor[*fuzzA](),
		reflect.TypeFor[*fuzzB](),
		reflect.TypeFor[fuzzLogger](),
		reflect.TypeFor[[]*fuzzA](),
		reflect.TypeFor[[]fuzzLogger](),
		reflect.TypeFor[chan int](),
		reflect.TypeFor[unsafe.Pointer](),
		reflect.TypeFor[error](),
		reflect.TypeFor[context.Context](),
		reflect.TypeFor[int](),
	}
	fieldTags = []reflect.StructTag{
		``,
		`optional:"true"`,
		`name:"k"`,
		`name:"k2"`,
		`group:"g"`,
		`name:"k" group:"g"`,
		`inject:"-"`,
		`optional:"true" name:"k"`,
		`optional:"true" group:"g"`,
		`optional:"yes"`,
	}
)

// byteReader hands out fuzz bytes, then zeros once they run out.
type byteReader struct{ data []byte }

func (r *byteReader) next() int {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[0]
	r.data = r.data[1:]
	return int(b)
}

// genStruct builds a struct embedding marker (In or Out) at a fuzzed position
// among up to three exported fields with fuzzed types and tags.
func genStruct(r *byteReader, marker reflect.Type) reflect.Type {
	n := r.next() % 4
	embedAt := r.next() % (n + 1)
	fields := make([]reflect.StructField, 0, n+1)
	for i := range n {
		if i == embedAt {
			fields = append(fields, reflect.StructField{Name: marker.Name(), Type: marker, Anonymous: true})
		}
		fields = append(fields, reflect.StructField{
			Name: fmt.Sprintf("F%d", i),
			Type: fieldTypes[r.next()%len(fieldTypes)],
			Tag:  fieldTags[r.next()%len(fieldTags)],
		})
	}
	if embedAt == n {
		fields = append(fields, reflect.StructField{Name: marker.Name(), Type: marker, Anonymous: true})
	}
	return reflect.StructOf(fields)
}

func genType(r *byteReader) reflect.Type {
	c := typeChoice(r.next() % int(numTypeChoices))
	switch c {
	case choiceInStruct:
		return genStruct(r, inType)
	case choiceInStructPtr:
		return reflect.PointerTo(genStruct(r, inType))
	case choiceOutStruct:
		return genStruct(r, outType)
	case choiceOutStructPtr:
		return reflect.PointerTo(genStruct(r, outType))
	default:
		return simpleTypes[c]
	}
}

// genFuncType decodes a constructor signature: up to four parameters and
// three results, optionally variadic (when the last parameter is a slice).
func genFuncType(data []byte) reflect.Type {
	r := &byteReader{data: data}
	in := make([]reflect.Type, r.next()%5)
	for i := range in {
		in[i] = genType(r)
	}
	out := make([]reflect.Type, r.next()%4)
	for i := range out {
		out[i] = genType(r)
	}
	variadic := r.next()%2 == 1 && len(in) > 0 && in[len(in)-1].Kind() == reflect.Slice
	return reflect.FuncOf(in, out, variadic)
}

// makeFunc implements fnType with a body returning zero values.
func makeFunc(fnType reflect.Type) any {
	return reflect.MakeFunc(fnType, func([]reflect.Value) []reflect.Value {
		results := make([]reflect.Value, fnType.NumOut())
		for i := range results {
			results[i] = reflect.Zero(fnType.Out(i))
		}
		return results
	}).Interface()
}

var constructorSeeds = [][]byte{
	{},                                 // func()
	{0, 1, 0},                          // func() *A
	{1, 0, 2, 0, 6},                    // func(*A) (*A, error)
	{2, 0, 1, 2, 0, 1, 6},              // func(*A, *B) (*A, *B, error)
	{1, 14, 3, 1, 0, 0, 1, 0},          // func(In{F0 *A, F1 *A `optional`}) *A
	{1, 15, 2, 0, 3, 4, 1, 0},          // func(*In{..., group slice}) *A
	{1, 14, 1, 0, 0, 4, 1, 0},          // In with group tag on a non-slice field
	{1, 14, 1, 0, 3, 5, 1, 0},          // In with name+group
	{2, 14, 0, 0, 0, 1, 0},             // In mixed with another parameter
	{0, 2, 16, 2, 0, 0, 2, 1, 4, 6},    // func() (Out{...}, error)
	{0, 3, 17, 1, 0, 0, 0, 0, 0},       // func() (*Out, *A, *A): too many
	{0, 2, 16, 0, 0, 0},                // func() (Out, *A): non-error second
	{0, 1, 9},                          // func() fuzzStructError
	{0, 2, 0, 9},                       // func() (*A, fuzzStructError)
	{0, 2, 6, 6},                       // func() (error, error)
	{0, 2, 6, 0},                       // func() (error, *A)
	{0, 1, 4},                          // func() chan int
	{0, 1, 5},                          // func() unsafe.Pointer
	{1, 4, 1, 0},                       // func(chan int) *A
	{1, 5, 1, 0},                       // func(unsafe.Pointer) *A
	{1, 3, 1, 0, 1},                    // func(...*A) *A
	{1, 7, 1, 7},                       // func(context.Context) context.Context
	{0, 2, 0, 10},                      // func() (*A, *fuzzStructError)
	{1, 16, 1, 0, 0, 0, 1, 0},          // Out struct used as a parameter
	{0, 1, 14, 1, 0, 0, 0},             // In struct used as a result
	{0, 1, 16, 3, 3, 0, 0, 1, 0, 2, 3}, // Out with group slice + keyed fields
}

func FuzzAnalyzeGeneratedConstructor(f *testing.F) {
	for _, seed := range constructorSeeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		fnType := genFuncType(data)
		fn := makeFunc(fnType)
		a := New(WithNotFound(NotFoundPolicy))

		info, err := a.Analyze(fn)
		if err != nil {
			// Analysis of a function only fails for a non-struct In/Out,
			// which hasEmbeddedType never reports.
			t.Fatalf("Analyze(%s): %v", fnType, err)
		}
		checkConstructorInfo(t, info, fnType)

		again, err := a.Analyze(fn)
		if err != nil || again != info {
			t.Fatalf("Analyze(%s) again = %p, %v; want cached %p", fnType, again, err, info)
		}

		uncached, err := a.AnalyzeUncached(fn)
		if err != nil {
			t.Fatalf("AnalyzeUncached(%s): %v", fnType, err)
		}
		if uncached == info {
			t.Fatalf("AnalyzeUncached(%s) returned the cached analysis", fnType)
		}
		if !sameAnalysis(info, uncached) {
			t.Fatalf("AnalyzeUncached(%s) = %+v, Analyze = %+v", fnType, uncached, info)
		}

		// The derived accessors must not panic, and must agree with Returns.
		serviceType, serviceErr := a.GetServiceType(fn)
		resultTypes, _ := a.GetResultTypes(fn)
		nonError := 0
		for _, ret := range info.Returns {
			if !ret.IsError {
				nonError++
			}
		}
		if len(resultTypes) != nonError {
			t.Fatalf("GetResultTypes(%s) = %v, want %d types", fnType, resultTypes, nonError)
		}
		if (serviceErr == nil) != (len(info.Returns) > 0 && (info.IsResultObject || nonError > 0)) {
			t.Fatalf("GetServiceType(%s) = %v, %v inconsistent with Returns %+v", fnType, serviceType, serviceErr, info.Returns)
		}

		// Invoking the analysis must not panic either: failures are errors.
		// (Variadic constructors are rejected before they are ever invoked.)
		if !fnType.IsVariadic() {
			invokeGenerated(t, a, info, fnType)
		}
	})
}

// checkConstructorInfo verifies an analysis against the signature it came
// from, using only reflect and the documented rules.
func checkConstructorInfo(t *testing.T, info *ConstructorInfo, fnType reflect.Type) {
	t.Helper()
	if !info.IsFunc || info.Type != fnType {
		t.Fatalf("IsFunc=%v Type=%v, want a function analysis of %s", info.IsFunc, info.Type, fnType)
	}

	wantParamObject := fnType.NumIn() == 1 && embeds(fnType.In(0), inType)
	if info.IsParamObject != wantParamObject {
		t.Fatalf("%s: IsParamObject = %v, want %v", fnType, info.IsParamObject, wantParamObject)
	}
	if wantParamObject {
		checkParamObject(t, info, fnType.In(0))
	} else {
		if len(info.Parameters) != fnType.NumIn() {
			t.Fatalf("%s: %d parameters analyzed, want %d", fnType, len(info.Parameters), fnType.NumIn())
		}
		for i, p := range info.Parameters {
			pt := fnType.In(i)
			if p.Type != pt || p.Index != i || p.IsSlice != (pt.Kind() == reflect.Slice) ||
				p.Key != nil || p.Group != "" || p.Optional {
				t.Fatalf("%s: parameter %d = %+v", fnType, i, p)
			}
			if p.IsSlice != (p.ElemType != nil) || (p.IsSlice && p.ElemType != pt.Elem()) {
				t.Fatalf("%s: parameter %d ElemType = %v", fnType, i, p.ElemType)
			}
		}
	}

	deps := info.Dependencies()
	if len(deps) != len(info.Parameters) {
		t.Fatalf("%s: %d dependencies for %d parameters", fnType, len(deps), len(info.Parameters))
	}
	for i, d := range deps {
		p := info.Parameters[i]
		wantType := p.Type
		if p.IsSlice && p.Group != "" {
			wantType = p.ElemType
		}
		if d.Type != wantType || d.Key != p.Key || d.Group != p.Group || d.Optional != p.Optional ||
			d.Index != p.Index || d.FieldName != p.Name {
			t.Fatalf("%s: dependency %d = %+v for parameter %+v", fnType, i, d, p)
		}
	}

	wantResultObject := fnType.NumOut() > 0 && embeds(fnType.Out(0), outType)
	if info.IsResultObject != wantResultObject {
		t.Fatalf("%s: IsResultObject = %v, want %v", fnType, info.IsResultObject, wantResultObject)
	}
	switch {
	case fnType.NumOut() == 0:
		if len(info.Returns) != 0 || info.HasErrorReturn {
			t.Fatalf("%s: Returns=%+v HasErrorReturn=%v for no results", fnType, info.Returns, info.HasErrorReturn)
		}
	case wantResultObject:
		checkResultObject(t, info, fnType.Out(0))
		wantErr := fnType.NumOut() == 2 && implementsError(fnType.Out(1))
		if info.HasErrorReturn != wantErr {
			t.Fatalf("%s: HasErrorReturn = %v, want %v", fnType, info.HasErrorReturn, wantErr)
		}
	default:
		if len(info.Returns) != fnType.NumOut() {
			t.Fatalf("%s: %d returns analyzed, want %d", fnType, len(info.Returns), fnType.NumOut())
		}
		last := fnType.NumOut() - 1
		for i, ret := range info.Returns {
			wantErr := i == last && implementsError(fnType.Out(i))
			if ret.Type != fnType.Out(i) || ret.Index != i || ret.IsError != wantErr || ret.Key != nil || ret.Group != "" {
				t.Fatalf("%s: return %d = %+v", fnType, i, ret)
			}
		}
		if info.HasErrorReturn != implementsError(fnType.Out(last)) {
			t.Fatalf("%s: HasErrorReturn = %v", fnType, info.HasErrorReturn)
		}
	}
}

// checkParamObject verifies the analyzed fields of an In struct (or pointer).
func checkParamObject(t *testing.T, info *ConstructorInfo, paramType reflect.Type) {
	t.Helper()
	st := paramType
	if st.Kind() == reflect.Pointer {
		st = st.Elem()
	}
	var want []ParameterInfo
	for i := range st.NumField() {
		field := st.Field(i)
		if !field.IsExported() || (field.Anonymous && field.Type == inType) {
			continue
		}
		tag := referenceTagInfo(field.Tag)
		if tag.Ignore {
			continue
		}
		p := ParameterInfo{
			Type: field.Type, Name: field.Name, Tag: string(field.Tag), Index: i,
			Optional: tag.Optional, Group: tag.Group,
			IsSlice: field.Type.Kind() == reflect.Slice,
		}
		if p.IsSlice {
			p.ElemType = field.Type.Elem()
		}
		if tag.Name != "" {
			p.Key = tag.Name
		}
		want = append(want, p)
	}
	if !reflect.DeepEqual(nilIfEmpty(info.Parameters), want) {
		t.Fatalf("In %v: parameters\n got %+v\nwant %+v", paramType, info.Parameters, want)
	}
}

// checkResultObject verifies the analyzed fields of an Out struct (or pointer).
func checkResultObject(t *testing.T, info *ConstructorInfo, resultType reflect.Type) {
	t.Helper()
	st := resultType
	if st.Kind() == reflect.Pointer {
		st = st.Elem()
	}
	var want []ReturnInfo
	for i := range st.NumField() {
		field := st.Field(i)
		if !field.IsExported() || (field.Anonymous && field.Type == outType) {
			continue
		}
		tag := referenceTagInfo(field.Tag)
		if tag.Ignore {
			continue
		}
		ret := ReturnInfo{Type: field.Type, Name: field.Name, Tag: string(field.Tag), Index: i, Group: tag.Group}
		if tag.Name != "" {
			ret.Key = tag.Name
		}
		want = append(want, ret)
	}
	if !reflect.DeepEqual(nilIfEmpty(info.Returns), want) {
		t.Fatalf("Out %v: returns\n got %+v\nwant %+v", resultType, info.Returns, want)
	}
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// embeds reports whether t (or the struct it points to) has marker as an
// embedded field, independently of hasEmbeddedType.
func embeds(t, marker reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := range t.NumField() {
		if f := t.Field(i); f.Anonymous && f.Type == marker {
			return true
		}
	}
	return false
}

func sameAnalysis(a, b *ConstructorInfo) bool {
	return a.Type == b.Type && a.IsFunc == b.IsFunc &&
		a.IsParamObject == b.IsParamObject && a.IsResultObject == b.IsResultObject &&
		a.HasErrorReturn == b.HasErrorReturn &&
		reflect.DeepEqual(a.Parameters, b.Parameters) &&
		reflect.DeepEqual(a.Returns, b.Returns) &&
		reflect.DeepEqual(a.dependencies, b.dependencies)
}

// fuzzResolver resolves every request with a non-nil value of the requested
// type where one can be made up, and reports interfaces as not registered.
type fuzzResolver struct{}

func (fuzzResolver) value(t reflect.Type) (any, error) {
	switch t.Kind() {
	case reflect.Interface:
		return nil, ErrServiceNotFound
	case reflect.Pointer:
		return reflect.New(t.Elem()).Interface(), nil
	default:
		return reflect.New(t).Elem().Interface(), nil
	}
}

func (r fuzzResolver) Get(t reflect.Type) (any, error) { return r.value(t) }

func (r fuzzResolver) GetKeyed(t reflect.Type, _ any) (any, error) { return r.value(t) }

func (r fuzzResolver) GetGroup(t reflect.Type, _ string) ([]any, error) {
	v, err := r.value(t)
	if err != nil {
		return []any{}, nil
	}
	return []any{v}, nil
}

// invokeGenerated runs the analyzed constructor against fuzzResolver. Every
// outcome is an error or a full result list; nothing may panic.
func invokeGenerated(t *testing.T, a *Analyzer, info *ConstructorInfo, fnType reflect.Type) {
	t.Helper()
	results, err := a.GetInvoker().Invoke(info, fuzzResolver{})
	if err != nil {
		var panicErr *PanicError
		if errors.As(err, &panicErr) {
			t.Fatalf("Invoke(%s) panicked: %v", fnType, panicErr.Panic)
		}
		// A struct error can never be nil, so it always fails; otherwise the
		// only failures are interface dependencies the resolver lacks.
		// A group tag on a non-slice field is reported when it is resolved.
		msg := err.Error()
		if !strings.Contains(msg, "constructor error") && !strings.Contains(msg, "group field must be slice") &&
			!errors.Is(err, ErrServiceNotFound) {
			t.Fatalf("Invoke(%s): unexpected error %v", fnType, err)
		}
		return
	}
	if len(results) != fnType.NumOut() {
		t.Fatalf("Invoke(%s) returned %d results", fnType, len(results))
	}
	if info.IsResultObject {
		if _, err := ResultObjectOutputs(results[0], info.Returns); err != nil && !strings.Contains(err.Error(), "result object is nil") {
			t.Fatalf("ResultObjectOutputs(%s): %v", fnType, err)
		}
	}
}
