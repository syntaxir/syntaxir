package codegentest

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// ActionKind says how a generated action expression is evaluated.
type ActionKind int

const (
	// ValueAction puts the action expression in the value action of the rule main.
	ValueAction ActionKind = iota
	// PredicateAction puts a bool action expression in a predicate that the rule main tries before an optional "b".
	PredicateAction
)

// ActionCase is a generated grammar that evaluates one action expression.
type ActionCase struct {
	Case
	Action string
	// Type is the declared Type of the rule main of a ValueAction case, "" for a PredicateAction case.
	Type string
	Kind ActionKind
	// Lit is the text of the literal that the rule main matches, and $x captures: "a" in the action expressions of
	// ActionCases, and "a", "é" or "😀" in those of ActionCasesWide, whose positions differ in the two units.
	Lit string
	// Captures returns the captures of the rule main after it matched the input, for the reference
	// evaluator of action expressions; nil for the generated cases, whose rule captures only $x, the literal. The
	// fixed cases write them by hand from their rules, so that the reference evaluator reads the
	// captures that the grammar defines without a parser of its own.
	Captures func(in string) map[string]Capture
	// Recovers is set if the rule recovers from an error on some input, so that its parse reports the
	// syntax errors it recovered from besides the value.
	Recovers bool
	// MissingTokens is set if the rule inserts a missing token on some input: only the engine's closure
	// backend parses it, with the insertion of missing tokens enabled.
	MissingTokens bool
}

// Capture is a node that a capture holds after a match, with its range in characters of the input: a
// string node, a tuple node or a list node of Elems, with its own captures. The zero Capture is a capture
// that did not match, which reads as nil.
type Capture struct {
	Shape    CaptureShape
	From, To int
	Elems    []Capture
	Caps     map[string]Capture
	// Expected is the literal that the token of a MissingCapture inserted. Diagnostic is the message of the
	// error that an ErrorCapture or a MissingCapture recovered, without its position, when the parse records
	// expectations.
	Expected, Diagnostic string
}

// CaptureShape is the shape of the node of a Capture.
type CaptureShape int

// Shapes of captured nodes.
const (
	Unmatched CaptureShape = iota
	StringCapture
	TupleCapture
	ListCapture
	// ErrorCapture is an error value of recovery over [From, To).
	ErrorCapture
	// MissingCapture is the error value of a missing token at From, whose range [From, To) is empty.
	MissingCapture
)

// litCap is the string node of the literal at character i.
func litCap(i int) Capture { return Capture{Shape: StringCapture, From: i, To: i + 1} }

// litsCap is the node of the repetition of the literal over the characters [from, to).
func litsCap(from, to int) Capture {
	c := Capture{Shape: ListCapture, From: from, To: to}
	for i := from; i < to; i++ {
		c.Elems = append(c.Elems, litCap(i))
	}
	return c
}

// lits is the number of characters of the input in, where the literal lit repeats from the start.
func lits(in, lit string) int {
	n := 0
	for strings.HasPrefix(in, lit) {
		in, n = in[len(lit):], n+1
	}
	return n
}

// xyzCaptures are the captures of x:lit y:lit z:"c"? on an input of the literal: z does not match.
func xyzCaptures(string) map[string]Capture {
	return map[string]Capture{"x": litCap(0), "y": litCap(1)}
}

// xyzRsCaptures are the captures of x:lit y:lit z:"c"? rs:(w:lit)*: rs is the list of the tuple nodes of the
// literals after y, each of one element, captured as w.
func xyzRsCaptures(lit string) func(string) map[string]Capture {
	return func(in string) map[string]Capture {
		n := lits(in, lit)
		rs := Capture{Shape: ListCapture, From: 2, To: n}
		for i := 2; i < n; i++ {
			rs.Elems = append(rs.Elems, Capture{Shape: TupleCapture, From: i, To: i + 1, Elems: []Capture{litCap(i)},
				Caps: map[string]Capture{"w": litCap(i)}})
		}
		return map[string]Capture{"x": litCap(0), "y": litCap(1), "rs": rs}
	}
}

// actionTypes are the Types of the values a generated action expression may have.
var actionTypes = []string{"int", "string", "bool", "[]int", "[]string", "R", "(int, string)"}

// actionPrelude declares the struct type of the generated grammars.
const actionPrelude = "type R = { A int; B string; C bool }\n@start\n"

// widePrelude declares the struct types of the grammars of ActionCasesWide: R has a field of each kind
// whose zero value is not a scalar, which a constructor may leave unset: a list, a tuple, an optional
// string and a struct, which holds a struct in turn, so that an action expression reads fields of structs nested
// more than one level deep.
const widePrelude = "type Leaf = { V int; S string }\n" +
	"type Inner = { N int; Q Leaf }\n" +
	"type R = { A int; B string; C bool; L []int; T (int, string); O *string; In Inner }\n@start\n"

// ActionInputs are the inputs of a generated case: "a" is what the rule main matches; "ab" shows
// whether a predicate holds.
var ActionInputs = []string{"a", "ab"}

// ActionCases returns n well-typed action expressions generated from seed, as grammars. The action expressions read the capture
// $x, a string node of the input "a", and use integer and string literals small enough that
// division by zero, an index out of range, a cast that fails and a nil field often occur. The grammars
// are well typed by construction; a caller drops any that the compiler rejects and counts them.
//
// With unset, a struct constructor may leave fields out. A field that is left out reads as the zero
// value of its Type on every target.
func ActionCases(seed uint64, n int, unset bool) []ActionCase {
	g := &actionGen{rng: rand.New(rand.NewPCG(seed, 0x7e12a)), unset: unset}
	out := make([]ActionCase, 0, n)
	for i := 0; i < n; i++ {
		if i%4 == 3 {
			out = append(out, g.predicateCase(i))
		} else {
			out = append(out, g.actionCase(i))
		}
	}
	return out
}

// ActionCasesWide returns n well-typed action expressions generated from seed like ActionCases, with more forms: strings
// with characters from U+E000 to U+FFFF and outside the Basic Multilingual Plane, a captured literal
// that is not ASCII, tuple indexing, nil lists and nil elements, lists of bools, of structs, of lists
// and of tuples, casts to bool and to a struct, and the errors that only run time can find (the
// comparison of structured values, the operand of && that is not a bool, the filter that gives no
// bool), and a struct R with a list, a tuple, an optional string and a struct field, which an action expression
// reads whether or not the constructor set them. ActionCases is not changed by the forms, so that what the goldens pin stays the same.
func ActionCasesWide(seed uint64, n int, unset bool) []ActionCase {
	g := &actionGen{rng: rand.New(rand.NewPCG(seed, 0x91de5)), unset: unset, wide: true}
	out := make([]ActionCase, 0, n)
	for i := 0; i < n; i++ {
		if i%4 == 3 {
			out = append(out, g.predicateCase(i))
		} else {
			out = append(out, g.actionCase(i))
		}
	}
	return out
}

// wideTypes are the Types of the values an action expression of ActionCasesWide may have, besides actionTypes.
var wideTypes = []string{"[]bool", "[]R", "[][]int", "[](int, string)", "Inner", "Leaf", "*string"}

// wideLits are the literals the rule main may match in an action expression of ActionCasesWide.
var wideLits = []string{"a", "é", "😀"}

func (g *actionGen) actionCase(i int) ActionCase {
	typ, lit := "", "a"
	if g.wide {
		lit = wideLits[g.rng.IntN(len(wideLits))]
		all := append(append([]string{}, actionTypes...), wideTypes...)
		typ = all[g.rng.IntN(len(all))]
	} else {
		typ = actionTypes[g.rng.IntN(len(actionTypes))]
	}
	actionExpr := g.actionExpr(typ, 3, nil)
	return ActionCase{
		Case: Case{Name: fmt.Sprintf("action %d: %s", i, actionExpr),
			Src: fmt.Sprintf("%srule main %s = x:%q \"b\"? $$ => %s\n", g.prelude(), typ, lit, actionExpr), Inputs: []string{lit}},
		Action: actionExpr, Type: typ, Kind: ValueAction, Lit: lit,
	}
}

func (g *actionGen) predicateCase(i int) ActionCase {
	lit := "a"
	if g.wide {
		lit = wideLits[g.rng.IntN(len(wideLits))]
	}
	actionExpr := g.actionExpr("bool", 3, nil)
	return ActionCase{
		Case: Case{Name: fmt.Sprintf("predicate %d: %s", i, actionExpr),
			Src: fmt.Sprintf("%srule main int = x:%q ({%s} \"b\" / \"\") $$ => 1\n", g.prelude(), lit, actionExpr), Inputs: []string{lit, lit + "b"}},
		Action: actionExpr, Kind: PredicateAction, Lit: lit,
	}
}

func (g *actionGen) prelude() string {
	if g.wide {
		return widePrelude
	}
	return actionPrelude
}

type actionGen struct {
	rng   *rand.Rand
	next  int
	unset bool
	wide  bool // the forms of ActionCasesWide
	// noNil is positive while a list that is indexed is generated.
	noNil int
}

