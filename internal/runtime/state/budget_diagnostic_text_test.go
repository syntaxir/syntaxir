//go:build !sxir_budget_controls_reference

package state

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestBudgetDiagnosticRecoveryTextMatchesNative(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	cases := []*value.SyntaxError{
		{Line: 1, Col: 1},
		{Line: 0, Col: 0, Messages: []string{""}, Expected: []string{"ignored"}},
		{Line: -1, Col: -2, Messages: []string{"", "two", "", "é\xff"}, Expected: []string{"ignored"}},
		{Line: maxInt, Col: -maxInt - 1, Expected: []string{"", "x", "", "é", "\xff"}},
		{Line: 99, Col: 101, Messages: []string{strings.Repeat("λ\xff", 180)}},
		{Line: 2, Col: 3, Expected: make([]string, 320)},
		{Line: 4, Col: 5, Expected: []string{"end of input", `"a"`, `"b"`, `"c"`, `"d"`, `"e"`, `"f"`, `"g"`, "[0-9]", "[x-z]"}},
		{Line: 4, Col: 5, Expected: []string{"end of input", `"a"`, `"b"`, `"c"`, `"d"`, `"e"`, `"f"`, `"g"`, `"h"`, "[0-9]", "[x-z]"}},
	}
	for i, e := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// The text of an error is no event: it counts nothing, so it is made under a limit of one
			// unit, as the native text.
			want := e.Error()
			p := diagnosticParser(runtime.Limits{Compute: 1})
			var got string
			if err := catchFatal(t, func() { got = p.RecoveryErrorText(e) }); err != nil || got != want || p.Control.Used != 0 {
				t.Fatalf("controlled text=%q, units=%d, err=%v; native=%q", got, p.Control.Used, err, want)
			}
		})
	}
}

func TestBudgetDiagnosticRecoveryTextCancellationAndOwnership(t *testing.T) {
	for _, messages := range [][]string{make([]string, 320), {strings.Repeat("invalid\xff", 120)}} {
		e := &value.SyntaxError{Line: 7, Col: 11, Messages: messages, Expected: []string{"ignored"}}
		before := append([]string(nil), e.Messages...)
		p := diagnosticParser(runtime.Limits{Compute: 1 << 50})
		retained := p.RecoveryErrorText(e)
		// Making the text counts nothing and makes no check, so a canceled context does not stop it.
		p.Control = NewBudgetControl(newPollingContext(1), runtime.Limits{})
		var got string
		if err := catchFatal(t, func() { got = p.RecoveryErrorText(e) }); err != nil || got != e.Error() {
			t.Fatalf("format under a canceled context: err=%v text=%q", err, got)
		}
		p.Control = NewBudgetControl(nil, runtime.Limits{Compute: 1 << 50})
		if retry := p.RecoveryErrorText(&value.SyntaxError{Line: 1, Col: 2, Messages: []string{"other"}}); retry != "1:2: other" {
			t.Fatal("formatter retry differs")
		}
		if retained != e.Error() || !reflect.DeepEqual(e.Messages, before) || e.Expected[0] != "ignored" {
			t.Fatal("formatter retry mutated retained diagnostic or string")
		}
	}
}

func TestBudgetDiagnosticSortParityAndMoves(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 7, 8, 9, 127, 128, 129, 511} {
		values := make([]string, n)
		for i := range values {
			values[i] = []string{"", "é", "\xff", "z", "\xc3\xff", "alpha", `"a"`, "[a]", `"\xff`, "[é"}[(n-i)%10] + strings.Repeat("p", (n-i)%3)
		}
		want := append([]string(nil), values...)
		sortExpectedEntries(want)
		p := diagnosticParser(runtime.Limits{Compute: 1 << 50})
		p.sortExpected(values)
		if !slices.Equal(values, want) {
			t.Fatalf("sort length%d differs from native", n)
		}
	}
	// Sorting the expectations of an error is no event: it counts nothing and makes no check, so it
	// completes under a limit of one unit and under a canceled context.
	p := diagnosticParser(runtime.Limits{Compute: 1})
	if err := catchFatal(t, func() { p.sortExpected(make([]string, 320)) }); err != nil || p.Control.Used != 0 {
		t.Fatalf("sort moves: err=%v units=%d", err, p.Control.Used)
	}
	p.Control = NewBudgetControl(newPollingContext(1), runtime.Limits{})
	if err := catchFatal(t, func() { p.sortExpected(make([]string, 320)) }); err != nil {
		t.Fatalf("sort under a canceled context: %v", err)
	}
}
