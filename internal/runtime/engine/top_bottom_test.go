package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestTopBottomAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range codegentest.TopBottomCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			program := mustCompile(t, fixture.Src)
			for _, input := range fixture.Inputs {
				baseline := codegentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure}))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					if actual := codegentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend})); actual != baseline {
						t.Fatalf("%s input=%q: %s / %s", backend, input, actual, baseline)
					}
				}
			}
		})
	}
}

// A list of _ only has to be a list value, whatever its elements.
func TestTopBottomListCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ field, want string }{
		{"[]_", "[]_"},
		{"*[]_", "*[]_"},
		{"*[]_ | int", "*([]_ | int)"},
	} {
		src := "type T = { A " + tc.field + " }\ntype U = { F _ }\nrule main = x:\"a\" => T{A: U{F: \"s\"}.F.(" + tc.field + ")}\nrule ok = x:\"a\"* => T{A: U{F: $x.Value}.F.(" + tc.field + ")}\nrule rule []_ = x:\"a\"* => U{F: $x.Value}.F.([]_)\nrule bad []_ = x:\"a\" => U{F: $x}.F.([]_)"
		for _, opts := range []checkpkg.Options{{}} {
			program := mustCompile(t, src, opts)
			data, err := program.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data)
			if err != nil {
				t.Fatalf("%s: %v", tc.field, err)
			}
			for _, p := range []*Program{program, loaded} {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					if p == loaded && backend == Closure {
						continue // a loaded program runs on the bytecode backends
					}
					if _, err := p.ParseWith("main", "a", ParseOptions{Backend: backend}); err == nil || !strings.Contains(err.Error(), "cannot cast string to "+tc.want) {
						t.Fatalf("%s %s: %v", tc.field, backend, err)
					}
					for _, start := range []string{"ok", "rule"} {
						if _, err := p.ParseWith(start, "aa", ParseOptions{Backend: backend}); err != nil {
							t.Fatalf("%s %s %s: %v", tc.field, start, backend, err)
						}
					}
					if _, err := p.ParseWith("bad", "a", ParseOptions{Backend: backend}); err == nil || !strings.Contains(err.Error(), "cannot cast node[string] to []_") {
						t.Fatalf("%s bad %s: %v", tc.field, backend, err)
					}
				}
			}
		}
	}
}

// TestTopBottomCompiledFile checks that a compiled file keeps none of the grammar's types, while the
// checked program keeps the meets and the top and bottom types in its source grammar.
func TestTopBottomCompiledFile(t *testing.T) {
	t.Parallel()
	src := "type A ~ []B & *C | _ & (D | B) | _|_\ntype E ~ [](B & C) & *(B | C)\ntype B = { Text string }\ntype C = { Text string }\ntype D = { Text string }\nrule main = x:\"a\" => $x.(_ & node[_])"
	program := mustCompile(t, src)
	bin, err := program.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(bin)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Static() != nil || bytes.Contains(bin, []byte("_|_")) {
		t.Fatalf("compiled file keeps the types: checked program %v", loaded.Static() != nil)
	}
	if again, err := loaded.MarshalBinary("main"); err != nil || !bytes.Equal(again, bin) {
		t.Fatalf("compiled round trip differs: %v", err)
	}
	if got := fmt.Format(program.Static().Grammar); !strings.Contains(got, "type A ~ []B & *C | _ & (D | B) | _|_") || !strings.Contains(got, "$x.(_ & node[_])") {
		t.Fatalf("source grammar:\n%s", got)
	}
}

func TestTopBottomSchema(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, "type T = { A _; B (node[string] | int) & node[_]; C U }\ntype U ~ _\nrule main = x:\"a\" => T{A: $x, B: $x, C: 1}")
	schema, err := outputSchemaPrototype(program, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`{"name":"A","shape":{"kind":"top"}}`, `{"name":"B","shape":{"kind":"node","value":{"kind":"scalar","scalar":"string"}}}`, `{"name":"C","shape":{"kind":"top"}}`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("schema lacks %s:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), `"declared/U"`) {
		t.Fatalf("schema exports the alias U:\n%s", data)
	}
	// Aliases are not exported, and a field cannot have a type without values.
	for _, src := range []string{"type B ~ _|_\nrule main = \"a\"", "type B ~ int & string\nrule main = \"a\"", "type B ~ node[int & string]\nrule main = \"a\""} {
		if _, err := outputSchemaPrototype(mustCompile(t, src), "main", false); err != nil {
			t.Fatalf("%q schema: %v", src, err)
		}
		if message := compileError(t, strings.Replace(src, "rule main", "type T = { F B }\nrule main", 1)); !strings.Contains(message, "no values") {
			t.Fatalf("%q field: %s", src, message)
		}
	}
	// *_|_ is nil, the optional type whose only value is nil, and []_|_ the list type whose only
	// value is the empty list: fields may have them.
	program = mustCompile(t, "type T = { F *(int & string); G []_|_ }\nrule main = x:(_|_)* => T{F: nil, G: $x.Value}")
	schema, err = outputSchemaPrototype(program, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(schema)
	for _, want := range []string{`{"name":"F","shape":{"kind":"nil"}}`, `{"name":"G","shape":{"kind":"list","element":{"kind":"join"}}}`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("schema lacks %s:\n%s", want, data)
		}
	}
	// _|_ adds nothing to a union.
	program = mustCompile(t, "type B ~ _|_ | node[string]\ntype T = { A _|_ | node[string]; C B }\nrule main = x:\"a\" => T{A: $x, C: $x}")
	schema, err = outputSchemaPrototype(program, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := json.Marshal(schema); !strings.Contains(string(data), `{"name":"A","shape":{"kind":"node","value":{"kind":"scalar","scalar":"string"}}}`) {
		t.Fatalf("bottom union schema:\n%s", data)
	}
}