// scope maps a lambda parameter to its Type.
type scope []scoped

type scoped struct{ name, typ string }

func (g *actionGen) pick(xs ...string) string { return xs[g.rng.IntN(len(xs))] }

func (g *actionGen) fresh(prefix string) string {
	g.next++
	return fmt.Sprintf("%s%d", prefix, g.next)
}

// vars returns the parameters in scope of the Type.
func (s scope) vars(typ string) []string {
	var out []string
	for _, v := range s {
		if v.typ == typ {
			out = append(out, v.name)
		}
	}
	return out
}

// action expression returns an action expression of Type typ with at most depth levels of nesting.
func (g *actionGen) actionExpr(typ string, depth int, s scope) string {
	if vs := s.vars(typ); len(vs) > 0 && g.rng.IntN(3) == 0 {
		return vs[g.rng.IntN(len(vs))]
	}
	switch typ {
	case "int":
		return g.intAction(depth, s)
	case "string":
		return g.stringAction(depth, s)
	case "bool":
		return g.boolAction(depth, s)
	case "[]int", "[]string", "[]bool", "[]R", "[][]int", "[](int, string)":
		return g.listAction(typ, depth, s)
	case "R":
		return g.structAction(depth, s)
	case "Inner":
		return g.innerAction(depth, s)
	case "Leaf":
		return g.leafAction(depth, s)
	case "*string":
		return g.optionalAction(depth, s)
	case "(int, string)":
		return "(" + g.actionExpr("int", depth-1, s) + ", " + g.actionExpr("string", depth-1, s) + ")"
	}
	panic("actiongen: " + typ)
}

var actionStrings = []string{`""`, `"a"`, `"bc"`, `"é"`, `"😀x"`}

// wideStrings are the strings besides actionStrings in ActionCasesWide: characters from U+E000 to U+FFFF,
// which sort before the characters outside the Basic Multilingual Plane by code point but after them by
// UTF-16 code unit, and such a character.
var wideStrings = []string{"\"\ue000\"", "\"\uffff\"", "\"\U0001F600\"", "\"a\uffff\"", "\"\U00010000\""}

func (g *actionGen) intAction(depth int, s scope) string {
	if depth <= 0 {
		switch g.rng.IntN(6) {
		case 0:
			return "len(text($x))"
		case 1:
			return "$x.Start"
		case 2:
			return "$x.End"
		case 3:
			// Beyond 2^53, where TypeScript numbers lose integers, and at the 64-bit limits.
			return g.pick("9223372036854775807", "4611686018427387904", "9007199254740993", "3037000500")
		}
		return fmt.Sprint(g.rng.IntN(7) - 2)
	}
	d := depth - 1
	if g.wide {
		switch g.rng.IntN(20) {
		case 6:
			return "len((" + g.actionExpr("R", d, s) + ").L)"
		case 7:
			return "(" + g.actionExpr("R", d, s) + ").T[0]"
		case 8:
			return "(" + g.actionExpr("R", d, s) + g.pick(").In.N", ").In.Q.V")
		case 9:
			return "len((" + g.actionExpr("R", d, s) + ").O)"
		case 0:
			return "(" + g.actionExpr("(int, string)", d, s) + ")[0]"
		case 1:
			return "len(" + g.source(g.pick("[]bool", "[]R", "[][]int", "[](int, string)"), d, s) + ")"
		case 5:
			// A nil element makes the Type of the list optional, which only len and the rest of the
			// action expression that does not read an element accept.
			return "len(list(" + g.pick("nil", "nil, 1", "1, nil, nil", "nil, \"a\"", "list(nil), nil", "nil, R{A: 1}") + "))"
		case 2:
			return "len(nil)"
		case 3:
			return "(" + g.indexed("[][]int", d, s) + ")[" + g.actionExpr("int", d, s) + "][" + g.actionExpr("int", d, s) + "]"
		case 4:
			return "(" + g.indexed("[]R", d, s) + ")[" + g.actionExpr("int", d, s) + "].A"
		}
	}
	switch g.rng.IntN(16) {
	case 0, 1, 2:
		return "(" + g.actionExpr("int", d, s) + " " + g.pick("+", "-", "*") + " " + g.actionExpr("int", d, s) + ")"
	case 3, 4:
		return "(" + g.actionExpr("int", d, s) + " " + g.pick("/", "%") + " " + g.actionExpr("int", d, s) + ")"
	case 5:
		return "-(" + g.actionExpr("int", d, s) + ")"
	case 6:
		return "len(" + g.actionExpr("string", d, s) + ")"
	case 7:
		return "len(" + g.source(g.pick("[]int", "[]string"), d, s) + ")"
	case 8:
		return g.index("int", d, s)
	case 9, 10:
		return g.fold("int", "int", d, s)
	case 11:
		return g.fold("int", "string", d, s)
	case 12:
		return "(" + g.actionExpr("R", d, s) + ").A"
	case 13:
		return "len(" + g.pick("$x", "text($x)", "list()", "concat()") + ")"
	case 14:
		return g.cast("int", d, s)
	}
	return g.intAction(0, s)
}

// index returns an element of a list of Type typ: list[i].
func (g *actionGen) index(typ string, d int, s scope) string {
	return "(" + g.indexed("[]"+typ, d, s) + ")[" + g.actionExpr("int", d, s) + "]"
}

// emptyList returns an empty list that a function reads: list(), or in ActionCasesWide nil too, which
// is the empty list as well. Where a list is the value of the action expression, nil has no Type that fits.
func (g *actionGen) emptyList() string {
	if g.wide && g.rng.IntN(2) == 0 {
		return "nil"
	}
	return "list()"
}

// source returns a list action expression of Type typ for len, which is nil sometimes.
func (g *actionGen) source(typ string, d int, s scope) string {
	if g.wide && g.rng.IntN(8) == 0 {
		return "nil"
	}
	return g.actionExpr(typ, d, s)
}

// indexed returns a list action expression of Type typ that is not nil.
func (g *actionGen) indexed(typ string, d int, s scope) string {
	g.noNil++
	defer func() { g.noNil-- }()
	return g.actionExpr(typ, d, s)
}

// fold returns foldl/foldr of the element Type elem into an accumulator of Type acc.
func (g *actionGen) fold(acc, elem string, d int, s scope) string {
	a, e := g.fresh("a"), g.fresh("e")
	if g.rng.IntN(6) == 0 {
		// An empty list has no element Type, so the body may not use the element.
		body := g.actionExpr(acc, d, append(s[:len(s):len(s)], scoped{a, acc}))
		return fmt.Sprintf("%s(%s, %s, (%s, %s) => %s)", g.pick("foldl", "foldr"), g.actionExpr(acc, d, s), g.emptyList(), a, e, body)
	}
	body := g.actionExpr(acc, d, append(s[:len(s):len(s)], scoped{a, acc}, scoped{e, elem}))
	return fmt.Sprintf("%s(%s, %s, (%s, %s) => %s)", g.pick("foldl", "foldr"), g.actionExpr(acc, d, s), g.actionExpr("[]"+elem, d, s), a, e, body)
}

// cast returns an element of a list that mixes Types, cast to typ: the list holds an action expression of typ and a
// value of the other Type, so that the cast is well typed and may fail.
func (g *actionGen) cast(typ string, d int, s scope) string {
	other := `"a"`
	if typ == "string" || typ == "bool" {
		other = "1"
	}
	items := []string{g.actionExpr(typ, d, s), other}
	for range g.rng.IntN(2) {
		if g.rng.IntN(3) == 0 {
			items = append(items, other)
		} else {
			items = append(items, g.actionExpr(typ, d, s))
		}
	}
	g.rng.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	return fmt.Sprintf("list(%s)[%d].(%s)", strings.Join(items, ", "), g.rng.IntN(len(items)+1), typ)
}

func (g *actionGen) stringAction(depth int, s scope) string {
	if depth <= 0 {
		if g.rng.IntN(4) == 0 {
			return "text($x)"
		}
		if g.wide && g.rng.IntN(3) == 0 {
			return wideStrings[g.rng.IntN(len(wideStrings))]
		}
		return actionStrings[g.rng.IntN(len(actionStrings))]
	}
	d := depth - 1
	if g.wide && g.rng.IntN(10) == 0 {
		return "(" + g.actionExpr("(int, string)", d, s) + ")[1]"
	}
	if g.wide && g.rng.IntN(10) == 0 {
		return "(" + g.actionExpr("R", d, s) + ").T[1]"
	}
	if g.wide && g.rng.IntN(10) == 0 {
		return "(" + g.actionExpr("R", d, s) + ").In.Q.S"
	}
	if g.wide && g.rng.IntN(12) == 0 {
		return "(" + g.actionExpr("Leaf", d, s) + ").S"
	}
	switch g.rng.IntN(9) {
	case 0, 1, 2:
		return "(" + g.actionExpr("string", d, s) + " + " + g.actionExpr("string", d, s) + ")"
	case 3:
		return g.index("string", d, s)
	case 4, 5:
		return g.fold("string", "string", d, s)
	case 6:
		return "(" + g.actionExpr("R", d, s) + ").B"
	case 7:
		return g.cast("string", d, s)
	}
	return g.stringAction(0, s)
}

