package print_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// ignoreCaseLines is the expected output of a program that parses each input and prints "ok" with
// the consumed input, or "error".
func ignoreCaseLines(t *testing.T, src string, inputs []string) string {
	t.Helper()
	prog := mustCompile(t, src)
	var b strings.Builder
	for _, in := range inputs {
		n, err := prog.ParseWith("main", in, engine.ParseOptions{Expectations: true, Backend: engine.Closure})
		if err != nil {
			b.WriteString("error\n")
			continue
		}
		r := []rune(in)
		fmt.Fprintf(&b, "ok %s\n", string(r[n.Start():n.End()]))
	}
	return b.String()
}

// TestIgnoreCaseGeneratedTS checks that the generated TypeScript parser agrees with the closure
// backend on case-insensitive literals.
func TestIgnoreCaseGeneratedTS(t *testing.T) {
	t.Parallel()
	want := ignoreCaseLines(t, codegentest.IgnoreCaseGenGrammar, codegentest.IgnoreCaseGenInputs)
	var list strings.Builder
	for _, in := range codegentest.IgnoreCaseGenInputs {
		fmt.Fprintf(&list, "  %s,\n", strconv.Quote(in))
	}
	script := "import { parse } from \"./parser.ts\";\nfor (const s of [\n" + list.String() + "]) {\n" +
		"  const r = parse(s);\n  if (r.error !== null) {\n    console.log(\"error\");\n    continue;\n  }\n" +
		"  console.log(\"ok \" + Array.from(s).slice(r.node.start, r.node.end).join(\"\"));\n}\n"
	if got := runTSScript(t, codegentest.IgnoreCaseGenGrammar, script); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestIgnoreCaseGeneratedTSInlineAction checks that the pragma reaches the body of an inline action,
// which is written inside it, and not a rule that the body calls (spec/language/pragmas.md).
func TestIgnoreCaseGeneratedTSInlineAction(t *testing.T) {
	t.Parallel()
	script := "import { parse } from \"./parser.ts\";\nfor (const s of [\"ABcd\", \"abcd\", \"ABCd\", \"ABcD\"]) {\n" +
		"  const r = parse(s);\n  console.log(r.error === null ? \"ok \" + s.slice(r.node.start, r.node.end) : \"error\");\n}\n"
	want := "ok ABcd\nok abcd\nerror\nerror\n"
	if got := runTSScript(t, "rule main = (\"ab\" c => 1) #ignorecase \"d\"\nrule c = \"c\"", script); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
