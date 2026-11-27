//go:build sxir_long

package print_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// typedConversionMutants each break one step of the conversion of typed TypeScript values (astConvert
// of the generated parser.ts), one row of the TypeScript table of typed values or more.
var typedConversionMutants = []struct{ name, from, to string }{
	{"an int converts as zero", `case "int": return isInt(v) ? v : 0;`, `case "int": return 0;`},
	{"a string gains a character", `case "string": return typeof v === "string" ? v : "";`, `case "string": return typeof v === "string" ? v + "?" : "";`},
	{"a list converts its elements in reverse", `array[i] = get(astKid(work.node, i), recipe.elem!);`, `array[work.node.children.length - 1 - i] = get(astKid(work.node, i), recipe.elem!);`},
	{"a tuple converts its elements as absent", `array[i] = get(astKid(work.node, i), recipe.elems![i]!);`, `array[i] = get(null, recipe.elems![i]!);`},
	{"a node starts one position late", `start: v.start, end: v.end, value: null }`, `start: v.start + 1, end: v.end, value: null }`},
	{"a string node loses its text", `if (recipe.mode === "text") obj["value"] = n.text;`, `if (recipe.mode === "text") obj["value"] = "";`},
	{"a struct field converts as absent", `obj[f.name] = get(work.node.field(f.name), f.repr);`, `obj[f.name] = get(null, f.repr);`},
	{"a recovered value loses its message", `obj["message"] = typeof msg === "string" ? msg : "";`, `obj["message"] = "";`},
	{"a missing value loses what it expects", `obj["expected"] = typeof expected === "string" ? expected : "";`, `obj["expected"] = "";`},
	{"a join converts no member", `const next = recipe.members!.get(v.type);`, `const next = recipe.members!.get("");`},
}

// TestTypedValuesDetectBrokenConversion is the negative control of TestTypedValuesMatchEngine: on the
// first grammar that converts types of each row of the TypeScript table of typed values, each
// mutant of typedConversionMutants, whose text must occur, must make a typed value differ from the
// engine's, and the unchanged parsers must not.
func TestTypedValuesDetectBrokenConversion(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	node := tooltest.RequireNode(t)
	cases := codegentest.TSTypedRows.Cover(t, typedTSCases(t))
	// The control passes the identity as its mutation, which skips the type check of the generated
	// files: TestTypedValuesMatchEngine runs it.
	if got := typedValuesMismatches(t, node, cases, func(code string) string { return code }); len(got) != 0 {
		t.Fatalf("control: the unchanged parsers differ:\n%s", strings.Join(got, "\n"))
	}
	for _, m := range typedConversionMutants {
		t.Run(m.name, func(t *testing.T) {
			var hits atomic.Int64
			got := typedValuesMismatches(t, node, cases, func(code string) string {
				if strings.Contains(code, m.from) {
					hits.Add(1)
				}
				return strings.ReplaceAll(code, m.from, m.to)
			})
			if hits.Load() == 0 {
				t.Fatalf("no generated file has %q", m.from)
			}
			values := 0
			for _, g := range got {
				if strings.Contains(g, ": typed value") {
					values++
				}
			}
			if values == 0 {
				t.Errorf("no typed value differs from the engine's; mismatches:\n%s", strings.Join(got, "\n"))
			}
			t.Logf("%d of %d mismatches in typed values", values, len(got))
		})
	}
}
