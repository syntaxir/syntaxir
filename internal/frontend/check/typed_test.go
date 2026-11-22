package check

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
)

// typedObserver records, at every check of its context, whether the model under check holds its
// checked types yet.
type typedObserver struct {
	context.Context
	m       *Model
	checks  int
	visible int
}

func (o *typedObserver) Err() error {
	o.checks++
	if o.m.Typed != nil {
		o.visible++
	}
	return nil
}

// The checked types have one home, Model.Typed, which the type check sets as its last step. While
// the check runs, every reader, through the model or through any checker of it, sees no types, as
// the readers of the model saw none before the check recorded its types on the model; once it
// returns, every checker of the model, a fresh one included, reads the types it recorded.
func TestTypedIsSetOnceTheCheckEnds(t *testing.T) {
	src := "type N = node[string]\ntype S = { A N }\ntype M ~ N\n" +
		"rule main N = x:\"a\" => N{$x}\nrule list S = n:main => S{A: $n}\nrule other = main list\n"
	p := mustCompile(t, src)
	// The same grammar with the flags of a compiled grammar skips the type check.
	m, err := compileStatic(mustParse(src), p.Opts, p.Flags())
	if err != nil {
		t.Fatal(err)
	}
	if m.Typed != nil {
		t.Fatal("a model checked with the flags of a compiled grammar holds types")
	}
	obs := &typedObserver{Context: context.Background(), m: m}
	var errs diagnostic.ErrorList
	CheckTypesControlled(m.Checker(), &errs, &cancel.Control{Ctx: obs})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if obs.checks < 2 {
		t.Fatalf("the check polled %d times; the test observes nothing", obs.checks)
	}
	if obs.visible > 0 {
		t.Errorf("the types were visible at %d of %d polls during the check", obs.visible, obs.checks)
	}
	if m.Typed == nil {
		t.Fatal("the check recorded no types")
	}
	if m.Checker().Typed != m.Typed || NewChecker(m).Typed != m.Typed {
		t.Error("a checker of the model reads other types than the model holds")
	}
	if got, ok := m.RuleTypeWithPoll("list", nil); !ok || got != "S" {
		t.Errorf("rule list: type %q, %v; want S", got, ok)
	}
}
