package elaborate_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestCoreBuildShapes(t *testing.T) {
	src := `rule main = a:x b:("c" / d)* ("e" [0-9]) #atomic -"f" !"g" &(h:"h") $$ => $a
rule x = "x" "y"
rule d = "d"
`
	prog := coreProgram(t, src)
	if prog.HasMissing {
		t.Fatal("does not compile")
	}
	m, err := elaborate.CoreFrom(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		t.Fatal(err)
	}
	got := core.Print(m, false)
	for _, want := range []string{
		"scope=[a b h] => $0:a",
		"  seq\n    capture 0 a\n      call 1:x\n",
		"    capture 1 b\n      repeat 0 -1 resume=1 shiftable\n",
		"    text\n      seq\n",
		"    drop\n      lit \"f\"\n",
		"    not\n      lit \"g\"\n",
		"    and\n      capture 2 h\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Print does not contain %q:\n%s", want, got)
		}
	}
	main := m.Rules[0]
	if main.Want || main.Finish.Kind != core.FinishAction {
		t.Errorf("main: want=%v finish=%v; want a lean rule with an action", main.Want, main.Finish.Kind)
	}
	// The capture builds the value of its body, a call of x itself rather than its twin.
	capture := main.Body.(*core.Seq).Items[0].(*core.Capture)
	if call := capture.Body.(*core.Call); !call.Attrs().Want || m.Rules[call.Rule].Policy.NoValue {
		t.Errorf("captured call: want=%v novalue=%v", call.Attrs().Want, m.Rules[call.Rule].Policy.NoValue)
	}
	if x := m.Rules[1]; !x.Want || x.Finish.Kind != core.FinishScope || !x.Finish.Shape.Tuple {
		t.Errorf("x: want=%v finish=%v shape=%+v", x.Want, x.Finish.Kind, x.Finish.Shape)
	}
}

// coreProgram compiles src to its static program, failing the test if it does not compile.
func coreProgram(t *testing.T, src string) *compile.Program {
	t.Helper()
	return compiletest.Program(t, src, compile.Options{})
}

// typeString returns the checker's spelling of a Core type handle.
func typeString(x core.Type) string {
	if x == nil {
		return "<nil>"
	}
	return x.(types.Ty).String()
}

// TestCoreBuildTypes checks that the module carries the checked types: named types, rule values,
// capture slots of rules and of repetition elements, and every action expression.
func TestCoreBuildTypes(t *testing.T) {
	prog := coreProgram(t, `type P = { N int; S string }
var k int = 2
rule main P = p:pair xs:(-"," q:pair)* => P{N: $p.N + len($xs) * k, S: $p.S}
rule pair P = n:[0-9]+ s:[a-z] => P{N: len($n), S: text($s)}
`)
	m, err := elaborate.CoreFrom(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		t.Fatal(err)
	}
	if !m.Typed || typeString(m.Types["P"]) != "P" {
		t.Fatalf("typed %v, types %v", m.Typed, m.Types)
	}
	if len(m.Vars) != 1 || m.Vars[0].Name != "k" || typeString(m.Vars[0].Type) != "int" || m.Vars[0].Init != 2 {
		t.Errorf("vars %+v", m.Vars)
	}
	main, pair := m.Rules[0], m.Rules[1]
	if typeString(main.Type) != "P" || typeString(pair.Type) != "P" {
		t.Errorf("rule types %s %s", typeString(main.Type), typeString(pair.Type))
	}
	slotTypes := func(s []core.Slot) string {
		var out []string
		for _, x := range s {
			out = append(out, x.Name+" "+typeString(x.Type))
		}
		return strings.Join(out, ", ")
	}
	// A capture of a call has the type of the rule's node.
	if got := slotTypes(main.Scope); got != `p pair, xs node[[]node[(pair,), {"q": 0}]]` {
		t.Errorf("main scope %s", got)
	}
	if got := slotTypes(pair.Scope); got != "n node[[]node[string]], s node[string]" {
		t.Errorf("pair scope %s", got)
	}
	var element []core.Slot
	core.Walk(main.Body, func(n core.Node) {
		if r, ok := n.(*core.Repeat); ok && r.Scope != nil {
			element = r.Scope
		}
	})
	if got := slotTypes(element); got != "q pair" {
		t.Errorf("element scope %s", got)
	}
	var actionExprs []string
	core.WalkActionExpr(main.Finish.Action, func(x core.ActionExpr) {
		actionExprs = append(actionExprs, core.ActionExprString(x)+" : "+typeString(core.TypeOf(x)))
	})
	if len(actionExprs) == 0 || !strings.HasSuffix(actionExprs[0], " : P") {
		t.Errorf("action expressions %v", actionExprs)
	}
	for _, s := range actionExprs {
		if strings.HasSuffix(s, " : <nil>") {
			t.Errorf("untyped action %s", s)
		}
	}
	if !slices.Contains(actionExprs, "var:k : int") && !slices.ContainsFunc(actionExprs, func(s string) bool { return strings.HasSuffix(s, "k : int") }) {
		t.Errorf("variable action expression not typed int: %v", actionExprs)
	}
}

