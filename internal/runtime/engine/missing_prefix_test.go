package engine

import (
	"errors"
	"fmt"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// unitName names a position unit in the messages of the prefix tests.
func unitName(u sxirinput.PositionUnit) string {
	if u == sxirinput.Bytes {
		return "bytes"
	}
	return "code-points"
}

// TestPrefixInsertionMatchesParse checks that a prefix parse with a missing-token limit is the
// evaluation of a whole-input parse without the implicit end check, on every backend, in both units,
// at several limits, with and without recognition. Every start rule of the insertion corpus ends in an
// explicit $$, which a prefix parse also sees, so the prefix parse of an input returns what the
// whole-input parse returns, with the same compute units: the same tree and errors, the whole input
// consumed when the rule matches, and the zero result with the error when it fails or exhausts the
// limit. The corpus must reach the limit somewhere, so that the zero result of an exhausted limit is
// checked.
func TestPrefixInsertionMatchesParse(t *testing.T) {
	t.Parallel()
	exhausted := 0
	for _, c := range codegentest.InsertionCorpus() {
		prog := mustCompile(t, c.Src)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, u := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
				for _, limit := range insertionLimits {
					for _, recognize := range []bool{false, true} {
						for _, in := range c.Inputs {
							var parseUnits, prefixUnits uint64
							o := ParseOptions{Backend: b, Unit: u, Expectations: true, MissingTokens: limit, Recognize: recognize}
							o.ComputeReport = &parseUnits
							n, perr := prog.ParseWith("main", in, o)
							o.ComputeReport = &prefixUnits
							r, err := prog.ParsePrefixWith("main", in, o)
							where := fmt.Sprintf("%s %s: %s, limit %d, recognize %v, %q", b, unitName(u), c.Name, limit, recognize, in)
							if got, want := codegentest.ResultJSON(r.Node, err), codegentest.ResultJSON(n, perr); got != want {
								t.Errorf("%s:\n prefix %s\n parse  %s", where, got, want)
							}
							if prefixUnits != parseUnits {
								t.Errorf("%s: prefix units %d, parse units %d", where, prefixUnits, parseUnits)
							}
							var limitErr *runtime.MissingTokenLimitError
							if errors.As(perr, &limitErr) {
								exhausted++
							}
							matched := perr == nil || isSyntaxErrors(perr)
							want := PrefixResult{}
							if matched {
								want = PrefixResult{Node: r.Node, Consumed: utf8.RuneCountInString(in), ConsumedBytes: len(in)}
								if u == sxirinput.Bytes {
									want.Consumed = len(in)
								}
							}
							if r != want {
								t.Errorf("%s: prefix result %+v, want %+v", where, r, want)
							}
						}
					}
				}
			}
		}
	}
	if exhausted == 0 {
		t.Error("no parse of the corpus exhausted its missing-token limit")
	}
}

// TestPrefixInsertionAtTheEndOfThePrefix checks the prefix parses of a rule without $$ that ends in an
// insertable literal, on every backend: the rule inserts the literal where the input does not continue
// with it, at its end or before other text, and consumes no more than the text before the insertion,
// reporting the insertion as a recovered error; where the literal is present it consumes it and reports
// nothing. Without a limit the same rule matches only where the literal is present.
func TestPrefixInsertionAtTheEndOfThePrefix(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, "rule stmt = name \";\" #missing\nrule name = [a-z]+ #atomic")
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, c := range []struct {
			in       string
			limit    int
			consumed int
			inserted int  // recovered errors
			fails    bool // a syntax failure with the zero result
		}{
			{in: "ab", limit: 1, consumed: 2, inserted: 1},
			{in: "ab cd", limit: 1, consumed: 2, inserted: 1},
			{in: "ab;cd", limit: 1, consumed: 3},
			{in: "ab", fails: true},
			{in: "ab cd", fails: true},
			{in: "ab;cd", consumed: 3},
		} {
			where := fmt.Sprintf("%s %s: %q, limit %d", b, unitName(sxirinput.CodePoints), c.in, c.limit)
			r, err := prog.ParsePrefixWith("stmt", c.in, ParseOptions{Backend: b, MissingTokens: c.limit})
			var errs value.SyntaxErrors
			var one *value.SyntaxError
			switch {
			case c.fails:
				if r != (PrefixResult{}) || !errors.As(err, &one) {
					t.Errorf("%s: want a syntax failure, got %+v, %v", where, r, err)
				}
			case r.Consumed != c.consumed || r.ConsumedBytes != c.consumed || r.Node == nil:
				t.Errorf("%s: want %d consumed and a node, got %+v, %v", where, c.consumed, r, err)
			case c.inserted == 0 && err != nil:
				t.Errorf("%s: want no error, got %v", where, err)
			case c.inserted > 0 && (!errors.As(err, &errs) || len(errs) != c.inserted || errs[0].Pos != 2):
				t.Errorf("%s: want %d recovered errors at 2, got %v", where, c.inserted, err)
			}
		}
	}
}
