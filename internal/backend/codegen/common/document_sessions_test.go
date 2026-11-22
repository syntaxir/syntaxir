package common_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// generatedDocumentUnits reports whether the generated Go and TypeScript runtimes charge the same
// units for a Document parse, so that the units are compared. They do not yet: the two libraries
// charge their reads and decoding of the input differently (in the case "moves", 100 and 124 units
// for the first parse, before any edit).
const generatedDocumentUnits = false

// engineDocumentDifferences are the columns of the shared Document sessions in which the engine
// differs from the generated runtimes, each with the mechanism that makes it differ. The key is the
// case and the column ("units" or "reused"). Every Document event charges the same units on every
// backend, and Reused is the same on every backend (#310); an entry goes when the engine or the
// generated runtimes stop differing there, and a difference that no entry lists, or an entry whose
// difference is gone, fails the test.
var engineDocumentDifferences = map[string]string{
	"moves/units":       documentChargesDiffer + "; " + documentMovesDiffer,
	"moves bytes/units": documentChargesDiffer + "; " + documentMovesDiffer,
	"same length/units": documentChargesDiffer + "; " + documentMovesDiffer,
	"unit before/units": documentChargesDiffer + "; " + documentMovesDiffer,
	"pinned/units":      documentChargesDiffer + "; " + documentMovesDiffer,
	"recovered/units":   documentChargesDiffer + "; " + documentMovesDiffer,
	"lag 128/units":     documentChargesDiffer,
	"lag 129/units":     documentChargesDiffer,
	"lag moved/units":   documentChargesDiffer + "; " + documentMovesDiffer,
	"log full/units":    documentChargesDiffer,
	"runs/units":        documentChargesDiffer + "; " + documentMovesDiffer,
}

// The mechanisms of engineDocumentDifferences.
const (
	// documentChargesDiffer makes every parse differ, the first included: the generated runtimes
	// charge the memo protocol of a call (keeping and merging its expected set, one unit for each
	// expectation) and the reads and the decoding of the input by their own operations, where the
	// engine charges a Document parse as a parse without a Document.
	documentChargesDiffer = "[memo protocol and operation charges] the generated runtimes charge keeping and merging the expected set of a memoized call and their own operations; the engine charges as a parse without a Document"
	// documentMovesDiffer makes the parses after an edit differ further: a generated runtime copies
	// a reused value that moved and charges its check and its copy, where the engine moves the value
	// in place by generation.
	documentMovesDiffer = "[value moves] the generated runtimes copy a moved value and charge its check and copy; the engine moves it in place by generation"
)

// TestDocumentSessionsAcrossTargets runs the shared Document sessions (codegentest.DocumentSessions)
// on the Documents of the generated Go and TypeScript parsers and of the engine, and compares their
// transcripts: the generated runtimes exactly (their units once generatedDocumentUnits holds), the
// engine exactly in evaluations, ends and failures, and in units and reuses up to the known
// differences (engineDocumentDifferences).
func TestDocumentSessionsAcrossTargets(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	tooltest.RequireNode(t)
	if testing.Short() {
		t.Skip("builds generated Go programs")
	}
	seen, ran := map[string]bool{}, map[string]bool{}
	for _, c := range codegentest.DocumentSessions {
		if c.Long && !codegentest.Long() {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			ran[c.Name] = true
			ts := sessionLines(t, "TypeScript", codegentest.RunDocumentSessionTS(t, c))
			goLines := sessionLines(t, "Go", codegentest.RunDocumentSessionGo(t, c))
			eng := sessionLines(t, "engine", documentSessionEngine(t, c))
			if len(goLines) != len(ts) || len(eng) != len(ts) {
				t.Fatalf("parses: TypeScript %d, Go %d, engine %d", len(ts), len(goLines), len(eng))
			}
			for i := range ts {
				for col, name := range sessionColumns {
					if name == "units" && !generatedDocumentUnits {
						continue
					}
					if goLines[i][col] != ts[i][col] {
						t.Errorf("parse %d: %s: Go %s, TypeScript %s", i, name, goLines[i][col], ts[i][col])
					}
				}
				for col, name := range sessionColumns {
					if eng[i][col] == ts[i][col] {
						continue
					}
					key := c.Name + "/" + name
					if _, ok := engineDocumentDifferences[key]; ok && (name == "units" || name == "reused") {
						seen[key] = true
						continue
					}
					t.Errorf("parse %d: %s: engine %s, TypeScript %s", i, name, eng[i][col], ts[i][col])
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
	for key, why := range engineDocumentDifferences {
		name, _, _ := strings.Cut(key, "/")
		if !seen[key] && !t.Failed() && ran[name] {
			t.Errorf("the known engine difference %s (%s) no longer differs", key, why)
		}
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
