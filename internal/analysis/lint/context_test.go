package lint

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

type lintPollContext struct {
	context.Context
	cancel          context.CancelFunc
	calls, cancelAt int
}

func (c *lintPollContext) Err() error {
	c.calls++
	if c.cancelAt > 0 && c.calls == c.cancelAt {
		c.cancel()
	}
	return c.Context.Err()
}

func lintTestContext(t *testing.T, cancelAt int) *lintPollContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &lintPollContext{Context: ctx, cancel: cancel, cancelAt: cancelAt}
}

func lintContextGrammar(t *testing.T, source string) *ast.Grammar {
	t.Helper()
	g, err := parser.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Compile(starttest.MarkAll(g), entry.Options{}); err != nil {
		t.Fatal(err)
	}
	return g
}

func lintManyRules() string {
	var source strings.Builder
	source.WriteString("@start\nrule main = \"a\"\n")
	for i := range 512 {
		fmt.Fprintf(&source, "rule unused%d = \"b\"\n", i)
	}
	return source.String()
}

func TestRunContextCancellationDiscardsFindings(t *testing.T) {
	g := lintContextGrammar(t, lintManyRules())
	count := lintTestContext(t, 0)
	complete, err := testRunContext(count, g, "main", options{})
	if err != nil || len(complete) < 256 || count.calls < 4 {
		t.Fatalf("inadequate fixture: findings=%d calls=%d err=%v", len(complete), count.calls, err)
	}
	// The final prior polling point occurs after checks have accumulated findings.
	// Deterministic baseline counting avoids sleeps and fixed wall-clock limits.
	for _, cancelAt := range []int{1, 3, count.calls - 1} {
		ctx := lintTestContext(t, cancelAt)
		findings, err := testRunContext(ctx, g, "main", options{})
		if findings != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelAt=%d returned partial findings %v / %v", cancelAt, findings, err)
		}
	}
}

func TestLintContextPhaseCancellation(t *testing.T) {
	g := lintContextGrammar(t, lintManyRules())
	an := newAnalysis(g)
	// The proofs run on long expressions of a grammar: a sequence of 1024 items that always
	// succeed, a negative lookahead of a choice of 1024 alternatives that always fail, and a choice
	// of 1024 equal alternatives.
	long := func(item, sep string) string { return strings.TrimSuffix(strings.Repeat(item+sep, 1024), sep) }
	proofs := newAnalysis(lintContextGrammar(t, "rule succeed = "+long("_", " ")+
		"\nrule fail = !("+long("_|_", " / ")+")\nrule shadowed = "+long(`"a"`, " / ")))
	body := func(name string) ast.Expr { return proofs.rules[name].Expr }
	for _, phase := range []string{"analysis", "success-proof", "failure-proof", "cut-proof", "dependency-graph", "shadowed", "suppression"} {
		t.Run(phase, func(t *testing.T) {
			control := &lintControl{ctx: lintTestContext(t, 1)}
			defer func() {
				failure := recover()
				if err := cancellation(failure); !errors.Is(err, context.Canceled) {
					t.Fatalf("phase did not stop at a cancellation: %+v", failure)
				}
			}()
			switch phase {
			case "analysis":
				testNewAnalysisControlled(g, control)
			case "success-proof":
				proofs.withControl(control).succeeds(body("succeed"))
			case "failure-proof":
				proofs.withControl(control).succeeds(body("fail"))
			case "cut-proof":
				// A loose cut is found without following calls; the items are proven one by one.
				controlled := proofs.withControl(control)
				for _, item := range body("succeed").(*ast.Seq).Items {
					controlled.hasLooseCut(item)
				}
			case "dependency-graph":
				edges := map[string][]string{}
				for i := range 1024 {
					edges[fmt.Sprint(i)] = []string{fmt.Sprint(i + 1)}
				}
				findCycleControlled("0", edges, control)
			case "shadowed":
				l := &linter{control: control, an: proofs.withControl(control)}
				l.shadowedAlts(body("shadowed").(*ast.Choice).Alts, "alternative")
			case "suppression":
				findings := make([]Finding, 1024)
				for i := range findings {
					findings[i] = Finding{Check: CheckUnreachableRule, Pos: ast.Pos{Line: 1, Col: 1}, Message: fmt.Sprint(i)}
				}
				l := &linter{control: control, an: an, findings: findings}
				l.suppress(g)
			}
			t.Fatal("phase completed without cancellation")
		})
	}
}

func TestRunContextOrdinaryParity(t *testing.T) {
	for _, source := range []string{
		lintManyRules(),
		"// lint:ignore shadowed-alternative reason\nrule main = \"a\" / \"ab\"",
		`rule main = #operators { operand [0-9]; level sum { infix left "+" } }`,
		`rule main = "" / "a"`,
		`rule main = ("a" #cut "b") / ""`,
	} {
		g := lintContextGrammar(t, source)
		for _, opts := range []options{{}, {Disable: map[string]bool{CheckUnreachableRule: true}}} {
			want := runUnchecked(g, "main", opts)
			got, err := testRunContext(lintTestContext(t, 0), g, "main", opts)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("controlled lint changed findings: %v / %v, %v", got, want, err)
			}
		}
	}
}
