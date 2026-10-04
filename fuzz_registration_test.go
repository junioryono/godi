package godi

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

// FuzzRegistrationValidation registers generated constructor shapes and
// checks registration against an oracle written from the documented rules,
// then checks that Validate and Build agree on the resulting collection.
// `go test` runs only the seed corpus; run it with
//
//	go test -run '^$' -fuzz '^FuzzRegistrationValidation$' -fuzztime 30s .
//
// The generator mirrors the one in internal/reflection/fuzz_test.go (test
// code cannot be shared across packages).

type (
	fuzzRegA       struct{ ID int }
	fuzzRegB       struct{ ID int }
	fuzzRegLogger  interface{ Log(string) }
	fuzzRegConsole struct{}
	fuzzRegErr     struct{ Msg string } // an error that can never be nil
)

func (fuzzRegConsole) Log(string)  {}
func (e fuzzRegErr) Error() string { return e.Msg }
func (fuzzRegB) String() string    { return "b" }
func (*fuzzRegA) Close() error     { return nil }

var (
	fuzzInType  = reflect.TypeFor[In]()
	fuzzOutType = reflect.TypeFor[Out]()
	fuzzErrType = reflect.TypeFor[error]()

	fuzzSimpleTypes = []reflect.Type{
		reflect.TypeFor[*fuzzRegA](),
		reflect.TypeFor[*fuzzRegB](),
		reflect.TypeFor[fuzzRegLogger](),
		reflect.TypeFor[[]*fuzzRegA](),
		reflect.TypeFor[chan int](),
		reflect.TypeFor[unsafe.Pointer](),
		reflect.TypeFor[error](),
		reflect.TypeFor[context.Context](),
		reflect.TypeFor[Provider](),
		reflect.TypeFor[Scope](),
		reflect.TypeFor[func() int](),
		reflect.TypeFor[fuzzRegErr](),
		reflect.TypeFor[*fuzzRegErr](),
		reflect.TypeFor[int](),
		reflect.TypeFor[map[string]int](),
	}
	// Indexes past fuzzSimpleTypes select a generated In or Out struct, by
	// value or by pointer.
	fuzzNumTypeChoices = len(fuzzSimpleTypes) + 4

	fuzzFieldTypes = []reflect.Type{
		reflect.TypeFor[*fuzzRegA](),
		reflect.TypeFor[*fuzzRegB](),
		reflect.TypeFor[fuzzRegLogger](),
		reflect.TypeFor[[]*fuzzRegA](),
		reflect.TypeFor[[]fuzzRegLogger](),
		reflect.TypeFor[chan int](),
		reflect.TypeFor[unsafe.Pointer](),
		reflect.TypeFor[error](),
		reflect.TypeFor[context.Context](),
		reflect.TypeFor[int](),
	}
	fuzzFieldTags = []reflect.StructTag{
		``,
		`optional:"true"`,
		`name:"k"`,
		`name:"n"`,
		`group:"g"`,
		`name:"k" group:"g"`,
		`inject:"-"`,
		`optional:"true" name:"k"`,
		`optional:"true" group:"g"`,
	}
)

type fuzzBytes struct{ data []byte }

func (r *fuzzBytes) next() int {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[0]
	r.data = r.data[1:]
	return int(b)
}

func (r *fuzzBytes) genStruct(marker reflect.Type) reflect.Type {
	n := r.next() % 4
	embedAt := r.next() % (n + 1)
	fields := make([]reflect.StructField, 0, n+1)
	for i := range n {
		if i == embedAt {
			fields = append(fields, reflect.StructField{Name: marker.Name(), Type: marker, Anonymous: true})
		}
		fields = append(fields, reflect.StructField{
			Name: fmt.Sprintf("F%d", i),
			Type: fuzzFieldTypes[r.next()%len(fuzzFieldTypes)],
			Tag:  fuzzFieldTags[r.next()%len(fuzzFieldTags)],
		})
	}
	if embedAt == n {
		fields = append(fields, reflect.StructField{Name: marker.Name(), Type: marker, Anonymous: true})
	}
	return reflect.StructOf(fields)
}