func (g *actionGen) boolAction(depth int, s scope) string {
	if depth <= 0 {
		return g.pick("true", "false", "($x != nil)", "($x == nil)")
	}
	d := depth - 1
	if g.wide {
		switch g.rng.IntN(12) {
		case 0:
			return g.cast("bool", d, s)
		case 1:
			// Structured values and values of different kinds have no equality: the error says so, and
			// how to compare them. The checker accepts the operands only if their Types are not known,
			// like an element of a list that mixes Types.
			return g.pick("(list(R{A: 1}, 1)[0] == list(R{A: 1}, 1)[0])", "(list(list(1), 1)[0] != list(list(1), 1)[0])",
				"(list($x, 1)[0] == list($x, 1)[0])", "(list(($x, 1), 1)[0] == list(($x, 1), 1)[0])",
				"(list(1, \"a\")[0] == list(1, \"a\")[1])", "(list(1, true)[1] != list(1, true)[0])",
				"(list(1, \"a\")[1] == list(1, \"a\")[1])", "(list() != nil)", "(list(nil)[0] == list(nil)[0])")
		case 6:
			return "(" + g.indexed("[]bool", d, s) + ")[" + g.actionExpr("int", d, s) + "]"
		case 7:
			return "((" + g.actionExpr("R", d, s) + ").O " + g.pick("==", "!=") + " nil)"
		}
	}
	switch g.rng.IntN(12) {
	case 0, 1:
		return "(" + g.actionExpr("int", d, s) + " " + g.pick("==", "!=", "<", "<=", ">", ">=") + " " + g.actionExpr("int", d, s) + ")"
	case 2, 3:
		return "(" + g.actionExpr("string", d, s) + " " + g.pick("==", "!=", "<", "<=", ">", ">=") + " " + g.actionExpr("string", d, s) + ")"
	case 4:
		return "(" + g.actionExpr("bool", d, s) + " " + g.pick("==", "!=") + " " + g.actionExpr("bool", d, s) + ")"
	case 5:
		return "!" + g.actionExpr("bool", d, s)
	case 6, 7:
		return "(" + g.actionExpr("bool", d, s) + " " + g.pick("&&", "||") + " " + g.actionExpr("bool", d, s) + ")"
	case 8:
		return g.fold("bool", "int", d, s)
	case 9:
		return "(" + g.actionExpr("R", d, s) + ").C"
	case 10:
		return "(" + g.actionExpr(g.pick("int", "string", "[]int"), d, s) + " " + g.pick("==", "!=") + " nil)"
	}
	return g.boolAction(0, s)
}

func (g *actionGen) listAction(typ string, depth int, s scope) string {
	elem := strings.TrimPrefix(typ, "[]")
	other := map[string]string{"int": "string", "string": "int", "bool": "int", "R": "string", "[]int": "int", "(int, string)": "int"}[elem]
	if depth <= 0 {
		var items []string
		for range 1 + g.rng.IntN(3) {
			items = append(items, g.actionExpr(elem, 0, s))
		}
		return "list(" + strings.Join(items, ", ") + ")"
	}
	d := depth - 1
	if g.wide && typ == "[]int" && g.rng.IntN(8) == 0 {
		return "(" + g.actionExpr("R", d, s) + ").L"
	}
	switch g.rng.IntN(8) {
	case 0, 1:
		var items []string
		for range 1 + g.rng.IntN(3) {
			items = append(items, g.actionExpr(elem, d, s))
		}
		return "list(" + strings.Join(items, ", ") + ")"
	case 2, 3:
		e := g.fresh("e")
		src := g.pick(elem, other)
		if g.wide && g.rng.IntN(6) == 0 {
			// The elements of an empty list have no Type, so the function does not read one.
			return fmt.Sprintf("map(%s, (%s) => %s)", g.emptyList(), e, g.actionExpr(elem, d, s))
		}
		return fmt.Sprintf("map(%s, (%s) => %s)", g.actionExpr("[]"+src, d, s), e, g.actionExpr(elem, d, append(s[:len(s):len(s)], scoped{e, src})))
	case 4:
		e := g.fresh("e")
		return fmt.Sprintf("filter(%s, (%s) => %s)", g.actionExpr(typ, d, s), e, g.actionExpr("bool", d, append(s[:len(s):len(s)], scoped{e, elem})))
	case 5:
		var args []string
		for range 1 + g.rng.IntN(3) {
			args = append(args, g.actionExpr(typ, d, s))
		}
		return "concat(" + strings.Join(args, ", ") + ")"
	case 6:
		return "concat(" + g.actionExpr(typ, d, s) + ", map(" + g.actionExpr("[]"+other, d, s) + ", (" + g.fresh("e") + ") => " + g.actionExpr(elem, 0, s) + "))"
	}
	return g.listAction(typ, 0, s)
}

func (g *actionGen) structAction(depth int, s scope) string {
	d := depth - 1
	if depth <= 0 {
		d = 0
	}
	if g.wide && depth > 0 && g.rng.IntN(8) == 0 {
		return g.cast("R", d, s)
	}
	var fields []string
	if !g.unset || g.rng.IntN(5) != 0 { // a field left unset reads as its zero value
		fields = append(fields, "A: "+g.actionExpr("int", d, s))
	}
	if !g.unset || g.rng.IntN(5) != 0 {
		fields = append(fields, "B: "+g.actionExpr("string", d, s))
	}
	if !g.unset || g.rng.IntN(5) != 0 {
		fields = append(fields, "C: "+g.actionExpr("bool", d, s))
	}
	if g.wide {
		if !g.unset || g.rng.IntN(5) != 0 {
			fields = append(fields, "L: "+g.actionExpr("[]int", d, s))
		}
		if !g.unset || g.rng.IntN(5) != 0 {
			fields = append(fields, "T: "+g.actionExpr("(int, string)", d, s))
		}
		if !g.unset || g.rng.IntN(5) != 0 {
			fields = append(fields, "O: "+g.actionExpr("string", d, s))
		}
		if !g.unset || g.rng.IntN(5) != 0 {
			fields = append(fields, "In: "+g.actionExpr("Inner", d, s))
		}
	}
	return "R{" + strings.Join(fields, ", ") + "}"
}

// innerAction returns an action expression of Type Inner: a constructor, which may leave fields unset, or the field In
// of a struct R.
func (g *actionGen) innerAction(depth int, s scope) string {
	d := depth - 1
	if depth <= 0 {
		return "Inner{N: " + g.actionExpr("int", 0, s) + ", Q: " + g.actionExpr("Leaf", 0, s) + "}"
	}
	if g.rng.IntN(4) == 0 {
		return "(" + g.actionExpr("R", d, s) + ").In"
	}
	var fields []string
	if !g.unset || g.rng.IntN(5) != 0 {
		fields = append(fields, "N: "+g.actionExpr("int", d, s))
	}
	if !g.unset || g.rng.IntN(5) != 0 {
		fields = append(fields, "Q: "+g.actionExpr("Leaf", d, s))
	}
	return "Inner{" + strings.Join(fields, ", ") + "}"
}

// leafAction returns an action expression of Type Leaf: a constructor, which may leave fields unset, or the field Q of
// a struct Inner, which is nested two levels below a struct R.
func (g *actionGen) leafAction(depth int, s scope) string {
	d := depth - 1
	if depth <= 0 {
		return "Leaf{V: " + g.actionExpr("int", 0, s) + ", S: " + g.actionExpr("string", 0, s) + "}"
	}
	switch g.rng.IntN(4) {
	case 0:
		return "(" + g.actionExpr("Inner", d, s) + ").Q"
	case 1:
		return "(" + g.actionExpr("R", d, s) + ").In.Q"
	}
	var fields []string
	if !g.unset || g.rng.IntN(5) != 0 {
		fields = append(fields, "V: "+g.actionExpr("int", d, s))
	}
	if !g.unset || g.rng.IntN(5) != 0 {
		fields = append(fields, "S: "+g.actionExpr("string", d, s))
	}
	return "Leaf{" + strings.Join(fields, ", ") + "}"
}

// optionalAction returns an action expression of Type *string, an optional string used as a value: the field O of a
// struct R, which a constructor may leave unset (nil), nil, or a string.
func (g *actionGen) optionalAction(depth int, s scope) string {
	if depth <= 0 {
		return g.pick("nil", g.stringAction(0, s))
	}
	d := depth - 1
	switch g.rng.IntN(4) {
	case 0:
		return "(" + g.actionExpr("R", d, s) + ").O"
	case 1:
		return "nil"
	}
	return g.actionExpr("string", d, s)
}

