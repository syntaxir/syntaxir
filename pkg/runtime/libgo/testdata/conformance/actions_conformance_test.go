package libgo

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// The elements gathered and the values made of the node and the typed library under conformance: every
// operation runs through its binding in actions_bind_exec_test.go or tactions_bind_exec_test.go, which the
// test that copies the library prints with the library's printer, so that the suite runs the code that
// the library's compositions run.

// actionStateMachine binds the operations of the elements gathered and the values made of the node or the
// typed library, and the rest through the machine of the parse state.
type actionStateMachine struct {
	conform.Impl
	p      *Parser
	tp     *TParser
	binder conform.Binder
	used   func() uint64
	ids    map[any]int64
}

type meteredActionState struct{ *actionStateMachine }

func (m meteredActionState) Used() uint64 { return m.used() }

func newActionStateMachine(cfg ref.Config, typed, controlled bool) conform.Impl {
	m := &actionStateMachine{ids: map[any]int64{}}
	if typed {
		tm := newTypedMachine(cfg, controlled)
		var x *typedMachine
		switch y := tm.(type) {
		case *typedMachine:
			x = y
		case meteredTyped:
			x = y.typedMachine
		}
		m.Impl, m.p, m.tp = tm, x.p.Parser, x.p
		m.binder = conform.Binder{Methods: confTActionStateBindings(x.p), Conv: actionStateConv{}}
	} else {
		rm := newRTMachine(cfg, controlled, false, false)
		var x *rtMachine
		switch y := rm.(type) {
		case *rtMachine:
			x = y
		case meteredRT:
			x = y.rtMachine
		}
		m.Impl, m.p = rm, x.p
		m.binder = conform.Binder{Methods: confActionStateBindings(x.p), Conv: actionStateConv{}}
	}
	if controlled {
		m.used = func() uint64 { return m.p.UnitsNow() }
		return meteredActionState{m}
	}
	return m
}

func (m *actionStateMachine) Act() any {
	if m.tp != nil {
		return &TActCtx{P: m.tp}
	}
	return &ActCtx{P: m.p}
}

// value returns a new element or struct of the library with the identifier id: a node, or a value of the
// typed library.
func (m *actionStateMachine) value(id int64) any {
	var v any = new(sxirvalue.Node)
	if m.tp != nil {
		v = new(TMatch)
	}
	m.ids[v] = id
	return v
}

func (m *actionStateMachine) Elem(id int64) any { return m.value(id) }

func (m *actionStateMachine) Struct(id int64) any { return m.value(id) }

// itemsBox holds the elements of a list, which the harness compares by identity.
type itemsBox struct {
	nodes []*sxirvalue.Node
	vals  []any
}

func (m *actionStateMachine) Items(ids []int64) any {
	b := &itemsBox{nodes: []*sxirvalue.Node{}, vals: []any{}}
	for _, id := range ids {
		v := m.value(id)
		if m.tp != nil {
			b.vals = append(b.vals, v)
		} else {
			b.nodes = append(b.nodes, v.(*sxirvalue.Node))
		}
	}
	return b
}

func listIDs[T comparable](ids map[any]int64, vs []T) []int64 {
	out := []int64{}
	for _, v := range vs {
		out = append(out, ids[v])
	}
	return out
}

func (m *actionStateMachine) Gathered() []int64 {
	if m.tp != nil {
		return listIDs(m.ids, m.tp.KidStack)
	}
	return listIDs(m.ids, m.p.KidStack)
}

func (m *actionStateMachine) ItemIDs(items any) []int64 {
	if b := items.(*itemsBox); m.tp != nil {
		return listIDs(m.ids, b.vals)
	} else {
		return listIDs(m.ids, b.nodes)
	}
}

func (m *actionStateMachine) Made() []int64 {
	if m.tp != nil {
		return listIDs(m.ids, m.tp.Created)
	}
	return listIDs(m.ids, m.p.created)
}

func (m *actionStateMachine) Datum(id int64) any { return m.Impl.(conform.Environment).Datum(id) }

func (m *actionStateMachine) Rule(index int64) any { return m.Impl.(conform.Environment).Rule(index) }

func (m *actionStateMachine) Call(o *code.Op, args []any) []any {
	if !slices.Contains(conform.GatheredOps, o.Name) && !slices.Contains(conform.MadeOps, o.Name) {
		return m.Impl.Call(o, args)
	}
	defer interrupted()
	return m.binder.Call(o, args)
}

// actionStateConv converts the elements of a list to the slice that the bindings take, and the slice that
// action.elemsAbove returns to the elements of a list.
type actionStateConv struct{}

func (actionStateConv) ToImpl(arg any, t reflect.Type) (reflect.Value, bool) {
	b, ok := arg.(*itemsBox)
	if !ok {
		return reflect.Value{}, false
	}
	if t == reflect.TypeOf(b.vals) {
		return reflect.ValueOf(b.vals), true
	}
	return reflect.ValueOf(b.nodes), true
}

func (actionStateConv) FromImpl(v reflect.Value) (any, bool) {
	switch x := v.Interface().(type) {
	case []*sxirvalue.Node:
		return &itemsBox{nodes: x}, true
	case []any:
		return &itemsBox{vals: x}, true
	}
	return nil, false
}

