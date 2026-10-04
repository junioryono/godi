package godi_test

// Model-based property test: random programs of registrations, Build, scopes,
// resolutions and closes are run against godi and checked against a small
// reference model of the registry and against lifecycle invariants:
//
//   - every disposable produced is closed exactly once, by the time the
//     provider is closed (never, for NoDispose registrations);
//   - consumers are closed before their dependencies;
//   - a scoped service is unique within its scope, a singleton is shared;
//   - nothing is closed while its scope (or the provider) is open;
//   - Build and Validate agree, and both agree with the model;
//   - resolutions return what the model predicts (registration, output,
//     decorators applied) or fail when the model says they must.
//
// Constructors and decorators are generated with reflect.FuncOf and
// reflect.MakeFunc over a fixed set of service types, so dependency graphs are
// arbitrary. Each registered type has a level; a constructor depends only on
// types of a lower level than everything it registers, which keeps the graph
// acyclic (cycles are covered by dedicated tests).
//
// The run is seeded and logged. Environment variables:
//
//	GODI_MODEL_ITERATIONS  number of random programs (default modelDefaultIterations)
//	GODI_MODEL_SEED        master seed, to replay a whole test run
//	GODI_MODEL_RUN_SEED    seed of a single program, to replay one failure verbosely

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junioryono/godi/v5"
)

// modelDefaultIterations keeps the default run around a second under -race.
const modelDefaultIterations = 300

// ---------------------------------------------------------------------------
// Service types
// ---------------------------------------------------------------------------

// modelNode is one value produced by a generated constructor or decorator (or
// registered as an instance). Disposable service types embed it.
type modelNode struct {
	id        int
	tr        *modelTracker
	typ       int          // index in modelTypes of the value's registered type
	reg       *modelReg    // registration that produced it (for decorators: of the wrapped value)
	slot      int          // output slot of reg
	decs      []int        // decorator IDs applied, innermost first
	scopeID   string       // scope whose context the constructor received; "" for instances
	deps      []*modelNode // values the constructor or decorator received
	closable  bool
	noDispose bool

	// guarded by tr.mu
	closes      int
	closeSeq    int
	closedEarly bool // closed while its owner was still open
	inBuild     bool // closed while Build was running
}

func (n *modelNode) Close() error {
	n.tr.onClose(n)
	return nil
}

func (n *modelNode) modelNodeOf() *modelNode { return n }
func (n *modelNode) modelAsA()               {}
func (n *modelNode) modelAsB()               {}

type modelNoder interface{ modelNodeOf() *modelNode }

type (
	modelS0 struct{ *modelNode }
	modelS1 struct{ *modelNode }
	modelS2 struct{ *modelNode }
	modelS3 struct{ *modelNode }
	modelS4 struct{ *modelNode }
	modelS5 struct{ *modelNode }
	modelS6 struct{ *modelNode }
	modelS7 struct{ *modelNode }
	modelS8 struct{ *modelNode }
	modelS9 struct{ *modelNode }

	// Plain (non-disposable) services.
	modelP0 struct{ n *modelNode }
	modelP1 struct{ n *modelNode }

	modelAsA interface{ modelAsA() }
	modelAsB interface{ modelAsB() }
)

func (p *modelP0) modelNodeOf() *modelNode { return p.n }
func (p *modelP1) modelNodeOf() *modelNode { return p.n }

// modelKey is a non-string key registered with godi.Key.
type modelKey struct{ N int }

type modelType struct {
	rt       reflect.Type
	iface    bool
	closable bool
	wrap     func(*modelNode) any
}

// modelTypes is ordered by level: a constructor depends only on types earlier
// in the list than every type it registers.
var modelTypes = []modelType{
	{rt: reflect.TypeFor[*modelS0](), closable: true, wrap: func(n *modelNode) any { return &modelS0{n} }},
	{rt: reflect.TypeFor[*modelS1](), closable: true, wrap: func(n *modelNode) any { return &modelS1{n} }},
	{rt: reflect.TypeFor[*modelP0](), wrap: func(n *modelNode) any { return &modelP0{n} }},
	{rt: reflect.TypeFor[*modelS2](), closable: true, wrap: func(n *modelNode) any { return &modelS2{n} }},
	{rt: reflect.TypeFor[*modelS3](), closable: true, wrap: func(n *modelNode) any { return &modelS3{n} }},
	{rt: reflect.TypeFor[modelAsA](), iface: true, closable: true, wrap: func(n *modelNode) any { return &modelS0{n} }},
	{rt: reflect.TypeFor[*modelS4](), closable: true, wrap: func(n *modelNode) any { return &modelS4{n} }},
	{rt: reflect.TypeFor[*modelS5](), closable: true, wrap: func(n *modelNode) any { return &modelS5{n} }},
	{rt: reflect.TypeFor[*modelP1](), wrap: func(n *modelNode) any { return &modelP1{n} }},
	{rt: reflect.TypeFor[*modelS6](), closable: true, wrap: func(n *modelNode) any { return &modelS6{n} }},
	{rt: reflect.TypeFor[*modelS7](), closable: true, wrap: func(n *modelNode) any { return &modelS7{n} }},
	{rt: reflect.TypeFor[modelAsB](), iface: true, closable: true, wrap: func(n *modelNode) any { return &modelS0{n} }},
	{rt: reflect.TypeFor[*modelS8](), closable: true, wrap: func(n *modelNode) any { return &modelS8{n} }},
	{rt: reflect.TypeFor[*modelS9](), closable: true, wrap: func(n *modelNode) any { return &modelS9{n} }},
}

const (
	modelTypeAsA = 5
	modelTypeAsB = 11
)

var (
	modelCtxType   = reflect.TypeFor[context.Context]()
	modelErrorType = reflect.TypeFor[error]()
	modelInType    = reflect.TypeFor[godi.In]()
	modelOutType   = reflect.TypeFor[godi.Out]()
)

func modelAsOption(typ int) godi.AddOption {
	if typ == modelTypeAsA {
		return godi.As[modelAsA]()
	}
	return godi.As[modelAsB]()
}

// ---------------------------------------------------------------------------
// Tracker: records every node and close event
// ---------------------------------------------------------------------------

type modelTracker struct {
	mu       sync.Mutex
	nodes    []*modelNode
	seq      int
	closing  map[string]bool // scope IDs whose Close has begun
	provider bool            // provider Close has begun
	building bool
}

func (tr *modelTracker) newNode(typ int, reg *modelReg, slot int, scopeID string, deps []*modelNode) *modelNode {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	n := &modelNode{
		id:        len(tr.nodes) + 1,
		tr:        tr,
		typ:       typ,
		reg:       reg,
		slot:      slot,
		scopeID:   scopeID,
		deps:      deps,
		closable:  modelTypes[typ].closable,
		noDispose: reg.noDispose,
	}
	tr.nodes = append(tr.nodes, n)
	return n
}

func (tr *modelTracker) onClose(n *modelNode) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.seq++
	n.closes++
	if n.closes == 1 {
		n.closeSeq = tr.seq
	}
	if tr.building {
		n.inBuild = true
		return
	}
	if !tr.provider && !tr.closing[n.scopeID] {
		n.closedEarly = true
	}
}

func (tr *modelTracker) markClosing(ids ...string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, id := range ids {
		tr.closing[id] = true
	}
}

func (tr *modelTracker) setProviderClosing() {
	tr.mu.Lock()
	tr.provider = true
	tr.mu.Unlock()
}

func (tr *modelTracker) setBuilding(b bool) {
	tr.mu.Lock()
	tr.building = b
	tr.mu.Unlock()
}

func modelNodeOfValue(v reflect.Value) *modelNode {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
	}
	return v.Interface().(modelNoder).modelNodeOf()
}

func modelScopeID(ctx context.Context) string {
	s, err := godi.FromContext(ctx)
	if err != nil {
		panic(fmt.Sprintf("constructor context carries no scope: %v", err))
	}
	return s.ID()
}

// ---------------------------------------------------------------------------
// Reference model
// ---------------------------------------------------------------------------

type modelShape int

const (
	shapePlain modelShape = iota
	shapeMulti
	shapeOut
	shapeAs
	shapeInstance
)

func (s modelShape) String() string {
	return [...]string{"plain", "multi", "out", "as", "instance"}[s]
}

type modelDep struct {
	typ      int
	key      string // name tag
	group    string // group tag (the field is a slice)
	optional bool
}

type modelOutput struct {
	typ    int
	key    any    // Out field name tag
	group  string // Out field group tag
	absent bool   // Out field left nil
}

