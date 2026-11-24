package print

import (
	"os"
	"path/filepath"
	"testing"

	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestLiteralFailureKeepsPosition checks the TypeScript library's literal matchers against the
// contract of flow.matchLit: a literal that fails leaves the position where it was, also when the input
// matches a prefix of it. Each case matches a part of the literal and then differs, on the comparison of
// code points, on the case-folding comparison, and in bytes on the plain comparison and on the
// comparison of a literal with U+FFFD, which decodes the input character by character.
func TestLiteralFailureKeepsPosition(t *testing.T) {
	node := tooltest.RequireNode(t)
	source, err := os.ReadFile(filepath.Join(libDir, tsLibFileName))
	if err != nil {
		t.Fatal(err)
	}
	// The library calls the helpers of the budget module, which the generator places before it.
	helpers, err := os.ReadFile(filepath.Join(libDir, "budget.ts"))
	if err != nil {
		t.Fatal(err)
	}
	source = append(helpers, source...)
	path := filepath.Join(t.TempDir(), "literal_failure.ts")
	if err := os.WriteFile(path, append(source, []byte(literalFailureTypeScriptTest)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := tooltest.NodeCombinedOutput(t, node, filepath.Dir(path), filepath.Base(path)); err != nil {
		t.Fatalf("node: %v\n%s", err, output)
	}
}

const literalFailureTypeScriptTest = `

// The generator prints the method that records an expectation; recording none is enough here.
(Parser.prototype as any).expect = function () {};
const lit = (text: string): Lit => ({ text, cps: Array.from(text, (c) => c.codePointAt(0)!), bytes: Array.from(new TextEncoder().encode(text)) } as any);
const cases: [string, string | Uint8Array, Unit, string, boolean][] = [
  ["code points", "xabd", CodePoints, "abc", false],
  ["code points, folded", "xABd", CodePoints, "abc", true],
  ["bytes", "xabd", Bytes, "abc", false],
  ["bytes, folded", "xABd", Bytes, "abc", true],
  ["bytes, U+FFFD", new Uint8Array([0x78, 0x61, 0xff, 0x64]), Bytes, "a�c", false],
];
for (const [name, input, unit, text, fold] of cases) {
  const p = new Parser(mkGrammar(100, true, false), input, unit, 1, 100);
  p.pos = 1;
  const v = fold ? p.matchLiteralFold(lit(text), 0, false) : p.matchLiteral(lit(text), 0, false);
  if (v !== undefined) throw new Error(name + ": the literal matched");
  if (p.pos !== 1) throw new Error(name + ": a failed literal left the position at " + p.pos + ", not 1");
  // The matched prefix: the literal without its last character matches up to the differing one.
  const q = new Parser(mkGrammar(100, true, false), input, unit, 1, 100);
  q.pos = 1;
  const prefix = text.slice(0, -1);
  const w = fold ? q.matchLiteralFold(lit(prefix), 0, false) : q.matchLiteral(lit(prefix), 0, false);
  if (w !== null || q.pos !== 1 + prefix.length) throw new Error(name + ": the prefix of the literal ends at " + q.pos);
}
`