// actionStateSuite is the suite of the operations of the elements gathered or the values made of the node or
// the typed library, metered if controlled; a metered library counts no unit for a drop or a copy.
func actionStateSuite(ops []string, typed, controlled bool) conform.Suite {
	name := "runtime action expressions"
	if typed {
		name = "typed library action expressions"
	}
	if controlled {
		name += " (budget)"
	}
	s := conform.Suite{Name: name, Ops: ops, New: func(c ref.Config) conform.Impl { return newActionStateMachine(c, typed, controlled) }}
	if controlled {
		// Gathering and dropping the values of an action are no events: the action counts the elements
		// it builds (units.items, units.since).
		s.Free = []string{"act.pushAll", "action.dropKids", "action.elemsAbove", "action.keep", "action.dropCreated"}
	}
	return s
}

// actionStateFamily is the cases of the elements gathered or of the values made.
type actionStateFamily struct {
	ops    []string
	cases  func() []conform.Case
	random func(name string, rng *rand.Rand, n int) conform.Case
	broken func() []conform.ActionStateBroken
}

var actionStateFamilies = []actionStateFamily{
	{conform.GatheredOps, conform.GatheredCases, conform.RandomGathered, conform.GatheredBrokenOps},
	{conform.MadeOps, conform.MadeCases, conform.RandomMade, conform.MadeBrokenOps},
}

// actionStateConformanceFailures runs the handwritten cases of fam and seeds randomized cases of n calls on s,
// and returns the failures and the number of cases that ran.
func actionStateConformanceFailures(fam actionStateFamily, s conform.Suite, seeds, n int) (fails []error, ran int) {
	run := func(c conform.Case) {
		skipped, err := conform.Run(s, c)
		if !skipped {
			ran++
		}
		if err != nil {
			fails = append(fails, err)
		}
	}
	for _, c := range fam.cases() {
		run(c)
	}
	for seed := range seeds {
		run(fam.random(fmt.Sprint("random_", seed), rand.New(rand.NewPCG(uint64(seed), 71)), n))
	}
	return fails, ran
}

func TestExecConformanceActionState(t *testing.T) {
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, fam := range actionStateFamilies {
		for _, typed := range []bool{false, true} {
			for _, controlled := range []bool{false, true} {
				fails, ran := actionStateConformanceFailures(fam, actionStateSuite(fam.ops, typed, controlled), seeds, 120)
				if ran < seeds {
					t.Fatalf("only %d cases ran", ran)
				}
				for _, err := range fails {
					t.Error(err)
				}
			}
		}
	}
}

// brokenActionState is the library with one operation of the elements gathered or the values made replaced.
type brokenActionState struct {
	*actionStateMachine
	op   string
	edit func(call func(op string, args ...any) []any, args []any) []any
}

func (b brokenActionState) Call(o *code.Op, args []any) []any {
	if o.Name != b.op {
		return b.actionStateMachine.Call(o, args)
	}
	mod := conform.ActionStateModule()
	return b.edit(func(op string, a ...any) []any { return b.actionStateMachine.Call(mod.Op(op), a) }, args)
}

// brokenActionStateCharge is the metered library whose op, which is no event, counts a unit, or two if
// twice is set: a drop or copy charged as the budget charged it before compute units were logical.
type brokenActionStateCharge struct {
	meteredActionState
	op    string
	twice bool
}

func (b brokenActionStateCharge) Call(o *code.Op, args []any) []any {
	if o.Name != b.op {
		return b.meteredActionState.Call(o, args)
	}
	defer func() {
		b.p.unitsSized(1)
		if b.twice {
			b.p.unitsSized(1)
		}
	}()
	return b.meteredActionState.Call(o, args)
}

// TestExecConformanceActionStateDetectsBrokenLib is the negative control: the node and the typed library with
// one operation broken, as a wrong line of its printed code would break it, or with a drop or copy that
// counts units, which no event makes, fail the suite.
func TestExecConformanceActionStateDetectsBrokenLib(t *testing.T) {
	for _, fam := range actionStateFamilies {
		for _, typed := range []bool{false, true} {
			for _, br := range fam.broken() {
				t.Run(fmt.Sprint(br.Name, " typed=", typed), func(t *testing.T) {
					s := actionStateSuite(fam.ops, typed, false)
					s.New = func(c ref.Config) conform.Impl {
						return brokenActionState{newActionStateMachine(c, typed, false).(*actionStateMachine), br.Op, br.Edit}
					}
					if fails, _ := actionStateConformanceFailures(fam, s, 30, 120); len(fails) == 0 {
						t.Fatal("no case detected the broken library")
					}
				})
			}
			for _, br := range []struct {
				op    string
				twice bool
			}{{"act.pushAll", false}, {"action.dropKids", false}, {"action.elemsAbove", false}, {"action.elemsAbove", true}, {"action.dropCreated", false}} {
				if !slices.Contains(fam.ops, br.op) {
					continue
				}
				what := " counts a unit"
				if br.twice {
					what = " counts two units"
				}
				t.Run(fmt.Sprint(br.op, what, " typed=", typed), func(t *testing.T) {
					s := actionStateSuite(fam.ops, typed, true)
					s.New = func(c ref.Config) conform.Impl {
						return brokenActionStateCharge{newActionStateMachine(c, typed, true).(meteredActionState), br.op, br.twice}
					}
					if fails, _ := actionStateConformanceFailures(fam, s, 30, 120); len(fails) == 0 {
						t.Fatal("no case detected the broken library")
					}
				})
			}
		}
	}
}