func (r *fuzzBytes) genType() reflect.Type {
	c := r.next() % fuzzNumTypeChoices
	switch c - len(fuzzSimpleTypes) {
	case 0:
		return r.genStruct(fuzzInType)
	case 1:
		return reflect.PointerTo(r.genStruct(fuzzInType))
	case 2:
		return r.genStruct(fuzzOutType)
	case 3:
		return reflect.PointerTo(r.genStruct(fuzzOutType))
	default:
		return fuzzSimpleTypes[c]
	}
}

// fuzzRegistration is one decoded registration: a constructor signature, a
// lifetime, an option, and whether common dependencies are pre-registered.
type fuzzRegistration struct {
	fnType   reflect.Type
	lifetime Lifetime
	optName  string // godi.Name
	optGroup string // godi.Group
	withDeps bool
}

func decodeRegistration(data []byte) fuzzRegistration {
	r := &fuzzBytes{data: data}
	var reg fuzzRegistration
	reg.lifetime = Lifetime(r.next() % 3)
	switch r.next() % 4 {
	case 1:
		reg.optName = "n"
	case 2:
		reg.optName = "k"
	case 3:
		reg.optGroup = "g"
	}
	reg.withDeps = r.next()%2 == 1

	in := make([]reflect.Type, r.next()%4)
	for i := range in {
		in[i] = r.genType()
	}
	out := make([]reflect.Type, r.next()%4)
	for i := range out {
		out[i] = r.genType()
	}
	variadic := r.next()%2 == 1 && len(in) > 0 && in[len(in)-1].Kind() == reflect.Slice
	reg.fnType = reflect.FuncOf(in, out, variadic)
	return reg
}

// fuzzValue makes a non-nil value of t where possible, so that generated
// constructors succeed and Build failures come only from wiring.
func fuzzValue(t reflect.Type) reflect.Value {
	switch {
	case t == fuzzErrType || t.Kind() == reflect.UnsafePointer:
		return reflect.Zero(t)
	case t == reflect.TypeFor[fuzzRegLogger]():
		return reflect.ValueOf(fuzzRegLogger(fuzzRegConsole{}))
	case t == reflect.TypeFor[context.Context]():
		return reflect.ValueOf(context.Background())
	}
	switch t.Kind() {
	case reflect.Pointer:
		if t.Implements(fuzzErrType) {
			return reflect.Zero(t) // a nil error pointer is success
		}
		return reflect.New(t.Elem())
	case reflect.Slice:
		return reflect.MakeSlice(t, 0, 0)
	case reflect.Chan:
		return reflect.MakeChan(t, 0)
	case reflect.Map:
		return reflect.MakeMap(t)
	case reflect.Func:
		return reflect.MakeFunc(t, func([]reflect.Value) []reflect.Value {
			results := make([]reflect.Value, t.NumOut())
			for i := range results {
				results[i] = reflect.Zero(t.Out(i))
			}
			return results
		})
	case reflect.Struct:
		v := reflect.New(t).Elem()
		if t.Implements(fuzzErrType) {
			return v
		}
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() && !f.Anonymous {
				v.Field(i).Set(fuzzValue(f.Type))
			}
		}
		return v
	default:
		return reflect.Zero(t)
	}
}

func fuzzConstructor(fnType reflect.Type) any {
	return reflect.MakeFunc(fnType, func([]reflect.Value) []reflect.Value {
		results := make([]reflect.Value, fnType.NumOut())
		for i := range results {
			results[i] = fuzzValue(fnType.Out(i))
		}
		return results
	}).Interface()
}

// fuzzFieldTag is the tag rule of In/Out fields.
type fuzzFieldTag struct {
	name, group string
	ignore      bool
}

func fuzzTagOf(tag reflect.StructTag) fuzzFieldTag {
	var t fuzzFieldTag
	t.name, _ = tag.Lookup("name")
	t.group, _ = tag.Lookup("group")
	v, ok := tag.Lookup("inject")
	t.ignore = ok && v == "-"
	return t
}

func fuzzEmbeds(t, marker reflect.Type) bool {
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

// fuzzStructFields returns the injectable fields of an In/Out struct.
func fuzzStructFields(t, marker reflect.Type) []reflect.StructField {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var fields []reflect.StructField
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() || (f.Anonymous && f.Type == marker) || fuzzTagOf(f.Tag).ignore {
			continue
		}
		fields = append(fields, f)
	}
	return fields
}

