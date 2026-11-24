package common_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// TestDocumentSessionsAcrossTargets runs the shared Document sessions (codegentest.DocumentSessions)
// on the Documents of the generated Go and TypeScript parsers and of the engine, and compares their
// transcripts, which agree exactly in every column: a Document counts the same evaluations, reuses
// and compute units on every backend.
func TestDocumentSessionsAcrossTargets(t *testing.T) {
	t.Parallel()
	if !state.BudgetControlsEnabled {
		t.Skip("the reference build counts no compute units")
	}
	codegentest.NeedsBudgetControls(t)
	tooltest.RequireNode(t)
	if testing.Short() {
		t.Skip("builds generated Go programs")
	}
	for _, c := range codegentest.DocumentSessions {
		if c.Long && !codegentest.Long() {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			ts := sessionLines(t, "TypeScript", codegentest.RunDocumentSessionTS(t, c))
			goLines := sessionLines(t, "Go", codegentest.RunDocumentSessionGo(t, c))
			eng := sessionLines(t, "engine", documentSessionEngine(t, c))
			if len(goLines) != len(ts) || len(eng) != len(ts) {
				t.Fatalf("parses: TypeScript %d, Go %d, engine %d", len(ts), len(goLines), len(eng))
			}
			for i := range ts {
				for col, name := range sessionColumns {
					if goLines[i][col] != ts[i][col] {
						t.Errorf("parse %d: %s: Go %s, TypeScript %s", i, name, goLines[i][col], ts[i][col])
					}
					if eng[i][col] != ts[i][col] {
						t.Errorf("parse %d: %s: engine %s, TypeScript %s", i, name, eng[i][col], ts[i][col])
					}
				}
			}
			for _, x := range []struct {
				target string
				lines  [][]string
			}{{"TypeScript", ts}, {"Go", goLines}, {"engine", eng}} {
				t.Logf("%s: %s\n%s", x.target, codegentest.DocumentSessionLine, strings.Join(joinLines(x.lines), "\n"))
			}
		})
	}
}

// sessionColumns are the columns of a transcript line (codegentest.DocumentSessionLine).
var sessionColumns = strings.Fields(codegentest.DocumentSessionLine)

// sessionLines splits the transcript out of target into its lines and their columns.
func sessionLines(t *testing.T, target, out string) [][]string {
	t.Helper()
	var lines [][]string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(l)
		if len(f) != len(sessionColumns) {
			t.Fatalf("%s: the line %q is not %q", target, l, codegentest.DocumentSessionLine)
		}
		lines = append(lines, f)
	}
	return lines
}

func joinLines(lines [][]string) []string {
	var out []string
	for _, l := range lines {
		out = append(out, strings.Join(l, " "))
	}
	return out
}

// documentSessionEngine returns the transcript of the session c on an engine Document.
func documentSessionEngine(t *testing.T, c codegentest.DocumentSession) string {
	t.Helper()
	prog := compileBare(t, c.Grammar)
	unit := input.CodePoints
	if c.Bytes {
		unit = input.Bytes
	}
	parses := 0
	for _, s := range c.Steps {
		if s.Parse {
			parses++
		}
	}
	// replay makes the Document and runs the steps up to the parse k, which it parses with the
	// Compute limit compute; the parses before it have no limit.
	replay := func(k int, compute uint64) (runtime.ParseStats, int, error) {
		d, err := prog.NewDocumentWith("main", c.Text, engine.ParseOptions{Unit: unit})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, s := range c.Steps {
			if !s.Parse {
				for range s.Times {
					if err := d.Edit(s.Start, s.End, s.Text); err != nil {
						t.Fatal(err)
					}
				}
				continue
			}
			if n == k {
				node, err := d.ParseWith(engine.ParseOptions{Unit: unit, Limits: runtime.Limits{Compute: compute}})
				end := -1
				if node != nil {
					end = node.End()
				}
				return d.Stats(), end, err
			}
			n++
			d.ParseWith(engine.ParseOptions{Unit: unit, Limits: runtime.Limits{Compute: runtime.Unlimited}})
		}
		t.Fatalf("no parse %d", k)
		return runtime.ParseStats{}, 0, nil
	}
	// fits reports whether the parse k fits the Compute limit; a parse that fits must end as the
	// unlimited parse, whose error is want.
	fits := func(k int, limit uint64, want string) bool {
		_, _, err := replay(k, limit)
		var le *runtime.LimitError
		if errors.As(err, &le) && le.Kind == runtime.LimitCompute {
			return false
		}
		if fmt.Sprint(err) != want {
			t.Fatalf("parse %d under the limit %d: %v, want %s", k, limit, err, want)
		}
		return true
	}
	var b strings.Builder
	for k := range parses {
		s, end, err := replay(k, runtime.Unlimited)
		want := fmt.Sprint(err)
		hi := uint64(16)
		for !fits(k, hi, want) {
			hi *= 2
		}
		lo := uint64(0)
		for hi-lo > 1 {
			if mid := lo + (hi-lo)/2; fits(k, mid, want) {
				hi = mid
			} else {
				lo = mid
			}
		}
		fmt.Fprintln(&b, s.Evaluated, s.Reused, hi, end, err != nil)
	}
	return b.String()
}
