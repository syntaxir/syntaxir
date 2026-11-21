package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/internal/testing/tsconformtest"
)

// What action expressions make, of the conformance suite on the TypeScript library: the elements gathered on the
// parser's stack for lists and tuples, and the values made. The printer prints act.push, act.pushAll,
// act.made, action.kids, action.created and the two drops, and the suite runs that printed code. It prints neither
// action.elemsAbove nor action.keep: the TypeScript target of Exec IR (tsTarget.Op in
// internal/backend/codegen/typescript/print/exec_lib.go) has no implementation of them and panics on them,
// which print.OperationMethods reports as unprinted (TestTypeScriptConformancePrintedOps). The bindings run
// those two on the library's own stack methods (kids, and the stack itself).

type tsActionFamily struct {
	name   string
	ops    []string
	cases  func() []conform.Case
	random func(name string, rng *rand.Rand, n int) conform.Case
}

var tsActionFamilies = []tsActionFamily{
	{"gathered", conform.GatheredOps, conform.GatheredCases, conform.RandomGathered},
	{"made", conform.MadeOps, conform.MadeCases, conform.RandomMade},
}

func tsActionRun(r tsconformtest.Runner, fam tsActionFamily, controlled bool, seeds, n int) tsConformResult {
	s := conform.Suite{Name: fmt.Sprint("typescript ", fam.name, " (controlled=", controlled, ")"), Ops: append(append([]string{}, fam.ops...), budgetOps(controlled)...), Exact: tsActionExact(controlled)}
	return tsConformRunCases(r, tsconformtest.Action(controlled), s, fam.cases(), seeds, func(seed int) conform.Case {
		return fam.random(fmt.Sprint("random_", seed), rand.New(rand.NewPCG(uint64(seed), 13)), n)
	})
}

// tsActionExact are the operations whose charge the suite holds to the model's exactly: those that spend for
// each element.
func tsActionExact(controlled bool) []string {
	if !controlled {
		return nil
	}
	return []string{"act.pushAll", "action.dropKids", "action.elemsAbove", "action.dropCreated"}
}

func TestTypeScriptConformanceAction(t *testing.T) {
	node := tooltest.RequireNode(t)
	lib := tsConformLib(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, fam := range tsActionFamilies {
		for _, controlled := range []bool{false, true} {
			t.Run(fmt.Sprint(fam.name, "/controlled=", controlled), func(t *testing.T) {
				runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
				res := tsActionRun(runner, fam, controlled, seeds, 120)
				if res.ran < seeds {
					t.Fatalf("only %d cases ran", res.ran)
				}
				s := append(append([]string{}, fam.ops...), budgetOps(controlled)...)
				tsReport(t, res, s, map[string]string{})
			})
		}
	}
}

// TestTypeScriptConformanceActionDetectsMutants is the negative control of TestTypeScriptConformanceAction.
// The printed operations have mutants of their printed text; the library's own functions pushAll, kids and
// dropSpend have source mutants (kids is also what action.elemsAbove runs on).
func TestTypeScriptConformanceActionDetectsMutants(t *testing.T) {
	node := tooltest.RequireNode(t)
	libMutants := []tsMutant{
		{name: "pushAll pushes nothing", sig: "pushAll(items: readonly (Node | null)[])", old: "this.p.kidStack.push(it);", new: "void it;"},
		{name: "pushAll pushes the first element", sig: "pushAll(items: readonly (Node | null)[])", old: "this.p.kidStack.push(it);", new: "this.p.kidStack.push(items[0]!);"},
		{name: "pushAll charges nothing", sig: "pushAll(items: readonly (Node | null)[])", old: "this.spend();", new: "", charges: true, controlled: true},
		{name: "kids leaves the elements", sig: "kids(base: number): (Node | null)[]", old: "const ks = this.kidStack.slice(base);\n    this.kidStack.length = base;", new: "const ks = this.kidStack.slice(base);"},
		{name: "kids takes one element too many", sig: "kids(base: number): (Node | null)[]", old: "const ks = this.kidStack.slice(base);\n    this.kidStack.length = base;", new: "const ks = this.kidStack.slice(base);\n    this.kidStack.length = Math.max(base - 1, 0);"},
		{name: "elemsAbove charges one unit for each element", sig: "elemsAbove(base: number)", old: "this.control?.charge(2 * n);", new: "this.control?.charge(n);", charges: true, controlled: true},
		{name: "dropKids drops one fewer", sig: "dropSpend(values: unknown[], base: number)", old: "values.length = base;", new: "values.length = Math.min(base + 1, values.length);"},
		{name: "dropSpend charges nothing", sig: "dropSpend(values: unknown[], base: number)", old: "this.spend(values.length - base);", new: "", charges: true, controlled: true},
	}
	for _, m := range libMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, tsMutate(t, m))}
			detected := false
			for _, fam := range tsActionFamilies {
				res := tsActionRun(runner, fam, m.controlled, 150, 120)
				if len(res.fails) > 0 {
					detected = true
					t.Logf("detected: %v", strings.SplitN(res.fails[0].Error(), "\ncase:", 2)[0])
					break
				}
			}
			if !detected {
				t.Fatal("the suite did not notice")
			}
		})
	}
	printedMutants := []tsPrintedMutant{
		{name: "act.push pushes nothing", method: "op_act_push", old: "this.kidStack.push(p1);", new: "void p1;"},
		{name: "act.push pushes the null element", method: "op_act_push", old: "push(p1)", new: "push(null)"},
		{name: "action.kids is one more", method: "op_action_kids", old: "this.kidStack.length", new: "this.kidStack.length + 1"},
		{name: "act.made records nothing", method: "op_act_made", old: "this.created.push(p1);", new: "void p1;"},
		{name: "action.created is one more", method: "op_action_created", old: "this.created.length", new: "this.created.length + 1"},
		{name: "action.dropCreated is ignored", method: "op_action_dropCreated", old: "this.dropSpend(this.created, p0);", new: "void p0;"},
		{name: "action.dropKids is ignored", method: "op_action_dropKids", old: "this.dropSpend(this.kidStack, p0);", new: "void p0;"},
		{name: "action.dropKids drops one fewer", method: "op_action_dropKids", old: "this.kidStack, p0", new: "this.kidStack, p0 + 1"},
		{name: "act.pushAll pushes through a wrong context", method: "op_act_pushAll", old: "p0.pushAll(p1);", new: "p0.pushAll(p1.slice(1));"},
	}
	for _, m := range printedMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			runner := tsconformtest.Runner{Session: tsconformtest.StartSources(t, node, tsPrintedSources(t, m))}
			for _, fam := range tsActionFamilies {
				res := tsActionRun(runner, fam, false, 150, 120)
				if len(res.fails) > 0 {
					t.Logf("detected: %v", strings.SplitN(res.fails[0].Error(), "\ncase:", 2)[0])
					return
				}
			}
			t.Fatal("the suite did not notice")
		})
	}
}