func fuzzReserved(t reflect.Type) bool {
	return t == reflect.TypeFor[context.Context]() || t == reflect.TypeFor[Provider]() || t == reflect.TypeFor[Scope]()
}

func fuzzCanBeNil(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return true
	default:
		return false
	}
}

// fuzzUnsupportedService reports whether t may not be a service type (the
// rules for constructor return values).
func fuzzUnsupportedService(t reflect.Type) bool {
	return t.Kind() == reflect.Chan || t.Kind() == reflect.UnsafePointer
}

// expectAccepted is the oracle: whether registering reg should succeed, given
// the registry keys already occupied. rule names the rule that rejects it.
// knownBug is set when the oracle (the documented rule) and the
// implementation are known to disagree.
func expectAccepted(reg *fuzzRegistration, occupied map[TypeKey]bool) (ok bool, rule, knownBug string) {
	fn := reg.fnType
	numOut := fn.NumOut()

	if fn.IsVariadic() {
		return false, "variadic", ""
	}

	void := true
	for i := range numOut {
		if !fn.Out(i).Implements(fuzzErrType) {
			void = false
		}
	}
	// Void constructors get a synthetic key unless named, so they can't join
	// a group.
	if (void || reg.optName != "") && reg.optGroup != "" {
		return false, "key and group", ""
	}
	if void && reg.lifetime == Transient {
		return false, "void transient", ""
	}

	resultObject := numOut > 0 && fuzzEmbeds(fn.Out(0), fuzzOutType)
	if resultObject {
		if numOut > 2 {
			return false, "Out with more than (Out, error)", ""
		}
		if numOut == 2 && !fn.Out(1).Implements(fuzzErrType) {
			return false, "Out with a non-error second result", ""
		}
	}
	for i := range numOut {
		t := fn.Out(i)
		if t.Implements(fuzzErrType) {
			if i != numOut-1 {
				return false, "error not last", ""
			}
			if !fuzzCanBeNil(t) {
				return false, "error type that cannot be nil", ""
			}
			continue
		}
		if fuzzUnsupportedService(t) {
			return false, "chan or unsafe.Pointer result", ""
		}
	}

	if fn.NumIn() == 1 && fuzzEmbeds(fn.In(0), fuzzInType) {
		for _, f := range fuzzStructFields(fn.In(0), fuzzInType) {
			tag := fuzzTagOf(f.Tag)
			if tag.name != "" && tag.group != "" {
				return false, "In field with name and group", ""
			}
			if tag.group != "" && f.Type.Kind() != reflect.Slice {
				return false, "In group field not a slice", ""
			}
			if tag.group == "" && fuzzUnsupportedService(f.Type) {
				return false, "chan or unsafe.Pointer dependency", ""
			}
		}
	} else {
		for in := range fn.Ins() {
			if fuzzEmbeds(in, fuzzInType) {
				return false, "In mixed with other parameters", ""
			}
			if fuzzUnsupportedService(in) {
				return false, "chan or unsafe.Pointer dependency", ""
			}
		}
	}

	primary := reflect.TypeFor[struct{}]()
	if !void {
		primary = fn.Out(0)
	}
	if fuzzReserved(primary) {
		return false, "reserved type", ""
	}

	// The registry keys the registration occupies.
	type output struct {
		typ   reflect.Type
		key   any
		group string
	}
	var outputs []output
	switch {
	case resultObject:
		if reg.optName != "" || reg.optGroup != "" {
			return false, "Name or Group on an Out constructor", ""
		}
		for _, f := range fuzzStructFields(fn.Out(0), fuzzOutType) {
			tag := fuzzTagOf(f.Tag)
			if tag.name != "" && tag.group != "" {
				return false, "Out field with name and group", ""
			}
			o := output{typ: f.Type, group: tag.group}
			if tag.name != "" {
				o.key = tag.name
			}
			outputs = append(outputs, o)
		}
	case void:
		// A void constructor occupies a fresh synthetic key, or its name.
		if reg.optName != "" {
			outputs = append(outputs, output{typ: primary, key: reg.optName})
		}
	default:
		for i := range numOut {
			t := fn.Out(i)
			if i == numOut-1 && t.Implements(fuzzErrType) {
				continue
			}
			o := output{typ: t, group: reg.optGroup}
			if i == 0 && reg.optName != "" {
				o.key = reg.optName
			}
			outputs = append(outputs, o)
		}
	}

	seen := make(map[TypeKey]bool)
	for key := range occupied {
		seen[key] = true
	}
	for _, o := range outputs {
		if o.key == nil && o.group != "" {
			continue // group members never collide
		}
		key := TypeKey{Type: o.typ, Key: o.key}
		if seen[key] {
			return false, "already registered", ""
		}
		seen[key] = true
	}

	// Known gaps: outputs other than the primary one skip some of the
	// service-type checks. The rules above say they should be rejected; the
	// skipped tests below reproduce each gap.
	for i, o := range outputs {
		if resultObject || i > 0 {
			if fuzzReserved(o.typ) {
				knownBug = "reserved type accepted as a later output (TestRegistrationRejectsReservedSecondaryOutputs)"
			}
			if resultObject && (fuzzUnsupportedService(o.typ) || o.typ == fuzzErrType) {
				knownBug = "Out field type not validated (TestRegistrationValidatesOutFieldTypes)"
			}
		}
	}
	return true, "", knownBug
}

