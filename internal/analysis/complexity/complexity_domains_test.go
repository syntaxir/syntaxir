package complexity

import (
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestTextLenRawLiteral requires the length bound of a literal that is not valid UTF-8, which a
// grammar built as an AST can hold, to be that of the text the literal matches: each byte of an
// invalid sequence matches as U+FFFD (nodes.LiteralValue), three bytes in UTF-8, not as the byte
// itself. A case-insensitive literal is bounded by its characters.
func TestTextLenRawLiteral(t *testing.T) {
	t.Parallel()
	// The raw literal values of internal/testing/codegentest, which imports this package.
	for _, raw := range []string{"\xc3", "\x80", "\xff", "\xc0\xaf", "\xed\xa0\x80", "é\xc3x", "abcdefghijk\xffxyz"} {
		for _, fold := range []bool{false, true} {
			src := "rule main = \"a\"\n"
			if fold {
				src = "rule main = \"a\"i\n"
			}
			g, err := parser.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			g.Rules()[0].Expr.(*ast.Literal).Value = raw
			k, _, err := prepareBaseClassifier(starttest.MarkAll(g), "", nil)
			if err != nil {
				t.Fatalf("%q: %v", raw, err)
			}
			text := string([]rune(raw))
			want := len(text)
			if fold {
				want = utf8.UTFMax * utf8.RuneCountInString(text)
			}
			got, ok := textLen(k.ruleBody("main"), k, map[string]bool{})
			if !ok || got != want {
				t.Errorf("%q (fold %v): textLen = %d, %v; want %d, true", raw, fold, got, ok, want)
			}
		}
	}
}
