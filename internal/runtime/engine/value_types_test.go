package engine

import (
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// tracedParses returns the traced parses of the program of src on each backend of the engine, and
// on a program loaded from its compiled grammar, for codegentest.EngineValueTypeMismatches.
func tracedParses(t *testing.T, src string) (map[string]codegentest.TracedParse, codegentest.RuleParse) {
	t.Helper()
	prog := compileBare(t, src, checkpkg.Options{})
	data, err := prog.Marshal("main", CompiledDocument)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	traced := func(p *Program, b Backend) codegentest.TracedParse {
		return func(input string, missing int, call func(codegentest.RuleCall)) (*value.Node, error) {
			return p.ParseWith("main", input, ParseOptions{Unit: sxirinput.Bytes, Backend: b, MissingTokens: missing, Trace: func(e TraceEvent) {
				if v, ok := state.TraceValue(e); ok {
					call(codegentest.RuleCall{Rule: e.Rule, Start: e.Pos, End: e.End, Value: v})
				}
			}})
		}
	}
	parses := map[string]codegentest.TracedParse{
		"closure":            traced(prog, Closure),
		"bytecode":           traced(prog, Bytecode),
		"bytecode iterative": traced(prog, BytecodeIterative),
		"loaded":             traced(loaded, Default),
	}
	rule := func(r, input string, missing int) (*value.Node, error) {
		return prog.ParseWith(r, input, ParseOptions{Unit: sxirinput.Bytes, MissingTokens: missing})
	}
	return parses, rule
}

// TestEngineValuesHaveTheirTypes checks the first part of the type-soundness theorem
// (spec/proofs/type-soundness.md) on the engine: every successful evaluation of a rule that a traced
// parse of the corpus reports, on every backend, produces a well-formed value of the rule's checked
// type, and so does every successful parse of a rule on a part of an input that an evaluation of it
// matched.
func TestEngineValuesHaveTheirTypes(t *testing.T) {
	calls, parses := 0, 0
	for _, c := range codegentest.ValueTypeCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			backends, rule := tracedParses(t, c.Src)
			got := codegentest.EngineValueTypeMismatches(t, c, backends, rule, nil)
			for _, m := range got.Mismatches {
				t.Error(m)
			}
			calls += got.Calls
			parses += got.RuleParses
		})
	}
	if calls == 0 || parses == 0 {
		t.Fatalf("checked %d evaluations and %d parses of rules: the traces report no values", calls, parses)
	}
	t.Logf("checked %d evaluations and %d parses of rules", calls, parses)
}

// TestEngineValueTypesDetectWrongValues is the negative control of TestEngineValuesHaveTheirTypes:
// the check fails on the engine's values when a capture of a node holds a value of another type than
// the one its node type records, when a struct field holds a value of another type than its declared
// one, and when a value is checked against a narrower type than its rule's.
func TestEngineValueTypesDetectWrongValues(t *testing.T) {
	c := codegentest.Case{Name: "captures and fields", Src: `type S = { A node[string]; N int }
rule main S = p:pair n:num => S{A: $p.$x, N: $n}
rule pair = x:"a" y:"b"?
rule num = [0-9] => 1`, Inputs: []string{"a1", "ab2"}}
	backends, rule := tracedParses(t, c.Src)
	if got := codegentest.EngineValueTypeMismatches(t, c, backends, rule, nil); len(got.Mismatches) > 0 {
		t.Fatalf("the unchanged values do not have their types: %s", strings.Join(got.Mismatches, "\n"))
	}
	one := nodes.Ptr(nodes.Data{Kind: valueimpl.BoxKind, Box: 1})
	for _, m := range []struct {
		name string
		// change changes a copy of the value of an evaluation, and reports whether it applies.
		change func(n *value.Node) bool
	}{
		{"a capture holds an int", func(n *value.Node) bool {
			d := nodes.D(n)
			if n.Kind() != value.KindTuple || len(d.Fields) == 0 {
				return false
			}
			d.Fields[0].Value = one
			return true
		}},
		{"a struct field holds an int", func(n *value.Node) bool {
			d := nodes.D(n)
			if n.Kind() != value.KindStruct {
				return false
			}
			valueimpl.SetField(&d.Fields, "A", one)
			return true
		}},
		{"a struct field holds a list", func(n *value.Node) bool {
			d := nodes.D(n)
			if n.Kind() != value.KindStruct {
				return false
			}
			valueimpl.SetField(&d.Fields, "N", nodes.Ptr(nodes.Data{Kind: valueimpl.ListValueKind}))
			return true
		}},
	} {
		applied := 0
		got := codegentest.EngineValueTypeMismatches(t, c, backends, rule, func(n *value.Node) *value.Node {
			if n == nil {
				return nil
			}
			n = n.Clone()
			if m.change(n) {
				applied++
			}
			return n
		})
		if applied == 0 {
			t.Errorf("%s: the change applies to no value", m.name)
		} else if len(got.Mismatches) == 0 {
			t.Errorf("%s: the check passes the changed values", m.name)
		}
	}
	// A narrower type than the rule's: the option of pair is absent on "a1".
	vt := codegentest.ValueTypesOf(t, c)
	n, err := compileBare(t, c.Src).ParseWith("pair", "a", ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pair, _ := vt.RuleType("pair")
	if err := vt.CheckType(pair, n); err != nil {
		t.Fatalf("the value of pair does not have its type %s: %v", pair, err)
	}
	narrow := types.MapTy(pair, func(x types.Ty) (types.Ty, bool) {
		if o, ok := x.(types.OptTy); ok {
			return o.Elem, true
		}
		return nil, false
	})
	if narrow.String() == pair.String() {
		t.Fatalf("the type of pair, %s, has no option to narrow", pair)
	}
	if err := vt.CheckType(narrow, n); err == nil {
		t.Errorf("the value of pair on %q has the narrower type %s", "a", narrow)
	}
}
