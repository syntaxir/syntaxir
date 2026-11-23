package plandata

import (
	"fmt"
	"io"
	"maps"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Plan is the plan of one rule table, which flowplan makes (flowplan.Plan, flowplan.PlanTyped): the
// functions that the table runs from Flow IR, keyed by their use, and the capture-slot layout of each
// rule's frame. A plan does not change once it is made: the lowering of a table takes its decisions on
// a copy (Refusing).
type Plan struct {
	// Funcs holds the functions in the order a backend lowers them: the bodies of the rules by ID,
	// then the skip and the lines of each Operators Rule, then the outlined nodes that the lowering
	// adds (Outlining).
	Funcs []*FuncSpec
	// Layouts holds the capture-slot layout of each rule's frame by rule ID, which the rows of the Node
	// and recognizer tables keep; nil in the typed table, whose rows have their own scopes.
	Layouts [][]string
	// Typed reports the plan of the typed table, whose flat functions have no fallback and are never
	// refused.
	Typed bool
	byKey map[Key]*FuncSpec
	// outlined finds the function of an outlined call by its number (Outlined).
	outlined map[int]Key
}

// Outlining returns the plan p with the functions of the outlined nodes added after its functions,
// as the lowering of the table makes them: each is a function of the form lowerdata.Outline, keyed
// with the use Outlined, and runs the outlined call whose number (prim.Outline.Call) calls holds at
// its index. p is unchanged, and is returned if outlined is empty.
func (p *Plan) Outlining(outlined []*FuncSpec, calls []int) *Plan {
	if len(outlined) == 0 {
		return p
	}
	out := &Plan{Funcs: slices.Clone(p.Funcs), Layouts: p.Layouts, Typed: p.Typed, byKey: maps.Clone(p.byKey), outlined: maps.Clone(p.outlined)}
	if out.byKey == nil {
		out.byKey = map[Key]*FuncSpec{}
	}
	if out.outlined == nil {
		out.outlined = map[int]Key{}
	}
	for i, s := range outlined {
		out.Funcs = append(out.Funcs, s)
		out.byKey[s.Key] = s
		out.outlined[calls[i]] = s.Key
	}
	return out
}

// Outlined returns the key of the function that runs the outlined call with the number call
// (prim.Outline.Call), or false if the lowering of the table made none.
func (p *Plan) Outlined(call int) (Key, bool) {
	if p == nil {
		return Key{}, false
	}
	k, ok := p.outlined[call]
	return k, ok
}

// Add adds the function s to the plan, as the planning makes it.
func (p *Plan) Add(s *FuncSpec) {
	if p.byKey == nil {
		p.byKey = map[Key]*FuncSpec{}
	}
	p.Funcs = append(p.Funcs, s)
	p.byKey[s.Key] = s
}

// Func returns the function planned for the use k, or false if the table has none: the rule is left
// out, or Flow IR cannot lower it, which a backend reports.
func (p *Plan) Func(k Key) (*FuncSpec, bool) {
	if p == nil {
		return nil, false
	}
	s, ok := p.byKey[k]
	return s, ok
}

// CheckLayout reports an error if the frame of rule has slots other than its planned layout. A
// backend calls it with the slots of each table row it emits.
func (p *Plan) CheckLayout(rule int, name string, slots []string) error {
	if !slices.Equal(p.Layouts[rule], slots) && (len(p.Layouts[rule]) != 0 || len(slots) != 0) {
		return fmt.Errorf("internal error: the frame of rule %s has slots %v, not the planned %v", name, slots, p.Layouts[rule])
	}
	return nil
}

// Refusing returns the plan p with the decisions of its lowering taken: each flat function of refused
// is replaced by its fallback, or left out if its rule has none. p is unchanged, and is returned if
// refused is empty.
func (p *Plan) Refusing(refused []*FuncSpec) *Plan {
	if len(refused) == 0 {
		return p
	}
	out := &Plan{Layouts: p.Layouts, Typed: p.Typed, byKey: make(map[Key]*FuncSpec, len(p.byKey)), outlined: p.outlined}
	for _, s := range p.Funcs {
		if slices.Contains(refused, s) {
			if s.Fallback == nil {
				continue
			}
			s = s.Fallback
		}
		out.Funcs = append(out.Funcs, s)
		out.byKey[s.Key] = s
	}
	return out
}

// Planned is one table's plan with the Core IR and the lowering options of the table: what the
// lowering of a table takes (flowlower.Lowering).
type Planned struct {
	Plan *Plan
	IR   *TableIR
	// Refuse decides, on the Flow IR of a flat function of the plan as the lowering writes it and
	// before any pass, that the table runs the function's fallback in its place (flowplan.Unit); nil
	// refuses none.
	Refuse func(k Key, f *code.Func) bool
}

// Table is a planned table whose functions are lowered to the stage S: its plan, with the decisions
// of the lowering taken, its Core IR and lowering options, and the module of its functions, which
// holds the function of each function of the plan at its place. It is the one value that carries the
// plan, the Core module and the module of a table to the printers and the bytecode VM, which read the
// function of a use with Index. Only NewTable makes one, and a pass that changes the module keeps the
// functions of the plan in it (With).
type Table[S code.Stage] struct {
	// Plan is nil for a module that no plan holds the functions of, such as a module that a test
	// lowers on its own.
	Plan   *Plan
	IR     *TableIR
	Module code.At[S]
	index  map[Key]int
}

// NewTable returns the table of the plan p and the module m of its functions, the function of each
// key being the function at its place in m (Index). A module that does not hold the functions of the
// plan in order is an internal error. A nil p binds no key.
func NewTable[S code.Stage](p *Plan, ir *TableIR, m code.At[S]) (*Table[S], error) {
	t := &Table[S]{Plan: p, IR: ir, Module: m}
	if p == nil {
		return t, nil
	}
	if err := t.holds(m); err != nil {
		return nil, err
	}
	t.index = make(map[Key]int, len(p.Funcs))
	for i, s := range p.Funcs {
		t.index[s.Key] = i
	}
	return t, nil
}

// holds reports an error unless m holds the functions of the plan of t in order.
func (t *Table[S]) holds(m code.At[S]) error {
	if t.Plan == nil {
		return nil
	}
	fs := m.Module().Funcs
	if len(fs) != len(t.Plan.Funcs) {
		return fmt.Errorf("internal error: the module of the table holds %d functions, not the %d of its plan", len(fs), len(t.Plan.Funcs))
	}
	for i, s := range t.Plan.Funcs {
		if fs[i].Name != s.Name {
			return fmt.Errorf("internal error: function %d of the module of the table is %s, not %s", i, fs[i].Name, s.Name)
		}
	}
	return nil
}

// With returns the table t with the module m in place of its module, which must hold the functions of
// the plan of t in order. t is unchanged.
func (t *Table[S]) With(m code.At[S]) (*Table[S], error) {
	if err := t.holds(m); err != nil {
		return nil, err
	}
	out := *t
	out.Module = m
	return &out, nil
}

// Index returns the place in the module of t of the function of the use k, or false if the table has
// none.
func (t *Table[S]) Index(k Key) (int, bool) {
	i, ok := t.index[k]
	return i, ok
}

// PlannedIR is the kind of a planned table, which the lowering of a table takes.
var PlannedIR = pipe.RegisterIR(&pipe.IR[Planned]{Name: "planned"})

// tableIR declares the kind of a table at the stage S, whose module is a unit of the kind k: verified
// as k verifies its module and checked to hold the functions of its plan, printed as k prints its
// module, and copied with a copy of its module.
func tableIR[S code.Stage](name string, k *pipe.IR[code.At[S]]) *pipe.IR[*Table[S]] {
	return pipe.RegisterIR(&pipe.IR[*Table[S]]{
		Name: name,
		Verify: func(t *Table[S]) error {
			if err := k.Verify(t.Module); err != nil {
				return err
			}
			return t.holds(t.Module)
		},
		Print: func(w io.Writer, t *Table[S]) { k.Print(w, t.Module) },
		Copy: func(t *Table[S]) *Table[S] {
			out := *t
			out.Module = k.Copy(t.Module)
			return &out
		},
	})
}

// FlowTable and ExecTable are the kinds of a table at the Flow and the Exec stage.
var (
	FlowTable = tableIR("table@flow", code.FlowStage)
	ExecTable = tableIR("table@exec", code.ExecStage)
)

// moduleLens declares the lens of the module of a table at the stage S, whose module is a unit of the
// kind m, for the passes of the stage, which keep the functions of the plan in it (Table.With).
func moduleLens[S code.Stage](name string, t *pipe.IR[*Table[S]], m *pipe.IR[code.At[S]]) *pipe.Lens[*Table[S], *Table[S], code.At[S], code.At[S]] {
	return pipe.RegisterLens(t, t, m, m, &pipe.Lens[*Table[S], *Table[S], code.At[S], code.At[S]]{
		Name: name,
		Get:  func(t *Table[S]) code.At[S] { return t.Module },
		Put:  (*Table[S]).With,
	})
}

// FlowModule and ExecModule are the lenses of the module of a table at the Flow and the Exec stage.
var (
	FlowModule = moduleLens("module@flow", FlowTable, code.FlowStage)
	ExecModule = moduleLens("module@exec", ExecTable, code.ExecStage)
)
