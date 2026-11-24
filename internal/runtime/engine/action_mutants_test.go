package engine_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// engineMutant changes the printed code of the operators of action expressions in the engine in one way: in the
// compositions of internal/runtime/state/action_exec.go, or in the primitives of action_value.go and the
// other files it calls.
type engineMutant struct {
	name, file, fn, old, new string
}

var engineMutants = []engineMutant{
	{"swapped operands of -", "action_exec.go", "actionBinary", "any(l.(int) - r.(int))", "any(r.(int) - l.(int))"},
	{"a node is equal to a node", "action_exec.go", "actionEqual", "return false, false\n}", "return true, true\n}"},
	{"an int equals a bool", "action_exec.go", "actionEqual", "if ka != kb {\n\t\treturn false, false\n\t}", "if ka != kb {\n\t\treturn false, true\n\t}"},
	{"an index may be the length", "action_value.go", "intIndex", "k >= n", "k > n"},
	{"the least index is 1", "action_value.go", "intIndex", "k < 0", "k < 1"},
	{"a division by zero succeeds", "action_exec.go", "actionBinary", "if r.(int) == 0 {", "if false {"},
	{"< is <=", "action_exec.go", "actionBinary", "intCmp(l, r) < 0", "intCmp(l, r) <= 0"},
	{"strings compare as ints", "action_exec.go", "actionBinary", "p.strCmp(l, r) < 0", "p.strCmp(l, r) > 0"},
	{"! of a bool does not negate", "action_exec.go", "actionUnary", "return any(!x.(bool)), nil", "return any(x.(bool)), nil"},
	{"a logical operand need not be a bool", "action_exec.go", "actionLogic", "if valKind(x) != 3 {", "if false {"},
	{"the controlled concatenation drops the right string", "action_value.go", "strAdd", "for _, s := range [...]string{ls, rs} {", "for _, s := range [...]string{ls} {"},
	{"the counted equality ignores the content", "value_spend.go", "TextEqual", "p.valueUnits(min(len(l), len(r)))\n\treturn l == r", "p.valueUnits(min(len(l), len(r)))\n\treturn len(l) == len(r)"},
	{"the counted comparison orders a prefix after its extension", "value_spend.go", "compareText", "p.valueUnits(min(len(l), len(r)))\n\treturn strings.Compare(l, r)", "p.valueUnits(min(len(l), len(r)))\n\tif len(l) < len(r) && strings.HasPrefix(r, l) {\n\t\treturn 1\n\t}\n\treturn strings.Compare(l, r)"},
	{"the fast path of the value equality ignores the control", "budget_value.go", "ValueEqual", "return p.TextEqual(ls, rs)", "return ls == rs"},
	{"the fast path of the value equality negates", "budget_value.go", "ValueEqual", "return p.TextEqual(ls, rs)", "return !p.TextEqual(ls, rs)"},
	{"a remainder floors", "action_exec.go", "actionBinary", `any(l.(int) % r.(int))`, `any(func() int { m := l.(int) % r.(int); if m != 0 && (m < 0) != (r.(int) < 0) { m += r.(int) }; return m }())`},
	{"a division floors", "action_exec.go", "actionBinary", `any(l.(int) / r.(int))`, `any(func() int { q := l.(int) / r.(int); if (l.(int)%r.(int) != 0) && ((l.(int) < 0) != (r.(int) < 0)) { q-- }; return q }())`},
	{"a box is not opened", "action_value_engine.go", "unboxed", "return unboxed(nodes.D(n).Box)", "return v"},
	{"Start reads the value", "action_exec.go", "actionMember", "if kind == 1 {", "if kind == 0 {"},
	{"an unset field of a struct is nil", "action_exec.go", "actionMember", "if !st {\n\t\treturn nil, nil\n\t}", "if true {\n\t\treturn nil, nil\n\t}"},
	{"a struct has captures", "action_exec.go", "actionMember", "if st {\n\t\t\treturn nil, p.actionError(9", "if false {\n\t\t\treturn nil, p.actionError(9"},
	{"a set field of a struct reads as its zero", "action_exec.go", "actionMember", "if set {\n\t\treturn f, nil\n\t}", "if set && !st {\n\t\treturn f, nil\n\t}"},
	{"a capture keeps its box", "action_exec.go", "actionMember", "return unboxed(v), nil", "return v, nil"},
	{"End is a field", "action_exec.go", "memberKind", "return MemberEnd, name", "return MemberField, name"},
	{"a struct declares every field", "eval.go", "memberZero", "if !p.HasValueField(", "if false && !p.HasValueField("},
	{"len counts no units", "budget_value.go", "ValueTextLen", "p.valueUnits(len(s))", "_ = s"},
	{"nil has length 1", "action_exec.go", "actionLength", "return any(0), nil", "return any(1), nil"},
	{"the length of a string node is that of its text", "action_exec.go", "actionLength", "if nodes.D(n).Terminal {", "if false {"},
	{"a bool is not an element", "action_exec.go", "actionPush", "if k == 1 || k == 2 || k == 3 {", "if k == 1 || k == 2 {"},
	{"a function is an element", "action_exec.go", "actionPush", "return p.actionError(11, v, any(fn), 0, 0)", "return nil"},
	{"nil pushes no element", "action_exec.go", "actionPush", "p.KidStack = append(p.KidStack, nil)\n", ""},
	{"a node pushes no element", "action_exec.go", "actionPush", "p.KidStack = append(p.KidStack, n)\n", "_ = n\n"},
	{"concat pushes no elements", "action_exec.go", "actionPushItems", "\t\tp.KidStack = appendSpend(", "\t\t_ = appendSpend("},
	{"concat takes a value that is not a list", "action_exec.go", "actionPushItems", "if ok {", "if ok || true {"},
	{"concat counts the elements it pushes", "action_exec.go", "actionPushItems", "p.KidStack = appendSpend(p, p.KidStack, items)", "p.countUnits(len(items))\n\t\tp.KidStack = appendSpend(p, p.KidStack, items)"},
	{"concat takes any value", "action_exec.go", "actionItems", "return items, p.actionError(12, v, any(fn), 0, 0)", "return items, nil"},
	{"the text of a string node is the input", "action_exec.go", "actionText", "if held && int(nodes.ShapeOf(n)) != 3 {", "if held && false {"},
	{"a list value converts", "action_exec.go", "actionConvert", "if nodes.HasBase(n) {", "if true {"},
	{"a value that does not convert is nil", "action_exec.go", "actionConvert", "return nil, p.actionError(15, x, any(t[0].Type), 0, 0)", "return nil, nil"},
	{"a node field value does not cover", "action_exec.go", "actionCover", "if !ok {\n\t\treturn first", "if !ok && false {\n\t\treturn first"},
	{"the start of the range is the latest", "action_exec.go", "actionCover", "cs < ns", "cs > ns"},
	{"the end of the range is the earliest", "action_exec.go", "actionCover", "ce > ne", "ce < ne"},
	{"a later node replaces the range", "action_exec.go", "actionCover", "if first || cs < ns {", "if true {"},
	{"a struct without a node keeps no range", "action_exec.go", "actionMade", "if first {", "if false {"},
	{"a struct is not recorded", "action_exec.go", "actionMade", "c.made(n)", ""},
	{"an element of a hull counts a unit", "action_exec.go", "actionHull", "p.stepElem()", "p.countUnits(1)"},
	{"an element that adds no range widens the range", "action_exec.go", "actionHull", "if has {", "if has || true {"},
	{"the start of a hull is the latest", "action_exec.go", "actionHull", "es < lo", "es > lo"},
	{"the end of a hull is the earliest", "action_exec.go", "actionHull", "ee > hi", "ee < hi"},
	{"a later element replaces the range of a hull", "action_exec.go", "actionHull", "if first || es < lo {", "if true {"},
	{"an empty hull is not at the start of the rule", "action_exec.go", "actionHull", "lo := rs", "lo := 0"},
	{"foldr passes the elements from the first", "action_exec.go", "actionItemAt", "if right {", "if false {"},
	{"the folds pass the elements in each other's order", "action_exec.go", "actionItemAt", "if right {", "if !right {"},
	{"an iteration passes a box", "action_exec.go", "actionItemAt", "return nodes.UnboxNode(items[i])", "return items[i]"},
	{"filter takes a result that is not a bool", "action_exec.go", "actionFilterKeep", "if valKind(v) != 3 {", "if valKind(v) == 0 {"},
	{"filter drops what it keeps", "action_exec.go", "actionFilterKeep", "return v.(bool), nil", "return !v.(bool), nil"},
	{"an element of a list counts a unit", "action_exec.go", "actionElemsConform", "p.stepElem()", "p.countUnits(1)"},
	{"an element of a tuple counts a unit", "action_exec.go", "actionTupleConform", "p.stepElem()", "p.countUnits(1)"},
	{"a tuple of another length conforms", "action_exec.go", "actionTupleConform", "if count != len(t.Alts) {", "if count > len(t.Alts) {"},
	{"a list type takes any value", "action_exec.go", "actionConforms", "if !(shape == 6) {", "if false {"},
	{"a tuple type takes a list value", "action_exec.go", "actionConforms", "if !(shape == 5) {", "if !(shape == 5 || shape == 6) {"},
	{"an optional type does not take nil", "action_exec.go", "actionConforms", "return kx == 0 || p.actionConforms(t.Elem, x)", "return p.actionConforms(t.Elem, x)"},
	{"a join takes the value of no alternative", "action_exec.go", "actionConforms", "if p.actionConforms(t.Alts[i], x) {", "if !p.actionConforms(t.Alts[i], x) {"},
	{"a named type takes any value", "action_exec.go", "actionConforms", "return n.Type() == t.Name", "return true"},
	{"a node whose value is a list has any elements", "action_exec.go", "actionValueConforms", "if !(shape == 2) {", "if false {"},
	{"a box is not opened by a cast", "action_exec.go", "actionConforms", "x := unboxed(v)", "x := v"},
}