// modelReg is one Add/TryAdd/Replace call.
type modelReg struct {
	id        int
	lifetime  godi.Lifetime
	shape     modelShape
	outputs   []modelOutput
	as        []int // interface types (shapeAs)
	key       any   // godi.Name / godi.Key
	group     string
	deps      []modelDep
	inStruct  bool
	errReturn bool
	noDispose bool
	lazy      bool
	instance  *modelNode

	mu          sync.Mutex
	invocations map[string]int // scope ID -> constructor calls
}

// modelEntry is one registry entry (one registered descriptor).
type modelEntry struct {
	reg   *modelReg
	slot  int
	typ   int
	key   any
	group string
	decs  []*modelDec // attached at Build
}

type modelDec struct {
	id        int
	target    int
	key       any
	group     string
	deps      []int
	errReturn bool
}

type modelSKey struct {
	typ int
	key any
}

type modelGKey struct {
	typ   int
	group string
}

type modelRegistry struct {
	services   map[modelSKey]*modelEntry
	groups     map[modelGKey][]*modelEntry
	order      []*modelEntry
	decorators []*modelDec
	err        bool // a registration error was recorded
}

func (m *modelRegistry) count() int { return len(m.order) }

// entries returns the entries reg would register, without registering them.
func (m *modelRegistry) entriesFor(reg *modelReg) []*modelEntry {
	switch reg.shape {
	case shapeOut:
		var out []*modelEntry
		for slot, o := range reg.outputs {
			out = append(out, &modelEntry{reg: reg, slot: slot, typ: o.typ, key: o.key, group: o.group})
		}
		return out
	case shapeAs:
		var out []*modelEntry
		for _, iface := range reg.as {
			out = append(out, &modelEntry{reg: reg, slot: 0, typ: iface, key: reg.key, group: reg.group})
		}
		return out
	default:
		var out []*modelEntry
		for slot, o := range reg.outputs {
			e := &modelEntry{reg: reg, slot: slot, typ: o.typ, group: reg.group}
			if slot == 0 {
				e.key = reg.key
			}
			out = append(out, e)
		}
		return out
	}
}

// add registers reg like collection.addService: transactional, failing on a
// duplicate type/key.
func (m *modelRegistry) add(reg *modelReg) bool {
	entries := m.entriesFor(reg)
	seen := map[modelSKey]bool{}
	for _, e := range entries {
		if e.key == nil && e.group != "" {
			continue
		}
		k := modelSKey{e.typ, e.key}
		if m.services[k] != nil || seen[k] {
			m.err = true
			return false
		}
		seen[k] = true
	}
	for _, e := range entries {
		if e.key == nil && e.group != "" {
			gk := modelGKey{e.typ, e.group}
			m.groups[gk] = append(m.groups[gk], e)
		} else {
			m.services[modelSKey{e.typ, e.key}] = e
		}
		m.order = append(m.order, e)
	}
	return true
}

// targets returns the registry keys Replace/TryAdd consider for reg.
func (m *modelRegistry) targets(reg *modelReg) []modelSKey {
	var out []modelSKey
	for _, e := range m.entriesFor(reg) {
		out = append(out, modelSKey{e.typ, e.key})
	}
	return out
}

func (m *modelRegistry) prune(removed map[*modelEntry]bool) {
	m.order = slices.DeleteFunc(m.order, func(e *modelEntry) bool { return removed[e] })
}

func (m *modelRegistry) replace(reg *modelReg) {
	removed := map[*modelEntry]bool{}
	for _, k := range m.targets(reg) {
		if e := m.services[k]; e != nil {
			delete(m.services, k)
			removed[e] = true
		}
	}
	if len(removed) == 0 {
		m.err = true
		return
	}
	m.prune(removed)
	m.add(reg)
}

func (m *modelRegistry) tryAdd(reg *modelReg) {
	for _, k := range m.targets(reg) {
		if m.services[k] != nil {
			return
		}
	}
	m.add(reg)
}

func (m *modelRegistry) remove(typ int) {
	removed := map[*modelEntry]bool{}
	for k, e := range m.services {
		if k.typ == typ {
			removed[e] = true
			delete(m.services, k)
		}
	}
	for k, es := range m.groups {
		if k.typ == typ {
			for _, e := range es {
				removed[e] = true
			}
			delete(m.groups, k)
		}
	}
	m.prune(removed)
}

func (m *modelRegistry) removeKeyed(typ int, key any) {
	k := modelSKey{typ, key}
	if e := m.services[k]; e != nil {
		delete(m.services, k)
		m.prune(map[*modelEntry]bool{e: true})
	}
}

// attach attaches decorators to entries as Build does, reporting whether
// every decorator matched a registration.
func (m *modelRegistry) attach() bool {
	ok := true
	for _, e := range m.order {
		e.decs = nil
	}
	for _, d := range m.decorators {
		var targets []*modelEntry
		if d.group != "" {
			targets = m.groups[modelGKey{d.target, d.group}]
		} else if e := m.services[modelSKey{d.target, d.key}]; e != nil {
			targets = []*modelEntry{e}
		}
		if len(targets) == 0 {
			ok = false
		}
		for _, e := range targets {
			e.decs = append(e.decs, d)
		}
	}
	return ok
}

// entryDeps returns everything e's descriptor depends on: its constructor's
// dependencies and its decorators'.
func entryDeps(e *modelEntry) []modelDep {
	deps := slices.Clone(e.reg.deps)
	for _, d := range e.decs {
		for _, t := range d.deps {
			deps = append(deps, modelDep{typ: t})
		}
	}
	return deps
}

func (m *modelRegistry) depEntries(dep modelDep) []*modelEntry {
	if dep.group != "" {
		return m.groups[modelGKey{dep.typ, dep.group}]
	}
	var key any
	if dep.key != "" {
		key = dep.key
	}
	if e := m.services[modelSKey{dep.typ, key}]; e != nil {
		return []*modelEntry{e}
	}
	return nil
}

func (m *modelRegistry) missingDependency() bool {
	for _, e := range m.order {
		for _, dep := range entryDeps(e) {
			if dep.optional || dep.group != "" {
				continue
			}
			if len(m.depEntries(dep)) == 0 {
				return true
			}
		}
	}
	return false
}

// lifetimeConflict reports a singleton that reaches a scoped service directly
// or through transients.
func (m *modelRegistry) lifetimeConflict() bool {
	return m.lifetimeConflictVia(entryDeps)
}

// constructionDeps returns everything one construction for e resolves: its
// constructor's dependencies and the decorators of every output it publishes.
func (m *modelRegistry) constructionDeps(e *modelEntry) []modelDep {
	reg := e.reg
	if reg.shape == shapePlain || reg.shape == shapeInstance ||
		(reg.shape == shapeAs && reg.lifetime == godi.Transient) {
		return entryDeps(e)
	}
	deps := slices.Clone(reg.deps)
	for _, sib := range m.order {
		if sib.reg != reg || reg.outputs[sib.slot].absent {
			continue
		}
		for _, d := range sib.decs {
			for _, t := range d.deps {
				deps = append(deps, modelDep{typ: t})
			}
		}
	}
	return deps
}

// lifetimeGap reports a singleton that captures a scoped service only through
// a decorator of another output of a constructor it depends on, which godi's
// lifetime validation does not follow.
func (m *modelRegistry) lifetimeGap() bool {
	return !m.lifetimeConflict() && m.lifetimeConflictVia(m.constructionDeps)
}

func (m *modelRegistry) lifetimeConflictVia(depsOf func(*modelEntry) []modelDep) bool {
	memo := map[*modelEntry]bool{}
	var reachesScoped func(e *modelEntry) bool
	reachesScoped = func(e *modelEntry) bool {
		switch e.reg.lifetime {
		case godi.Scoped:
			return true
		case godi.Transient:
			if r, ok := memo[e]; ok {
				return r
			}
			memo[e] = false
			for _, dep := range depsOf(e) {
				for _, de := range m.depEntries(dep) {
					if reachesScoped(de) {
						memo[e] = true
						return true
					}
				}
			}
			return false
		default:
			return false
		}
	}
	for _, e := range m.order {
		if e.reg.lifetime != godi.Singleton {
			continue
		}
		for _, dep := range depsOf(e) {
			for _, de := range m.depEntries(dep) {
				if reachesScoped(de) {
					return true
				}
			}
		}
	}
	return false
}

// decoratorReachesOwnConstructor reports whether a decorator depends, directly
// or transitively, on its service's own registration (e.g. on another output
// of the same multi-return constructor).
func (m *modelRegistry) decoratorReachesOwnConstructor() bool {
	for _, e := range m.order {
		for _, d := range e.decs {
			for _, t := range d.deps {
				if m.reaches(modelDep{typ: t}, e.reg, map[*modelEntry]bool{}) {
					return true
				}
			}
		}
	}
	return false
}