// TestCoreBuildStreamAndMissing checks that #stream at the top of a rule body builds a Stream in
// every rule, also one that starts no parse, where it delivers nothing (no Commit effect), and that
// #missing builds a Missing of its literal, numbered per attribute and keeping an @error written
// before it, which then labels inserted tokens.
func TestCoreBuildStreamAndMissing(t *testing.T) {
	prog := coreProgram(t, `@start
rule main = x:item* #stream $$
rule item = "a" ";" #missing / inner / "c" ":" @error(message="colon") #missing
rule inner = "b"* #stream
`)
	m, err := elaborate.CoreFrom(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		t.Fatal(err)
	}
	got := core.Print(m, false)
	for _, want := range []string{
		"    capture 0 x\n      stream\n        repeat 0 -1 resume=1 shiftable\n",
		"      seq\n        lit \"a\"\n        missing site=1\n          lit \";\"\n",
		"        missing site=2\n          error \"colon\" missing-scope\n            lit \":\"\n",
		"  stream\n    repeat 0 -1 resume=2 shiftable\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Print does not contain %q:\n%s", want, got)
		}
	}
	if _, ok := m.Rules[2].Body.(*core.Stream); !ok {
		t.Errorf("inner: %T, want the Stream of a rule that starts no parse", m.Rules[2].Body)
	}
	if m.Rules[0].Body.Attrs().Eff.Set&core.EffCommit == 0 || m.Rules[2].Body.Attrs().Eff.Set&core.EffCommit != 0 {
		t.Errorf("commit effects: main %v, inner %v", m.Rules[0].Body.Attrs().Eff, m.Rules[2].Body.Attrs().Eff)
	}
}

// TestCoreBuildStreamUnderNestedCaptures checks that a #stream repetition that a top-level item
// captures through nested captures is built as a Stream under those captures, and verifies, in a
// start rule and in a rule that is not one.
func TestCoreBuildStreamUnderNestedCaptures(t *testing.T) {
	prog := coreProgram(t, `@start
rule main = x:(y:"a"* #stream) inner $$
rule inner = u:(v:"b"* #stream)
`)
	m, err := elaborate.CoreFrom(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		t.Fatal(err)
	}
	got := core.Print(m, false)
	for _, want := range []string{
		"    capture 0 x\n      capture 1 y\n        stream\n          repeat 0 -1 resume=1 shiftable\n",
		"  capture 0 u\n    capture 1 v\n      stream\n        repeat 0 -1 resume=2 shiftable\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Print does not contain %q:\n%s", want, got)
		}
	}
}