// TestPrintedOperatorMutants is the proof that TestEngineValueConformance, TestEngineValueBoxedOperands,
// TestEngineValueMemberTable, TestEngineValueBuiltinTable, TestEngineValueLengthUnits and TestEngineValueStructTable hold the printed code of the operators,
// the member reads, the built-in functions, the conversions and the ranges of structs of action expressions in the engine: with a source overlay of each mutant, one
// of them fails.
func TestPrintedOperatorMutants(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	root := codegentest.ModuleRoot()
	broken := make([]string, len(engineMutants))
	index := make([]int, len(engineMutants))
	var batch []mutanttest.Mutant
	for i, m := range engineMutants {
		path, src := mutanttest.Source(t, "internal/runtime/state/"+m.file)
		mutated, ok := codegentest.MutateFunc(string(src), m.fn, m.old, m.new)
		index[i] = len(batch)
		if !ok {
			broken[i] = fmt.Sprintf("%s has no %s with %q", m.file, m.fn, m.old)
			continue
		}
		batch = append(batch, mutanttest.Mutant{Replace: map[string]string{path: mutated}, Pkgs: []string{"./internal/runtime/state"},
			Run: "^TestEngineValue(Conformance|BoxedOperands|EqualCharges|MemberTable|BuiltinTable|LengthUnits|StructTable|HullTable|IterTable|ItemAtOpensBoxes|PushTable|CastTable)$"})
	}
	// -vet=off (in the Batch): the verdict reads what the tests report, and vet would only analyze the mutated code
	// again. The mutants build together, as one mutant schema (mutanttest.Batch).
	b := mutanttest.NewBatch(t, root, append(os.Environ(), "GOFLAGS="), nil, batch)
	for i, m := range engineMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			if broken[i] != "" {
				t.Fatal(broken[i])
			}
			out, err := b.Test(index[i])
			if err == nil {
				t.Fatalf("the value tests of the engine pass with the mutant %q:\n%s", m.name, out)
			}
			if !strings.Contains(string(out), "--- FAIL: TestEngineValue") {
				t.Fatalf("the mutant %q fails no value test of the engine:\n%s", m.name, out)
			}
		})
	}
}