func (m *modelRegistry) reaches(dep modelDep, target *modelReg, seen map[*modelEntry]bool) bool {
	for _, de := range m.depEntries(dep) {
		if de.reg == target {
			return true
		}
		if seen[de] {
			continue
		}
		seen[de] = true
		for _, dd := range entryDeps(de) {
			if m.reaches(dd, target, seen) {
				return true
			}
		}
	}
	return false
}

// instanceDecoratorDeps reports whether a decorator of an instance (value)
// registration has dependencies.
func (m *modelRegistry) instanceDecoratorDeps() bool {
	for _, e := range m.order {
		if e.reg.shape != shapeInstance {
			continue
		}
		for _, d := range e.decs {
			if len(d.deps) > 0 {
				return true
			}
		}
	}
	return false
}

type modelStatus int

const (
	statusOK modelStatus = iota
	statusAbsent
	statusFail
)

// resolution predicts, for a built registry, whether each entry resolves.
type resolution struct {
	m    *modelRegistry
	memo map[*modelEntry]modelStatus
}

func (r *resolution) status(e *modelEntry) modelStatus {
	if s, ok := r.memo[e]; ok {
		return s
	}
	r.memo[e] = statusFail // the graph is acyclic; this only guards re-entry
	s := statusOK
	switch {
	case !r.regDepsOK(e.reg) || !r.decoratorsOK(e):
		s = statusFail
	case e.reg.outputs[e.slot].absent:
		s = statusAbsent
	}
	r.memo[e] = s
	return s
}

func (r *resolution) depOK(dep modelDep) bool {
	if dep.group != "" {
		for _, me := range r.m.groups[modelGKey{dep.typ, dep.group}] {
			if r.status(me) == statusFail {
				return false
			}
		}
		return true
	}
	es := r.m.depEntries(dep)
	if len(es) == 0 {
		return dep.optional
	}
	switch r.status(es[0]) {
	case statusOK:
		return true
	case statusAbsent:
		return dep.optional
	default:
		return false
	}
}

func (r *resolution) regDepsOK(reg *modelReg) bool {
	for _, dep := range reg.deps {
		if !r.depOK(dep) {
			return false
		}
	}
	return true
}

func (r *resolution) chainOK(e *modelEntry) bool {
	for _, d := range e.decs {
		for _, t := range d.deps {
			if !r.depOK(modelDep{typ: t}) {
				return false
			}
		}
	}
	return true
}

// liveEntries returns the registry entries of reg.
func (r *resolution) liveEntries(reg *modelReg) []*modelEntry {
	var out []*modelEntry
	for _, e := range r.m.order {
		if e.reg == reg {
			out = append(out, e)
		}
	}
	return out
}

// decoratorsOK reports whether every decorator chain one construction for e
// runs succeeds: the chains of every output the constructor publishes.
func (r *resolution) decoratorsOK(e *modelEntry) bool {
	reg := e.reg
	if reg.shape == shapePlain || reg.shape == shapeInstance ||
		(reg.shape == shapeAs && reg.lifetime == godi.Transient) {
		return r.chainOK(e)
	}
	for _, sib := range r.liveEntries(reg) {
		if reg.outputs[sib.slot].absent {
			continue
		}
		if !r.chainOK(sib) {
			return false
		}
	}
	return true
}