// MemberActionCases returns fixed action expressions that read every kind of member on the struct types of
// ActionCasesWide: the range of a node, a capture of a node, and each field of R and Inner, set by the
// constructor or left unset so that it reads as its zero value, alone and as the operand of an
// operator, an index or len, and the captures of a node, alone and in the field of a struct. Unlike the
// generated cases, each read is sure to occur.
func MemberActionCases() []ActionCase {
	actions := []struct{ typ, actionExpr string }{
		{"int", "$x.Start"},
		{"int", "$x.End"},
		{"int", "$x.End - $x.Start"},
		{"int", "(R{}).A"},
		{"string", "(R{}).B"},
		{"bool", "(R{}).C"},
		{"[]int", "(R{}).L"},
		{"int", "len((R{}).L)"},
		{"(int, string)", "(R{}).T"},
		{"int", "(R{}).T[0]"},
		{"string", "(R{}).T[1]"},
		{"bool", "(R{}).O == nil"},
		{"Inner", "(R{}).In"},
		{"int", "(R{}).In.N"},
		{"int", "(R{}).In.Q.V"},
		{"string", "(R{}).In.Q.S"},
		{"Leaf", "(R{}).In.Q"},
		{"Leaf", "(Inner{}).Q"},
		{"Leaf", "(R{In: Inner{Q: Leaf{V: 3}}}).In.Q"},
		{"int", "(R{In: Inner{Q: Leaf{V: $x.End}}}).In.Q.V"},
		{"bool", "(R{}).L == nil"},
		{"bool", "(R{L: list()}).L != nil"},
		{"*string", "(R{}).O"},
		{"*string", "(R{O: text($x)}).O"},
		{"int", "(R{A: 7}).A"},
		{"string", "(R{B: text($x)}).B"},
		{"[]int", "(R{L: list(1, 2, 3)}).L"},
		{"int", "(R{L: list($x.Start, 2)}).L[1]"},
		{"(int, string)", "(R{T: ($x.End, \"t\")}).T"},
		{"string", "(R{O: \"o\"}).O.(string)"},
		{"int", "(R{In: Inner{N: $x.End}}).In.N"},
		{"int", "(Inner{}).N"},
		{"int", "list(R{}, R{A: 2})[1].A + len(list(R{}, R{A: 2})[0].L)"},
		{"[]int", "map(list(R{}, R{L: list(4)}), (r) => len(r.L))"},
		{"int", "foldl(0, list(R{In: Inner{N: 1}}, R{}), (a, r) => a + r.In.N + r.T[0])"},
	}
	var out []ActionCase
	for _, a := range actions {
		for _, lit := range []string{"a", "😀"} {
			out = append(out, ActionCase{
				Case: Case{Name: "member: " + a.actionExpr,
					Src: fmt.Sprintf("%srule main %s = x:%q \"b\"? $$ => %s\n", widePrelude, a.typ, lit, a.actionExpr), Inputs: []string{lit}},
				Action: a.actionExpr, Type: a.typ, Kind: ValueAction, Lit: lit,
				Captures: func(string) map[string]Capture { return map[string]Capture{"x": litCap(0)} },
			})
		}
	}
	// A capture of a node, matched and unmatched, read directly and by a predicate.
	capture := widePrelude + "rule main string = x:sub $$ => text($x.$y) + text($x.$z)\nrule sub = y:\"a\" z:\"b\"?\n"
	// $x is the tuple node of the sequence of sub, which carries its captures y and z.
	subCaptures := func(in string) map[string]Capture {
		y, z := litCap(0), Capture{}
		if in == "ab" {
			z = litCap(1)
		}
		return map[string]Capture{"x": {Shape: TupleCapture, From: 0, To: len(in), Elems: []Capture{y, z},
			Caps: map[string]Capture{"y": y, "z": z}}}
	}
	out = append(out, ActionCase{Case: Case{Name: "member: capture", Src: capture, Inputs: []string{"a", "ab"}},
		Action: "text($x.$y) + text($x.$z)", Type: "string", Kind: ValueAction, Lit: "a", Captures: subCaptures})
	// The same reads in the field of a struct, whose literal a target may make another way than the reads
	// alone.
	inStruct := "(R{B: text($x.$y) + text($x.$z)}).B"
	out = append(out, ActionCase{Case: Case{Name: "member: capture in a struct", Src: strings.Replace(capture, "text($x.$y) + text($x.$z)", inStruct, 1),
		Inputs: []string{"a", "ab"}}, Action: inStruct, Type: "string", Kind: ValueAction, Lit: "a", Captures: subCaptures})
	return out
}

// convertPrelude declares the named node types of ConvertActionCases: two of string nodes, one of the tuple
// node of a sequence of two literals and one of the list node of a repetition of a literal.
const convertPrelude = "type N = node[string]\ntype M = node[string]\ntype P = node[(node[string], node[string])]\n" +
	"type Ls = node[[]node[string]]\n@start\n"

// ConvertActionCases returns fixed action expressions that convert a value to a named node type: a string node, a node
// of another named node type, a node that already has the type, a tuple node and a list node, alone and
// as the operand of a member read, len or text, and the value of a rule of the named node type. Each
// conversion is sure to occur.
func ConvertActionCases() []ActionCase {
	// The captures of the three bodies: a literal, a sequence of two and a repetition.
	one := func(string) map[string]Capture { return map[string]Capture{"x": litCap(0)} }
	pair := func(string) map[string]Capture {
		return map[string]Capture{"x": {Shape: TupleCapture, From: 0, To: 2, Elems: []Capture{litCap(0), litCap(1)}}}
	}
	captures := map[string]func(string) func(string) map[string]Capture{
		"x:%[1]q":         func(string) func(string) map[string]Capture { return one },
		"x:(%[1]q %[1]q)": func(string) func(string) map[string]Capture { return pair },
		"x:%[1]q+": func(lit string) func(string) map[string]Capture {
			return func(in string) map[string]Capture { return map[string]Capture{"x": litsCap(0, lits(in, lit))} }
		},
	}
	actions := []struct{ typ, body, actionExpr string }{
		{"string", "x:%[1]q", "text(N{$x})"},
		{"int", "x:%[1]q", "N{$x}.End - N{$x}.Start"},
		{"string", "x:%[1]q", "N{$x}.Value"},
		{"string", "x:%[1]q", "text(M{N{$x}})"},
		{"int", "x:%[1]q", "len(N{N{$x}})"},
		{"N", "x:%[1]q", "N{$x}"},
		{"int", "x:(%[1]q %[1]q)", "len(P{$x})"},
		{"string", "x:(%[1]q %[1]q)", "text(P{$x}.Value[1])"},
		{"int", "x:%[1]q+", "len(Ls{$x})"},
		{"int", "x:%[1]q+", "len(Ls{Ls{$x}}.Value)"},
	}
	var out []ActionCase
	for _, a := range actions {
		for _, lit := range []string{"a", "😀"} {
			input := lit
			if strings.Contains(a.body, "%[1]q %[1]q") || strings.HasSuffix(a.body, "+") {
				input = lit + lit
			}
			out = append(out, ActionCase{
				Case: Case{Name: "convert: " + a.actionExpr,
					Src: fmt.Sprintf("%srule main %s = %s $$ => %s\n", convertPrelude, a.typ, fmt.Sprintf(a.body, lit), a.actionExpr), Inputs: []string{input}},
				Action: a.actionExpr, Type: a.typ, Kind: ValueAction, Lit: lit, Captures: captures[a.body](lit),
			})
		}
	}
	return out
}

// structPrelude declares the struct types of StructActionCases: S has two fields that hold nodes, an int, a
// struct and a list.
const structPrelude = "type Inner = { N int }\n" +
	"type S = { A *node[string]; B *node[string]; K int; I Inner; L []node[string] }\n@start\n"

// StructActionCases returns fixed action expressions that make a struct value whose fields hold nodes: none, one, two,
// a struct, a list and a node that did not match (nil), as the value of a rule, under a read of a field and
// as elements of a list, where the value shows the range of each struct.
// The range of a struct covers the ranges of the field values that are nodes and is the range of the rule if
// there is none, so each of the cases shows the range logic through the units it charges. The rule captures
// two nodes, of one literal each, and a third that does not match, each on a literal of one byte and one
// beyond the Basic Multilingual Plane.
func StructActionCases() []ActionCase {
	cases := []struct{ typ, actionExpr string }{
		{"S", "S{K: 1}"},
		{"S", "S{A: $y, K: 1}"},
		{"S", "S{A: $x, B: $y}"},
		{"S", "S{A: $y, B: $x}"},
		{"S", "S{A: $z, K: 2}"},
		{"S", "S{A: $z, B: $y}"},
		{"S", "S{I: Inner{N: 1}}"},
		{"S", "S{L: list($y, $x)}"},
		{"S", "S{A: $x, B: $y, K: 3, I: Inner{N: 4}, L: list($x)}"},
		{"int", "S{K: 1}.K"},
		{"int", "S{A: $y, K: 1}.K"},
		{"int", "S{I: Inner{N: 1}}.I.N"},
		{"int", "len(S{L: list($y, $x)}.L)"},
		{"bool", "S{A: $z, K: 2}.A == nil"},
		// A struct that is not the value of the rule keeps the range of its fields, which the list shows.
		{"[]S", "list(S{A: $x, B: $y}, S{B: $y})"},
		{"[]S", "list(S{B: $x}, S{A: $y, K: 1})"},
		{"[]S", "list(S{K: 1}, S{A: $x, B: $z})"},
	}
	var out []ActionCase
	for _, a := range cases {
		for _, lit := range []string{"a", "😀"} {
			out = append(out, ActionCase{
				Case: Case{Name: "struct: " + a.actionExpr,
					Src:    fmt.Sprintf("%srule main %s = x:%[3]q y:%[3]q z:\"c\"? $$ => %[4]s\n", structPrelude, a.typ, lit, a.actionExpr),
					Inputs: []string{lit + lit}},
				Action: a.actionExpr, Type: a.typ, Kind: ValueAction, Lit: lit, Captures: xyzCaptures,
			})
		}
	}
	return out
}

