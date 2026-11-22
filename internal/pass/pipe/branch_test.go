package pipe

import (
	"strconv"
	"strings"
	"testing"
)

// The tests of the edits of one branch of a Fork (InBranch) and of the splits that read features
// (Split.Features, SplitFeature).

var (
	// targetTables splits a program into its tables, each named after the target of the enclosing
	// scope.
	targetTables = RegisterSplit(progIR, tableIR, modIR, modsIR, &Split[*prog, table, *mod, *mods]{Name: "t-target-tables",
		Labels:   []string{"a", "b", "c"},
		Features: []FeatureID{targetFeature.ID()},
		Parts: func(c SplitCtx, p *prog) ([]table, []string, error) {
			var ts []table
			for _, n := range p.tables {
				ts = append(ts, table{n + "-" + SplitFeature(c, targetFeature)})
			}
			return ts, p.tables, nil
		},
		Join: func(_ SplitCtx, p *prog, ms []*mod) (*mods, error) { return &mods{names: p.tables, list: ms}, nil }})

	// undeclaredTables reads a feature it does not declare.
	undeclaredTables = RegisterSplit(progIR, tableIR, modIR, modsIR, &Split[*prog, table, *mod, *mods]{Name: "t-undeclared-tables",
		Labels: []string{"a", "b", "c"},
		Parts: func(c SplitCtx, p *prog) ([]table, []string, error) {
			return []table{{SplitFeature(c, targetFeature)}}, []string{"a"}, nil
		},
		Join: func(_ SplitCtx, p *prog, ms []*mod) (*mods, error) { return &mods{names: []string{"a"}, list: ms}, nil }})

	wholeKey = RegisterKey(progIR, &Key[*prog]{Name: "t-whole-key", Of: func(p *prog) string { return strings.Join(p.tables, ",") }})
)

// TestInBranch checks that the edit InBranch applies its edit to the branches of the given name only,
// and that a name no branch has is an error.
func TestInBranch(t *testing.T) {
	k := NewFeature("t-in-branch", strconv.Itoa, strconv.Atoi)
	p := Fork(B("x", WithConst(k, 1, Seq(modIR))), B("y", WithConst(k, 2, Seq(modIR))))
	q, err := Apply(p, InBranch("y", SetConst(k, 3)))
	if err != nil {
		t.Fatal(err)
	}
	if text := Describe(q).String(); !strings.Contains(text, "t-in-branch=1 {") || !strings.Contains(text, "t-in-branch=3 {") || strings.Contains(text, "t-in-branch=2") {
		t.Errorf("Describe after InBranch:\n%s", text)
	}
	if text := Describe(p).String(); !strings.Contains(text, "t-in-branch=2 {") {
		t.Errorf("Apply changed its pipeline:\n%s", text)
	}
	if _, err := Apply(p, InBranch("z", SetConst(k, 3))); err == nil || !strings.Contains(err.Error(), "no branch z") {
		t.Errorf("InBranch of a branch the Fork lacks: %v", err)
	}
	if _, err := Apply(Seq(modIR), InBranch("x", SetConst(k, 3))); err == nil || !strings.Contains(err.Error(), "no branch x") {
		t.Errorf("InBranch without a Fork: %v", err)
	}
	if _, err := Apply(p, InBranch("x", SetConst(NewFeature("t-in-branch-other", strconv.Itoa, strconv.Atoi), 3))); err == nil {
		t.Errorf("InBranch of an edit its branch refuses: no error")
	}
}

// TestSplitReadsItsFeatures checks that a split reads the features it declares as the enclosing
// scopes provide them, that the check refuses a split whose feature no scope provides, that a
// Shared block keys its result by the features of the splits inside it, and that reading an
// undeclared feature panics.
func TestSplitReadsItsFeatures(t *testing.T) {
	body := Each(targetTables, Then(Of(build), Seq(modIR)))
	out, err := run(t, &Run{}, WithConst(targetFeature, "go", body), &prog{tables: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "a-go1: x") || !strings.Contains(got, "b-go2: y") {
		t.Errorf("output %q", got)
	}
	if _, err := run(t, &Run{}, body, &prog{tables: []string{"a"}}); err == nil || !strings.Contains(err.Error(), "split t-target-tables reads the feature t-target, which no enclosing scope provides") {
		t.Errorf("a split without its feature: %v", err)
	}

	// The two branches share the program unit, but not the result of a split that reads their
	// targets.
	shared := Shared(wholeKey, body)
	p := Fork(B("go", WithConst(targetFeature, "go", shared)), B("ts", WithConst(targetFeature, "ts", shared)))
	l, err := run(t, &Run{}, p, &prog{tables: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := l[0].String() + l[1].String(); !strings.Contains(got, "a-go1") || !strings.Contains(got, "a-ts1") {
		t.Errorf("the branches share the result of a split that reads their targets: %q", got)
	}

	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "split t-undeclared-tables reads the undeclared feature t-target") {
			t.Errorf("recover() = %v", r)
		}
	}()
	run(t, &Run{}, WithConst(targetFeature, "go", Each(undeclaredTables, Then(Of(build), Seq(modIR)))), &prog{tables: []string{"a"}})
}