var registrationSeeds = [][]byte{
	// lifetime, option, withDeps, nIn, in..., nOut, out..., variadic
	{0, 0, 0, 0, 1, 0},                    // func() *A
	{0, 0, 1, 1, 0, 1, 1},                 // func(*A) *B, deps registered
	{1, 0, 1, 1, 0, 1, 0},                 // scoped func(*A) *A: duplicate of a dep
	{0, 0, 0, 1, 1, 1, 0},                 // func(*B) *A: self-contained missing dep
	{0, 0, 0, 1, 1, 1, 1},                 // func(*B) *B: cycle
	{2, 0, 0, 0, 0},                       // transient func()
	{1, 0, 0, 0, 1, 6},                    // scoped func() error
	{0, 3, 0, 0, 0},                       // void with group
	{0, 1, 0, 0, 2, 0, 1},                 // multi-return with name
	{0, 0, 0, 0, 2, 0, 0},                 // (*A, *A): duplicate output
	{0, 3, 0, 0, 2, 0, 0},                 // (*A, *A) in a group
	{0, 0, 0, 0, 2, 0, 7},                 // (*A, context.Context)
	{0, 0, 0, 0, 1, 7},                    // func() context.Context
	{0, 0, 0, 0, 1, 4},                    // func() chan int
	{0, 0, 0, 0, 2, 11, 6},                // (fuzzRegErr, error)
	{0, 0, 0, 0, 1, 11},                   // func() fuzzRegErr
	{0, 0, 0, 0, 2, 0, 12},                // (*A, *fuzzRegErr)
	{0, 0, 1, 1, 15, 2, 0, 1, 1, 7, 1, 0}, // void func(In{*B optional, error optional})
	{0, 0, 1, 1, 15, 1, 0, 3, 4, 1, 1},    // In with group slice
	{0, 0, 0, 1, 15, 1, 0, 0, 4, 1, 1},    // In with group on a non-slice
	{0, 0, 0, 2, 15, 0, 0, 0, 1, 1},       // In mixed with another parameter
	{0, 0, 0, 0, 1, 17, 2, 0, 0, 0, 1, 2}, // Out{*A, *B `name:"k"`}
	{0, 0, 1, 0, 1, 17, 1, 0, 0, 2},       // Out{*A `name:"k"`} colliding with a dep
	{0, 0, 0, 0, 2, 17, 1, 0, 5, 0, 6},    // (Out{chan int}, error)
	{0, 0, 0, 0, 1, 17, 1, 0, 8, 0},       // Out{context.Context}
	{0, 1, 0, 0, 1, 17, 1, 0, 0, 0},       // Out with godi.Name
	{1, 0, 0, 1, 3, 1, 0, 1},              // variadic func(...*A) *A
}

