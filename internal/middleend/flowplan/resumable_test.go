package flowplan_test

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// resumableTable lowers the table of src for a suspended stream, with the outlined nodes that outline
// chooses, and runs the pass Resumable on it with the feature of the suspended stream set to suspend.
func resumableTable(t *testing.T, src string, suspend bool, outline func(*core.Module) map[core.Node]bool) *plandata.Table[code.Flow] {
	t.Helper()
	ir, rules := tableIR(t, src, check.BuildValues)
	ir.Opts.Suspend = true
	if outline != nil {
		ir.Opts.Outlined = outline(ir.M)
	}
	p := flowplan.Plan(ir, planRules(rules), nil)
	lower := pipe.Then(pipe.Of(flowlower.Lowering), pipe.WithConst(flowplan.SuspendFeature, suspend, pipe.Of(flowplan.Resumable)))
	tbl, err := pipe.Execute(context.Background(), &pipe.Run{Stop: true}, lower, flowplan.Unit(p, ir, nil))
	if err != nil {
		t.Fatal(err)
	}
	return tbl
}

// resumableOf returns whether the function of the table of the key k is resumable, failing if the table has none.
func resumableOf(t *testing.T, tbl *plandata.Table[code.Flow], k plandata.Key) bool {
	t.Helper()
	f, ok := flowFunc(tbl, k)
	if !ok {
		t.Fatalf("the table has no function of %+v", k)
	}
	a := flow.AttrsOf(f)
	if a == nil {
		t.Fatalf("function %s has no attributes", f.Name)
	}
	return a.Resumable
}

// ruleKey returns the key of the body of the rule named name in the table's IR.
func ruleKey(t *testing.T, rules []*check.Rule, name string) plandata.Key {
	t.Helper()
	for _, r := range rules {
		if r.Name == name {
			return plandata.Key{Use: plandata.RuleBody, Rule: r.ID}
		}
	}
	t.Fatalf("no rule %s", name)
	return plandata.Key{}
}

// TestResumableMarksTheFunctionsThatReachADemand checks the resumable functions of a suspended table: a function
// that demands input is resumable, and so is a function that calls one, through its rule body. An empty literal
// emits a demand of no units, so its rule is resumable too: the flag is conservative there, and the demand is
// the lowering's, which the suspended checker accepts.
func TestResumableMarksTheFunctionsThatReachADemand(t *testing.T) {
	const src = "rule main = a b\nrule a = \"x\"\nrule b = c\nrule c = \"y\"\nrule empty = \"\"\nrule caller = empty\n"
	_, rules := tableIR(t, src, check.BuildValues)
	tbl := resumableTable(t, src, true, nil)
	for name, want := range map[string]bool{"a": true, "c": true, "b": true, "main": true, "empty": true, "caller": true} {
		if got := resumableOf(t, tbl, ruleKey(t, rules, name)); got != want {
			t.Errorf("rule %s: resumable %v, want %v", name, got, want)
		}
	}
	if off := resumableTable(t, src, false, nil); resumableOf(t, off, ruleKey(t, rules, "a")) {
		t.Error("a table that is not suspended has a resumable function")
	}
}

// TestResumableFollowsOutlinedCalls checks that a function that calls an outlined node is resumable when the
// node demands input: the call is an edge of the fixpoint through the outlined function of the plan.
func TestResumableFollowsOutlinedCalls(t *testing.T) {
	const src = "rule main = item*\nrule item = (\"a\" \"b\") / [c]\n"
	outlineItem := func(m *core.Module) map[core.Node]bool {
		var item *core.Rule
		for _, r := range m.Rules {
			if r != nil && r.Name == "item" {
				item = r
			}
		}
		under := map[core.Node]bool{}
		core.Walk(item.Body, func(n core.Node) { under[n] = n != item.Body && flowlower.Outlinable(n) })
		return under
	}
	_, rules := tableIR(t, src, check.BuildValues)
	tbl := resumableTable(t, src, true, outlineItem)
	var outlined, caller bool
	for _, s := range tbl.Plan.Funcs {
		if s.Key.Use != plandata.Outlined {
			continue
		}
		outlined = true
		if !resumableOf(t, tbl, s.Key) {
			t.Errorf("outlined node %s demands input but is not resumable", s.Name)
		}
	}
	if !outlined {
		t.Fatal("the test grammar outlines no node")
	}
	caller = resumableOf(t, tbl, ruleKey(t, rules, "item"))
	if !caller {
		t.Error("item calls an outlined node that demands input but is not resumable")
	}
	if !resumableOf(t, tbl, ruleKey(t, rules, "main")) {
		t.Error("main calls item, which is resumable, but is not resumable")
	}
}

// TestResumableFollowsOperatorsRuleCalls checks that a call of an Operators Rule, which has no body function, is
// resumable when a function of its skip or its lines demands input: the call is an edge of the fixpoint to each.
func TestResumableFollowsOperatorsRuleCalls(t *testing.T) {
	const src = "rule main = expr\nrule expr = #operators {\n    skip \" \"*\n    operand [0-9]+\n    level { infix left \"+\" }\n}\n"
	_, rules := tableIR(t, src, check.BuildValues)
	tbl := resumableTable(t, src, true, nil)
	var lines int
	for _, s := range tbl.Plan.Funcs {
		if s.Key.Use != plandata.OperatorsLine && s.Key.Use != plandata.OperatorsSkip {
			continue
		}
		lines++
		if !resumableOf(t, tbl, s.Key) {
			t.Errorf("line %+v demands input but is not resumable", s.Key)
		}
	}
	if lines != 3 {
		t.Fatalf("the Operators Rule has %d functions of its skip and lines, want 3", lines)
	}
	if !resumableOf(t, tbl, ruleKey(t, rules, "main")) {
		t.Error("main calls the Operators Rule expr, whose lines demand input, but is not resumable")
	}
}
