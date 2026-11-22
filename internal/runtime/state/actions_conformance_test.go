package state

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// The engine's elements gathered and values made under conformance: every operation runs through its
// binding in actions_bind_exec_test.go, which the engine's printer prints from the operation as the backends
// print it into their own packages, so that the suite runs the code that the engine's compositions run.

// actionStateMachine binds the operations of the elements gathered and the values made of the engine.
type actionStateMachine struct {
	*engineMachine
	binder conform.Binder
	ctx    *EvalCtx
	ids    map[*value.Node]int64
}

type meteredActionState struct{ *actionStateMachine }

func (m meteredActionState) Used() uint64 { return m.p.Control.Used }

func newActionStateMachine(cfg ref.Config, controlled bool) conform.Impl {
	var em *engineMachine
	switch x := newEngineMachine(cfg, controlled, false).(type) {
	case *engineMachine:
		em = x
	case meteredEngine:
		em = x.engineMachine
	}
	m := &actionStateMachine{engineMachine: em, ctx: &EvalCtx{P: em.p}, ids: map[*value.Node]int64{}}
	m.binder = conform.Binder{Methods: confActionStateBindings(m.p), Conv: actionStateConv{}}
	if controlled {
		return meteredActionState{m}
	}
	return m
}

func (m *actionStateMachine) Act() any { return m.ctx }

func (m *actionStateMachine) node(id int64) *value.Node {
	n := new(value.Node)
	m.ids[n] = id
	return n
}

func (m *actionStateMachine) Elem(id int64) any { return m.node(id) }

func (m *actionStateMachine) Struct(id int64) any { return m.node(id) }

// itemsBox holds the elements of a list, which the harness compares by identity.
type itemsBox struct{ items []*value.Node }

func (m *actionStateMachine) Items(ids []int64) any {
	b := &itemsBox{}
	for _, id := range ids {
		b.items = append(b.items, m.node(id))
	}
	return b
}

func (m *actionStateMachine) list(ns []*value.Node) []int64 {
	ids := []int64{}
	for _, n := range ns {
		ids = append(ids, m.ids[n])
	}
	return ids
}

func (m *actionStateMachine) Gathered() []int64 { return m.list(m.p.KidStack) }

func (m *actionStateMachine) ItemIDs(items any) []int64 { return m.list(items.(*itemsBox).items) }

func (m *actionStateMachine) Made() []int64 { return m.list(m.p.Created) }

func (m *actionStateMachine) Call(o *code.Op, args []any) []any {
	if !slices.Contains(conform.GatheredOps, o.Name) && !slices.Contains(conform.MadeOps, o.Name) {
		return m.engineMachine.Call(o, args)
	}
	defer interrupt()
	return m.binder.Call(o, args)
}

// actionStateConv converts the elements of a list to the slice that the bindings take, and the slice that
// action.elemsAbove returns to the elements of a list.
type actionStateConv struct{}

func (actionStateConv) ToImpl(arg any, t reflect.Type) (reflect.Value, bool) {
	if b, ok := arg.(*itemsBox); ok {
		return reflect.ValueOf(b.items), true
	}
	return reflect.Value{}, false
}

func (actionStateConv) FromImpl(v reflect.Value) (any, bool) {
	if ns, ok := v.Interface().([]*value.Node); ok {
		return &itemsBox{items: ns}, true
	}
	return nil, false
}

// actionStateSuite is the suite of the engine's operations of the elements gathered or the values made, metered
// if controlled; a metered engine charges each drop and copy to the unit. The reference build has no budget
// controls, so there the suite checks the behavior alone.
func actionStateSuite(ops []string, controlled bool) conform.Suite {
	controlled = controlled && BudgetControlsEnabled
	name := "engine action expressions"
	if controlled {
		name += " (budget)"
	}
	s := conform.Suite{Name: name, Ops: ops, New: func(c ref.Config) conform.Impl { return newActionStateMachine(c, controlled) }}
	if controlled {
		s.Exact = []string{"act.pushAll", "action.dropKids", "action.elemsAbove", "action.keep", "action.dropCreated"}
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

func TestEngineActionStateConformance(t *testing.T) {
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, fam := range actionStateFamilies {
		for _, controlled := range []bool{false, true} {
			fails, ran := actionStateConformanceFailures(fam, actionStateSuite(fam.ops, controlled), seeds, 120)
			if ran < seeds {
				t.Fatalf("only %d cases ran", ran)
			}
			for _, err := range fails {
				t.Error(err)
			}
		}
	}
}

// brokenActionState is the engine with one operation of the elements gathered or the values made replaced.
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

// brokenActionStateCharge is the metered engine whose op charges nothing, or half of what it charges if half
// is set: action.elemsAbove charging for the copy and not for taking the elements off.
type brokenActionStateCharge struct {
	meteredActionState
	op   string
	half bool
}

func (b brokenActionStateCharge) Call(o *code.Op, args []any) []any {
	if o.Name != b.op {
		return b.meteredActionState.Call(o, args)
	}
	used := b.p.Control.Used
	defer func() {
		if b.half {
			b.p.Control.Used = used + (b.p.Control.Used-used)/2
		} else {
			b.p.Control.Used = used
		}
	}()
	return b.meteredActionState.Call(o, args)
}

// TestEngineActionStateConformanceDetectsBrokenEngine is the negative control: the engine with one operation
// broken, as a wrong line of its printed code would break it, or with a drop or copy that charges nothing,
// fails the suite.
func TestEngineActionStateConformanceDetectsBrokenEngine(t *testing.T) {
	for _, fam := range actionStateFamilies {
		for _, br := range fam.broken() {
			t.Run(br.Name, func(t *testing.T) {
				s := actionStateSuite(fam.ops, false)
				s.New = func(c ref.Config) conform.Impl {
					return brokenActionState{newActionStateMachine(c, false).(*actionStateMachine), br.Op, br.Edit}
				}
				if fails, _ := actionStateConformanceFailures(fam, s, 30, 120); len(fails) == 0 {
					t.Fatal("no case detected the broken engine")
				}
			})
		}
		for _, br := range []struct {
			op   string
			half bool
		}{{"act.pushAll", false}, {"action.dropKids", false}, {"action.elemsAbove", false}, {"action.elemsAbove", true}, {"action.dropCreated", false}} {
			if !slices.Contains(fam.ops, br.op) {
				continue
			}
			name := br.op + " charges nothing"
			if br.half {
				name = br.op + " charges only the copy"
			}
			t.Run(name, func(t *testing.T) {
				if !BudgetControlsEnabled {
					t.Skip("budget controls are unavailable in the reference build")
				}
				s := actionStateSuite(fam.ops, true)
				s.New = func(c ref.Config) conform.Impl {
					return brokenActionStateCharge{newActionStateMachine(c, true).(meteredActionState), br.op, br.half}
				}
				if fails, _ := actionStateConformanceFailures(fam, s, 30, 120); len(fails) == 0 {
					t.Fatal("no case detected the broken engine")
				}
			})
		}
	}
}
