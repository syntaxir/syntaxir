package libgo_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// A mutant changes the printed code of the operators of action expressions in one way: in the compositions of
// runtime/action_exec.go, or in the primitives of runtime/action_value.go. Each is in the node library
// and in the typed one, whose compositions are printed apart (the receiver *TParser).
type goMutant struct {
	name, file, fn, old, new string
}

var goMutants = []goMutant{
	{"swapped operands of -", "action_exec.go", "ActionBinary", "any(l.(int) - r.(int))", "any(r.(int) - l.(int))"},
	{"a node is equal to a node", "action_exec.go", "actionEqual", "return false, false\n}", "return true, true\n}"},
	{"an int equals a bool", "action_exec.go", "actionEqual", "if ka != kb {\n\t\treturn false, false\n\t}", "if ka != kb {\n\t\treturn false, true\n\t}"},
	{"an index may be the length", "action_value.go", "intIndex", "k >= n", "k > n"},
	{"the least index is 1", "action_value.go", "intIndex", "k < 0", "k < 1"},
	{"a division by zero succeeds", "action_exec.go", "ActionBinary", "if r.(int) == 0 {", "if false {"},
	{"< is <=", "action_exec.go", "ActionBinary", "intCmp(l, r) < 0", "intCmp(l, r) <= 0"},
	{"strings compare as ints", "action_exec.go", "ActionBinary", "p.strCmp(l, r) < 0", "p.strCmp(l, r) > 0"},
	{"the controlled concatenation drops the right string", "action_value.go", "strAdd", "for _, s := range [...]string{ls, rs} {", "for _, s := range [...]string{ls} {"},
	{"the counted equality ignores the content", "value_spend.go", "TextEqual", "if p.valueUnits(min(len(l), len(r))) {\n\t\treturn l == r", "if p.valueUnits(min(len(l), len(r))) {\n\t\treturn len(l) == len(r)"},
	{"the counted comparison orders a prefix after its extension", "value_spend.go", "compareText", "if p.valueUnits(min(len(l), len(r))) {\n\t\treturn strings.Compare(l, r)", "if p.valueUnits(min(len(l), len(r))) {\n\t\tif len(l) < len(r) && strings.HasPrefix(r, l) {\n\t\t\treturn 1\n\t\t}\n\t\treturn strings.Compare(l, r)"},
	{"the typed string fast path orders < as <=", "budget.go", "compareStrings", "return p.compareText(l, r) < 0, true", "return p.compareText(l, r) <= 0, true"},
	{"the typed string fast path decides == without the budget control", "budget.go", "compareStrings", "case OpEq:\n\t\treturn p.TextEqual(l, r), true", "case OpEq:\n\t\treturn l == r, true"},
	{"the typed string fast path negates ==", "budget.go", "compareStrings", "return p.TextEqual(l, r), true", "return !p.TextEqual(l, r), true"},
	{"a remainder floors", "action_exec.go", "ActionBinary", `any(l.(int) % r.(int))`, `any(func() int { m := l.(int) % r.(int); if m != 0 && (m < 0) != (r.(int) < 0) { m += r.(int) }; return m }())`},
	{"a division floors", "action_exec.go", "ActionBinary", `any(l.(int) / r.(int))`, `any(func() int { q := l.(int) / r.(int); if (l.(int)%r.(int) != 0) && ((l.(int) < 0) != (r.(int) < 0)) { q-- }; return q }())`},
	{"a box is not opened", "action_exec.go", "actionEqual", "a := UnboxValue(l)", "a := l"},
	{"a boxed string compares without the budget control", "action_exec.go", "actionEqual", "a := UnboxValue(l)\n\tb := UnboxValue(r)\n", "a := UnboxValue(l)\n\tb := UnboxValue(r)\n\tif _, boxed := l.(*sxirvalue.Node); boxed {\n\t\tif sa, ok := a.(string); ok {\n\t\t\tif sb, ok := b.(string); ok {\n\t\t\t\treturn sa == sb, true\n\t\t\t}\n\t\t}\n\t}\n"},
	{"! of a bool does not negate", "action_exec.go", "ActionUnary", "return any(!x.(bool)), nil", "return any(x.(bool)), nil"},
	{"a logical operand need not be a bool", "action_exec.go", "ActionLogic", "if valKind(x) != 3 {", "if false {"},
	{"Start reads the value", "action_exec.go", "ActionMember", "if kind == 1 {", "if kind == 0 {"},
	{"an unset field of a struct is nil", "action_exec.go", "ActionMember", "if !st {\n\t\treturn nil, nil\n\t}", "if true {\n\t\treturn nil, nil\n\t}"},
	{"a struct has captures", "action_exec.go", "ActionMember", "if st {\n\t\t\treturn nil, p.actionError(9", "if false {\n\t\t\treturn nil, p.actionError(9"},
	{"a set field of a struct reads as its zero", "action_exec.go", "ActionMember", "if set {\n\t\treturn f, nil\n\t}", "if set && !st {\n\t\treturn f, nil\n\t}"},
	{"a capture keeps its box", "action_exec.go", "ActionMember", "return UnboxValue(v), nil", "return v, nil"},
	{"End is a field", "action_exec.go", "memberKind", "return MemberEnd, name", "return MemberField, name"},
	{"len counts no units", "lib.go", "unitLen", "p.valueUnits(len(s))", "_ = s"},
	{"nil has length 1", "action_exec.go", "ActionLength", "return any(0), nil", "return any(1), nil"},
	{"a string has no length", "action_exec.go", "ActionLength", "if k == 2 {", "if false {"},
	{"a bool is not an element", "action_exec.go", "ActionPush", "if k == 1 || k == 2 || k == 3 {", "if k == 1 || k == 2 {"},
	{"a function is an element", "action_exec.go", "ActionPush", "return p.actionError(11, v, any(fn), 0, 0)", "return nil"},
	{"nil pushes no element", "action_exec.go", "ActionPush", "p.KidStack = append(p.KidStack, nil)\n", ""},
	{"a node pushes no element", "action_exec.go", "ActionPush", "p.KidStack = append(p.KidStack, n)\n", "_ = n\n"},
	{"concat pushes no elements", "action_exec.go", "ActionPushItems", "\t\tp.KidStack = appendSpend(", "\t\t_ = appendSpend("},
	{"concat takes a value that is not a list", "action_exec.go", "ActionPushItems", "if ok {", "if ok || true {"},
	{"concat counts the elements it pushes", "action_exec.go", "ActionPushItems", "p.KidStack = appendSpend(p, p.KidStack, items)", "p.unitsSized(len(items))\n\t\tp.KidStack = appendSpend(p, p.KidStack, items)"},
	{"typed concat counts the elements it pushes", "action_exec.go", "ActionPushItems", "p.KidStack = appendSpend(p.Parser, p.KidStack, items)", "p.unitsSized(len(items))\n\t\tp.KidStack = appendSpend(p.Parser, p.KidStack, items)"},
	{"concat takes any value", "action_exec.go", "ActionItems", "return items, p.actionError(12, v, any(fn), 0, 0)", "return items, nil"},
	{"the text of a string is empty", "action_exec.go", "ActionText", "return x.(string), nil", "return \"\", nil"},
	{"a list value converts", "action_exec.go", "ActionConvert", "if ok {\n\t\tif ", "if ok {\n\t\tif true || "},
	{"a value that does not convert is nil", "action_exec.go", "ActionConvert", "return nil, p.actionError(15, ", "return nil, nil\n\treturn nil, p.actionError(15, "},
	{"a node field value does not cover", "action_exec.go", "ActionCover", "if !ok {\n\t\treturn first", "if !ok && false {\n\t\treturn first"},
	{"the start of the range is the latest", "action_exec.go", "ActionCover", "cs < ns", "cs > ns"},
	{"the end of the range is the earliest", "action_exec.go", "ActionCover", "ce > ne", "ce < ne"},
	{"a later node replaces the range", "action_exec.go", "ActionCover", "if first || cs < ns {", "if true {"},
	{"a struct without a node keeps no range", "action_exec.go", "ActionMade", "if first {", "if false {"},
	{"a struct is not recorded", "action_exec.go", "ActionMade", "c.made(n)", ""},
	{"an element of a hull counts a unit", "action_exec.go", "ActionHull", "for j = 0; j < count; j++ {", "for j = 0; j < count; j++ {\n\t\tp.unitsSized(1)"},
	{"an element that adds no range widens the range", "action_exec.go", "ActionHull", "if has {", "if has || true {"},
	{"the start of a hull is the latest", "action_exec.go", "ActionHull", "es < lo", "es > lo"},
	{"the end of a hull is the earliest", "action_exec.go", "ActionHull", "ee > hi", "ee < hi"},
	{"a later element replaces the range of a hull", "action_exec.go", "ActionHull", "if first || es < lo {", "if true {"},
	{"an empty hull is not at the start of the rule", "action_exec.go", "ActionHull", "lo := rs", "lo := 0"},
	{"foldr passes the elements from the first", "action_exec.go", "ActionItemAt", "if right {", "if false {"},
	{"the folds pass the elements in each other's order", "action_exec.go", "ActionItemAt", "if right {", "if !right {"},
	{"an iteration passes a box", "action_exec.go", "ActionItemAt", "return UnboxNode(items[i])", "return items[i]"},
	{"filter takes a result that is not a bool", "action_exec.go", "ActionFilterKeep", "if valKind(v) != 3 {", "if valKind(v) == 0 {"},
	{"filter drops what it keeps", "action_exec.go", "ActionFilterKeep", "return v.(bool), nil", "return !v.(bool), nil"},
	{"an element of a list counts a unit", "action_exec.go", "actionElemsConform", "for j = 0; j < count; j++ {", "for j = 0; j < count; j++ {\n\t\tp.unitsSized(1)"},
	{"an element of a tuple counts a unit", "action_exec.go", "actionTupleConform", "for j = 0; j < count; j++ {", "for j = 0; j < count; j++ {\n\t\tp.unitsSized(1)"},
	{"a tuple of another length conforms", "action_exec.go", "actionTupleConform", "if count != len(t.Alts) {", "if count > len(t.Alts) {"},
	{"a list type takes any value", "action_exec.go", "ActionConforms", "if !(shape == 6) {", "if false {"},
	{"a tuple type takes a list value", "action_exec.go", "ActionConforms", "if !(shape == 5) {", "if !(shape == 5 || shape == 6) {"},
	{"an optional type does not take nil", "action_exec.go", "ActionConforms", "return kx == 0 || p.ActionConforms(t.Elem, x)", "return p.ActionConforms(t.Elem, x)"},
	{"a join takes the value of no alternative", "action_exec.go", "ActionConforms", "if p.ActionConforms(t.Alts[i], x) {", "if !p.ActionConforms(t.Alts[i], x) {"},
	{"a named type takes the values of other names", "action_exec.go", "ActionConforms", " == t.Name", " != t.Name"},
	{"a node whose value is a list has any elements", "action_exec.go", "actionValueConforms", "if !(shape == 2) {", "if false {"},
	{"a box is not opened by a cast", "action_exec.go", "ActionConforms", "x := UnboxValue(v)", "x := v"},
}

