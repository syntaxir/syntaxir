package flowplan_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/pipetest"
)

// A call enters the wrapper of a plain rule, and in the typed table that of any rule but a
// left-recursion leader, unless the rule is an Operators Rule or the table enters every slot.
func TestCallsWrapper(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		table plandata.CoreTable
		rule  plandata.CallRule
		want  bool
	}{
		{plandata.CoreTable{}, plandata.CallRule{}, false},
		{plandata.CoreTable{}, plandata.CallRule{Plain: true}, true},
		{plandata.CoreTable{}, plandata.CallRule{Plain: true, Operators: true}, false},
		{plandata.CoreTable{Slots: true}, plandata.CallRule{Plain: true}, false},
		{plandata.CoreTable{Typed: true}, plandata.CallRule{}, true},
		{plandata.CoreTable{Typed: true}, plandata.CallRule{Leader: true}, false},
		{plandata.CoreTable{Typed: true}, plandata.CallRule{Plain: true, Leader: true}, true},
		{plandata.CoreTable{Typed: true}, plandata.CallRule{Operators: true}, false},
	} {
		if got := flowplan.CallsWrapper(c.table, c.rule); got != c.want {
			t.Errorf("CallsWrapper(%+v, %+v) = %v, want %v", c.table, c.rule, got, c.want)
		}
	}
}

// The plan of a program reaches what its entry points call, through the routes of the calls: a rule
// that only a call of a plain rule enters is entered through its wrapper, and a rule that nothing
// calls is not reached. The step checks its input.
func TestPlanProgramReachesThroughTheRoutes(t *testing.T) {
	t.Parallel()
	prog := compiletest.Program(t, "rule main = a b\nrule a = \"a\"\nrule b = c\nrule c = \"c\"\nrule unused = \"u\"\n", check.Options{})
	m, err := elaborate.CoreProjecting(prog, false, false)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]int{}
	tab := plandata.CoreTable{Name: "rules", M: m, Rules: make([]plandata.CallRule, len(m.Rules))}
	for _, r := range m.Rules {
		byName[r.Name] = r.ID
	}
	tab.Rules[byName["a"]].Plain = true
	main := plandata.Entry{Table: "rules", ID: byName["main"]}
	c := &plandata.Cores{Tables: []plandata.CoreTable{tab}, Roots: []plandata.Entry{main}}
	p, err := planProgram(c)
	if err != nil {
		t.Fatal(err)
	}
	want := map[plandata.Entry]bool{
		main: true,
		{Table: "rules", ID: byName["a"], Wrapper: true}: true,
		{Table: "rules", ID: byName["b"]}:                true,
		{Table: "rules", ID: byName["c"]}:                true,
	}
	if len(p.Reach) != len(want) {
		t.Errorf("the plan reaches %v, want %v", p.Reach, want)
	}
	for e := range want {
		if !p.Reach[e] {
			t.Errorf("the plan does not reach %+v", e)
		}
	}
	if !p.Wrapper("rules", byName["a"]) || p.Wrapper("rules", byName["b"]) {
		t.Errorf("routes %v, want only a through its wrapper", p.Tables)
	}
	bad := &plandata.Cores{Tables: []plandata.CoreTable{{Name: "rules", M: m}}, Roots: []plandata.Entry{main}}
	if _, err := planProgram(bad); err == nil || !strings.Contains(err.Error(), "call facts") {
		t.Errorf("a table without call facts: %v, want an error that says so", err)
	}
}

// The dump of the plan shows its entry points, and each rule's route and reach. The driver dumps it
// only when -print-after names it, not under all (TestDumpsGroupByTable).
func TestPlanDump(t *testing.T) {
	t.Parallel()
	prog := compiletest.Program(t, "rule main = a\nrule a = \"a\"\nrule unused = \"u\"\n", check.Options{})
	m, err := elaborate.CoreProjecting(prog, false, false)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]int{}
	for _, r := range m.Rules {
		byName[r.Name] = r.ID
	}
	tab := plandata.CoreTable{Name: "rules", M: m, Rules: make([]plandata.CallRule, len(m.Rules))}
	tab.Rules[byName["a"]].Plain = true
	c := &plandata.Cores{Tables: []plandata.CoreTable{tab}, Roots: []plandata.Entry{{Table: "rules", ID: byName["main"]}}}
	var named bytes.Buffer
	dump := &pipetest.Watch{Print: true, Fn: func(e pipe.Event, r *pipe.Result) {
		if e.Step.Name == flowplan.PlanName && r.Err == nil {
			fmt.Fprintf(&named, "-- after %s --\n", e.Step.Instance)
			r.Print(&named)
		}
	}}
	if _, err := planProgram(c, dump); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"-- after plan --",
		"roots rules.main",
		"table rules",
		fmt.Sprintf("rule %d main call=slot reach=slot", byName["main"]),
		fmt.Sprintf("rule %d a call=wrapper reach=wrapper", byName["a"]),
		fmt.Sprintf("rule %d unused call=slot reach=none", byName["unused"]),
	} {
		if !strings.Contains(named.String(), line+"\n") {
			t.Errorf("the dump lacks %q:\n%s", line, named.String())
		}
	}
}

// planProgram runs the step that plans the program c (flowplan.Planning), computing its reach, with
// the observers obs.
func planProgram(c *plandata.Cores, obs ...pipe.Observer) (*plandata.Program, error) {
	return pipe.Execute(context.Background(), &pipe.Run{Observers: obs}, pipe.WithConst(plandata.ReachFeature, nil, pipe.Of(flowplan.Planning)), c)
}