// castPrelude declares the types of CastActionCases: the named node type Num of string nodes, the named
// types Pair and Many of tuple and list nodes, and the struct Inner.
const castPrelude = "type Inner = { N int }\ntype Num = node[string]\ntype Pair = node[(node[string], node[string])]\n" +
	"type Many = node[[]node[string]]\n"

// CastActionCases returns fixed action expressions that cast a value to a type, each with the type it checks against and
// the value it checks. The rule captures a string node $x, a tuple node $y of two string nodes, a list node
// $z of one or more string nodes and an optional node $w that does not match, and each case runs with one and
// two elements of $z. A cast
// charges one step for each element of a list or a tuple it examines, up to the first that fails, so the
// action expressions cast lists and tuples of the values of the rule, and of literals, that are of the type, of another
// type at the first, the middle and the last element, of another length and of another shape, besides
// every other type of a cast: the scalars, an optional, a join, the top type, the nodes of each shape, the
// named node types and a struct.
//
// Each action expression puts the value in a list with a field of a struct of the type, which a cast needs to accept a
// value whose static type it does not know to be of the type; the field is never cast.
func CastActionCases() []ActionCase {
	casts := []struct{ typ, value string }{
		{"int", "1"}, {"int", `"s"`}, {"int", "true"}, {"string", `"s"`}, {"string", "1"}, {"bool", "true"}, {"bool", "1"},
		{"_", "1"}, {"_", `"s"`},
		{"*int", "1"}, {"*int", `"s"`}, {"*Num", "$w"}, {"Num", "$w"}, {"*Num", "$x"},
		{"int | string", "1"}, {"int | string", `"s"`}, {"int | string", "true"}, {"Num | int", "$x"},
		{"node[string]", "$x"}, {"node[string]", "$y"}, {"node[string]", "1"},
		{"node[_]", "$x"}, {"node[_]", "$y"}, {"node[_]", "$z"}, {"node[_]", "1"}, {"node[_]", "$z.Value"},
		{"error", "$x"},
		{"Num", "$x"}, {"Num", "Num{$x}"}, {"Num", "$y"}, {"Num", "1"},
		{"Pair", "$y"}, {"Pair", "Pair{$y}"}, {"Many", "Many{$z}"}, {"Many", "$z"},
		{"Inner", "Inner{N: 1}"}, {"Inner", "Num{$x}"}, {"Inner", "1"},
		{"[]Inner", "list(Inner{N: 1}, Inner{N: 2})"}, {"[]Inner", "list(Inner{N: 1}, 2)"},
		// Nodes whose value has a type.
		{"node[(node[string], node[string])]", "$y"}, {"node[(node[string], int)]", "$y"}, {"node[(int, node[string])]", "$y"},
		{"node[(node[string], node[string], node[string])]", "$y"}, {"node[(node[string], node[string])]", "$x"},
		{"node[[]node[string]]", "$z"}, {"node[[]int]", "$z"}, {"node[[]_]", "$z"}, {"node[[]node[string]]", "$y"},
		{"node[*string]", "$x"}, {"node[*string]", "$y"}, {"node[string | []node[string]]", "$z"},
		{"node[string | []node[string]]", "$y"}, {"node[int]", "$x"},
		// Lists and tuples of values.
		{"[]node[string]", "$z.Value"}, {"[]int", "$z.Value"}, {"[]_", "$z.Value"}, {"[][]int", "$z.Value"},
		{"[]int", "list(1, 2, 3)"}, {"[]int", `list("s", 2, 3)`}, {"[]int", `list(1, "s", 3)`}, {"[]int", `list(1, 2, "s")`},
		{"[]string", "list(1, 2)"}, {"[]_", "list(1, true)"}, {"[]int", "list(1, true)"},
		{"[](int | string)", `list(1, "s", 2)`}, {"[](int | string)", "list(1, true)"}, {"[]*int", "list(1, 2)"},
		{"[][]int", "list(list(1, 2), list(3))"}, {"[][]int", `list(list(1, 2), list(3, "s"))`}, {"[][]int", "list(list(1, 2), 3)"},
		{"[]int", "list(list(1))"}, {"int", "list(1)"},
		{"(Num, Num)", "$y.Value"}, {"(Num, int)", "$y.Value"}, {"(int, Num)", "$y.Value"}, {"(Num, Num, Num)", "$y.Value"},
		{"(Num)", "$y.Value"}, {"(_, _)", "$y.Value"}, {"(Num, Num)", "$z.Value"},
		{"(int, string)", `(1, "a")`}, {"(int, string)", `(1, 2)`}, {"(int, string)", `("a", "b")`},
		{"(int, string)", `(1, "a", true)`}, {"(int, string)", `(1,)`},
		{"(int, []int)", "(1, list(2, 3))"}, {"(int, []int)", `(1, list(2, "s"))`}, {"([]int, string)", `(list(1, 2), "a")`},
		{"(int, (int, string))", `(1, (2, "a"))`}, {"(int, (int, string))", `(1, (2, 3))`},
		{"[](int, string)", `list((1, "a"), (2, "b"))`}, {"[](int, string)", `list((1, "a"), (2, 3))`},
		{"(int, string)", "list(1, 2)"}, {"[]int", "(1, 2)"},
	}
	// The struct F has a field of each type that a case casts to, or an optional one where the type has no
	// zero value.
	var fieldOf = map[string]int{}
	var fields []string
	for _, a := range casts {
		if _, ok := fieldOf[a.typ]; !ok {
			typ := a.typ
			if strings.ContainsAny(typ, "|") || strings.Contains(typ, "node") || strings.Contains(typ, "Num") ||
				strings.Contains(typ, "Pair") || strings.Contains(typ, "Many") || typ == "error" {
				if !strings.HasPrefix(typ, "*") {
					typ = "*(" + typ + ")"
				}
			}
			fieldOf[a.typ] = len(fields)
			fields = append(fields, fmt.Sprintf("F%d %s", len(fields), typ))
		}
	}
	prelude := castPrelude + "type F = { " + strings.Join(fields, "; ") + " }\n@start\n"
	var out []ActionCase
	for _, a := range casts {
		for _, lit := range []string{"a"} {
			actionExpr := fmt.Sprintf("list(%s, (F{}).F%d)[0].(%s)", a.value, fieldOf[a.typ], a.typ)
			out = append(out, ActionCase{
				Case: Case{Name: "cast: " + actionExpr,
					Src:    fmt.Sprintf("%srule main %s = x:%[3]q y:(%[3]q %[3]q) z:%[3]q+ w:\"c\"? $$ => %[4]s\n", prelude, a.typ, lit, actionExpr),
					Inputs: []string{strings.Repeat(lit, 4), strings.Repeat(lit, 5)}},
				Action: actionExpr, Type: a.typ, Kind: ValueAction, Lit: lit, Captures: castCaptures(lit),
			})
		}
	}
	return out
}

// castCaptures are the captures of the rule of CastActionCases, x:lit y:(lit lit) z:lit+ w:"c"?: w does not
// match.
func castCaptures(lit string) func(string) map[string]Capture {
	return func(in string) map[string]Capture {
		return map[string]Capture{"x": litCap(0), "y": {Shape: TupleCapture, From: 1, To: 3, Elems: []Capture{litCap(1), litCap(2)}},
			"z": litsCap(3, lits(in, lit))}
	}
}