// TestScanKeepsTheOriginThroughStream checks that the Scan that takes the place of the #stream
// repetition of a rule that is not a start rule has the ID of that repetition, and so its origin.
func TestScanKeepsTheOriginThroughStream(t *testing.T) {
	prog := coreProgram(t, `@start
rule main = inner $$
rule inner = [a-z]* #stream
`)
	m, err := elaborate.CoreFrom(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range m.Rules {
		r.Want = false
		core.Walk(r.Body, func(n core.Node) { n.Attrs().Want = false })
	}
	core.Analyze(m)
	corepass.Scans(m)
	s, ok := m.Rules[1].Body.(*core.Scan)
	if !ok {
		t.Fatalf("inner after scans: %T, want *core.Scan", m.Rules[1].Body)
	}
	if _, ok := m.Origin.Of(s).(*ast.Repeat); !ok {
		t.Errorf("origin of the scan: %T, want *ast.Repeat", m.Origin.Of(s))
	}
}

// TestCoreBuildNumbersEveryNode checks that the builder gives every node of the module an ID of
// its own, and that the origin map of the module holds the IDs of its nodes only.
func TestCoreBuildNumbersEveryNode(t *testing.T) {
	prog := coreProgram(t, `@start
rule main = (a:"x" / [0-9]+ b:main)* $$
`)
	m, err := elaborate.CoreFrom(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[core.NodeID]core.Node{}
	for _, r := range m.Rules {
		core.Walk(r.Body, func(n core.Node) {
			id := n.Attrs().ID
			if id == 0 || id > m.LastID {
				t.Errorf("%T has ID %d, LastID %d", n, id, m.LastID)
			}
			if o, ok := seen[id]; ok && o != n {
				t.Errorf("%T and %T share the ID %d", o, n, id)
			}
			seen[id] = n
		})
	}
	if len(m.Origin.Exprs) == 0 {
		t.Fatal("the module has no origin map")
	}
	for id, e := range m.Origin.Exprs {
		n, ok := seen[id]
		if !ok || e == nil {
			t.Errorf("origin map holds ID %d, which no node of the module has, or no expression", id)
			continue
		}
		if m.Origin.Of(n) != e {
			t.Errorf("origin of %T differs from the map", n)
		}
	}
}

// TestCoreBuildNumbersEveryActionExpr checks that the builder gives every action expression of the
// typed and of the untyped module an ID of its own, apart from the IDs of the nodes, and that the
// origin map gives each one the action expression of the grammar it is built from, of the kind
// that it is built from, and holds the IDs of the action expressions only.
func TestCoreBuildNumbersEveryActionExpr(t *testing.T) {
	prog := coreProgram(t, `var n int = 0
type Pair = {
    A string
    B int
}
@start
rule main = {n == 0} x:item {n := n + 1} ys:item* e $$ => Pair{A: $x, B: len(map($ys, (y) => len(y)))}
rule item string = v:[a-z]+ {len($v) > (0)} => text($v)
rule e int = #operators {
    operand d:[0-9] => 1
    level { infix left "+" => $lhs + $rhs }
}
`)
	typed, err := elaborate.CoreFrom(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	untyped, err := elaborate.Structural(structuralInput(prog), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		m    *core.Module
	}{{"typed", typed}, {"untyped", untyped}} {
		seen := map[core.ActionExprID]core.ActionExpr{}
		kinds := map[string]bool{}
		visit := func(top core.ActionExpr) {
			core.WalkActionExpr(top, func(x core.ActionExpr) {
				id := core.ActionExprIDOf(x)
				if id == 0 || id > c.m.LastActionExprID {
					t.Errorf("%s: %T has ID %d, LastActionExprID %d", c.name, x, id, c.m.LastActionExprID)
				}
				if o, ok := seen[id]; ok && o != x {
					t.Errorf("%s: %T and %T share the ID %d", c.name, o, x, id)
				}
				seen[id] = x
				o := c.m.Origin.OfActionExpr(x)
				if o == nil {
					t.Errorf("%s: %T has no origin", c.name, x)
					return
				}
				if _, group := o.(*ast.ActionGroup); group {
					t.Errorf("%s: the origin of %T is a parenthesized expression, not the expression inside it", c.name, x)
				}
				kinds[fmt.Sprintf("%T<-%T", x, o)] = true
			})
		}
		for _, r := range c.m.Rules {
			core.Walk(r.Body, func(n core.Node) {
				switch n := n.(type) {
				case *core.Pred:
					visit(n.Action)
				case *core.BindNode:
					visit(n.Action)
				case *core.OperatorsRule:
					for _, l := range n.Lines() {
						if l.Action != nil {
							visit(l.Action)
						}
					}
				}
			})
			if r.Finish.Action != nil {
				visit(r.Finish.Action)
			}
		}
		for _, want := range []string{"*core.Binary<-*ast.Binary", "*core.VarRef<-*ast.VarRef", "*core.Const<-*ast.IntLit",
			"*core.New<-*ast.Constructor", "*core.Builtin<-*ast.Call", "*core.Lambda<-*ast.Lambda",
			"*core.Local<-*ast.CaptureRef", "*core.SlotRef<-*ast.CaptureRef"} {
			if !kinds[want] {
				t.Errorf("%s: no action expression %s among %v", c.name, want, kinds)
			}
		}
		if len(c.m.Origin.ActionExprs) != len(seen) {
			t.Errorf("%s: the origin map holds %d action expressions, the module %d", c.name, len(c.m.Origin.ActionExprs), len(seen))
		}
		for id := range c.m.Origin.ActionExprs {
			if _, ok := seen[id]; !ok {
				t.Errorf("%s: the origin map holds ID %d, which no action expression of the module has", c.name, id)
			}
		}
	}
}

// TestCoreBuildCallLevels checks the minimum binding level of calls of an Operators Rule, and that
// a level the rule lacks, or a level of a rule that has none, is an error.
func TestCoreBuildCallLevels(t *testing.T) {
	prog := coreProgram(t, `rule main = level(e, product) "+" e
rule e = #operators {
    operand [0-9]
    level sum { infix left "+" }
    level product { infix left "*" }
}
`)
	m, err := elaborate.CoreFrom(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	var levels []int
	core.Walk(m.Rules[0].Body, func(n core.Node) {
		if c, ok := n.(*core.Call); ok {
			levels = append(levels, c.Level)
		}
	})
	if !slices.Equal(levels, []int{1, 0}) {
		t.Errorf("levels %v, want [1 0]", levels)
	}
	e := prog.CoreInput().Rules[1]
	if _, err := elaborate.CallLevel(&ast.Ref{Name: "e", Level: "power"}, e); err == nil {
		t.Error("unknown level: no error")
	}
	if _, err := elaborate.CallLevel(&ast.Ref{Name: "main", Level: "sum"}, prog.CoreInput().Rules[0]); err == nil {
		t.Error("level of a rule without levels: no error")
	}
}

// TestCoreBuildMemberField checks that the Core IR of an action carries, on each member read, the
// capture a projection of an element can stand for: e.$v gives v, and a node member gives
// none.
func TestCoreBuildMemberField(t *testing.T) {
	prog := coreProgram(t, `rule main = x:item* => concat(map($x, (e) => e.$v), map($x, (e) => e.Start))
rule item = v:"a"
`)
	m, err := elaborate.CoreFrom(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	core.WalkActionExpr(m.Rules[0].Finish.Action, func(t core.ActionExpr) {
		if mem, ok := t.(*core.Member); ok {
			got[mem.Name] = mem.Field
		}
	})
	want := map[string]string{"$v": "v", "Start": ""}
	if !maps.Equal(got, want) {
		t.Errorf("member fields = %v, want %v", got, want)
	}
}
