package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/schema"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func requireInsertions(t *testing.T, prog *Program, input string, o ParseOptions, count int) (*value.Node, value.SyntaxErrors) {
	t.Helper()
	n, err := prog.ParseWith("main", input, o)
	var recovered value.SyntaxErrors
	if !errors.As(err, &recovered) || len(recovered) != count {
		t.Fatalf("input %q: node=%v, recoveries=%#v, error=%v; want %d insertions", input, n, recovered, err, count)
	}
	if o.Recognize {
		if n != nil {
			t.Fatal("recognition returned a tree")
		}
	} else if n == nil {
		t.Fatal("recovered parse returned no tree")
	}
	return n, recovered
}

func TestMissingTokensValidationAndRepresentations(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{`"" #missing`, `[a-z] #missing`, `("a" "b") #missing`, `"a" @missing`, `"a" #atomic #missing`} {
		if message := compileError(t, `rule main = `+expression); !strings.Contains(message, "missing") {
			t.Fatal(expression, message)
		}
	}
	if message := compileError(t, `type Missing = { Text string }
rule main = "a"`); !strings.Contains(message, "type Missing is reserved") {
		t.Fatal(message)
	}
	const source = `type Item ~ node[string] | missing
type Root = { Token Item }
rule main Root = x:(";" #missing) => Root{Token: $x}`
	prog := mustCompile(t, source)
	n, _ := requireInsertions(t, prog, "", ParseOptions{Expectations: true, MissingTokens: 1}, 1)
	if len(statetest.MissingNodes(t, n)) != 1 {
		t.Fatal("declared join lost Missing identity")
	}
	data, err := ast.MarshalJSON(prog.Static().Grammar)
	if err != nil {
		t.Fatal(err)
	}
	g, err := ast.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Format(g)
	parsed, err := parseTest(formatted)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Compile(parsed, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	requireInsertions(t, reloaded, "", ParseOptions{Expectations: true, MissingTokens: 1}, 1)
	sch, err := outputSchemaPrototype(prog, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip schema.Output
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `{"name":"Token","shape":{"kind":"join","alternatives":[{"kind":"missing"},`) {
		t.Fatalf("schema lost Missing: %s", data)
	}
}
