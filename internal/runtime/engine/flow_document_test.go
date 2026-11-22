//go:build !sxir_budget_controls_reference

package engine

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/document"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime"
)

// TestFlowBodyDocumentRuns checks that a Document parse on the bytecode VM resumes the repetition runs
// of the previous parse from the Flow bodies' run operations (RESUMESTART and the rest) as the closure
// backend resumes them: after each edit of a sequence, the value, the error, the number of resumed
// elements and the units that the parse charges are the closure backend's, but for the units of the
// guard reads of a choice (knownGuardReadDivergence).
func TestFlowBodyDocumentRuns(t *testing.T) {
	codegentest.NeedsBudgetControls(t)
	var lines strings.Builder
	for i := range 30 {
		fmt.Fprintf(&lines, "x = %d;\nyz = [1,22,3,];\n\n", i)
	}
	for _, c := range []struct {
		name, grammar, text string
		pieces              []string
	}{
		{"captures", `rule main = stmt* $$
rule stmt = k:key " = " v:val ";\n" / "{\n" body:stmt* "}\n" / "\n"
rule key = [a-z]+ #atomic
rule val = [0-9]+ #atomic / "[" items:([0-9]+ #atomic ",")* "]"`, lines.String(), []string{"a", "1", "q = 5;\n", "\n", "{\nk = 1;\n}\n", "[", "2,", ""}},
		{"inline", `rule main = ([a-z]+ #atomic " = " ([0-9]+ #atomic / "[" ([0-9]+ #atomic ",")* "]") ";\n" / "\n")* $$`, lines.String(), []string{"a", "1", "q = 5;\n", "\n", "2,", ""}},
		{"positional", `type P = { At int }
rule main = stmt* $$
rule stmt = assign / "\n"
rule assign = k:key " = " v:val ";\n" => P{At: $k.Start}
rule key = [a-z]+ #atomic
rule val = [0-9]+ #atomic / "[" items:([0-9]+ #atomic ",")* "]"`, lines.String(), []string{"a", "1", "q = 5;\n", "\n", ""}},
		{"scan site", `rule main = (ws "a" / ws "b" / ws "c")* $$
rule ws = [ ]*`, strings.Repeat(strings.Repeat(" ", 40)+"a", 20), []string{" ", "b", "  ", ""}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			prog := mustCompile(t, c.grammar)
			m := prog.Module()
			starts := 0
			for _, f := range m.Flows {
				for _, in := range f.Code {
					if in.Op == bytecode.FlowResumeStart {
						starts++
					}
				}
			}
			if starts == 0 {
				t.Fatalf("no Flow body starts a resumed run:\n%s", m.Disassemble())
			}
			for _, b := range []Backend{Bytecode, BytecodeIterative} {
				// The closure backend and the Flow bodies.
				var used [2]uint64
				var docs [2]*document.Document
				for i, run := range []Backend{Closure, b} {
					docs[i] = chargedDocument(t, prog, run, c.text, &used[i])
				}
				rng := rand.New(rand.NewSource(5))
				resumed := 0
				for round := range 40 {
					text := []rune(docs[0].Text())
					start := rng.Intn(len(text))
					end := min(start+rng.Intn(3), len(text))
					ins := c.pieces[rng.Intn(len(c.pieces))]
					var got [2]string
					var stats [2]runtime.ParseStats
					for i, d := range docs {
						if err := d.Edit(start, end, ins); err != nil {
							t.Fatal(err)
						}
						v, err := d.Parse()
						got[i], stats[i] = dump(t, v, err), d.Stats()
					}
					units := used[1] == used[0] || knownGuardReadDivergence(m, used[0], used[1])
					if got[1] != got[0] || stats[1].ResumedElements != stats[0].ResumedElements || !units {
						t.Errorf("%s, round %d, edit [%d, %d) %q:\n closure %s resumed %d units %d\n Flow    %s resumed %d units %d",
							b, round, start, end, ins, got[0], stats[0].ResumedElements, used[0], got[1], stats[1].ResumedElements, used[1])
					}
					resumed += stats[1].ResumedElements
				}
				if resumed == 0 || used[1] == 0 {
					t.Errorf("%s: the parses resumed %d elements, and the last charged %d units", b, resumed, used[1])
				}
			}
		})
	}
}

// chargedDocument returns a Document of text of prog on backend b, parsed once, whose parses leave in
// *used the units that they charge.
func chargedDocument(t *testing.T, prog *Program, b Backend, text string, used *uint64) *document.Document {
	t.Helper()
	o := ParseOptions{Backend: b, Limits: runtime.Limits{Compute: 1 << 40}, Trace: func(ev TraceEvent) {
		if ev.Kind == TraceExit && ev.Rule == "main" {
			*used = state.TraceParser(ev).Control.Used
		}
	}}
	d, err := prog.NewDocumentWith("main", text, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Parse(); err != nil {
		t.Fatal(err)
	}
	return d
}
