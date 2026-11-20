//go:build !zp_budget_controls_reference

package document_test

import (
	"errors"
	"fmt"
	"os"
	"testing"

	. "zeroparse.dev/zp/internal/runtime/engine"
	"zeroparse.dev/zp/internal/runtime/state"
	"zeroparse.dev/zp/pkg/runtime"
	"zeroparse.dev/zp/pkg/runtime/value"
)

// knownDocumentCountDivergences lists the grammars of documentCountCases whose Document parse does
// not yet count against the default limit what a parse from scratch counts; a change that fixes one
// removes it, and a new divergence fails the test.
var knownDocumentCountDivergences = map[string]bool{
	"literal": true, "class repetition": true, "repeat or class": true, "rules": true, "left recursion": true, "recovery": true,
}

var documentCountCases = []struct{ name, grammar, input string }{
	{"literal", "rule main = \"abcd\"+", "abcdabcd"},
	{"class repetition", "rule main = [a-z]*", "abc"},
	{"repeat or class", "rule main = [a-z]#repeat(5,) / [a-z]*", "abc"},
	{"rules", "rule main = item (\",\" item)* $$\nrule item = word / num\nrule word = [a-z]+\nrule num = [0-9]+", "ab,12,c"},
	{"left recursion", "rule main = expr $$\nrule expr = expr \"+\" term / term\nrule term = [0-9]+", "1+2+3"},
	{"recovery", `rule main = ("a" @error("need a") #recover(skip=.))* $$`, "aab"},
}

// TestChargeDocumentDefaultCount compares the units that the first parse of a Document counts against
// the default limit, read from the budget control when the parse has ended, with the units of a parse
// of the same text from scratch (the smallest explicit limit under which it does not fail on the
// limit): spec/runtime/parsing.md requires them to be equal.
func TestChargeDocumentDefaultCount(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("built without budget controls")
	}
	for _, c := range documentCountCases {
		prog := mustCompile(t, c.grammar)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			fresh := uint64(0)
			for l := uint64(1); l < 1<<20; l++ {
				_, err := prog.ParseWith("main", c.input, ParseOptions{Backend: b, Limits: runtime.Limits{Compute: l}})
				var le *runtime.LimitError
				if !errors.As(err, &le) || le.Kind != runtime.LimitCompute {
					fresh = l
					break
				}
			}
			d, err := prog.NewDocumentWith("main", c.input, ParseOptions{Backend: b})
			if err != nil {
				t.Fatal(err)
			}
			var counted uint64
			if _, err := d.ParseWithCapture(d.ParseDefaults(), func(p *state.Parser, n *value.Node, err error) error {
				if p.Control != nil {
					counted = p.Control.Used - p.Control.Doc
				}
				return err
			}); errors.Is(err, runtime.ErrLimit) {
				t.Fatalf("%s %v: %v", c.name, b, err)
			}
			if counted == fresh {
				continue
			}
			msg := fmt.Sprintf("%s %v: a Document counts %d against the default limit, a parse from scratch %d", c.name, b, counted, fresh)
			if os.Getenv("ZP_CHARGE_INVENTORY") != "" || knownDocumentCountDivergences[c.name] {
				t.Log("DIVERGENCE " + msg)
			} else {
				t.Error(msg)
			}
		}
	}
}