// regDiscards reports whether a construction of reg can run and then have its
// outputs discarded because a decorator chain fails.
func (r *resolution) regDiscards(reg *modelReg) bool {
	for _, e := range r.liveEntries(reg) {
		if !r.chainOK(e) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// A run: one random program
// ---------------------------------------------------------------------------

type modelScope struct {
	s        godi.Scope
	id       string
	parent   *modelScope
	children []*modelScope
	closed   bool
}

type modelRun struct {
	rng      *rand.Rand
	seed     uint64
	tr       *modelTracker
	c        godi.Collection
	m        *modelRegistry
	log      []string
	failures []string
	nextID   int

	allowErrors bool

	// runtime
	p              godi.Provider
	validateScopes bool
	res            *resolution
	scopes         []*modelScope
	rootID         string
	singletonNode  map[*modelEntry]*modelNode
	scopedNode     map[*modelEntry]map[string]*modelNode
	transientSeen  map[*modelNode]bool
	regs           []*modelReg

	stats *modelStats
}

type modelStats struct {
	runs, built, skipped    int
	registrationErrs        int
	unmatched, missing      int
	conflicts, eagerFails   int
	resolves, nodes, closes int
}

func (r *modelRun) logf(format string, args ...any) {
	r.log = append(r.log, fmt.Sprintf(format, args...))
}

func (r *modelRun) failf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

func (r *modelRun) id() int {
	r.nextID++
	return r.nextID
}

func newModelRun(seed uint64, stats *modelStats) *modelRun {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	return &modelRun{
		rng:  rng,
		seed: seed,
		tr:   &modelTracker{closing: map[string]bool{}},
		c:    godi.NewCollection(),
		m: &modelRegistry{
			services: map[modelSKey]*modelEntry{},
			groups:   map[modelGKey][]*modelEntry{},
		},
		allowErrors:   rng.IntN(10) == 0,
		singletonNode: map[*modelEntry]*modelNode{},
		scopedNode:    map[*modelEntry]map[string]*modelNode{},
		transientSeen: map[*modelNode]bool{},
		stats:         stats,
	}
}

func (r *modelRun) chance(percent int) bool { return r.rng.IntN(100) < percent }

func pick[T any](r *modelRun, xs []T) T { return xs[r.rng.IntN(len(xs))] }

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

func (r *modelRun) randomLifetime() godi.Lifetime {
	return pick(r, []godi.Lifetime{godi.Singleton, godi.Singleton, godi.Scoped, godi.Scoped, godi.Transient})
}

func (r *modelRun) concreteTypes() []int {
	var out []int
	for i, t := range modelTypes {
		if !t.iface {
			out = append(out, i)
		}
	}
	return out
}

func (r *modelRun) disposableConcreteTypes() []int {
	var out []int
	for i, t := range modelTypes {
		if !t.iface && t.closable {
			out = append(out, i)
		}
	}
	return out
}

// acceptableFor reports whether e is a sensible dependency of a registration
// with lifetime l: singletons avoid non-singletons so most programs build.
func acceptableFor(l godi.Lifetime, es []*modelEntry) bool {
	if l != godi.Singleton {
		return true
	}
	for _, e := range es {
		if e.reg.lifetime != godi.Singleton {
			return false
		}
	}
	return true
}

// genDeps chooses dependencies of a level below maxLevel.
func (r *modelRun) genDeps(lifetime godi.Lifetime, maxLevel int) (deps []modelDep, inStruct bool) {
	type cand struct {
		dep modelDep
		es  []*modelEntry
	}
	var cands []cand
	for k, e := range r.m.services {
		if k.typ >= maxLevel {
			continue
		}
		switch key := k.key.(type) {
		case nil:
			cands = append(cands, cand{modelDep{typ: k.typ}, []*modelEntry{e}})
		case string:
			cands = append(cands, cand{modelDep{typ: k.typ, key: key}, []*modelEntry{e}})
		}
	}
	for k, es := range r.m.groups {
		if k.typ < maxLevel {
			cands = append(cands, cand{modelDep{typ: k.typ, group: k.group}, es})
		}
	}
	// Map iteration order is random; sort for reproducibility.
	slices.SortFunc(cands, func(a, b cand) int {
		return strings.Compare(fmt.Sprint(a.dep), fmt.Sprint(b.dep))
	})

	n := r.rng.IntN(4)
	for range n {
		var dep modelDep
		switch {
		case len(cands) > 0 && !r.chance(8):
			c := pick(r, cands)
			if !acceptableFor(lifetime, c.es) && !r.chance(10) {
				continue
			}
			dep = c.dep
		case maxLevel > 0:
			// Possibly unregistered: optional, or (rarely) missing.
			dep = modelDep{typ: r.rng.IntN(maxLevel), optional: !r.chance(10)}
			if r.chance(30) {
				dep.key = pick(r, []string{"a", "b"})
			}
		default:
			continue
		}
		if dep.group == "" && r.chance(20) {
			dep.optional = true
		}
		deps = append(deps, dep)
	}
	inStruct = r.chance(30)
	for _, d := range deps {
		if d.key != "" || d.group != "" || d.optional {
			inStruct = true
		}
	}
	return deps, inStruct
}

func (r *modelRun) genKeyOption(reg *modelReg) {
	switch v := r.rng.IntN(100); {
	case v < 20:
		reg.key = pick(r, []string{"a", "b"})
	case v < 26:
		reg.key = modelKey{N: 1 + r.rng.IntN(2)}
	case v < 38 && reg.shape != shapeMulti:
		reg.group = pick(r, []string{"g", "h"})
	}
}

// genReg generates a registration. shapes limits the shapes chosen.
func (r *modelRun) genReg(lifetime godi.Lifetime, shapes []modelShape) *modelReg {
	reg := &modelReg{id: r.id(), lifetime: lifetime, invocations: map[string]int{}}
	shape := pick(r, shapes)
	if shape == shapeInstance && lifetime != godi.Singleton {
		shape = shapePlain
	}
	reg.shape = shape

	concrete := r.concreteTypes()
	switch shape {
	case shapePlain, shapeInstance:
		reg.outputs = []modelOutput{{typ: pick(r, concrete)}}
	case shapeMulti:
		perm := r.rng.Perm(len(concrete))
		for _, i := range perm[:2+r.rng.IntN(2)] {
			reg.outputs = append(reg.outputs, modelOutput{typ: concrete[i]})
		}
	case shapeOut:
		perm := r.rng.Perm(len(concrete))
		for _, i := range perm[:1+r.rng.IntN(3)] {
			o := modelOutput{typ: concrete[i]}
			switch v := r.rng.IntN(100); {
			case v < 20:
				o.key = pick(r, []string{"a", "b"})
			case v < 40:
				o.group = pick(r, []string{"g", "h"})
			}
			o.absent = r.chance(25)
			reg.outputs = append(reg.outputs, o)
		}
	case shapeAs:
		reg.outputs = []modelOutput{{typ: pick(r, r.disposableConcreteTypes())}}
		switch r.rng.IntN(3) {
		case 0:
			reg.as = []int{modelTypeAsA}
		case 1:
			reg.as = []int{modelTypeAsB}
		default:
			reg.as = []int{modelTypeAsA, modelTypeAsB}
		}
	}
	if shape != shapeOut {
		r.genKeyOption(reg)
	}
	reg.noDispose = r.chance(10)
	reg.lazy = lifetime == godi.Singleton && shape != shapeInstance && r.chance(20)
	reg.errReturn = shape != shapeInstance && r.chance(30)

	if shape == shapeInstance {
		reg.instance = r.tr.newNode(reg.outputs[0].typ, reg, 0, "", nil)
		return reg
	}

	minLevel := len(modelTypes)
	for _, e := range r.m.entriesFor(reg) {
		minLevel = min(minLevel, e.typ)
	}
	reg.deps, reg.inStruct = r.genDeps(lifetime, minLevel)
	return reg
}

func (r *modelRun) options(reg *modelReg) []godi.AddOption {
	var opts []godi.AddOption
	switch key := reg.key.(type) {
	case string:
		opts = append(opts, godi.Name(key))
	case modelKey:
		opts = append(opts, godi.Key(key))
	}
	if reg.group != "" {
		opts = append(opts, godi.Group(reg.group))
	}
	for _, t := range reg.as {
		opts = append(opts, modelAsOption(t))
	}
	if reg.noDispose {
		opts = append(opts, godi.NoDispose())
	}
	if reg.lazy {
		opts = append(opts, godi.Lazy())
	}
	return opts
}

func describeReg(reg *modelReg) string {
	var b strings.Builder
	fmt.Fprintf(&b, "reg#%d %s %s outputs=[", reg.id, reg.lifetime, reg.shape)
	for i, o := range reg.outputs {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(modelTypes[o.typ].rt.String())
		if o.key != nil {
			fmt.Fprintf(&b, ":%v", o.key)
		}
		if o.group != "" {
			fmt.Fprintf(&b, "[%s]", o.group)
		}
		if o.absent {
			b.WriteString("(nil)")
		}
	}
	b.WriteString("]")
	for _, t := range reg.as {
		fmt.Fprintf(&b, " as=%v", modelTypes[t].rt)
	}
	if reg.key != nil {
		fmt.Fprintf(&b, " key=%#v", reg.key)
	}
	if reg.group != "" {
		fmt.Fprintf(&b, " group=%s", reg.group)
	}
	if len(reg.deps) > 0 {
		fmt.Fprintf(&b, " deps=%v in=%v", reg.deps, reg.inStruct)
	}
	if reg.noDispose {
		b.WriteString(" NoDispose")
	}
	if reg.lazy {
		b.WriteString(" Lazy")
	}
	if reg.errReturn {
		b.WriteString(" +error")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Generated constructors and decorators
// ---------------------------------------------------------------------------

func depFieldType(dep modelDep) reflect.Type {
	t := modelTypes[dep.typ].rt
	if dep.group != "" {
		return reflect.SliceOf(t)
	}
	return t
}

func depTag(dep modelDep) reflect.StructTag {
	var tags []string
	if dep.key != "" {
		tags = append(tags, fmt.Sprintf("name:%q", dep.key))
	}
	if dep.group != "" {
		tags = append(tags, fmt.Sprintf("group:%q", dep.group))
	}
	if dep.optional {
		tags = append(tags, `optional:"true"`)
	}
	return reflect.StructTag(strings.Join(tags, " "))
}

// constructor builds reg's constructor with reflect.MakeFunc, or returns the
// instance value.
func (r *modelRun) constructor(reg *modelReg) any {
	if reg.shape == shapeInstance {
		return modelTypes[reg.outputs[0].typ].wrap(reg.instance)
	}

	var in []reflect.Type
	if reg.inStruct {
		fields := []reflect.StructField{
			{Name: "In", Type: modelInType, Anonymous: true},
			{Name: "Ctx", Type: modelCtxType},
		}
		for i, dep := range reg.deps {
			fields = append(fields, reflect.StructField{
				Name: "F" + strconv.Itoa(i),
				Type: depFieldType(dep),
				Tag:  depTag(dep),
			})
		}
		in = []reflect.Type{reflect.StructOf(fields)}
	} else {
		in = []reflect.Type{modelCtxType}
		for _, dep := range reg.deps {
			in = append(in, modelTypes[dep.typ].rt)
		}
	}

	var out []reflect.Type
	var outStruct reflect.Type
	if reg.shape == shapeOut {
		fields := []reflect.StructField{{Name: "Out", Type: modelOutType, Anonymous: true}}
		for i, o := range reg.outputs {
			var tag reflect.StructTag
			if o.key != nil {
				tag = reflect.StructTag(fmt.Sprintf("name:%q", o.key))
			} else if o.group != "" {
				tag = reflect.StructTag(fmt.Sprintf("group:%q", o.group))
			}
			fields = append(fields, reflect.StructField{
				Name: "F" + strconv.Itoa(i),
				Type: modelTypes[o.typ].rt,
				Tag:  tag,
			})
		}
		outStruct = reflect.StructOf(fields)
		out = []reflect.Type{outStruct}
	} else {
		for _, o := range reg.outputs {
			out = append(out, modelTypes[o.typ].rt)
		}
	}
	if reg.errReturn {
		out = append(out, modelErrorType)
	}

	fnType := reflect.FuncOf(in, out, false)
	fn := reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		ctx, deps := extractArgs(reg.inStruct, args)
		scopeID := modelScopeID(ctx)
		reg.mu.Lock()
		reg.invocations[scopeID]++
		reg.mu.Unlock()

		results := make([]reflect.Value, 0, len(out))
		if outStruct != nil {
			sv := reflect.New(outStruct).Elem()
			for slot, o := range reg.outputs {
				if o.absent {
					continue
				}
				n := r.tr.newNode(o.typ, reg, slot, scopeID, deps)
				sv.Field(slot + 1).Set(reflect.ValueOf(modelTypes[o.typ].wrap(n)))
			}
			results = append(results, sv)
		} else {
			for slot, o := range reg.outputs {
				n := r.tr.newNode(o.typ, reg, slot, scopeID, deps)
				results = append(results, reflect.ValueOf(modelTypes[o.typ].wrap(n)))
			}
		}
		if reg.errReturn {
			results = append(results, reflect.Zero(modelErrorType))
		}
		return results
	})
	return fn.Interface()
}

func extractArgs(inStruct bool, args []reflect.Value) (context.Context, []*modelNode) {
	values := args
	if inStruct {
		sv := args[0]
		values = make([]reflect.Value, 0, sv.NumField()-1)
		for i := 1; i < sv.NumField(); i++ {
			values = append(values, sv.Field(i))
		}
	}
	ctx := values[0].Interface().(context.Context)
	var deps []*modelNode
	for _, v := range values[1:] {
		if v.Kind() == reflect.Slice {
			for i := range v.Len() {
				if n := modelNodeOfValue(v.Index(i)); n != nil {
					deps = append(deps, n)
				}
			}
			continue
		}
		if n := modelNodeOfValue(v); n != nil {
			deps = append(deps, n)
		}
	}
	return ctx, deps
}

func (r *modelRun) decorator(dec *modelDec) any {
	target := modelTypes[dec.target].rt
	in := []reflect.Type{target, modelCtxType}
	for _, t := range dec.deps {
		in = append(in, modelTypes[t].rt)
	}
	out := []reflect.Type{target}
	if dec.errReturn {
		out = append(out, modelErrorType)
	}
	fn := reflect.MakeFunc(reflect.FuncOf(in, out, false), func(args []reflect.Value) []reflect.Value {
		wrapped := modelNodeOfValue(args[0])
		scopeID := modelScopeID(args[1].Interface().(context.Context))
		deps := []*modelNode{wrapped}
		for _, v := range args[2:] {
			if n := modelNodeOfValue(v); n != nil {
				deps = append(deps, n)
			}
		}
		n := r.tr.newNode(dec.target, wrapped.reg, wrapped.slot, scopeID, deps)
		n.decs = append(slices.Clone(wrapped.decs), dec.id)
		n.noDispose = wrapped.noDispose
		results := []reflect.Value{reflect.ValueOf(modelTypes[dec.target].wrap(n))}
		if dec.errReturn {
			results = append(results, reflect.Zero(modelErrorType))
		}
		return results
	})
	return fn.Interface()
}

// ---------------------------------------------------------------------------
// Registration phase
// ---------------------------------------------------------------------------

var (
	allShapes      = []modelShape{shapePlain, shapePlain, shapePlain, shapeMulti, shapeOut, shapeAs, shapeAs, shapeInstance}
	replaceShapes  = []modelShape{shapePlain, shapeMulti, shapeAs, shapeInstance}
	lifetimeAdders = map[godi.Lifetime]func(godi.Collection, any, ...godi.AddOption){
		godi.Singleton: godi.Collection.AddSingleton,
		godi.Scoped:    godi.Collection.AddScoped,
		godi.Transient: godi.Collection.AddTransient,
	}
	replacers = map[godi.Lifetime]func(any, ...godi.AddOption) godi.ModuleOption{
		godi.Singleton: godi.ReplaceSingleton,
		godi.Scoped:    godi.ReplaceScoped,
		godi.Transient: godi.ReplaceTransient,
	}
	tryAdders = map[godi.Lifetime]func(any, ...godi.AddOption) godi.ModuleOption{
		godi.Singleton: godi.TryAddSingleton,
		godi.Scoped:    godi.TryAddScoped,
		godi.Transient: godi.TryAddTransient,
	}
)

// wouldConflict reports whether adding reg would fail on a duplicate.
func (r *modelRun) wouldConflict(reg *modelReg) bool {
	seen := map[modelSKey]bool{}
	for _, e := range r.m.entriesFor(reg) {
		if e.key == nil && e.group != "" {
			continue
		}
		k := modelSKey{e.typ, e.key}
		if r.m.services[k] != nil || seen[k] {
			return true
		}
		seen[k] = true
	}
	return false
}

func (r *modelRun) opAdd() {
	var reg *modelReg
	for range 10 {
		reg = r.genReg(r.randomLifetime(), allShapes)
		if !r.wouldConflict(reg) || r.allowErrors {
			break
		}
	}
	if r.wouldConflict(reg) && !r.allowErrors {
		return
	}
	r.regs = append(r.regs, reg)
	r.logf("Add %s", describeReg(reg))
	lifetimeAdders[reg.lifetime](r.c, r.constructor(reg), r.options(reg)...)
	r.m.add(reg)
}

func (r *modelRun) opTryAdd() {
	// TryAdd is a no-op when any target is registered, so it never conflicts.
	reg := r.genReg(r.randomLifetime(), replaceShapes)
	reg.group = ""
	r.regs = append(r.regs, reg)
	r.logf("TryAdd %s", describeReg(reg))
	r.c.AddModules(tryAdders[reg.lifetime](r.constructor(reg), r.options(reg)...))
	r.m.tryAdd(reg)
}

func (r *modelRun) opReplace() {
	var keys []modelSKey
	for k := range r.m.services {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b modelSKey) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
	if len(keys) == 0 && !r.allowErrors {
		return
	}

	var target modelSKey
	if len(keys) > 0 {
		target = pick(r, keys)
	} else {
		target = modelSKey{typ: pick(r, r.concreteTypes())}
	}
	// Mostly keep the lifetime, so that dependents stay valid.
	lifetime := r.randomLifetime()
	if e := r.m.services[target]; e != nil && r.chance(70) {
		lifetime = e.reg.lifetime
	}
	reg := &modelReg{id: r.id(), lifetime: lifetime, invocations: map[string]int{}}
	reg.key = target.key
	switch {
	case modelTypes[target.typ].iface:
		reg.shape = shapeAs
		reg.outputs = []modelOutput{{typ: pick(r, r.disposableConcreteTypes())}}
		reg.as = []int{target.typ}
	case lifetime == godi.Singleton && r.chance(20):
		reg.shape = shapeInstance
		reg.outputs = []modelOutput{{typ: target.typ}}
	case r.chance(20):
		reg.shape = shapeMulti
		reg.outputs = []modelOutput{{typ: target.typ}}
		for _, t := range r.rng.Perm(len(modelTypes)) {
			if !modelTypes[t].iface && t != target.typ {
				reg.outputs = append(reg.outputs, modelOutput{typ: t})
				break
			}
		}
	default:
		reg.shape = shapePlain
		reg.outputs = []modelOutput{{typ: target.typ}}
	}
	reg.noDispose = r.chance(10)
	reg.lazy = lifetime == godi.Singleton && reg.shape != shapeInstance && r.chance(20)
	if reg.shape == shapeInstance {
		reg.instance = r.tr.newNode(target.typ, reg, 0, "", nil)
	} else {
		reg.errReturn = r.chance(30)
		minLevel := len(modelTypes)
		for _, e := range r.m.entriesFor(reg) {
			minLevel = min(minLevel, e.typ)
		}
		reg.deps, reg.inStruct = r.genDeps(lifetime, minLevel)
	}
	// Replacing removes every target first, so only duplicates within reg
	// itself could conflict; multi-return outputs are distinct types.
	r.regs = append(r.regs, reg)
	r.logf("Replace %s", describeReg(reg))
	r.c.AddModules(replacers[lifetime](r.constructor(reg), r.options(reg)...))
	r.m.replace(reg)
}

func (r *modelRun) decoratedTypes() map[int]bool {
	out := map[int]bool{}
	for _, d := range r.m.decorators {
		out[d.target] = true
	}
	return out
}

// requiredBySomething reports whether a registration requires a service of
// type typ (with key, unless anyKey).
func (r *modelRun) requiredBySomething(typ int, key any, anyKey bool) bool {
	for _, e := range r.m.order {
		for _, dep := range e.reg.deps {
			if dep.typ != typ || dep.optional || dep.group != "" {
				continue
			}
			var depKey any
			if dep.key != "" {
				depKey = dep.key
			}
			if anyKey || depKey == key {
				return true
			}
		}
	}
	for _, d := range r.m.decorators {
		if key == nil && slices.Contains(d.deps, typ) {
			return true
		}
	}
	return false
}

// keepRemoval decides whether to go ahead with removing something decorated
// or required, which mostly makes Build fail.
func (r *modelRun) keepRemoval(typ int, key any, anyKey bool) bool {
	if r.decoratedTypes()[typ] || r.requiredBySomething(typ, key, anyKey) {
		return r.chance(10)
	}
	return true
}

func (r *modelRun) opRemove() {
	typ := r.rng.IntN(len(modelTypes))
	if !r.keepRemoval(typ, nil, true) {
		return
	}
	r.logf("Remove %v", modelTypes[typ].rt)
	r.c.Remove(modelTypes[typ].rt)
	r.m.remove(typ)
}

func (r *modelRun) opRemoveKeyed() {
	typ := r.rng.IntN(len(modelTypes))
	key := pick(r, []any{"a", "b", modelKey{N: 1}})
	if !r.keepRemoval(typ, key, false) {
		return
	}
	r.logf("RemoveKeyed %v %#v", modelTypes[typ].rt, key)
	r.c.RemoveKeyed(modelTypes[typ].rt, key)
	r.m.removeKeyed(typ, key)
}

func (r *modelRun) opDecorate() {
	type target struct {
		typ   int
		key   any
		group string
	}
	var targets []target
	for k := range r.m.services {
		targets = append(targets, target{typ: k.typ, key: k.key})
	}
	for k := range r.m.groups {
		targets = append(targets, target{typ: k.typ, group: k.group})
	}
	slices.SortFunc(targets, func(a, b target) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
	var t target
	switch {
	case len(targets) > 0 && !r.chance(5):
		t = pick(r, targets)
	case r.allowErrors:
		t = target{typ: r.rng.IntN(len(modelTypes))}
	default:
		return
	}
	dec := &modelDec{id: r.id(), target: t.typ, key: t.key, group: t.group, errReturn: r.chance(30)}
	// A decorator of a scoped service may depend on anything; others mostly
	// on singletons, to avoid lifetime conflicts.
	var decorated []*modelEntry
	if t.group != "" {
		decorated = r.m.groups[modelGKey{t.typ, t.group}]
	} else if e := r.m.services[modelSKey{t.typ, t.key}]; e != nil {
		decorated = []*modelEntry{e}
	}
	singletonDepsOnly := false
	for _, e := range decorated {
		if e.reg.lifetime != godi.Scoped {
			singletonDepsOnly = !r.chance(10)
		}
	}
	// Decorator dependencies are plain (unkeyed) parameters of a lower level.
	for range r.rng.IntN(3) {
		var cands []int
		for k, e := range r.m.services {
			if k.key == nil && k.typ < t.typ && (e.reg.lifetime == godi.Singleton || !singletonDepsOnly) {
				cands = append(cands, k.typ)
			}
		}
		slices.Sort(cands)
		if len(cands) > 0 {
			dec.deps = append(dec.deps, pick(r, cands))
		}
	}
	var opts []godi.AddOption
	switch key := t.key.(type) {
	case string:
		opts = append(opts, godi.Name(key))
	case modelKey:
		opts = append(opts, godi.Key(key))
	}
	if t.group != "" {
		opts = append(opts, godi.Group(t.group))
	}
	r.logf("Decorate dec#%d %v key=%#v group=%q deps=%v", dec.id, modelTypes[t.typ].rt, t.key, t.group, dec.deps)
	r.c.AddModules(godi.Decorate(r.decorator(dec), opts...))
	r.m.decorators = append(r.m.decorators, dec)
}

// checkRegistry compares the collection with the model after an operation.
func (r *modelRun) checkRegistry(op string) {
	if got, want := r.c.Err() != nil, r.m.err; got != want {
		r.failf("after %s: Err()=%v, model expects error=%v", op, r.c.Err(), want)
		r.m.err = got // report once
	}
	if r.m.err {
		return // after a failed registration counts may legitimately differ
	}
	if got, want := r.c.Count(), r.m.count(); got != want {
		r.failf("after %s: Count()=%d, model has %d entries", op, got, want)
	}
	for i, t := range modelTypes {
		_, want := r.m.services[modelSKey{typ: i}]
		if got := r.c.Contains(t.rt); got != want {
			r.failf("after %s: Contains(%v)=%v, model %v", op, t.rt, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Build phase
// ---------------------------------------------------------------------------

func (r *modelRun) build() bool {
	matched := r.m.attach()
	modelValid := !r.m.err && matched && !r.m.missingDependency() && !r.m.lifetimeConflict()
	switch {
	case r.m.err:
		r.stats.registrationErrs++
	case !matched:
		r.stats.unmatched++
	case r.m.missingDependency():
		r.stats.missing++
	case r.m.lifetimeConflict():
		r.stats.conflicts++
	}

	validateErr := godi.Validate(r.c)
	if (validateErr == nil) != modelValid {
		r.failf("Validate: err=%v, model valid=%v (decorators matched=%v missing=%v conflict=%v)",
			validateErr, modelValid, matched, r.m.missingDependency(), r.m.lifetimeConflict())
	}

	if knownDecoratorSelfDependency && modelValid && r.m.decoratorReachesOwnConstructor() {
		// Build or resolution would deadlock (or overflow the stack).
		r.logf("skipped: a decorator depends on its own constructor (known bug)")
		r.stats.skipped++
		return false
	}
	if knownSiblingDecoratorLifetimeGap && modelValid && r.m.lifetimeGap() {
		r.logf("skipped: a singleton captures a scoped service through a sibling output's decorator (known bug)")
		r.stats.skipped++
		return false
	}

	r.res = &resolution{m: r.m, memo: map[*modelEntry]modelStatus{}}
	eagerOK := true
	for _, e := range r.m.order {
		if e.reg.lifetime == godi.Singleton && !e.reg.lazy && r.res.status(e) == statusFail {
			eagerOK = false
		}
	}
	if modelValid && !eagerOK {
		r.stats.eagerFails++
	}

	r.validateScopes = r.chance(30)
	r.tr.setBuilding(true)
	p, err := r.c.BuildWithOptions(&godi.ProviderOptions{ValidateScopes: r.validateScopes})
	r.tr.setBuilding(false)
	r.logf("Build(ValidateScopes=%v): %v", r.validateScopes, err)

	if modelValid && eagerOK {
		if err != nil {
			r.failf("Build failed but Validate passed and every eager singleton resolves: %v", err)
		}
	} else if err == nil {
		r.failf("Build succeeded; model expects failure (valid=%v eagerOK=%v, Validate err=%v)", modelValid, eagerOK, validateErr)
	}
	if validateErr != nil && err == nil {
		r.failf("Build succeeded but Validate failed: %v", validateErr)
	}
	if err != nil {
		if p != nil {
			r.failf("Build returned a provider with an error")
		}
		return false
	}
	r.p = p

	// The root scope's ID, to attribute constructions made in it.
	ctx, gerr := godi.Resolve[context.Context](p)
	if gerr != nil {
		r.failf("resolve context.Context from provider: %v", gerr)
		return false
	}
	r.rootID = modelScopeID(ctx)

	r.tr.mu.Lock()
	for _, n := range r.tr.nodes {
		if n.inBuild {
			r.failf("node %d (%s) was closed during a successful Build", n.id, describeReg(n.reg))
		}
	}
	r.tr.mu.Unlock()
	return true
}

// ---------------------------------------------------------------------------
// Runtime phase
// ---------------------------------------------------------------------------

func (r *modelRun) openScopes() []*modelScope {
	var out []*modelScope
	for _, s := range r.scopes {
		if !s.closed {
			out = append(out, s)
		}
	}
	return out
}

func (r *modelRun) closedScopes() []*modelScope {
	var out []*modelScope
	for _, s := range r.scopes {
		if s.closed {
			out = append(out, s)
		}
	}
	return out
}

func (r *modelRun) opCreateScope() {
	var parent *modelScope
	var s godi.Scope
	var err error
	if open := r.openScopes(); len(open) > 0 && r.chance(30) {
		parent = pick(r, open)
		s, err = parent.s.CreateScope(context.Background())
	} else {
		s, err = r.p.CreateScope(context.Background())
	}
	if err != nil {
		r.failf("CreateScope: %v", err)
		return
	}
	ms := &modelScope{s: s, id: s.ID(), parent: parent}
	if parent != nil {
		parent.children = append(parent.children, ms)
	}
	r.scopes = append(r.scopes, ms)
	r.logf("CreateScope %s (parent %v)", ms.id, parent != nil)
}

func (ms *modelScope) subtree() []*modelScope {
	out := []*modelScope{ms}
	for _, c := range ms.children {
		out = append(out, c.subtree()...)
	}
	return out
}

func (r *modelRun) opCloseScope() {
	open := r.openScopes()
	if len(open) == 0 {
		return
	}
	ms := pick(r, open)
	var ids []string
	for _, s := range ms.subtree() {
		ids = append(ids, s.id)
	}
	r.tr.markClosing(ids...)
	var err error
	if r.chance(50) {
		r.logf("Close scope %s", ms.id)
		err = ms.s.Close()
	} else {
		r.logf("Shutdown scope %s", ms.id)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		err = godi.Shutdown(ctx, ms.s)
		cancel()
	}
	if err != nil {
		r.failf("closing scope %s: %v", ms.id, err)
	}
	for _, s := range ms.subtree() {
		s.closed = true
	}
}

// directEntries returns the entries resolvable with Get/GetKeyed.
func (r *modelRun) directEntries(fromRoot bool) []*modelEntry {
	var out []*modelEntry
	for _, e := range r.m.order {
		if e.group != "" && e.key == nil {
			continue
		}
		// Transients resolved directly from the root provider are owned by
		// the caller, so they are not resolved there.
		if fromRoot && e.reg.lifetime == godi.Transient {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (r *modelRun) get(res godi.Resolver, e *modelEntry) (any, error) {
	rt := modelTypes[e.typ].rt
	if e.key == nil {
		return res.Get(rt)
	}
	return res.GetKeyed(rt, e.key)
}

// checkNode checks a resolved value against the model's entry.
func (r *modelRun) checkNode(where string, e *modelEntry, v any) *modelNode {
	noder, ok := v.(modelNoder)
	if !ok {
		r.failf("%s: resolved %T, not a model value", where, v)
		return nil
	}
	n := noder.modelNodeOf()
	if n.reg != e.reg || n.slot != e.slot {
		r.failf("%s: resolved node %d from %s slot %d; model expects %s slot %d",
			where, n.id, describeReg(n.reg), n.slot, describeReg(e.reg), e.slot)
	}
	var want []int
	for _, d := range e.decs {
		want = append(want, d.id)
	}
	if !slices.Equal(n.decs, want) {
		r.failf("%s: decorators applied %v, model expects %v", where, n.decs, want)
	}
	if !reflect.TypeOf(v).AssignableTo(modelTypes[e.typ].rt) {
		r.failf("%s: resolved %T, not assignable to %v", where, v, modelTypes[e.typ].rt)
	}
	return n
}

// checkIdentity checks lifetime identity: shared singletons, one scoped value
// per scope, a fresh transient each time.
func (r *modelRun) checkIdentity(where string, e *modelEntry, scopeID string, n *modelNode) {
	switch e.reg.lifetime {
	case godi.Singleton:
		if prev, ok := r.singletonNode[e]; ok && prev != n {
			r.failf("%s: singleton resolved to node %d, previously %d", where, n.id, prev.id)
		}
		r.singletonNode[e] = n
	case godi.Scoped:
		perScope := r.scopedNode[e]
		if perScope == nil {
			perScope = map[string]*modelNode{}
			r.scopedNode[e] = perScope
		}
		if prev, ok := perScope[scopeID]; ok && prev != n {
			r.failf("%s: scoped service resolved to node %d in scope %s, previously %d", where, n.id, scopeID, prev.id)
		}
		for other, prev := range perScope {
			if other != scopeID && prev == n {
				r.failf("%s: scoped node %d shared by scopes %s and %s", where, n.id, other, scopeID)
			}
		}
		perScope[scopeID] = n
		if n.scopeID != scopeID {
			r.failf("%s: scoped node %d constructed in scope %s, resolved from %s", where, n.id, n.scopeID, scopeID)
		}
	case godi.Transient:
		if r.transientSeen[n] {
			r.failf("%s: transient resolution returned node %d again", where, n.id)
		}
		r.transientSeen[n] = true
	}
}

func (r *modelRun) resolveEntry(ms *modelScope, e *modelEntry) {
	var res godi.Resolver = r.p
	scopeID := r.rootID
	where := fmt.Sprintf("resolve %v key=%#v from root", modelTypes[e.typ].rt, e.key)
	if ms != nil {
		res, scopeID = ms.s, ms.id
		where = fmt.Sprintf("resolve %v key=%#v from %s", modelTypes[e.typ].rt, e.key, ms.id)
	}
	r.stats.resolves++
	v, err := r.get(res, e)
	r.logf("%s: err=%v", where, err)

	switch {
	case ms != nil && ms.closed:
		if !errors.Is(err, godi.ErrScopeDisposed) {
			r.failf("%s: closed scope returned err=%v, want ErrScopeDisposed", where, err)
		}
		return
	case ms == nil && r.validateScopes && e.reg.lifetime == godi.Scoped:
		if !errors.Is(err, godi.ErrScopeRequired) {
			r.failf("%s: err=%v, want ErrScopeRequired", where, err)
		}
		return
	}

	want := r.res.status(e)
	if want != statusOK {
		if err == nil {
			r.failf("%s: succeeded, model expects failure (status %d)", where, want)
		}
		return
	}
	if err != nil {
		r.failf("%s: %v (model expects success)", where, err)
		return
	}
	if n := r.checkNode(where, e, v); n != nil {
		r.checkIdentity(where, e, scopeID, n)
	}
}

func (r *modelRun) opResolve() {
	open := r.openScopes()
	fromRoot := len(open) == 0 || r.chance(20)
	entries := r.directEntries(fromRoot)
	if len(entries) == 0 {
		return
	}
	e := pick(r, entries)
	if fromRoot {
		r.resolveEntry(nil, e)
		return
	}
	r.resolveEntry(pick(r, open), e)
}

func (r *modelRun) opResolveClosed() {
	closed := r.closedScopes()
	entries := r.directEntries(false)
	if len(closed) == 0 || len(entries) == 0 {
		return
	}
	r.resolveEntry(pick(r, closed), pick(r, entries))
}

func (r *modelRun) opResolveGroup() {
	open := r.openScopes()
	if len(open) == 0 || len(r.m.groups) == 0 {
		return
	}
	var keys []modelGKey
	for k := range r.m.groups {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b modelGKey) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
	k := pick(r, keys)
	ms := pick(r, open)
	where := fmt.Sprintf("group %v[%s] from %s", modelTypes[k.typ].rt, k.group, ms.id)

	vs, err := ms.s.GetGroup(modelTypes[k.typ].rt, k.group)
	r.logf("%s: %d values, err=%v", where, len(vs), err)

	var want []*modelEntry
	fail := false
	for _, e := range r.m.groups[k] {
		switch r.res.status(e) {
		case statusOK:
			want = append(want, e)
		case statusFail:
			fail = true
		}
	}
	if fail {
		if err == nil {
			r.failf("%s: succeeded, model expects a member to fail", where)
		}
		return
	}
	if err != nil {
		r.failf("%s: %v (model expects success)", where, err)
		return
	}
	if len(vs) != len(want) {
		r.failf("%s: %d members, model expects %d", where, len(vs), len(want))
		return
	}
	for i, e := range want {
		if n := r.checkNode(fmt.Sprintf("%s member %d", where, i), e, vs[i]); n != nil {
			r.checkIdentity(where, e, ms.id, n)
		}
	}
}

// opConcurrentResolve resolves one cacheable service from several goroutines
// at once; all must receive the same value.
func (r *modelRun) opConcurrentResolve() {
	open := r.openScopes()
	if len(open) == 0 {
		return
	}
	var entries []*modelEntry
	for _, e := range r.directEntries(false) {
		if e.reg.lifetime != godi.Transient && r.res.status(e) == statusOK {
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return
	}
	e := pick(r, entries)
	ms := pick(r, open)
	where := fmt.Sprintf("concurrent resolve %v key=%#v from %s", modelTypes[e.typ].rt, e.key, ms.id)
	r.logf("%s", where)

	const workers = 4
	values := make([]any, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			<-start
			values[i], errs[i] = r.get(ms.s, e)
		})
	}
	close(start)
	wg.Wait()
	for i := range workers {
		if errs[i] != nil {
			r.failf("%s: worker %d: %v", where, i, errs[i])
			return
		}
		if values[i] != values[0] {
			r.failf("%s: workers received different values", where)
			return
		}
	}
	if n := r.checkNode(where, e, values[0]); n != nil {
		r.checkIdentity(where, e, ms.id, n)
	}
}

func (r *modelRun) closeProvider() {
	r.tr.setProviderClosing()
	switch r.rng.IntN(4) {
	case 0, 1:
		r.logf("Close provider")
		if err := r.p.Close(); err != nil {
			r.failf("provider Close: %v", err)
		}
	case 2:
		r.logf("Shutdown provider (deadline)")
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := godi.Shutdown(ctx, r.p); err != nil {
			r.failf("provider Shutdown: %v", err)
		}
	default:
		r.logf("Shutdown provider (cancelled), then Close")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := godi.Shutdown(ctx, r.p); err != nil && !errors.Is(err, context.Canceled) {
			r.failf("provider Shutdown with a cancelled context: %v, want nil or context.Canceled", err)
		}
		// Close waits for the cleanup Shutdown left running.
		if err := r.p.Close(); err != nil {
			r.failf("provider Close after Shutdown: %v", err)
		}
	}

	// A closed provider rejects resolution and scope creation.
	if _, err := r.p.CreateScope(context.Background()); !errors.Is(err, godi.ErrProviderDisposed) {
		r.failf("CreateScope after Close: err=%v, want ErrProviderDisposed", err)
	}
	if entries := r.directEntries(true); len(entries) > 0 {
		if _, err := r.get(r.p, pick(r, entries)); err == nil {
			r.failf("resolution after provider Close succeeded")
		}
	}
	for _, ms := range r.scopes {
		ms.closed = true
	}
}

// ---------------------------------------------------------------------------
// Invariants
// ---------------------------------------------------------------------------

// Known bugs the model works around. Each has a skipped regression test in
// known_bugs_test.go; set the constant to false once the bug is fixed.
const (
	// The results of all but the last decorator of a service are never
	// closed (TestDecoratorChainClosesIntermediateResults).
	knownDecoratorChainLeak = true

	// When a decorator of one output of a multi-output constructor (multi
	// return, godi.Out, or godi.As aliases) fails, the outputs decorated
	// before it are never closed
	// (TestDecoratorFailureClosesSiblingOutputs).
	knownSiblingDecoratorFailureLeak = true

	// A decorator that depends on another output of its service's own
	// constructor deadlocks Build or resolution; such programs are skipped
	// (TestDecoratorDependingOnSiblingOutput).
	knownDecoratorSelfDependency = true

	// Transient dependencies of a decorator of an instance registration are
	// never closed (TestInstanceDecoratorClosesTransientDependencies).
	knownInstanceDecoratorTransientLeak = true

	// Lifetime validation ignores the decorators of a constructor's other
	// outputs, so a singleton can capture a scoped service through them;
	// such programs are skipped (TestLifetimeValidationFollowsSiblingDecorators).
	knownSiblingDecoratorLifetimeGap = true
)

// knownLeak reports whether n may be left unclosed because of a known bug.
func (r *modelRun) knownLeak(n *modelNode) bool {
	if knownDecoratorChainLeak && r.isIntermediateDecoration(n) {
		return true
	}
	if knownSiblingDecoratorFailureLeak && r.res != nil && n.reg.shape != shapePlain &&
		n.reg.shape != shapeInstance && r.res.regDiscards(n.reg) {
		return true
	}
	// Before Build succeeds, every construction runs in the root scope.
	inRoot := r.p == nil || n.scopeID == r.rootID
	if knownInstanceDecoratorTransientLeak && n.reg.lifetime == godi.Transient &&
		inRoot && r.m.instanceDecoratorDeps() {
		return true
	}
	return false
}

// isIntermediateDecoration reports whether n is the result of a decorator
// other than the last one applied to its service.
func (r *modelRun) isIntermediateDecoration(n *modelNode) bool {
	if len(n.decs) == 0 || r.res == nil {
		return false
	}
	for _, e := range r.res.liveEntries(n.reg) {
		if e.typ == n.typ && e.slot == n.slot {
			return len(n.decs) < len(e.decs)
		}
	}
	return false
}

func (r *modelRun) checkLifecycle(built bool) {
	r.tr.mu.Lock()
	defer r.tr.mu.Unlock()
	r.stats.nodes += len(r.tr.nodes)

	discards := func(n *modelNode) bool { return r.res != nil && r.res.regDiscards(n.reg) }

	for _, n := range r.tr.nodes {
		desc := fmt.Sprintf("node %d (%v of %s, decorators %v, scope %q)", n.id, modelTypes[n.typ].rt, describeReg(n.reg), n.decs, n.scopeID)
		r.stats.closes += n.closes
		switch {
		case n.closes > 1:
			r.failf("%s closed %d times", desc, n.closes)
		case n.noDispose && n.closes > 0:
			r.failf("%s is NoDispose but was closed", desc)
		case !n.closable || n.noDispose:
		case n.reg.shape == shapeInstance && n.scopeID == "":
			// An instance is owned once Build publishes it.
			if built && n.closes != 1 && r.liveAtBuild(n.reg) {
				r.failf("%s: instance live at Build was not closed", desc)
			}
		case n.closes == 0:
			if r.knownLeak(n) {
				continue
			}
			r.failf("%s was never closed", desc)
		}
		if n.closedEarly && !discards(n) {
			r.failf("%s was closed while its scope was open", desc)
		}
	}

	// Consumers are closed before their dependencies.
	for _, n := range r.tr.nodes {
		if n.closes == 0 {
			continue
		}
		for _, d := range n.deps {
			if d.closes > 0 && d.closeSeq < n.closeSeq && !discards(n) {
				r.failf("node %d (%v) was closed after its dependency node %d (%v)",
					n.id, modelTypes[n.typ].rt, d.id, modelTypes[d.typ].rt)
			}
		}
	}

	if !built {
		return
	}
	// One construction per scope (scoped) or per provider (singleton).
	for _, reg := range r.regs {
		reg.mu.Lock()
		total := 0
		for scopeID, calls := range reg.invocations {
			total += calls
			if reg.lifetime == godi.Scoped && calls > 1 && !r.regFails(reg) {
				r.failf("%s constructed %d times in scope %s", describeReg(reg), calls, scopeID)
			}
		}
		if reg.lifetime == godi.Singleton && total > 1 && !r.regFails(reg) {
			r.failf("%s (singleton) constructed %d times", describeReg(reg), total)
		}
		reg.mu.Unlock()
	}
}

func (r *modelRun) liveAtBuild(reg *modelReg) bool {
	for _, e := range r.m.order {
		if e.reg == reg {
			return !reg.lazy
		}
	}
	return false
}

// regFails reports whether a construction of reg can fail after running (so
// a retry legitimately constructs again).
func (r *modelRun) regFails(reg *modelReg) bool {
	for _, e := range r.res.liveEntries(reg) {
		if r.res.status(e) == statusFail {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Driver
// ---------------------------------------------------------------------------

func (r *modelRun) run() {
	defer func() {
		if p := recover(); p != nil {
			r.failf("panic: %v", p)
		}
	}()

	for range 4 + r.rng.IntN(22) {
		var op string
		switch v := r.rng.IntN(100); {
		case v < 50:
			op = "add"
			r.opAdd()
		case v < 60:
			op = "tryAdd"
			r.opTryAdd()
		case v < 70:
			op = "replace"
			r.opReplace()
		case v < 76:
			op = "remove"
			r.opRemove()
		case v < 80:
			op = "removeKeyed"
			r.opRemoveKeyed()
		default:
			op = "decorate"
			r.opDecorate()
		}
		r.checkRegistry(op)
	}

	built := r.build()
	if built {
		r.stats.built++
		for range 10 + r.rng.IntN(40) {
			switch v := r.rng.IntN(100); {
			case v < 15:
				r.opCreateScope()
			case v < 55:
				r.opResolve()
			case v < 67:
				r.opResolveGroup()
			case v < 77:
				r.opCloseScope()
			case v < 80:
				r.opResolveClosed()
			case v < 86:
				r.opConcurrentResolve()
			default:
				r.opResolve()
			}
		}
		r.closeProvider()
	}
	r.checkLifecycle(built)
}

func envInt(name string, def uint64) (uint64, bool) {
	s := os.Getenv(name)
	if s == "" {
		return def, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("%s=%q: %v", name, s, err))
	}
	return v, true
}

func TestModelBasedInvariants(t *testing.T) {
	t.Parallel()

	stats := &modelStats{}
	report := func(r *modelRun) {
		t.Errorf("program with seed %d failed (replay: GODI_MODEL_RUN_SEED=%d go test -run TestModelBasedInvariants -v)\nfailures:\n  %s",
			r.seed, r.seed, strings.Join(r.failures, "\n  "))
		t.Logf("operations:\n  %s", strings.Join(r.log, "\n  "))
	}

	if runSeed, ok := envInt("GODI_MODEL_RUN_SEED", 0); ok {
		r := newModelRun(runSeed, stats)
		r.run()
		t.Logf("operations:\n  %s", strings.Join(r.log, "\n  "))
		if len(r.failures) > 0 {
			report(r)
		}
		return
	}

	seed, fixed := envInt("GODI_MODEL_SEED", 0)
	if !fixed {
		seed = rand.Uint64()
	}
	iterations, _ := envInt("GODI_MODEL_ITERATIONS", modelDefaultIterations)
	t.Logf("seed %d (replay: GODI_MODEL_SEED=%d), %d programs", seed, seed, iterations)

	master := rand.New(rand.NewPCG(seed, 0))
	for range iterations {
		stats.runs++
		r := newModelRun(master.Uint64(), stats)
		r.run()
		if len(r.failures) > 0 {
			report(r)
			return
		}
	}
	t.Logf("%d programs: %d built; Build failed on %d registration errors, %d unmatched decorators, "+
		"%d missing dependencies, %d lifetime conflicts, %d failing singletons; %d skipped (known bug)",
		stats.runs, stats.built, stats.registrationErrs, stats.unmatched, stats.missing, stats.conflicts,
		stats.eagerFails, stats.skipped)
	t.Logf("%d resolutions, %d values constructed, %d closes", stats.resolves, stats.nodes, stats.closes)
}