func FuzzRegistrationValidation(f *testing.F) {
	for _, seed := range registrationSeeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		reg := decodeRegistration(data)

		c := NewCollection()
		occupied := map[TypeKey]bool{}
		if reg.withDeps {
			c.AddSingleton(&fuzzRegA{})
			c.AddSingleton(func() fuzzRegLogger { return fuzzRegConsole{} })
			c.AddSingleton(&fuzzRegA{}, Name("k"))
			c.AddSingleton(func() *fuzzRegA { return &fuzzRegA{} }, Group("g"))
			if err := c.Err(); err != nil {
				t.Fatalf("registering dependencies: %v", err)
			}
			occupied[TypeKey{Type: reflect.TypeFor[*fuzzRegA]()}] = true
			occupied[TypeKey{Type: reflect.TypeFor[fuzzRegLogger]()}] = true
			occupied[TypeKey{Type: reflect.TypeFor[*fuzzRegA](), Key: "k"}] = true
		}

		var opts []AddOption
		if reg.optName != "" {
			opts = append(opts, Name(reg.optName))
		}
		if reg.optGroup != "" {
			opts = append(opts, Group(reg.optGroup))
		}

		before := c.Count()
		switch reg.lifetime {
		case Singleton:
			c.AddSingleton(fuzzConstructor(reg.fnType), opts...)
		case Scoped:
			c.AddScoped(fuzzConstructor(reg.fnType), opts...)
		case Transient:
			c.AddTransient(fuzzConstructor(reg.fnType), opts...)
		}
		regErr := c.Err()

		wantOK, rule, knownBug := expectAccepted(&reg, occupied)
		if (regErr == nil) != wantOK {
			t.Fatalf("Add%s(%s, %v): err = %v; oracle accepts = %v (%s)",
				reg.lifetime, reg.fnType, opts, regErr, wantOK, rule)
		}
		if regErr != nil && c.Count() != before {
			t.Fatalf("Add%s(%s): rejected registration left %d services behind",
				reg.lifetime, reg.fnType, c.Count()-before)
		}
		if knownBug != "" {
			t.Skip("bug: " + knownBug)
		}

		// Build and Validate run the same checks: Build fails exactly when
		// Validate does (the generated constructors themselves succeed).
		validateErr := Validate(c)
		p, buildErr := c.Build()
		if p != nil {
			if err := p.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}
		if (validateErr == nil) != (buildErr == nil) {
			t.Fatalf("Add%s(%s, %v): Validate = %v, Build = %v",
				reg.lifetime, reg.fnType, opts, validateErr, buildErr)
		}
	})
}

// Found by FuzzRegistrationValidation: a constructor's single result may not
// be a reserved type (context.Context, Provider, Scope), but a later
// multi-return value or an Out field of that type is registered, and then
// silently shadowed by the container's own value at resolution.
func TestRegistrationRejectsReservedSecondaryOutputs(t *testing.T) {
	t.Skip("bug: reserved types are accepted as later multi-return values and as Out fields")

	type Results struct {
		Out
		Ctx context.Context
	}
	for name, ctor := range map[string]any{
		"multi-return": func() (*fuzzRegA, context.Context) { return &fuzzRegA{}, context.Background() },
		"Out field":    func() Results { return Results{Ctx: context.Background()} },
	} {
		t.Run(name, func(t *testing.T) {
			c := NewCollection()
			c.AddSingleton(ctor)
			if c.Err() == nil {
				t.Fatal("registering a context.Context output succeeded; want the reserved-type error")
			}
		})
	}
}

// Found by FuzzRegistrationValidation: constructor results of type chan,
// unsafe.Pointer, or error (other than a last error) are rejected, but Out
// fields of those types are registered as services.
func TestRegistrationValidatesOutFieldTypes(t *testing.T) {
	t.Skip("bug: Out struct fields skip the service-type checks applied to constructor results")

	type ChanOut struct {
		Out
		C chan int
	}
	type ErrorOut struct {
		Out
		Err error
	}
	for name, ctor := range map[string]any{
		"chan":  func() ChanOut { return ChanOut{C: make(chan int)} },
		"error": func() ErrorOut { return ErrorOut{Err: fmt.Errorf("x")} },
	} {
		t.Run(name, func(t *testing.T) {
			c := NewCollection()
			c.AddSingleton(ctor)
			if c.Err() == nil {
				t.Fatalf("registering an Out field of type %s succeeded; want it rejected like a constructor result", name)
			}
		})
	}
}