// ErrorCastActionCases returns fixed action expressions that cast the value of a capture that holds an error value of
// recovery or a string node, to the types that admit it and to those that do not. The rule captures the
// string node $x and $e, the value of "b" #recover(skip=";"), which is a string node on the input "ab" and an
// error value that covers the ";" on the input "a;". The action expressions compare the cast value with nil, so that a
// successful cast gives true and a failed one the error of the cast, which the action expressions put in a list with a
// field of the type, as CastActionCases does, to cast a value whose static type is a join.
func ErrorCastActionCases() []ActionCase {
	casts := []struct{ typ, value string }{
		{"error", "$e"}, {"*error", "$e"}, {"_", "$e"}, {"error | int", "$e"}, {"node[string] | error", "$e"},
		{"node[string]", "$e"}, {"node[_]", "$e"}, {"string", "$e"}, {"int", "$e"}, {"bool", "$e"}, {"[]_", "$e"},
		{"Num", "$e"}, {"error", "$x"}, {"error", "1"}, {"*error", "1"},
	}
	fieldOf := map[string]int{}
	var fields []string
	for _, a := range casts {
		if _, ok := fieldOf[a.typ]; !ok {
			typ := a.typ
			if (strings.ContainsAny(typ, "|") || strings.Contains(typ, "node") || strings.Contains(typ, "Num") || typ == "error") && !strings.HasPrefix(typ, "*") {
				typ = "*(" + typ + ")"
			}
			fieldOf[a.typ] = len(fields)
			fields = append(fields, fmt.Sprintf("F%d %s", len(fields), typ))
		}
	}
	prelude := "type Num = node[string]\ntype F = { " + strings.Join(fields, "; ") + " }\n@start\n"
	var out []ActionCase
	for _, a := range casts {
		actionExpr := fmt.Sprintf("list(%s, (F{}).F%d)[0].(%s) != nil", a.value, fieldOf[a.typ], a.typ)
		out = append(out, ActionCase{
			Case: Case{Name: "error cast: " + actionExpr,
				Src:    fmt.Sprintf("%srule main bool = x:\"a\" e:(\"b\" #recover(skip=\";\")) $$ => %s\n", prelude, actionExpr),
				Inputs: []string{"ab", "a;"}},
			Action: actionExpr, Type: "bool", Kind: ValueAction, Lit: "a", Recovers: true,
			Captures: func(in string) map[string]Capture {
				e := Capture{Shape: StringCapture, From: 1, To: 2}
				if in == "a;" {
					e.Shape, e.Diagnostic = ErrorCapture, `syntax error: expected "b"`
				}
				return map[string]Capture{"x": litCap(0), "e": e}
			},
		})
	}
	return out
}

// recoveredActions are the action expressions of RecoveredActionCases and MissingActionCases, with the Type of each, over $e,
// a capture whose value is a string node or a recovered value: its range, its value, which a recovered
// value has none of, its text and its length, which is that of nil for a recovered value, read directly,
// as an element of a list and after a cast to the type of recovered values.
var recoveredActions = []struct{ typ, actionExpr string }{
	{"int", "$e.Start"},
	{"int", "$e.End"},
	{"*string", "$e.Value"},
	{"bool", "$e.Value == nil"},
	{"int", "len($e)"},
	{"string", "text($e)"},
	{"int", "len(text($e)) * 10 + len($e)"},
	{"int", "len(list($e)[0])"},
	{"string", "text(list($e)[0])"},
	{"[]bool", "map(list($e), (i) => i.Value == nil)"},
	{"int", "len(concat(list($e.Value)))"},
	{"bool", "$e.(error).Value == nil"},
	{"int", "len($e.(error))"},
	{"string", "text($e.(error))"},
	{"int", "$e.(error).End - $e.(error).Start"},
}

// RecoveredActionCases returns fixed action expressions (recoveredActions) that read the value of a capture that holds an error
// value of recovery or a string node. The rule captures the string node $x and $e, the value of
// "b" #recover(skip=";;"), which is a string node on the input "ab" and an error value that covers the ";;" on
// the input "a;;", whose text and length differ from those of the node.
func RecoveredActionCases() []ActionCase {
	var out []ActionCase
	for _, r := range recoveredActions {
		out = append(out, ActionCase{
			Case: Case{Name: "recovered: " + r.actionExpr,
				Src:    fmt.Sprintf("@start\nrule main %s = x:\"a\" e:(\"b\" #recover(skip=\";;\")) $$ => %s\n", r.typ, r.actionExpr),
				Inputs: []string{"ab", "a;;"}},
			Action: r.actionExpr, Type: r.typ, Kind: ValueAction, Lit: "a", Recovers: true,
			Captures: func(in string) map[string]Capture {
				e := Capture{Shape: StringCapture, From: 1, To: 2}
				if in == "a;;" {
					e = Capture{Shape: ErrorCapture, From: 1, To: 3, Diagnostic: `syntax error: expected "b"`}
				}
				return map[string]Capture{"x": litCap(0), "e": e}
			},
		})
	}
	return out
}

// MissingActionCases returns the fixed action expressions of RecoveredActionCases over a capture that holds a missing token or
// a string node. The rule captures the string node $x and $e, the value of the rule semi, ";" #missing,
// which is a string node on the input "a;" and the missing token inserted at the end of the input "a".
// Only the engine's closure backend parses the rule (MissingTokens).
func MissingActionCases() []ActionCase {
	var out []ActionCase
	for _, r := range recoveredActions {
		out = append(out, ActionCase{
			Case: Case{Name: "missing: " + r.actionExpr,
				Src:    fmt.Sprintf("@start\nrule main %s = x:\"a\" e:semi $$ => %s\nrule semi = \";\" #missing\n", r.typ, r.actionExpr),
				Inputs: []string{"a;", "a"}},
			Action: r.actionExpr, Type: r.typ, Kind: ValueAction, Lit: "a", Recovers: true, MissingTokens: true,
			Captures: func(in string) map[string]Capture {
				e := Capture{Shape: StringCapture, From: 1, To: 2}
				if in == "a" {
					e = Capture{Shape: MissingCapture, From: 1, To: 1, Expected: ";", Diagnostic: `syntax error: expected ";"`}
				}
				return map[string]Capture{"x": litCap(0), "e": e}
			},
		})
	}
	return out
}

// recoveredStructPrelude declares the structs of RecoveredStructActionCases: S holds a value that may be a
// recovered value, alone or with a string node, and T holds an S.
const recoveredStructPrelude = "type S = { F *(node[string] | error); G *node[string] }\ntype T = { S S }\n" +
	"type L = { L []*(node[string] | error) }\n@start\n"

// recoveredStructActions are the action expressions of RecoveredStructActionCases over $x, the string node of the literal at the
// start, and $e, a string node or a recovered value after it: structs that are not the value of the rule, whose
// ranges the encoding shows, and that cover the range of a recovered value in a field as that of a node,
// alone, with $x, nested in a struct and as an element of a list in a field.
var recoveredStructActions = []struct{ typ, actionExpr string }{
	{"[]S", "list(S{F: $e})"},
	{"[]S", "list(S{F: $e, G: $x})"},
	{"[]S", "list(S{G: $x}, S{F: $e})"},
	{"T", "T{S: S{F: $e}}"},
	{"[]L", "list(L{L: list($e)})"},
}

// RecoveredStructActionCases returns the fixed action expressions (recoveredStructActions) that hold the value of $e in a field of a
// struct. Over a recovered value of #recover, the rule captures $x, the literal "é", whose width differs in the two
// position units, and $e, the value of "b" #recover(skip=";;"), which is a string node on the input "éb" and an error
// value that covers ";;" on the input "é;;". With missing set, $e is the value of the rule semi, ";" #missing,
// which is a string node on the input "é;" and the token inserted at the end of the input "é", and only the
// engine's closure backend parses the rule (MissingTokens).
func RecoveredStructActionCases(missing bool) []ActionCase {
	var out []ActionCase
	for _, r := range recoveredStructActions {
		c := ActionCase{
			Case: Case{Name: "recovered struct: " + r.actionExpr,
				Src:    fmt.Sprintf("%srule main %s = x:\"é\" e:(\"b\" #recover(skip=\";;\")) $$ => %s\n", recoveredStructPrelude, r.typ, r.actionExpr),
				Inputs: []string{"éb", "é;;"}},
			Action: r.actionExpr, Type: r.typ, Kind: ValueAction, Lit: "é", Recovers: true,
			Captures: func(in string) map[string]Capture {
				e := Capture{Shape: StringCapture, From: 1, To: 2}
				if in == "é;;" {
					e = Capture{Shape: ErrorCapture, From: 1, To: 3, Diagnostic: `syntax error: expected "b"`}
				}
				return map[string]Capture{"x": litCap(0), "e": e}
			},
		}
		if missing {
			c.Name = "missing struct: " + r.actionExpr
			c.Src = fmt.Sprintf("%srule main %s = x:\"é\" e:semi $$ => %s\nrule semi = \";\" #missing\n", recoveredStructPrelude, r.typ, r.actionExpr)
			c.Inputs = []string{"é;", "é"}
			c.MissingTokens = true
			c.Captures = func(in string) map[string]Capture {
				e := Capture{Shape: StringCapture, From: 1, To: 2}
				if in == "é" {
					e = Capture{Shape: MissingCapture, From: 1, To: 1, Expected: ";", Diagnostic: `syntax error: expected ";"`}
				}
				return map[string]Capture{"x": litCap(0), "e": e}
			}
		}
		out = append(out, c)
	}
	return out
}