// TestPrintedOperatorMutants is the proof that the value conformance holds the printed code of the
// operators of action expressions and of the member reads in the generated Go library: with the source of each
// mutant, TestValueConformance, TestValueMemberTable, TestValueBuiltinTable, TestValueLengthUnits or TestValueStructTable fails. A mutant that no test notices would be
// one that the model and the code do not both bound.
func TestPrintedOperatorMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	for _, m := range goMutants {
		// The compositions are printed for each runtime; the primitives are plain functions.
		heads := []string{"func (p *Parser) ", "func (p *TParser) "}
		if m.file == "action_value.go" || m.fn == "memberKind" {
			heads = []string{"func "}
		}
		if m.file == "budget.go" || m.file == "value_spend.go" || m.file == "lib.go" || m.fn == "strAdd" {
			heads = []string{"func (p *Parser) "}
		}
		if strings.Contains(m.old, "UnboxValue") || strings.Contains(m.old, "UnboxNode") {
			heads = heads[:1] // a typed value is never boxed
		}
		if strings.Contains(m.old, "(p.Parser,") {
			heads = heads[1:] // the typed library passes its embedded parser
		} else if strings.Contains(m.old, "Spend(p,") {
			heads = heads[:1]
		}
		for _, head := range heads {
			t.Run(m.name+" "+strings.TrimSuffix(strings.TrimPrefix(head, "func "), " "), func(t *testing.T) {
				t.Parallel()
				overlay := mutatedLib(t, m.file, head, m.fn, m.old, m.new)
				// -vet=off: the verdict reads what the tests report, and vet would only analyze the mutated code again.
				out, err := goTestLib(overlay, "-count=1", "-vet=off", "-run", "^TestValue(Conformance|BoxedOperands|BoxedStringCharges|CompareStrings|MemberTable|BuiltinTable|LengthUnits|StructTable|HullTable|IterTable|PushTable|CastTable)$").CombinedOutput()
				if err == nil {
					t.Fatalf("the value tests pass with the mutant %q:\n%s", m.name, out)
				}
				if !strings.Contains(string(out), "--- FAIL: TestValue") {
					t.Fatalf("the mutant %q fails no value test:\n%s", m.name, out)
				}
			})
		}
	}
}

// mutatedLib returns the overlay of the library whose function fn, declared in file with the head
// head ("func (p *Parser) " or "func "), has its first old replaced with new. The library must
// have that function with old.
func mutatedLib(t *testing.T, file, head, fn, old, new string) string {
	t.Helper()
	done := false
	overlay := libOverlay(t, func(name, src string) string {
		if name != file {
			return src
		}
		i := strings.Index(src, head+fn+"(")
		if i < 0 {
			return src
		}
		end := len(src)
		if j := strings.Index(src[i+1:], "\nfunc "); j >= 0 {
			end = i + 1 + j
		}
		out, ok := codegentest.MutateFunc(src[i:end], fn, old, new)
		if !ok {
			return src
		}
		done = true
		return src[:i] + out + src[end:]
	})
	if !done {
		t.Fatalf("the library has no %s%s with %q", head, fn, old)
	}
	return overlay
}
