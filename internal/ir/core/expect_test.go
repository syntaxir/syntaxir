package core

import (
	"strconv"
	"testing"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestLiteralExpectedTextNegativeControl checks that LiteralDesc tells the grammar spelling of a
// literal from the Go quoting that it replaced, which writes \x00 and \U0001f4a9, spellings that a
// grammar rejects. The texts that a syntax error shows for literals are checked through the parsers
// in package engine.
func TestLiteralExpectedTextNegativeControl(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"\x00", "\x01", "\x7f", "\U0001f4a9\u0080"} {
		goText := strconv.Quote(v)
		if got := LiteralDesc(&ast.Literal{Value: v}); got == goText {
			t.Errorf("%q: the grammar spelling equals the Go quoting %s", v, goText)
		}
	}
}
