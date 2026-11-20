package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/internal/testing/tsconformtest"
)

// What terms make, of the conformance suite on the TypeScript library: the elements gathered on the
// parser's stack for lists and tuples, and the values made. The printer prints act.push, act.pushAll,
// act.made, term.kids, term.created and the two drops, and the suite runs that printed code. It prints neither
// term.elemsAbove nor term.keep: the TypeScript target of Exec IR (tsTarget.Op in
// internal/backend/codegen/typescript/print/exec_lib.go) has no implementation of them and panics on them,
// which print.OperationMethods reports as unprinted (TestTypeScriptConformancePrintedOps). The bindings run
// those two on the library's own stack methods (kids, and the stack itself).

type tsTermFamily struct {
	name   string
	ops    []string
	cases  func() []conform.Case
	random func(name string, rng *rand.Rand, n int) conform.Case
}

var tsTermFamilies = []tsTermFamily{
	{"gathered", conform.GatheredOps, conform.GatheredCases, conform.RandomGathered},
	{"made", conform.MadeOps, conform.MadeCases, conform.RandomMade},
}

func tsTermRun(r tsconformtest.Runner, fam tsTermFamily, controlled bool, seeds, n int) tsConformResult {
	s := conform.Suite{Name: fmt.Sprint("typescript ", fam.name, " (controlled=", controlled, ")"), Ops: append(append([]string{}, fam.ops...), budgetOps(controlled)...), Exact: tsTermExact(controlled)}
	return tsConformRunCases(r, tsconformtest.Term(controlled), s, fam.cases(), seeds, func(seed int) conform.Case {
		return fam.random(fmt.Sprint("random_", seed), rand.New(rand.NewPCG(uint64(seed), 13)), n)
	})
}

// tsTermExact are the operations whose charge the suite holds to the model's exactly: those that spend for
// each element.
func tsTermExact(controlled bool) []string {
	if !controlled {
		return nil
	}
	return []string{"act.pushAll", "term.dropKids", "term.elemsAbove", "term.dropCreated"}
}

func TestTypeScriptConformanceTerm(t *testing.T) {
	node := codegentest.RequireNode(t)
	lib := tsConformLib(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, fam := range tsTermFamilies {
		for _, controlled := range []bool{false, true} {
			t.Run(fmt.Sprint(fam.name, "/controlled=", controlled), func(t *testing.T) {
				runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
				res := tsTermRun(runner, fam, controlled, seeds, 120)
				if res.ran < seeds {
					t.Fatalf("only %d cases ran", res.ran)
				}
				s := append(append([]string{}, fam.ops...), budgetOps(controlled)...)
				tsReport(t, res, s, map[string]string{})
			})
		}
	}
}

// TestTypeScriptConformanceTermDetectsMutants is the negative control of TestTypeScriptConformanceTerm.
// The printed operations have mutants of their printed text; the library's own functions pushAll, kids and
// dropSpend have source mutants (kids is also what term.elemsAbove runs on).
func TestTypeScriptConformanceTermDetectsMutants(t *testing.T) {
	node := codegentest.RequireNode(t)
	libMutants := []tsMutant{
		{name: "pushAll pushes nothing", sig: "pushAll(items: readonly (Node | null)[])", old: "this.p.kidStack.push(it);", new: "void it;"},
		{name: "pushAll pushes the first element", sig: "pushAll(items: readonly (Node | null)[])", old: "this.p.kidStack.push(it);", new: "this.p.kidStack.push(items[0]!);"},
		{name: "pushAll charges nothing", sig: "pushAll(items: readonly (Node | null)[])", old: "this.spend();", new: "", charges: true, controlled: true},
		{name: "kids leaves the elements", sig: "kids(base: number): (Node | null)[]", old: "const ks = this.kidStack.slice(base);\n      this.kidStack.length = base;", new: "const ks = this.kidStack.slice(base);"},
		{name: "kids takes one element too many", sig: "kids(base: number): (Node | null)[]", old: "const ks = this.kidStack.slice(base);\n      this.kidStack.length = base;", new: "const ks = this.kidStack.slice(base);\n      this.kidStack.length = Math.max(base - 1, 0);"},
		{name: "kids charges one unit for each element", sig: "kids(base: number): (Node | null)[]", old: "c.charge(2 * n);", new: "c.charge(n);", charges: true, controlled: true},
		{name: "dropKids drops one fewer", sig: "dropSpend(values: unknown[], base: number)", old: "values.length = base;", new: "values.length = Math.min(base + 1, values.length);"},
		{name: "dropSpend charges nothing", sig: "dropSpend(values: unknown[], base: number)", old: "this.spend(values.length - base);", new: "", charges: true, controlled: true},
	}
	for _, m := range libMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, tsMutate(t, m))}
			detected := false
			for _, fam := range tsTermFamilies {
				res := tsTermRun(runner, fam, m.controlled, 150, 120)
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
		{name: "term.kids is one more", method: "op_term_kids", old: "this.kidStack.length", new: "this.kidStack.length + 1"},
		{name: "act.made records nothing", method: "op_act_made", old: "this.created.push(p1);", new: "void p1;"},
		{name: "term.created is one more", method: "op_term_created", old: "this.created.length", new: "this.created.length + 1"},
		{name: "term.dropCreated is ignored", method: "op_term_dropCreated", old: "this.dropSpend(this.created, p0);", new: "void p0;"},
		{name: "term.dropKids is ignored", method: "op_term_dropKids", old: "this.dropSpend(this.kidStack, p0);", new: "void p0;"},
		{name: "term.dropKids drops one fewer", method: "op_term_dropKids", old: "this.kidStack, p0", new: "this.kidStack, p0 + 1"},
		{name: "act.pushAll pushes through a wrong context", method: "op_act_pushAll", old: "p0.pushAll(p1);", new: "p0.pushAll(p1.slice(1));"},
	}
	for _, m := range printedMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			runner := tsconformtest.Runner{Session: tsconformtest.StartSources(t, node, tsPrintedSources(t, m))}
			for _, fam := range tsTermFamilies {
				res := tsTermRun(runner, fam, false, 150, 120)
				if len(res.fails) > 0 {
					t.Logf("detected: %v", strings.SplitN(res.fails[0].Error(), "\ncase:", 2)[0])
					return
				}
			}
			t.Fatal("the suite did not notice")
		})
	}
}