// missingFieldActions are the action expressions of MissingFieldActionCases, with the Type of each, over $e, a capture whose value
// is a string node or a missing token: the fields expected and message of the token, read after a cast, which
// fails for the string node, and through a list of the tokens among the value of $e.
var missingFieldActions = []struct{ typ, actionExpr string }{
	{"string", "$e.(missing).expected"},
	{"string", "$e.(missing).message"},
	{"int", "len($e.(missing).expected) * 100 + len($e.(missing).message)"},
	{"[]string", "map(filter(list($e), (i) => i.Value == nil), (i) => i.(missing).expected)"},
	{"[]string", "map(filter(list($e), (i) => i.Value == nil), (i) => i.(missing).message)"},
}

// missingSites are the rules semi of MissingFieldActionCases: each inserts a literal, whose value expected, with
// its escapes decoded and without the suffix of a case-insensitive literal, is the field expected of the token
// and is what the input with matches; diagnostic is the message of the
// error it recovered, without its position, when the parse records expectations: the literal as an expected
// item, with the suffix of a case-insensitive literal, or the message of an enclosing @error.
var missingSites = []struct{ rule, expected, with, diagnostic string }{
	{`";" #missing`, ";", ";", `syntax error: expected ";"`},
	{`"Ab"i #missing`, "Ab", "aB", `syntax error: expected "Ab"i`},
	{`"\té" #missing`, "\té", "\té", `syntax error: expected "\té"`},
	{`(";" #missing) @error("a semicolon")`, ";", ";", "a semicolon"},
}

// MissingFieldActionCases returns the fixed action expressions (missingFieldActions) that read the fields of a missing token. The
// rule captures the string node $x of "é", whose width differs in the two position units, and $e, the value of
// the rule semi of each of missingSites, which is a string node on the input that ends in the spelling of the
// literal and the token inserted at the end of the input "é". Only the engine's closure backend parses the
// rule (MissingTokens).
func MissingFieldActionCases() []ActionCase {
	var out []ActionCase
	for _, site := range missingSites {
		for _, r := range missingFieldActions {
			out = append(out, ActionCase{
				Case: Case{Name: "missing field: " + site.rule + ": " + r.actionExpr,
					Src:    fmt.Sprintf("@start\nrule main %s = x:\"é\" e:semi $$ => %s\nrule semi = %s\n", r.typ, r.actionExpr, site.rule),
					Inputs: []string{"é" + site.with, "é"}},
				Action: r.actionExpr, Type: r.typ, Kind: ValueAction, Lit: "é", Recovers: true, MissingTokens: true,
				Captures: func(in string) map[string]Capture {
					e := Capture{Shape: StringCapture, From: 1, To: 1 + len([]rune(site.with))}
					if in == "é" {
						e = Capture{Shape: MissingCapture, From: 1, To: 1, Expected: site.expected, Diagnostic: site.diagnostic}
					}
					return map[string]Capture{"x": litCap(0), "e": e}
				},
			})
		}
	}
	return out
}

// hullPrelude declares the struct type of HullActionCases: S has a list and a tuple field.
const hullPrelude = "type S = { L []node[string]; T (int, string) }\n@start\n"

// HullActionCases returns fixed action expressions that make a list value or a tuple value: of no element, of nodes in
// either order, of nodes with one that did not match (nil), of ints, strings and bools (which the node
// libraries box in nodes), and of lists and tuples of them, alone, as a field of a struct and under len,
// an index, map and concat. The range of such a value covers the elements that are nodes, which the value
// model does not show, so the cases pin what making the value charges for each element, whatever its kind,
// each on a literal of one byte and one beyond the Basic Multilingual Plane. The rule captures two nodes,
// of one literal each, and a third that does not match.
func HullActionCases() []ActionCase {
	cases := []struct{ typ, actionExpr string }{
		{"int", "len(list())"},
		{"int", "len(list($x))"},
		{"int", "len(list($y, $x))"},
		{"int", "len(list($z))"},
		{"int", "len(list($z, $y))"},
		{"int", "len(list($y, $z, $x))"},
		{"int", "len(list(1))"},
		{"int", "len(list($y, 1))"},
		{"int", `len(list("s", $x))`},
		{"int", "len(list(true, $y, 1))"},
		{"int", "len(list(1, 2, 3, 4))"},
		{"int", "len(list(list($y), $x))"},
		{"int", "len(list(($y, 1), $x))"},
		{"int", "len(list(list(), list(), $z))"},
		{"int", "len(list($x, 1, $y))"},
		{"int", "len(($x, $y, 2))"},
		{"int", "len(($x,))"},
		{"int", "list($x, $y)[1].End"},
		{"int", "($y, $x)[0].Start"},
		{"int", "($z, $y)[1].End"},
		{"int", "(1, $y)[1].End"},
		{"int", `("s", 2, true)[1]`},
		{"int", "(list($y), ($x, 1))[1][0].Start"},
		{"int", "len(S{L: list($y)}.L)"},
		{"int", "len(S{L: list($y, $x), T: (1, \"s\")}.L)"},
		{"int", "S{L: list($y, $x), T: (1, \"s\")}.T[0]"},
		{"int", "len(S{}.L)"},
		{"int", "len(map(list($x, $y), (n) => n.End))"},
		{"int", "len(concat(list($x), list($y, 1)))"},
		{"int", "len(concat(list($x), list($y, $z)))"},
	}
	var out []ActionCase
	for _, a := range cases {
		for _, lit := range []string{"a", "😀"} {
			out = append(out, ActionCase{
				Case: Case{Name: "hull: " + a.actionExpr,
					Src:    fmt.Sprintf("%srule main %s = x:%[3]q y:%[3]q z:\"c\"? $$ => %[4]s\n", hullPrelude, a.typ, lit, a.actionExpr),
					Inputs: []string{lit + lit}},
				Action: a.actionExpr, Type: a.typ, Kind: ValueAction, Lit: lit, Captures: xyzCaptures,
			})
		}
	}
	return out
}

// iterPrelude declares the struct type of IterActionCases: I has an int and a list field.
const iterPrelude = "type I = { N int; L []int }\n@start\n"

// IterActionCases returns fixed action expressions that iterate over a list with map, filter, foldl and foldr: over lists of
// no element, of nodes with one that did not match (nil), of ints, strings, tuples, lists and structs, and over
// a list field left unset; action expressions whose order of iteration shows in the value (foldl and foldr of strings and of
// digits); a lambda that fails part way through, so that the elements made so far are dropped; iterations
// nested in a lambda and in a fused concat; and the map that takes a projected repetition as it is. Each action expression
// is an action on a literal of one byte and one beyond the Basic Multilingual Plane, and a few are predicates,
// whose failure drops what the action expression made. The rule captures two nodes of one literal each, a third that does not
// match and a repetition of two more.
func IterActionCases() []ActionCase {
	actions := []struct{ typ, actionExpr string }{
		{"[]int", "map(list(1, 2, 3), (n) => n * 2)"},
		{"int", "len(map(list(), (n) => n))"},
		{"int", "len(map(list($x, $y, $z), (n) => n))"},
		{"[]string", "map(list($x, $y), (n) => text(n))"},
		{"int", "len(map(I{}.L, (n) => n))"},
		{"[]int", "map(list(list(1), list(2, 3), list()), (l) => len(l))"},
		{"[]int", "map(list(2, 1, 0), (n) => 6 / n)"},
		{"int", "foldl(0, list(1, 2, 3), (a, n) => a * 10 + n)"},
		{"int", "foldr(0, list(1, 2, 3), (a, n) => a * 10 + n)"},
		{"string", `foldl("", list("a", "b", "c"), (a, s) => a + s)`},
		{"string", `foldr("", list("a", "b", "c"), (a, s) => a + s)`},
		{"string", `foldr("", list($x, $y), (a, n) => a + text(n))`},
		{"int", "foldl(0, list(), (a, n) => a + n)"},
		{"int", "foldr(7, list(), (a, n) => a + n)"},
		{"int", "foldl(0, I{L: list(1, 2)}.L, (a, n) => a + n)"},
		{"int", "foldl(0, list((1, 2), (3, 4)), (a, t) => a + t[1])"},
		{"int", "foldl(0, list(1, 0), (a, n) => a + 1 / n)"},
		{"int", "foldr(0, list(0, 1), (a, n) => a + 1 / n)"},
		{"[]int", "filter(list(1, 2, 3, 4), (n) => n % 2 == 0)"},
		{"[]int", "filter(list(1, 2), (n) => false)"},
		{"int", "len(filter(list(), (n) => true))"},
		{"int", "len(filter(list($x, $y), (n) => text(n) != \"\"))"},
		{"[]string", `filter(list("a", "", "b"), (s) => s != "")`},
		{"[]int", "filter(list(1, 0), (n) => 1 / n == 1)"},
		{"int", "len(map(list(1, 2), (n) => len(map(list(n, n), (m) => m + 1))))"},
		{"int", "foldl(0, map(list(1, 2, 3), (n) => n * n), (a, n) => a + n)"},
		{"[]int", "map(list(1, 2), (n) => foldl(n, list(10, 20), (a, m) => a + m + n))"},
		{"int", "foldl(0, map(list(1, 2), (n) => I{N: n}), (a, s) => a + s.N)"},
		{"[]int", "map(filter(list(I{N: 1}, I{}), (s) => s.N > 0), (s) => s.N)"},
		{"int", "len(concat(list(1), map(list(2, 3), (n) => n), concat(list(4), filter(list(5, 6), (n) => n > 5))))"},
		{"[]int", "concat(map(list(1, 2), (n) => n + 1), list(9))"},
		{"[]int", "concat(list(1), map(list(1, 0), (n) => 1 / n))"},
		{"int", "len(map($rs, (e) => e.$w))"},
		{"int", "len(concat(list($x), map($rs, (e) => e.$w)))"},
	}
	predicates := []string{
		"len(map(list($x, $y), (n) => n)) == 2",
		"len(map(list(1, 0), (n) => 1 / n)) == 2",
		"foldl(0, filter(list(1, 2, 3), (n) => n > 1), (a, n) => a + n) == 5",
	}
	const rule = "x:%[3]q y:%[3]q z:\"c\"? rs:(w:%[3]q)*"
	var out []ActionCase
	for _, lit := range []string{"a", "😀"} {
		in := strings.Repeat(lit, 4)
		for _, a := range actions {
			out = append(out, ActionCase{
				Case: Case{Name: "iter: " + a.actionExpr,
					Src:    fmt.Sprintf("%srule main %s = "+rule+" $$ => %[4]s\n", iterPrelude, a.typ, lit, a.actionExpr),
					Inputs: []string{in}},
				Action: a.actionExpr, Type: a.typ, Kind: ValueAction, Lit: lit, Captures: xyzRsCaptures(lit),
			})
		}
		for _, p := range predicates {
			out = append(out, ActionCase{
				Case: Case{Name: "iter predicate: " + p,
					Src:    fmt.Sprintf("%srule main %s = "+rule+" ({%[4]s} \"b\" / \"\") $$ => 1\n", iterPrelude, "int", lit, p),
					Inputs: []string{in, in + "b"}},
				Action: p, Kind: PredicateAction, Lit: lit, Captures: xyzRsCaptures(lit),
			})
		}
	}
	return out
}

