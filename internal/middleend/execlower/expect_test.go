package execlower

import (
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// TestNumberedExpectationsMergeTheParts checks that the expectation table numbered from the parts of
// a table, a printed part and an unreachable part that hold disjoint functions of its full plan, is the
// table numbered from the table lowered from the full plan: the functions of the plan in its order,
// then the outlined functions in the order in which the lowering adds them (flowlower.Lowering),
// each node once, for its first caller. Each function records an expectation of its own, so the table
// shows the order of the functions. The rules are A, B, C and D, of which the printed part holds A
// and C. A outlines the node n1; B outlines n2 and n5, and n2 outlines n3; C outlines n5, which B
// outlined first; D outlines n4. Numbering the parts one after the other gives another order.
func TestNumberedExpectationsMergeTheParts(t *testing.T) {
	exps := []string{"A", "B", "C", "D", "n1", "n2", "n3", "n4", "n5"}
	exp := func(d string) prim.ExpID { return prim.ExpID(slices.Index(exps, d)) }
	ir := &plandata.TableIR{M: &core.Module{Exps: exps}}
	nodes := map[string]core.Node{}
	for _, n := range []string{"n1", "n2", "n3", "n4", "n5"} {
		nodes[n] = &core.Lit{Text: n, Exp: core.ExpID(exp(n))}
	}
	matchLit := primPrefix + strings.TrimPrefix(flow.OpMatchLit, flow.Dialect+".")
	// fn returns the function named name, which records the expectation d.
	fn := func(name, d string) *code.Func {
		return &code.Func{Name: name, Body: &code.Block{Stmts: []code.Stmt{
			&code.Do{Call: &code.Call{Callee: matchLit, Imm: prim.Lit{Text: d, Exp: exp(d)}}},
		}}}
	}
	rule := func(id int, name string) *plandata.FuncSpec {
		return &plandata.FuncSpec{Key: plandata.Key{Use: plandata.RuleBody, Rule: id}, Func: lowerdata.Func{Form: lowerdata.Frame, Name: name}}
	}
	outlined := func(rule, line int, name, node string) *plandata.FuncSpec {
		return &plandata.FuncSpec{Key: plandata.Key{Use: plandata.Outlined, Rule: rule, Line: line},
			Func: lowerdata.Func{Form: lowerdata.Outline, Name: name, Body: nodes[node]}}
	}
	type entry struct {
		spec *plandata.FuncSpec
		d    string
	}
	table := func(entries ...entry) *plandata.Table[code.Exec] {
		t.Helper()
		p := &plandata.Plan{}
		m := &code.Module{Name: "t", Stage: Stage, Attrs: &prim.Tables{Exps: exps}}
		for _, e := range entries {
			p.Add(e.spec)
			m.Funcs = append(m.Funcs, fn(e.spec.Name, e.d))
		}
		x, err := plandata.NewTable(p, ir, code.At[code.Exec]{}.With(m))
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	a, b, c, d := rule(0, "A"), rule(1, "B"), rule(2, "C"), rule(3, "D")
	full := &plandata.Plan{}
	for _, s := range []*plandata.FuncSpec{a, b, c, d} {
		full.Add(s)
	}
	// The table lowered from the full plan: C's call of n5 finds it lowered for B.
	whole := table(entry{a, "A"}, entry{b, "B"}, entry{c, "C"}, entry{d, "D"},
		entry{outlined(0, 0, "A/outline0", "n1"), "n1"}, entry{outlined(1, 0, "B/outline0", "n2"), "n2"},
		entry{outlined(1, 1, "B/outline1", "n5"), "n5"}, entry{outlined(3, 0, "D/outline0", "n4"), "n4"},
		entry{outlined(1, 2, "B/outline0/outline0", "n3"), "n3"})
	printed := table(entry{a, "A"}, entry{c, "C"},
		entry{outlined(0, 0, "A/outline0", "n1"), "n1"}, entry{outlined(2, 0, "C/outline0", "n5"), "n5"})
	unreachable := table(entry{b, "B"}, entry{d, "D"},
		entry{outlined(1, 0, "B/outline0", "n2"), "n2"}, entry{outlined(1, 1, "B/outline1", "n5"), "n5"},
		entry{outlined(3, 0, "D/outline0", "n4"), "n4"}, entry{outlined(1, 2, "B/outline0/outline0", "n3"), "n3"})
	want, err := Expectations(whole)
	if err != nil {
		t.Fatal(err)
	}
	p := &plandata.Program{Numbered: []plandata.Numbered{{Full: full, Parts: []int{0, 1}}}}
	got, err := NumberedExpectations(p, []*plandata.Table[code.Exec]{printed, unreachable})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("the parts number\n%q\nbut the full table numbers\n%q", got, want)
	}
	if !slices.Equal(want[len(core.FixedDescs):], []string{"A", "B", "C", "D", "n1", "n2", "n5", "n4", "n3"}) {
		t.Errorf("the full table numbers %q", want)
	}
	concat, err := Expectations(printed, unreachable)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(concat, want) {
		t.Error("the parts numbered one after the other give the order of the full table, which leaves the merge untested")
	}
}
