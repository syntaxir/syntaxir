package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver/checked"
	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestNamedNodeTypesAcrossBackends(t *testing.T) {
	t.Parallel()
	inputs := []string{"", "ab", "ab 12 c3", "<1,x>", "ab <12,y>[1]!bc", "12 ?", "[]!a"}
	for _, sourceInfo := range []bool{false, true} {
		prog := compileBare(t, codegentest.NodeTypeGrammar, checkpkg.Options{SourceInfo: sourceInfo})
		for _, input := range inputs {
			want := codegentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure}))
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				got := codegentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend}))
				if got != want {
					t.Errorf("sourceInfo=%t %s %q:\n got %s\nwant %s", sourceInfo, backend, input, got, want)
				}
			}
		}
	}
	prog := compileBare(t, codegentest.NodeTypeGrammar)
	n, err := prog.Parse("main", "ab 12<1,x>[3]")
	if err != nil {
		t.Fatal(err)
	}
	got := codegentest.ResultJSON(n, err)
	for _, want := range []string{`{"type":"Ident","start":0,"end":2,"value":"ab"}`, `{"type":"Num","start":3,"end":5,"value":"12"}`, `"Pair":{"type":"Pair","start":6`, `"Nums":{"type":"Nums"`, `"Len":4`, `"Text":"ab12"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	items, _ := n.Field("Items").(*value.Node)
	if items == nil || items.Elem(0).Type() != "Ident" || items.Elem(1).Type() != "Num" {
		t.Fatalf("item types: %s", got)
	}
}

// A module is rejected if a node type has fields or is not over a node type, if a rule converts
// its value to a type that is not a named node type or a text struct, or if ENODE names a type
// that is not a named node type.
func TestNamedNodeTypeModuleValidation(t *testing.T) {
	t.Parallel()
	prog := compileBare(t, codegentest.NodeTypeGrammar)
	base := prog.Module()
	if err := program.ValidateModule(base); err != nil {
		t.Fatal(err)
	}
	str := func(m *bytecode.Module, s string) int {
		for i, x := range m.Strings {
			if x == s {
				return i
			}
		}
		t.Fatalf("no string %q", s)
		return -1
	}
	clone := func() *bytecode.Module {
		m := *base
		m.Types = append([]bytecode.TypeInfo(nil), base.Types...)
		m.Rules = append([]bytecode.RuleInfo(nil), base.Rules...)
		m.Actions = cloneActions(base.Actions)
		return &m
	}
	for name, tc := range map[string]struct {
		edit func(m *bytecode.Module)
		want string
	}{
		"fields": {func(m *bytecode.Module) {
			for i := range m.Types {
				if m.Types[i].Node >= 0 {
					m.Types[i].Fields = []int{str(m, "Text")}
					m.Types[i].Zeros = []int{str(m, "Text")}
				}
			}
		}, "has fields"},
		"zeros": {func(m *bytecode.Module) {
			for i := range m.Types {
				if m.Types[i].Node < 0 && len(m.Types[i].Fields) > 0 {
					m.Types[i].Zeros = m.Types[i].Zeros[1:]
				}
			}
		}, "zero descriptors"},
		"underlying": {func(m *bytecode.Module) {
			for i := range m.Types {
				if m.Types[i].Node >= 0 {
					m.Types[i].Node = str(m, "Out")
				}
			}
		}, "is not over a node type"},
		"rule": {func(m *bytecode.Module) {
			for i := range m.Rules {
				if m.Rules[i].NodeType >= 0 {
					m.Rules[i].NodeType = str(m, "main")
				}
			}
		}, "is not a named node type"},
		"enode": {func(m *bytecode.Module) {
			for i := range m.Actions {
				for k := range m.Actions[i].NodeTypes {
					m.Actions[i].NodeTypes[k] = str(m, "Out")
				}
			}
		}, "of a conversion is not a named node type"},
	} {
		m := clone()
		tc.edit(m)
		if err := program.ValidateModule(m); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %s", name, err, tc.want)
		}
	}
}

// TestNamedNodeTypeResolution checks types that refer to each other through named node types: an
// alias may refer to itself through one, a recursive named node type keeps its value inside other
// node types, and many mutually recursive named node types resolve in a polynomial number of steps.
func TestNamedNodeTypeResolution(t *testing.T) {
	for _, src := range []string{
		`type Num = node[string]
type Lst = node[[]Expr]
type Expr ~ Num | Lst
rule main Expr = num / lst
rule num Num = [0-9] #atomic
rule lst Lst = "(" xs:main* ")" => Lst{$xs}`,
		`type Tree = node[[]Tree]
type O = { F node[[]Tree] }
rule main O = t:tree $$ => O{F: $t.Value[0]}
rule tree Tree = "(" ts:tree* ")" => Tree{$ts}`,
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		prog, err := Compile(g, checkpkg.Options{})
		if err == nil {
			_, err = prog.Parse("main", "((()))")
		}
		if err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}

	// Many mutually recursive named node types, each listing all of them, are counted in steps of the
	// compiler (cancel.Control), not in time: the steps of the checks grow with a power of the number
	// of types, and a resolution that follows every path through the types grows exponentially.
	small, large := mutualNodeTypeSteps(t, 20), mutualNodeTypeSteps(t, 40)
	t.Logf("%d steps for 20 types, %d for 40", small, large)
	if !growsPolynomially(small, large) {
		t.Errorf("mutually recursive node types: %d steps for 20 types, %d for 40: more than %d times as many", small, large, maxPolynomialRatio)
	}
}

// maxPolynomialRatio is the largest growth in steps for twice as many types that counts as polynomial.
// The steps grow with the third power (a factor of 6.5 to 7.4 between 20 and 80 types), so 16, the
// fourth power, passes them, while a resolution that follows every path passes 2^20 times as many
// steps at 40 types as at 20.
const maxPolynomialRatio = 16

func growsPolynomially(small, large uint64) bool { return large <= maxPolynomialRatio*small }

// mutualNodeTypeSteps returns the steps that the compiler takes on n named node types that refer to
// each other through their values.
func mutualNodeTypeSteps(t *testing.T, n int) uint64 {
	t.Helper()
	var b strings.Builder
	alts := make([]string, n)
	for i := range alts {
		alts[i] = fmt.Sprintf("T%d", i)
	}
	for i := range n {
		fmt.Fprintf(&b, "type T%d = node[[](%s)]\n", i, strings.Join(alts, " | "))
		fmt.Fprintf(&b, "rule r%d T%d = \"(\" xs:r%d* \")\" => T%d{$xs}\n", i, i, (i+1)%n, i)
	}
	g, err := parseTest(b.String())
	if err != nil {
		t.Fatal(err)
	}
	c := &cancel.Control{Ctx: context.Background()}
	if _, err := checked.Compile(g, checked.Options{}, c); err != nil {
		t.Fatal(err)
	}
	return uint64(c.Steps)
}

// TestPolynomialGrowthDetectorSeesExponentialSteps is the negative control of
// TestNamedNodeTypeResolution: the growth measure fails the steps of a resolution that follows every
// path through the types (2^n), and passes cubic and linear ones.
func TestPolynomialGrowthDetectorSeesExponentialSteps(t *testing.T) {
	cube := func(n int) uint64 { return uint64(n) * uint64(n) * uint64(n) }
	pow2 := func(n int) uint64 { return uint64(1) << n }
	if !growsPolynomially(cube(20), cube(40)) || !growsPolynomially(20, 40) {
		t.Error("polynomial steps were reported as growing faster")
	}
	if growsPolynomially(pow2(20), pow2(40)) {
		t.Error("exponential steps passed as polynomial")
	}
}

// TestNamedNodeTypeText checks that the S-expression of a node of a named node type names the type
// before its brackets, whatever it converts (Ident("x"), Pair(a b), Items[a]), on every backend
// and with source information, and that a node of a reserved type stays unnamed. The corpus
// comparisons of generated Go and TypeScript parsers check that they print the same strings.
func TestNamedNodeTypeText(t *testing.T) {
	t.Parallel()
	const want = `Out{Any: Any("bc"), Items: [Ident("ab") Num("12")], Len: 4, Nums: Nums[Num("3")], Pair: Pair(Num("1") Ident("x")), Start: 3, Text: "ab12"}`
	for _, sourceInfo := range []bool{false, true} {
		prog := compileBare(t, codegentest.NodeTypeGrammar, checkpkg.Options{SourceInfo: sourceInfo})
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := prog.ParseWith("main", "ab 12<1,x>[3]!bc", ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			if got := n.String(); got != want {
				t.Errorf("sourceInfo=%t %s:\n got %s\nwant %s", sourceInfo, backend, got, want)
			}
		}
	}
	// The plain node types of the same shapes stay unnamed.
	prog := compileBare(t, `type Out = { S node[string]; T node[(node[string], node[string])]; L node[[]node[string]] }
rule main Out = s:"a" #atomic t:("b" "c") l:("d"*) $$ => Out{S: $s, T: $t, L: $l}`)
	n, err := prog.Parse("main", "abcdd")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := n.String(), `Out{L: ["d" "d"], S: "a", T: ("b" "c")}`; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}