// pushPrelude declares the struct type of PushActionCases: P has a list of ints, a list of string nodes and a tuple.
const pushPrelude = "type P = { L []int; N []node[string]; T (int, string) }\n@start\n"

// PushActionCases returns fixed action expressions that push the elements of the lists and tuples they make: list and tuple
// literals of every kind of element, a list field left unset and the tuple and list zero values of a struct
// field; concat over arguments that are not list literals, maps or concats (fields, a repetition, a field
// left unset), which takes their elements as they are, and over such literals, which it fuses, nested;
// a concat of more elements (BIG, 130 ints) than a target that bounds the work charges in one piece; and a list or concat
// whose later argument fails, so that the elements pushed so far are dropped. Each action expression is an action on a
// literal of one byte and one beyond the Basic Multilingual Plane, and a few are predicates, whose failure
// drops what the action expression pushed. The rule captures two nodes of one literal each, a third that does not match
// and a repetition of two more.
func PushActionCases() []ActionCase {
	actions := []struct{ typ, actionExpr string }{
		{"[]int", "list(1, 2, 3)"},
		{"[]string", `list("a", "", "b")`},
		{"[]bool", "list(true, false)"},
		{"int", "len(list($x, $z, $y))"},
		{"int", `len(list((1, "a"), (2, "b")))`},
		{"int", "len(list(P{}, P{L: list(1)}))"},
		{"int", `len(($x, $z, 1, "s", true, list(2), P{}))`},
		{"int", "len(P{}.T)"},
		{"int", "len(P{}.L) + len(P{}.N)"},
		{"int", "len(concat(P{}.L, P{L: list(1, 2)}.L))"},
		{"int", "len(concat($rs, $rs))"},
		{"int", "len(concat(P{N: list($x, $y)}.N, list($z)))"},
		{"[]int", "concat(list(1), P{L: list(2, 3)}.L, list())"},
		{"[]int", "concat(concat(list(1), list(2)), concat(list(3), map(list(4), (n) => n)))"},
		{"int", "len(concat(P{L: list(BIG)}.L, list(1)))"},
		{"int", "len(concat(list(BIG), list(1)))"},
		{"[]int", "list(1, 2, 3 / len(list()))"},
		{"[]int", "concat(P{L: list(1)}.L, list(2, 1 / len(P{}.L)))"},
		{"int", "len(map(list(1, 2), (n) => (n, list(n), P{L: list(n)})))"},
	}
	predicates := []string{
		"len(concat(list($x), P{N: list($y)}.N, $rs)) == 4",
		"len(list(1, 2 / len(list()))) == 2",
		`len(($x, 1, "s")) == 3`,
	}
	const rule = "x:%[3]q y:%[3]q z:\"c\"? rs:(w:%[3]q)*"
	big := strings.TrimSuffix(strings.Repeat("1, ", 130), ", ")
	var out []ActionCase
	for _, lit := range []string{"a", "😀"} {
		in := strings.Repeat(lit, 4)
		for _, a := range actions {
			actionExpr := strings.ReplaceAll(a.actionExpr, "BIG", big)
			out = append(out, ActionCase{
				Case: Case{Name: "push: " + actionExpr,
					Src:    fmt.Sprintf("%srule main %s = "+rule+" $$ => %[4]s\n", pushPrelude, a.typ, lit, actionExpr),
					Inputs: []string{in}},
				Action: actionExpr, Type: a.typ, Kind: ValueAction, Lit: lit, Captures: xyzRsCaptures(lit),
			})
		}
		for _, p := range predicates {
			out = append(out, ActionCase{
				Case: Case{Name: "push predicate: " + p,
					Src:    fmt.Sprintf("%srule main %s = "+rule+" ({%[4]s} \"b\" / \"\") $$ => 1\n", pushPrelude, "int", lit, p),
					Inputs: []string{in, in + "b"}},
				Action: p, Kind: PredicateAction, Lit: lit, Captures: xyzRsCaptures(lit),
			})
		}
	}
	return out
}

// ScalarActionCases returns fixed action expressions of operators over scalars whose outcome depends on how a target
// evaluates them rather than on the values alone: a division by zero that fails the action expression, a logical
// operator whose right operand would fail if it were evaluated when the left one decides, equalities
// of two texts, and the arithmetic, comparison and unary operators. Each action expression is an action on a literal of one byte and one beyond the Basic Multilingual
// Plane, followed by "c", and a few are predicates, which guard a "b" after it. The rule captures the
// literal and the "c".
func ScalarActionCases() []ActionCase {
	actions := []struct{ typ, actionExpr string }{
		{"int", "1 / ($x.End - $x.End)"},
		{"int", "$y.Start - 1 / $x.Start"},
		{"bool", "$x.Start > 0 && 1 / $x.Start > 0"},
		{"bool", "$x.Start == 0 || 1 / $x.Start > 0"},
		{"bool", "$x.Start == 0 && 1 / $x.Start > 0"},
		{"bool", "text($x) == text($y)"},
		{"bool", "text($x) != text($y)"},
		{"bool", "text($x) == text($x)"},
		// Arithmetic, comparison and unary operators whose result differs from that of each neighbouring
		// operator on the literals: a negative dividend shows that / and % truncate toward zero, and
		// equal operands show < apart from <= and >=.
		{"int", "$x.End * ($y.End + 1)"},
		{"int", "(0 - $y.End - 5) / 2"},
		{"int", "(0 - $y.End - 5) % 3"},
		{"int", "($y.End + 7) / 2 - ($y.End + 7) % 3"},
		{"bool", "$x.End < $y.End"},
		{"bool", "$y.End < $x.End"},
		{"bool", "$x.End < $x.End"},
		{"int", "-$y.End"},
		{"int", "-(-($x.End * 3))"},
		{"bool", "!($x.Start < $x.End)"},
		{"bool", "!!($y.End < $x.End)"},
	}
	predicates := []string{
		"text($x) != text($y)",
		"$x.Start > 0 || text($x) == text($y)",
	}
	captures := func(string) map[string]Capture { return map[string]Capture{"x": litCap(0), "y": litCap(1)} }
	var out []ActionCase
	for _, lit := range []string{"a", "😀"} {
		in, rule := lit+"c", fmt.Sprintf("x:%q y:\"c\"", lit)
		for _, a := range actions {
			out = append(out, ActionCase{
				Case: Case{Name: "scalar: " + a.actionExpr,
					Src:    fmt.Sprintf("@start\nrule main %s = %s $$ => %s\n", a.typ, rule, a.actionExpr),
					Inputs: []string{in}},
				Action: a.actionExpr, Type: a.typ, Kind: ValueAction, Lit: lit, Captures: captures,
			})
		}
		for _, p := range predicates {
			out = append(out, ActionCase{
				Case: Case{Name: "scalar predicate: " + p,
					Src:    fmt.Sprintf("@start\nrule main int = %s ({%s} \"b\" / \"\") $$ => 1\n", rule, p),
					Inputs: []string{in, in + "b"}},
				Action: p, Kind: PredicateAction, Lit: lit, Captures: captures,
			})
		}
	}
	return out
}
