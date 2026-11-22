//go:build !sxir_budget_controls_reference

package document_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/document"
	. "syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
)

// countedSweep parses a new Document of text, and, if edit is set, applies it after that parse and
// parses again. The last parse runs under the default compute limit once for each l from 1 up, with
// the limit lowered at its first trace event so that the units that a parse from scratch counts may
// reach only l. It returns the Used of each limit error, which it requires to report no Requested
// units (spec/runtime/parsing.md#checks-and-interruptions), as "Used/Limit", and the least l at which
// the parse succeeds.
func countedSweep(t *testing.T, prog *Program, text string, edit func(*document.Document) error, b Backend) (errs []string, total uint64) {
	t.Helper()
	for l := uint64(1); l < 5000; l++ {
		d, err := prog.NewDocumentWith("main", text, ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(err)
		}
		if edit != nil {
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			if err := edit(d); err != nil {
				t.Fatal(err)
			}
		}
		set := false
		o := ParseOptions{Backend: b}
		o.Trace = func(e TraceEvent) {
			if c := state.TraceParser(e).Control; !set && c != nil {
				set = true
				c.Limits.Compute = max(l, c.Used-c.Doc) + c.Doc
			}
		}
		if _, err := d.ParseWith(o); err == nil {
			return errs, l
		} else if le := (*runtime.LimitError)(nil); !errors.As(err, &le) || le.Kind != runtime.LimitCompute {
			t.Fatalf("limit %d: %v", l, err)
		} else if le.Requested != 0 {
			t.Fatalf("limit %d: the error requests %d units, want 0", l, le.Requested)
		} else {
			errs = append(errs, fmt.Sprintf("%d/%d", le.Used, le.Limit))
		}
	}
	t.Fatal("the parse does not succeed within 5000 units")
	return nil, 0
}

// TestBudgetDocumentFailedRunDropsItsElements pins that a run of a Document parse that fails drops
// the elements it gathered and those it resumed from the record of the previous parse for free, as a
// repetition of a parse from scratch drops them on every backend (spec/runtime/parsing.md, Cancellation
// and Compute Limits), and that taking the elements of the list of the second alternative off is free
// as well: values the parse builds are not an item of the cost (spec/runtime/parsing.md#cost). The first
// alternative of each grammar fails, below its minimum or at a cut, after gathering elements, and the
// second makes the list, so every count is of a single unit and every limit error reports one unit
// past its limit. For the runs that fail below their minimum, the closure backend's resumed runs and
// the bytecode machine's runs count the same units in the same order, so every limit fails with the
// same Used on every backend.
func TestBudgetDocumentFailedRunDropsItsElements(t *testing.T) {
	trim := func(d *document.Document) error { return d.Edit(4, 5, "") } // "abcde" to "abcd"
	for _, c := range []struct {
		name, src, text string
		edit            func(*document.Document) error
		allBackends     bool // whether every backend fails each limit alike
	}{
		{"below minimum", "@start\nrule main = [a-z]#repeat(5,) / [a-z]*", "abc", nil, true},
		{"below minimum, rule elements", "@start\nrule main = x#repeat(5,) / x*\nrule x = [a-z]", "abc", nil, true},
		{"below minimum, resumed", "@start\nrule main = [a-z]#repeat(5,) / [a-z]*", "abcde", trim, true},
		{"below minimum, rule elements resumed", "@start\nrule main = x#repeat(5,) / x*\nrule x = [a-z]", "abcde", trim, true},
		// A cut in the element of a repetition is charged differently by the closure and bytecode
		// backends in every parse, from scratch too, so only the reservations are compared here.
		{"at a cut", "@start\nrule main = ([a-z] #cut [,])+ / [a-z,]*", "a,b,c", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			prog := mustCompile(t, c.src)
			var first []string
			for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
				errs, _ := countedSweep(t, prog, c.text, c.edit, b)
				var multi []string
				for _, e := range errs {
					var used, limit uint64
					if _, err := fmt.Sscanf(e, "%d/%d", &used, &limit); err != nil {
						t.Fatal(err)
					}
					if used != limit+1 && !slices.Contains(multi, e) {
						multi = append(multi, e)
					}
				}
				if len(multi) != 0 {
					t.Errorf("%s: counts of more than one unit past the limit %v, want none", b, multi)
				}
				if first == nil {
					first = errs
				} else if c.allBackends && !slices.Equal(errs, first) {
					t.Errorf("%s fails the limits with %v, the closure backend with %v", b, errs, first)
				}
			}
		})
	}
}
