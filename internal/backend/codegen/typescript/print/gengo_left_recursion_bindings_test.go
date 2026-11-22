package print_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime"
)

// TestGeneratedTSLeftRecursionGuardIgnoresVariables is TestLeftRecursionGuardIgnoresVariables
// for generated TypeScript parsers.
func TestGeneratedTSLeftRecursionGuardIgnoresVariables(t *testing.T) {
	t.Parallel()
	var script strings.Builder
	script.WriteString("import { parse, recognize } from \"./parser.ts\";\n")
	var want strings.Builder
	prog := compileBare(t, codegentest.LRBindingSource, check.Options{LeftRecursionBindings: true})
	for _, in := range codegentest.LRBindingInputs {
		fmt.Fprintf(&script, "{ const r = parse(%q); const e = recognize(%[1]q); console.log(r.error === null ? String(r.node) : \"error: \" + r.error.message, e === null ? \"null\" : e.message); }\n", in)
		n, err := prog.ParseWith("main", in, engine.ParseOptions{Expectations: true, Limits: runtime.Limits{Depth: 400}})
		_, recErr := prog.ParseWith("main", in, engine.ParseOptions{Expectations: true, Limits: runtime.Limits{Depth: 400}, Recognize: true})
		if err != nil {
			fmt.Fprintf(&want, "error: %v %v\n", err, recErr)
		} else {
			fmt.Fprintf(&want, "%s null\n", n.String())
		}
	}
	out := codegentest.RunTSGrammarScriptWith(t, mustParse(codegentest.LRBindingSource), script.String(), common.GenOptions{Start: "main", Recognize: true, MaxDepth: 400, LeftRecursionBindings: true})
	if out != want.String() {
		t.Fatalf("generated\n%s\nengine\n%s", out, want.String())
	}
}
