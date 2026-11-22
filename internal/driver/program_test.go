package driver

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
)

// ProgramOf wraps a run-time program checked elsewhere as it is, with its role and the options of the
// grammar's parser, and with no start rule and no recognizer view; a generating target on it is an
// error before any step. A program of a generation is the grammar's parser.
func TestProgramOf(t *testing.T) {
	g, err := checkSource(t, "@start\nrule A = \"a\"\n", common.CompileOptions{SourceInfo: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if g.Role() != Parser || !g.ParserOptions().SourceInfo {
		t.Errorf("a generation's program: role %s, parser options %+v", g.Role(), g.ParserOptions())
	}
	elab := g.checked.Elab.(*frontend.Result)
	parser := check.Options{SourceInfo: true, DisableMemo: true}
	p := ProgramOf(elab, Recognizer, parser)
	if p.checked.Elab.(*frontend.Result) != elab || p.View().Run != elab.Runtime() {
		t.Error("ProgramOf does not wrap the program it is given")
	}
	if p.Role() != Recognizer || p.ParserOptions() != parser || p.Start() != nil || !p.Options().SourceInfo {
		t.Errorf("role %s, parser options %+v, start %v, options %+v", p.Role(), p.ParserOptions(), p.Start(), p.Options())
	}
	if _, err := p.Recognizer(); err == nil || !strings.Contains(err.Error(), "the recognizer program of a run-time build has no recognizer view") {
		t.Errorf("the recognizer view of a run-time program: %v", err)
	}
	for _, b := range []Backend{result.Go, result.TypeScript} {
		if _, err := Compile(context.Background(), p, Options{}, Target{Backend: b}); err == nil || !strings.Contains(err.Error(), "the recognizer program of a run-time build generates no parser") {
			t.Errorf("a %s target on a run-time program: %v", b, err)
		}
	}
	if _, err := Compile(context.Background(), p, Options{}, Target{Backend: result.Go, Gen: common.GenOptions{Start: "A"}}); err == nil || !strings.Contains(err.Error(), "Start") {
		t.Errorf("a start rule on a run-time program: %v", err)
	}
	for r, want := range map[Role]string{Parser: "parser", Recognizer: "recognizer", Insertion: "insertion", 7: "Role(7)"} {
		if r.String() != want {
			t.Errorf("Role %d prints as %q, want %q", r, r.String(), want)
		}
	}
}
