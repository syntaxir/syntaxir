// Package pipe is the pass infrastructure of one pipeline per compilation: one sealed step type,
// typed pipelines built by combinators, and one manager that runs them, numbers, observes, verifies,
// times and counts every step the same way, and owns the copies of the units.
//
// A pass is created only by New and run only by Execute. A kind (IR) describes one Go type of unit
// to the manager: how to verify, print and copy it. A pipeline is an immutable step tree that the
// combinators build; Execute checks it and then runs it.
//
// The package is to fold into the parent package, which keeps the registry of the optimizations, the
// analyses and the error of a required step that the driver returns.
package pipe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"

	"zeroparse.dev/zp/internal/pass"
)

// IR describes the units of Go type M to the manager. A kind is identified by its Go type: one Go
// type is one kind, so two stages of one IR are two Go types.
type IR[M any] struct {
	Name   string                 // the spelling of the kind in the text form
	Verify func(m M) error        // the invariants of the kind
	Print  func(w io.Writer, m M) // the dump; nil: a unit of the kind is never dumped
	Copy   func(m M) M            // a deep copy; nil for a value kind
}

type kind struct {
	name   string
	typ    reflect.Type
	verify func(any) error
	print  func(io.Writer, any)
	copy   func(any) any
}

var (
	kindsByType = map[reflect.Type]*kind{}
	kindsByIR   = map[any]*kind{}
	// listMu guards the registration of the list kinds of Fork, which happens when a pipeline is
	// built and so may happen in several goroutines.
	listMu sync.Mutex
)

// RegisterIR makes a kind known to the manager and the text form. A name or a Go type registered
// twice panics. It returns ir, for package-level declarations.
func RegisterIR[M any](ir *IR[M]) *IR[M] {
	t := reflect.TypeFor[M]()
	if kindsByType[t] != nil {
		panic(fmt.Sprintf("pipe: Go type %v is registered twice", t))
	}
	claim(ir.Name)
	k := &kind{name: ir.Name, typ: t}
	if ir.Verify != nil {
		k.verify = func(u any) error { return ir.Verify(u.(M)) }
	}
	if ir.Print != nil {
		k.print = func(w io.Writer, u any) { ir.Print(w, u.(M)) }
	}
	if ir.Copy != nil {
		k.copy = func(u any) any { return ir.Copy(u.(M)) }
	}
	kindsByType[t], kindsByIR[ir] = k, k
	return ir
}

func kindOf[M any](ir *IR[M]) *kind {
	if k := kindsByIR[ir]; k != nil {
		return k
	}
	panic(fmt.Sprintf("pipe: kind %q is not registered", ir.Name))
}

