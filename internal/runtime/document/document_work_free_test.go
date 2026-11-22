//go:build !sxir_budget_controls_reference

package document_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	. "syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// capturingDocument is the Document of the document package with the test hook that shows the parser.
type capturingDocument interface {
	ParseWithCapture(state.ParseOptions, func(*state.Parser, *value.Node, error) error) (*value.Node, error)
	ParseDefaults() state.ParseOptions
}

// reparseUnits parses d under o and returns the units it counted against the limit (the units it
// reserved less the Document units, which no limit counts) and the results it reused.
func reparseUnits(t *testing.T, d capturingDocument, o state.ParseOptions) (units uint64, reused int) {
	t.Helper()
	if _, err := d.ParseWithCapture(o, func(p *state.Parser, n *value.Node, err error) error {
		if p.Control != nil {
			units = p.Control.Used - p.Control.Doc
		}
		reused = p.Stats.Reused
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return units, reused
}

// TestDocumentWorkIsNoEvent checks that the work of a Document parse (moving the values that an edit
// shifted, applying the edits that lag, walking the chains) counts no unit under any Compute limit.
// Two edit sequences end with a first item of one digit or of two. The two reparses reuse the same
// results for the other items, but the second shifts every one of them, so a charge for a move or an
// edit makes the difference of their units larger than the difference of the parses from scratch of
// the same texts, which is the units of the extra digit alone. The long sequences lag the memo
// entries past the pending edit limit, so the results are evaluated again and the same equality
// must still hold. The reparses after one, three and ninety-nine edits count the same units, since
// the entries that they reuse apply more or fewer edits for the same events.
func TestDocumentWorkIsNoEvent(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("built without budget controls")
	}
	const items = 40
	tail := strings.Repeat("x1 ", items-1)
	// The items are a right-recursive list, not a repetition, so that the reparse reuses memo entries only:
	// a repetition resumes the records of runs, which only the engine keeps and counts differently
	// from a memo hit.
	prog := mustCompile(t, "rule main = list $$\nrule list = item list / item\nrule item = \"x\" [0-9]+ \" \"")
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		// The reparses after few edits end with the same text, so they differ only in the edits that the
		// entries of the other items apply when a lookup finds them: a charge for an applied edit makes
		// the units grow with the number of edits.
		unitsAfter := map[string][2]uint64{}
		for _, edits := range []int{1, 2, 3, 99, state.PendingEditLimit + 2, 2*state.PendingEditLimit + 3} {
			for _, explicit := range []bool{false, true} {
				opts := func(d capturingDocument) state.ParseOptions {
					o := d.ParseDefaults()
					if explicit {
						o.Limits = runtime.Limits{Compute: 1 << 40}
					}
					return o
				}
				// run edits the digits of the first item edits times, alternating them with another
				// string, so that they end as final, and returns what the reparse counted and reused and
				// what a parse of the same text from scratch counts.
				run := func(final, other string) (reparse uint64, reused int, fresh uint64) {
					d, err := prog.NewDocumentWith("main", "x1 "+tail, ParseOptions{Backend: b})
					if err != nil {
						t.Fatal(err)
					}
					reparseUnits(t, d, opts(d))
					cur := "1"
					for i := 0; i < edits; i++ {
						next := other
						if (edits-1-i)%2 == 0 {
							next = final
						}
						if err := d.Edit(1, 1+len(cur), next); err != nil {
							t.Fatal(err)
						}
						cur = next
					}
					reparse, reused = reparseUnits(t, d, opts(d))
					f, err := prog.NewDocumentWith("main", "x"+final+" "+tail, ParseOptions{Backend: b})
					if err != nil {
						t.Fatal(err)
					}
					fresh, _ = reparseUnits(t, f, opts(f))
					return
				}
				sameLen, reusedSame, freshSame := run("2", "1")
				moved, reusedMoved, freshMoved := run("22", "1")
				name := func() string {
					l := "default"
					if explicit {
						l = "explicit"
					}
					return fmt.Sprintf("%v, %d edits, %s limit", b, edits, l)
				}()
				if reusedSame != reusedMoved {
					t.Errorf("%s: the reparses reused %d and %d results", name, reusedSame, reusedMoved)
				}
				if edits%2 == 1 && edits <= state.PendingEditLimit {
					key := fmt.Sprint(explicit)
					if first, ok := unitsAfter[key]; !ok {
						unitsAfter[key] = [2]uint64{sameLen, moved}
					} else if first != [2]uint64{sameLen, moved} {
						t.Errorf("%s: the reparse counts %d and %d units, after one edit it counted %d and %d: the edits that an entry applies are counted",
							name, sameLen, moved, first[0], first[1])
					}
				}
				if moved-sameLen != freshMoved-freshSame {
					t.Errorf("%s: the shifting reparse counts %d more units than the one that shifts nothing, a parse from scratch %d more",
						name, int64(moved)-int64(sameLen), int64(freshMoved)-int64(freshSame))
				}
			}
		}
	}
}

// TestDocumentReparseFitsItsCountedUnits checks the decision of an explicit limit on a reparse that
// shifts every later item: it passes with exactly the units that the reparse counts and fails with
// one fewer, so the work of moving and of applying edits is no part of what the limit compares.
func TestDocumentReparseFitsItsCountedUnits(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("built without budget controls")
	}
	prog := mustCompile(t, "rule main = list $$\nrule list = item list / item\nrule item = \"x\" [0-9]+ \" \"")
	text := "x1 " + strings.Repeat("x1 ", 39)
	reparse := func(b Backend, compute uint64) (uint64, error) {
		d, err := prog.NewDocumentWith("main", text, ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.ParseWith(d.ParseDefaults()); err != nil {
			t.Fatal(err)
		}
		cur := "1"
		for _, next := range []string{"22", "1", "22"} {
			if err := d.Edit(1, 1+len(cur), next); err != nil {
				t.Fatal(err)
			}
			cur = next
		}
		o := d.ParseDefaults()
		o.Limits = runtime.Limits{Compute: compute}
		var units uint64
		_, err = d.ParseWithCapture(o, func(p *state.Parser, n *value.Node, err error) error {
			if p.Control != nil {
				units = p.Control.Used - p.Control.Doc
			}
			return err
		})
		return units, err
	}
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		units, err := reparse(b, 1<<40)
		if err != nil {
			t.Fatalf("%v: %v", b, err)
		}
		if _, err := reparse(b, units); err != nil {
			t.Errorf("%v: a limit of the %d counted units failed the reparse: %v", b, units, err)
		}
		if _, err := reparse(b, units-1); !errors.Is(err, runtime.ErrLimit) {
			t.Errorf("%v: a limit of %d units passed a reparse that counts %d: %v", b, units-1, units, err)
		}
	}
}
