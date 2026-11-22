package flowplan_test

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// The pass outline sets the outlined nodes of each Node and recognizer table, outlinable ones only,
// once for the units that share a table, and leaves the tables it is given and the other tables as
// they are; the dump of its result names the outlined nodes by rule.
func TestOutliningChoosesTheNodesOfEachTable(t *testing.T) {
	src, _ := codegentest.OutlineKeywords()
	ir, _ := tableIR(t, src, check.BuildValues)
	ir.Values = true
	typed := *ir
	typed.Opts.Node = false
	c := &plandata.Cores{Units: []plandata.Unit{
		{Label: "rules", Table: "rules", IR: ir},
		{Label: "again", Table: "rules", IR: ir},
		{Label: "typed", Table: "typed", IR: &typed},
	}}
	got, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(flowplan.Outlining), c)
	if err != nil {
		t.Fatal(err)
	}
	if ir.Opts.Outlined != nil || c.Units[0].IR != ir {
		t.Errorf("outline changed the tables it was given")
	}
	out := got.Units[0].IR
	if out == ir || len(out.Opts.Outlined) == 0 {
		t.Fatalf("outline outlined no node of the Node table")
	}
	for n := range out.Opts.Outlined {
		if !flowlower.Outlinable(n) {
			t.Errorf("outline outlined a node that cannot run as a function of its own: %T", n)
		}
	}
	if got.Units[1].IR != out {
		t.Errorf("the units of one table do not share its outlined table")
	}
	if got.Units[2].IR != &typed {
		t.Errorf("outline changed a table that is not a Node or recognizer table")
	}
	var b strings.Builder
	plandata.CoresIR.Print(&b, got)
	dump := b.String()
	for _, want := range []string{"unit rules table=rules\n  rule 1 kw\n    outline ", "unit again table=rules\n", "unit typed table=typed\n"} {
		if !strings.Contains(dump, want) {
			t.Errorf("the dump has no %q:\n%s", want, dump)
		}
	}
}