// listKind is the kind of the results of a Fork: a list of units of e, whose verifier verifies
// each element. A list type registered with RegisterIR is that kind instead. The first Fork over e
// registers it; it is safe to call from several goroutines.
func listKind(e *kind) *kind {
	listMu.Lock()
	defer listMu.Unlock()
	t := reflect.SliceOf(e.typ)
	if k := kindsByType[t]; k != nil {
		return k
	}
	name := "[]" + e.name
	claim(name)
	k := &kind{name: name, typ: t}
	k.verify = func(u any) error {
		if e.verify == nil {
			return nil
		}
		v := reflect.ValueOf(u)
		for i := range v.Len() {
			if err := e.verify(v.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	}
	kindsByType[t] = k
	return k
}

// FeatureID names a feature in declarations and in the text form.
type FeatureID string

// FeatureKey is a typed fact of the compilation that an enclosing scope of the pipeline provides
// (WithConst, WithDerived). Its format gives it a text form, for the keys of Shared; the parser of
// the text form comes with the text door.
type FeatureKey[T any] struct {
	id     FeatureID
	format func(T) string
}

var featureFormats = map[FeatureID]func(any) string{}

// NewFeature declares and registers a feature. A name declared twice panics.
func NewFeature[T any](name string, format func(T) string) *FeatureKey[T] {
	id := FeatureID(name)
	if name == "" || featureFormats[id] != nil {
		panic(fmt.Sprintf("pipe: feature %q has no name or is declared twice", name))
	}
	featureFormats[id] = func(v any) string { return format(v.(T)) }
	return &FeatureKey[T]{id: id, format: format}
}

// ID returns the identifier of the feature, for Decl.Features.
func (k *FeatureKey[T]) ID() FeatureID { return k.id }

// Decl is what a pass declares besides its types and its function.
type Decl struct {
	Name       string      // unique among registered passes; the flag, dump and error name
	Required   bool        // runs whenever reached; has no flag
	Test       bool        // an optional pass without a flag, always enabled (tests)
	After      []string    // passes that must precede it in the pipeline
	Obligation string      // what the pass preserves, in one sentence
	Features   []FeatureID // the features it reads (Feature panics on any other)
	Params     []string    // the registry flags it reads (Ctx.Value panics on any other)
	Patterns   []string    // the registry flags of its sub-pass decisions (Ctx.Allow panics on any other)
}

// Def is the typed definition of a pass, given once to New. Its Run stays in the declaring package.
type Def[I, O any] struct {
	Decl
	In      *IR[I]
	Out     *IR[O]
	Applies func(c *Ctx[I]) bool
	Run     func(c *Ctx[I], in I) (O, pass.PreservedSet, error)
}

// Pass is a sealed step from units of I to units of O. Only the manager runs it.
type Pass[I, O any] struct{ d *passDesc }

type passDesc struct {
	Decl
	in, out *kind
	applies func(*ctxState) bool
	run     func(*ctxState, any) (any, pass.PreservedSet, error)
}

var passes = map[string]*passDesc{}

// New seals and registers a pass. A name registered twice panics.
func New[I, O any](d Def[I, O]) *Pass[I, O] {
	if d.Name == "" || passes[d.Name] != nil {
		panic(fmt.Sprintf("pipe: pass %q has no name or is registered twice", d.Name))
	}
	if d.Run == nil {
		panic(fmt.Sprintf("pipe: pass %q has no run function", d.Name))
	}
	if d.Required && d.Test {
		panic(fmt.Sprintf("pipe: pass %q is both required and a test pass", d.Name))
	}
	pd := &passDesc{Decl: d.Decl, in: kindOf(d.In), out: kindOf(d.Out)}
	run := d.Run
	pd.run = func(c *ctxState, in any) (any, pass.PreservedSet, error) { return run(ctxOf[I](c), in.(I)) }
	if d.Applies != nil {
		ap := d.Applies
		pd.applies = func(c *ctxState) bool { return ap(ctxOf[I](c)) }
	}
	passes[d.Name] = pd
	return &Pass[I, O]{d: pd}
}

// Name returns the name of the pass.
func (p *Pass[I, O]) Name() string { return p.d.Name }

// Decl returns what the pass declares.
func (p *Pass[I, O]) Decl() Decl {
	d := p.d.Decl
	d.After, d.Features, d.Params, d.Patterns = slices.Clone(d.After), slices.Clone(d.Features), slices.Clone(d.Params), slices.Clone(d.Patterns)
	return d
}

// Ctx is what a running pass sees, typed by its input kind. It is valid only during the call.
type Ctx[M any] struct{ s *ctxState }

type ctxState struct {
	rs       *runner
	env      *env
	pass     *passDesc
	instance string
	counts   map[string]int
	unit     any
	typed    any // the *Ctx[I] of the step, shared by Applies and Run
}

// ctxOf returns the typed context of a step, made once for Applies and Run.
func ctxOf[I any](c *ctxState) *Ctx[I] {
	if t, ok := c.typed.(*Ctx[I]); ok {
		return t
	}
	t := &Ctx[I]{s: c}
	c.typed = t
	return t
}

// Label names the unit a step runs on: its backend, its table and its function. Each combinator
// sets only the field it decides: a Fork branch the backend, an Each part the table, a PerFunc
// block the function.
type Label struct{ Backend, Table, Func string }

func (l Label) String() string {
	var at []string
	for _, s := range []string{l.Backend, l.Table, l.Func} {
		if s != "" {
			at = append(at, s)
		}
	}
	return strings.Join(at, "/")
}

// Label returns the label of the unit.
func (c *Ctx[M]) Label() Label { return c.s.env.label }

// Context returns the context of the run, for cancellation polling.
func (c *Ctx[M]) Context() context.Context { return c.s.rs.ctx }

// Value returns the value of a declared parameter: its setting. (The text door adds overrides per
// instance.)
func (c *Ctx[M]) Value(flag string) int {
	if !contains(c.s.pass.Params, flag) {
		panic(fmt.Sprintf("pipe: pass %s reads the undeclared parameter %s", c.s.pass.Name, flag))
	}
	return c.s.rs.r.Settings.Value(flag)
}

// Count adds n to a statistic of this pass on this unit.
func (c *Ctx[M]) Count(counter string, n int) {
	if c.s.counts == nil {
		c.s.counts = map[string]int{}
	}
	c.s.counts[counter] += n
}

// Allow reports whether one application of a declared pattern may run. The manager numbers each
// decision as a run of its own: the pattern's flag turns it off, and an observer may veto it.
func (c *Ctx[M]) Allow(pattern string) bool {
	if !contains(c.s.pass.Patterns, pattern) {
		panic(fmt.Sprintf("pipe: pass %s asks about the undeclared pattern %s", c.s.pass.Name, pattern))
	}
	return c.s.rs.allow(c.s, pattern)
}

// Feature returns a declared feature that an enclosing scope provides.
func Feature[T, M any](c *Ctx[M], k *FeatureKey[T]) T {
	if !contains(c.s.pass.Features, k.id) {
		panic(fmt.Sprintf("pipe: pass %s reads the undeclared feature %s", c.s.pass.Name, k.id))
	}
	return c.s.env.features[k.id].(T)
}

// Query returns an analysis of the current unit through the analysis manager of that unit. A pass
// that changes the unit reports what it preserved, and the manager discards the rest; a step that
// changes a part of a unit (PerFunc, Within) discards the analyses of the whole, and a lowering those
// of its input.
func Query[M, R any](c *Ctx[M], key pass.AnalysisKey[M, R]) (R, error) {
	rs, u := c.s.rs, c.s.unit
	if !reflect.ValueOf(u).Comparable() {
		return pass.Get(pass.NewAnalysisManager[M](rs.ctx), u.(M), key) // no identity to cache by
	}
	a := rs.analyses[u]
	if a == nil {
		am := pass.NewAnalysisManager[M](rs.ctx)
		a = &analyses{am: am, invalidate: am.Invalidate}
		rs.analyses[u] = a
	}
	return pass.Get(a.am.(*pass.AnalysisManager[M]), u.(M), key)
}

// analyses is the analysis manager of one unit.
type analyses struct {
	am         any
	invalidate func(pass.PreservedSet)
}

func contains[T comparable](s []T, v T) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// StepError is the failure of a step, naming its instance and its unit.
type StepError struct {
	Step  string
	Label Label
	Err   error
}

func (e *StepError) Error() string {
	if at := e.Label.String(); at != "" {
		return fmt.Sprintf("%s [%s]: %v", e.Step, at, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Step, e.Err)
}

func (e *StepError) Unwrap() error { return e.Err }

// errCheck is wrapped by the errors of a pipeline that fails its checks before any step runs.
var errCheck = errors.New("invalid pipeline")
