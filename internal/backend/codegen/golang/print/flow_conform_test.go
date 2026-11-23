package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// The conformance of the Go target to the Flow operations. Each call of a primitive operation that
// the Go printer prints is wrapped in a probe (testdata/flowconform/probe.go, added to a copy of
// the library) that records the parser state around it: the position, the logical compute units
// counted, the height of the stack of gathered values, the recovered errors and the silent depth, with
// the arguments and results that the checks read. The generated parsers of flowConformCases run on
// their inputs, and every recorded call is checked against its operation's declaration:
//
//   - an operation that does not write the position leaves it unchanged;
//   - the units it counts itself, those of the probed calls inside it aside, are those that the
//     event table of the specification assigns to its events (flowUnits);
//   - the check of its case (flowConformChecks) holds: the reads of the input agree with the input
//     model (ref.Input), and the operations on the stack, the recovered errors, the marks and the
//     silent depth change the state as their contracts say.
//
// Every Flow operation has a case: a call of it in the generated parsers of flowConformCases that
// ran, or for flow.declareNames, which has no Go form, its printing as nothing. The operations of
// runValueGrammar returns the source of the grammar of codegentest.RunValueGrammars with the name.
func runValueGrammar(name string) string {
	for _, g := range codegentest.RunValueGrammars {
		if g.Name == name {
			return g.Src
		}
	}
	panic("flow conformance: no run-value grammar " + name)
}

// flowOtherTargets are not printed for Go, which the test checks instead.

// flowConformCase is a grammar, the options it is generated with and the inputs its parser runs on,
// through each of its entries: Go statements that parse in with the options opts and set err, by
// default flowConformParse.
type flowConformCase struct {
	name    string
	src     string
	opts    common.GenOptions
	inputs  []string
	entries []string
	// noLimits generates the parser without limit controls: the ordinary form, whose calls charge
	// nothing.
	noLimits bool
}

const (
	flowConformParse    = "_, err = Parse(in, opts...)"
	flowConformOrdinary = "_, err = Parse(in, ordinary...)"
	// flowConformObserved parses as flowConformParse does, observed by a trace function that records
	// the skipped calls it is told of. Observation runs the traced functions, whose probes nest in
	// those of the tracing, so a call's own units are not checked there; the parse counts the units in
	// total that flowConformParse, the case's first entry, counts.
	flowConformObserved = "_, err = Parse(in, append(opts, observe)...)"
)

// flowConformDocument parses in as a Document, then again after an edit in its middle that adds a
// character, and after one that removes it. Only the last parse, of in, is recorded: the reads of the
// others are of other texts.
const flowConformDocument = `err = func() error {
	d, err := NewDocument(in, sxirruntime.WithExpectations())
	if err != nil {
		return err
	}
	if _, err := d.Parse(); err != nil {
		return err
	}
	at := len(in) / 2
	for at > 0 && in[at-1] != ';' {
		at--
	}
	if err := d.Edit(at, at, "x"); err != nil {
		return err
	}
	if _, err := d.Parse(); err != nil {
		return err
	}
	if err := d.Edit(at, at+1, ""); err != nil {
		return err
	}
	rt.ConfReset()
	_, err = d.Parse()
	return err
}()`

const flowConformLong = "abcdefghijklmnopqrstuvwxyzabcdefghij"

var flowConformCases = []flowConformCase{
	{name: "plain", src: `@start
rule main = ^ item ("," item)* ","? &$ $$
rule item = [a-z]+ / "x" &"y" "y" / !"q" [0-9] / "if"i ws / "\u{fffd}x"
rule ws = [ \n]* $
`, inputs: []string{"abc,x", "ab,7,IF", flowConformLong + ",b", "a,q", "", "a,,", "\ufffdx"},
		entries: []string{flowConformParse, flowConformOrdinary}},
	{name: "typed", src: `type P = { Name string; N int }
@start
rule main = xs:(p:pair ";"?)* tl:tail $$ => map($xs, (e) => e.$p)
rule tail = t:"!"? "."?
rule pair = n:name "=" v:num {len(text($n)) > 0 && len(text($v)) > 0} => P{Name: text($n), N: 1}
rule name = [a-z]+ #atomic
rule num = d:[0-9]+ => text($d)
`, opts: common.GenOptions{Types: true}, inputs: []string{"a=1;bc=23", "a=", flowConformLong + "=1", "", "a=1.", "a=1!."},
		entries: []string{flowConformParse, "_, err = ParseValue(in, opts...)"}},
	{name: "lean", src: `type Item = { Key Word; Vals []int; Sub Sub }
type Sub = { N int }
type Word = node[string]
@start
rule main = items:(it:item -";"?)* tail:(t:num "!")* $$ => map($items, (e) => e.$it)
rule item = (k:key "=" vs:(v:num -","?)+ {len(text($k)) < 40} => Item{Key: $k, Vals: map($vs, (e) => e.$v), Sub: Sub{N: 1}}) #recover(skip=(![;] .)*)
rule key Word = [a-zA-Z0-9_+*/%.]+ #atomic
rule num int = d:[0-9]+ => len(text($d))
`, opts: common.GenOptions{Types: true, TypedOnly: true, Recognize: true}, inputs: []string{"a=1,2;b.c=3", "a=;b=1", flowConformLong + "=1,", "", "a=1;1!22!"},
		entries: []string{"_, err = ParseValue(in, opts...)", "err = Recognize(in, opts...)", "_, err = ParseValue(in, ordinary...)",
			"err = Recognize(in, ordinary...)"}},
	{name: "scanhot", src: `type Word = node[string]
@start
rule main Word = (w:word "!" => $w) / (w:word "?" => $w) / (w:word "=" => $w) / (w:word ";" => $w)
rule word Word = [a-z]+ #atomic
`, opts: common.GenOptions{Types: true, TypedOnly: true, Recognize: true}, inputs: []string{flowConformLong + flowConformLong + ";", flowConformLong + "=", "ab;"},
		entries: []string{"_, err = ParseValue(in, opts...)", "err = Recognize(in, opts...)"}},
	{name: "scanvals", src: `@start
rule main = (w:letters "!" => $w) / (w:letters "?" => $w) / (w:letters "=" => $w) / (w:letters ":" => $w) / (w:letters "," => $w) / (w:letters "." => $w) / (w:letters ";" => $w)
rule letters = d:[a-z]+ => text($d)
`, opts: common.GenOptions{Types: true, TypedOnly: true}, inputs: []string{flowConformLong + flowConformLong + ";", "ab;"},
		entries: []string{"_, err = ParseValue(in, opts...)"}},
	{name: "scanvalsu", src: `@start
rule main = (w:letters "!" => $w) / (w:letters "?" => $w) / (w:letters "=" => $w) / (w:letters ":" => $w) / (w:letters "," => $w) / (w:letters "." => $w) / (w:letters ";" => $w)
rule letters = d:[a-z]+ => text($d)
`, inputs: []string{flowConformLong + flowConformLong + ";", "ab;"}},
	{name: "ordinary", src: `@start
rule main = (stmt ";")* $$
rule stmt = -([a-z] [0-9] [a-z]) "!" / (c:[a-zA-Z0-9_+*/%.] "#" => text($c)) / -kw "(" / -kw "[" / !kw id
rule kw = ([aA] ([bB] ([oO] [rR] [tT] / [sS] [oO] [lL] [uU] [tT] [eE]) / [cC] [tT] [iI] [oO] [nN] / [lL] [lL]) / [cC] ([aA] [lL] [lL] ([eE] [dD])? / [hH] [eE] [cC] [kK]) / [dD] [oO] / [a-e] [xX] / [eE] [nN] [dD] / [fF] [oO] [rR] / "q"? [zZ] [qQ]) ![a-z_]
rule id = [a-z_]+ #atomic
`, noLimits: true, opts: common.GenOptions{Types: true, TypedOnly: true, Recognize: true, Opt: map[string]int{"class-tables": 2}},
		inputs:  []string{"abort(;ab;a1b!;x#;do[;", "call(;", "a1;", ""},
		entries: []string{"_, err = ParseValue(in, ordinary...)", "err = Recognize(in, ordinary...)"}},
	{name: "bind", src: `var k int
@start
rule main = {k := 0} ({k := 1} "w")? !({k := 2} "q") item* end $$
rule item = "x" {k := k + 1} "!" / "x" {k := k + 1} / "y" {k < 3} / ("z" "z") @diagnostic(name="zz")
rule end = ";"?
`, inputs: []string{"xxy;", "wx!xxxy", "zz", "z", "w"},
		opts: common.GenOptions{Types: true}, entries: []string{flowConformParse, "_, err = ParseValue(in, opts...)"}},
	{name: "stream", src: `@start
rule main = (item ";")* #stream $$
rule item = [a-z]+
`, opts: common.GenOptions{Stream: true}, inputs: []string{"ab;c;", "a;b", ""},
		entries: []string{flowConformParse, "err = ParseStream(strings.NewReader(in), func(*Node) error { return nil }, opts...)"}},
	// Insertions of missing tokens, which the parses with a limit run on the tables of the insertion
	// program: under an @error label, backtracked past by an alternative, in a repetition, a lookahead and
	// left recursion, where w grows only through its insertion and reuses a result whose insertion its
	// round made again; the parses without a limit run the parser's tables, which insert nothing.
	{name: "missing", src: `@start
rule main = item* $$
rule item = ("(" [a-z]* ")" #missing) @error("close") ";" #missing / "x" ";" #missing "y" / "x" !(";" #missing) "z" / sum / w
rule sum = sum "+" #missing d / d
rule d = [0-9]
rule w = "<" #missing w ">" / "v"
`, opts: common.GenOptions{Recognize: true}, inputs: []string{"(a;", "(a", "x;y", "xy", "xz", "1+2", "12", "(", "", "v>", "v>>"},
		entries: []string{flowConformParse, "_, err = Parse(in, append(opts, sxirruntime.WithMissingTokens(4))...)",
			"err = Recognize(in, append(opts, sxirruntime.WithMissingTokens(4))...)", "_, err = Parse(in, append(ordinary, sxirruntime.WithMissingTokens(4))...)"}},
	// Actions that make structs inside the value they return, or return a struct they made inside
	// another, so that flow.ctxFinish searches the values the context made.
	{name: "made", src: `type In = { N int }
type Out = { A In; B In; N int }
@start
rule main = xs:(x:item ";")* $$ => map($xs, (e) => e.$x)
rule item = ("o" => Out{A: In{N: 1}, B: In{N: 2}, N: 3}) / ("i" => Out{A: In{N: 1}, B: In{N: 2}, N: 3}.B)
`, opts: common.GenOptions{Types: true}, inputs: []string{"o;i;", "i;o", ""},
		entries: []string{flowConformParse, "_, err = ParseValue(in, opts...)"}},
	{name: "shapes", src: `@start
rule main = item* opt one many many2 $$
rule item = lk:!"q" alt:(x:"a" y:"b" / z:"c") ","?
rule opt = o:"o"? "."?
rule one = c:[0-9]?
rule many = (q:"z"? "w")*
rule many2 = (c:[0-9])*
`, inputs: []string{"ab,ab", "c,c", "q", "ac", "", "ab,", "ab.", "abo.", "ab,c,o", "ab7", "abww", "abzw7zw", "ab7zwww", "ab7zw12", "ab12"}},
	{name: "recover", src: `@start
rule main = stmt* $$
rule stmt = ("let" ws name "=" [0-9]+ ";") #recover(skip=(![;] .)* ";") ws
rule name = [a-z]+
rule ws = [ ]*
`, inputs: []string{"let a=1;let b=;let c=2;", "let=;", "let x=1;"}},
	{name: "operators", src: `@start
rule main = expr $$
rule expr = #operators {
    operand [0-9]+
    level {
        infix left "+"
    }
    level {
        prefix "-"
    }
}
`, inputs: []string{"1+2+-3", "1+", "-"}},
	{name: "typedops", src: `type Number = node[string]
type Binary = { Left Expr; Op node[string]; Right Expr }
type Expr ~ Number | Binary
@start
rule main Expr = e:expr ws $$ => $e
rule expr Expr = #operators {
    skip ws
    operand number
    operand "(" e:expr ws ")" => $e
    level { infix left "+" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
}
rule number Number = [0-9]+ #atomic
rule ws = [ ]*
`, opts: common.GenOptions{Types: true}, inputs: []string{"1+(2+3)", "(1", "1+", "((4))"},
		entries: []string{flowConformParse, "_, err = ParseValue(in, opts...)"}},
	{name: "leftrec", src: `@start
rule main = sum $$
rule sum = sum "+" n:[0-9] ps:(a:[a-z] b:[0-9])* / [0-9]
`, opts: common.GenOptions{Types: true}, inputs: []string{"1+2+3", "1+", "1+2a1b2+3"},
		entries: []string{flowConformParse, "_, err = ParseValue(in, opts...)"}},
	// Logical compute units: the guards of x, u and item skip chains of calls through memoized rules,
	// which flow.skippedCalls makes the calls of.
	{name: "logical", src: `@start
rule main = (-x / -u / -item / z)* $$
rule x = inner ";" / "e"
rule u = outer "!" / outer "?" / "c"
rule item = inner "." / "b"
rule z = "y" &u
rule outer = inner "-"
rule inner = "a" tail
rule tail = [a-z]*
`, opts: common.GenOptions{}, inputs: []string{"ccbeyca.a;a-!a-?", "aqq-?aqqq.ya-!", "a-", "q", ""}},
	// The guards of the logical case over memoized rules that read a string variable, whose calls
	// and skips count the keys of the calls they make, with the bytes of the string.
	{name: "logicalkeys", src: `var k string
@start
rule main = ({ k := "ab" } -x / { k := "abcd" } -u / -item / z)* $$
rule x = inner ";" / "e"
rule u = outer "!" / outer "?" / "c"
rule item = inner "." / "b"
rule z = "y" &u
rule outer = inner "-"
rule inner = "a" { k != "z" } tail
rule tail = [a-z]*
`, opts: common.GenOptions{}, inputs: []string{"ccbeyca.a;a-!a-?", "aqq-?aqqq.ya-!", "a-", "q", ""}},
	// The guards of the logical case in a build with Documents, whose functions serve the plain parse
	// and the Document parse: the plain parse takes the guards whose skips pass over memoized calls,
	// and the Document parse evaluates those alternatives (flow.inDocument). The character "x" is an
	// item of its own, so that the edits of flowConformDocument leave a text that parses.
	{name: "docguards", src: `@start
rule main = (-x / -u / -item / z / "x")* $$
rule x = inner ";" / "e"
rule u = outer "!" / outer "?" / "c"
rule item = inner "." / "b"
rule z = "y" &u
rule outer = inner "-"
rule inner = "a" tail
rule tail = [a-z]*
`, opts: common.GenOptions{Document: true}, inputs: []string{"ccbeyca.a;a-!a-?", "aqq-?aqqq.ya-!", "a-", "q", ""},
		entries: []string{flowConformParse, flowConformDocument}},
	// A Document whose repetition resumes its run after edits: the elements before an edit are the
	// reused prefix, and those after it, moved by an edit that changes the length, the reused tail. The
	// parses run under the default limit, under which the work of the Document is not counted.
	{name: "document", src: `@start
rule main = (item ";")* $$
rule item = [a-z]+ ("=" [0-9]+)?
`, opts: common.GenOptions{Document: true}, inputs: []string{strings.Repeat("ab;", 40), strings.Repeat("ab=1;cd;", 20), "ab;"},
		entries: []string{flowConformDocument}},
	{name: "observe", src: `@start
rule main = (kw / id)* $$
rule kw = "if" / "else"
rule id = [a-z]+ ";"
`, opts: common.GenOptions{Observability: true}, inputs: []string{"if;else", "abc;if"}},
	// The guard of the alternative kw skips it where the next character cannot begin "if", and the
	// traced functions report the call of kw it skips to the observation.
	{name: "observeskip", src: `@start
rule main = (kw / id)* $$
rule kw = "if" ";"
rule id = [a-z]+ ";"
`, opts: common.GenOptions{Observability: true}, inputs: []string{"if;abc;", "abc;if;", "ab"},
		entries: []string{flowConformParse, flowConformObserved}},
	// A memoized rule whose body recovers: the result that the call stores takes the recovered errors
	// of the body.
	{name: "recmemo", src: `@start
rule main = (stmt ";" / stmt "!")* $$
rule stmt = ("let" name) #recover(skip=(![;!] .)*)
rule name = [a-z]+
`, inputs: []string{"leta;lex!lety;lt;", "q!", "letab!"}},
	// The run sites of codegentest.RunSiteGrammars and RunValueGrammars, on inputs long enough that
	// their walks go back over RunHotSpan of input and the sites turn hot: a value-free site, and a
	// site that builds values and is keyed by a variable, through both forms of the parser.
	{name: "runsites", src: codegentest.RunSiteGrammars[0].Src,
		inputs: []string{strings.Repeat("x", 300) + "z", strings.Repeat("xxq", 100) + "y", strings.Repeat("xyq", 40), "xq"}},
	{name: "runvalues", src: runValueGrammar("two environments at one position"), opts: common.GenOptions{Types: true},
		inputs:  []string{strings.Repeat("xxxq?xxxq!", 30), strings.Repeat("x", 200) + "q?" + strings.Repeat("x", 100) + "w!", "xxxq!"},
		entries: []string{flowConformParse, "_, err = ParseValue(in, opts...)"}},
	// Run sites whose elements may be empty and repeat a minimum number of times, and whose walks run
	// under lookahead before the walk of a list, capturing and testing the value there.
	{name: "runmin", src: runValueGrammar("minimum and empty elements"),
		inputs: []string{strings.Repeat("xy", 150) + "!", strings.Repeat("x", 200) + "z", strings.Repeat("xyx!", 60) + "q"}},
	{name: "runlook", src: runValueGrammar("lookahead walks before lists"),
		inputs: []string{strings.Repeat("ab", 150) + "!", strings.Repeat("a", 200) + "?" + strings.Repeat("ab", 100) + ",", strings.Repeat("aba?", 60)}},
	// A run site whose element binds a variable of the walk's key, which counts its key at each lookup.
	{name: "runkeys", src: runValueGrammar("a binding and a called rule that reads"),
		inputs: []string{strings.Repeat("xy", 150) + "?", strings.Repeat("x", 200) + "!" + strings.Repeat("yx", 100) + "?", strings.Repeat("xyx!", 60) + "z"}},
}

// flowOtherTargets are the operations that the Go printer never prints, with the reason; the test
// checks that none of its cases prints them.
var flowOtherTargets = map[string]string{
	flow.OpInserting:   "the Go tables that insert guard no call (lowerdata.GuardsNone), and the others never insert",
	flow.OpCallOutline: "only the TypeScript generator outlines nodes (lowerdata.Options.Outlined)",
	flow.OpInputDemand: "only a suspended stream demands input; the Go stream blocks in its reader and lowers without Suspend",
}

// flowProbe wraps the calls of primitive operations in probes and records what each one calls.
type flowProbe struct {
	mu    sync.Mutex
	calls []*code.Call
	// tables are the tables of the module of each call, which its immediate indexes.
	tables []*prim.Tables
	// printed are the operations whose Go form is nothing.
	empty map[string]bool
	// mutate changes the printed form of an operation (a negative control), if set.
	mutate func(op, printed string) string
}

// flowBodyProbes matches the first line of each generated function that takes the parser: the body of
// a rule, a frame, a line or an outlined node. A probe of its own (K -1) wraps it, so that the units of
// a body count as the body's, not as those of the call that runs it.
var flowBodyProbes = regexp.MustCompile(`(?m)^func \w+\(p \*rt\.T?Parser[^)]*\) [^{\n]*\{\n`)

// firstParser matches the first use of the parser p in a printed call.
var firstParser = regexp.MustCompile(`(^|[^.\w])p\b`)

// flowArgOps are the operations whose arguments the probes record, for their checks. The arguments of
// others are not wrapped: flow.append assigns to its first.
var flowArgOps = []string{flow.OpRestore, flow.OpDropKids, flow.OpKids, flow.OpInClass, flow.OpGuardFails, flow.OpText,
	flow.OpLiteralText, flow.OpReset, flow.OpMarkPos, flow.OpMergeExpected, flow.OpMergeRenamed, flow.OpKeep, flow.OpMakeError,
	flow.OpErrorValue, flow.OpNewErrorNode, flow.OpNewMatch, flow.OpNewTerminalNode, flow.OpNewBranchNode, flow.OpNewNode,
	flow.OpExpect, flow.OpSetFrame, flow.OpSetEnv, flow.OpSetCapture, flow.OpFrameVal,
	flow.OpScanReach, flow.OpScanKids, flow.OpCtxFinish, flow.OpFreshNode,
	flow.OpDropCreated, flow.OpUnfresh, flow.OpShapeSingle, flow.OpShapeCapture, flow.OpScopeValue, flow.OpAttachCaptures,
	flow.OpAttachFrame, flow.OpClearVals, flow.OpDropTrail, flow.OpFreeFrame, flow.OpEmitStream}

func (pr *flowProbe) add(c *code.Call, tab *prim.Tables) int {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.calls = append(pr.calls, c)
	pr.tables = append(pr.tables, tab)
	return len(pr.calls) - 1
}

func (pr *flowProbe) target(t *print.Target, tab *prim.Tables, pre *string) *print.Target {
	w := *t
	ops := flowOps()
	wrapArgs := func(c *code.Call, args []string) []string {
		if !slices.Contains(flowArgOps, execlower.FlowOp(c.Callee)) {
			return args
		}
		out := make([]string, len(args))
		for i, a := range args {
			if a == "nil" {
				// An untyped nil has no type to record (the event's argument stays nil).
				out[i] = a
				continue
			}
			out[i] = fmt.Sprintf("rt.ConfArg(%d, %s)", i, a)
		}
		return out
	}
	w.Op = func(c *code.Call, args []string) string {
		name := execlower.FlowOp(c.Callee)
		o := ops[name]
		if o == nil {
			return t.Op(c, args)
		}
		k := pr.add(c, tab)
		mark := len(*pre)
		s := t.Op(c, wrapArgs(c, args))
		if pr.mutate != nil {
			s = pr.mutate(name, s)
		}
		switch {
		case s == "":
			pr.mu.Lock()
			pr.empty[name] = true
			pr.mu.Unlock()
			return ""
		case len(o.Results) == 0:
			return fmt.Sprintf("rt.ConfPre(p, %d)\n%s\nrt.ConfPost()", k, s)
		case len(o.Results) == 1 && len(*pre) > mark:
			// The statements of the action expression run inside the call.
			*pre = (*pre)[:mark] + fmt.Sprintf("rt.ConfPre(p, %d)\n", k) + (*pre)[mark:]
			return fmt.Sprintf("rt.Conf1(nil, %s)", s)
		case len(o.Results) == 1:
			return fmt.Sprintf("rt.Conf1(rt.ConfPre(p, %d), %s)", k, s)
		}
		loc := firstParser.FindStringSubmatchIndex(s)
		if loc == nil {
			panic("flow probe: no parser in the form of " + name + ": " + s)
		}
		at := loc[3]
		s = s[:at] + fmt.Sprintf("rt.ConfPre(p, %d)", k) + s[at+1:]
		return fmt.Sprintf("rt.ConfPop%d(%s)", len(o.Results), s)
	}
	w.Negate = func(c *code.Call, args []string) (string, bool) {
		o := ops[execlower.FlowOp(c.Callee)]
		if o == nil || t.Negate == nil {
			if t.Negate == nil {
				return "", false
			}
			return t.Negate(c, args)
		}
		k := pr.add(c, tab)
		s, ok := t.Negate(c, wrapArgs(c, args))
		if !ok {
			return "", false
		}
		return fmt.Sprintf("rt.ConfNot(rt.ConfPre(p, %d), %s)", k, s), true
	}
	w.Stmt = func(c *code.Call, args []string, dst []string, define bool) (string, bool) {
		if ops[execlower.FlowOp(c.Callee)] == nil || t.Stmt == nil {
			if t.Stmt == nil {
				return "", false
			}
			return t.Stmt(c, args, dst, define)
		}
		k := pr.add(c, tab)
		stmtArgs := wrapArgs(c, args)
		pre := ""
		if execlower.FlowOp(c.Callee) == flow.OpAppend {
			// The statement assigns to its first argument: record the list before it, and the node
			// that the element is, around the assignment.
			pre = fmt.Sprintf("rt.ConfArg(0, %s)\n", args[0])
			stmtArgs = []string{args[0], fmt.Sprintf("rt.ConfArg(1, %s)", args[1])}
		}
		s, ok := t.Stmt(c, stmtArgs, dst, define)
		if !ok || s == "" {
			return s, ok
		}
		if pr.mutate != nil {
			s = pr.mutate(execlower.FlowOp(c.Callee), s)
		}
		res := make([]string, len(dst))
		for i, d := range dst {
			res[i] = d
			if d == "_" || strings.HasPrefix(s, "{") {
				res[i] = "nil"
			}
		}
		return fmt.Sprintf("rt.ConfPre(p, %d)\n%s%s\nrt.ConfPost(%s)", k, pre, s, strings.Join(res, ", ")), true
	}
	return &w
}

var flowOps = sync.OnceValue(func() map[string]*code.Op {
	m := map[string]*code.Op{}
	for _, o := range flow.Ops() {
		m[o.Name] = o
	}
	return m
})

// flowEvent is a call that a probe recorded (testdata/flowconform/probe.go).
type flowEvent struct {
	ArgFrameSet         bool
	ArgFrameSetAfter    bool
	ResultFrameSet      bool
	ArgFrameLen         int
	Trail, TrailAfter   int
	Streaming           bool
	Depth               int
	Emits, EmitsAfter   int
	EmitLast            any
	EmitLastAfter       any
	MemKept             uint64
	MemKeptAfter        uint64
	MemUsed             uint64
	MemUsedAfter        uint64
	K                   int
	Pos, PosAfter       int
	Counting            bool
	Up                  int
	Facts               []flowFact
	Used, UsedAfter     uint64
	Nested              uint64
	Kids, KidsAfter     int
	Rec, RecAfter       int
	Silent, SilentAfter int
	Far, FarAfter       int
	Exps, ExpsAfter     int
	Frame, FrameAfter   string
	Env, EnvAfter       string
	Vals, ValsAfter     []any
	Created             int
	CreatedAfter        int
	Fresh, FreshAfter   string
	Args                map[int]any
	Results             []any
	Done                bool
}

// flowRun is the record of a parse of an input.
type flowRun struct {
	Input  string
	Err    string
	Events []flowEvent
	// Skips are the skipped calls that the trace function of flowConformObserved was told of.
	Skips []flowSkip
}

// flowSkip is a skipped call that a trace reports: the rule and the position.
type flowSkip struct {
	Rule string
	Pos  int
}

// flowMutation breaks the code under test: lib changes the source of a file of the library copy by
// name, and print changes the printed form of an operation by its name; either may be nil.
type flowMutation struct {
	lib   func(name, src string) string
	print func(op, printed string) string
}

// flowConformRun builds the parsers of the cases against a copy of the library and the printer that
// mut changes (the zero value leaves them as they are), runs them and returns the failures of the
// checks of every recorded call and the number of recorded calls of each operation.
func flowConformRun(t *testing.T, cases []flowConformCase, mut flowMutation) (msgs []string, covered map[string]int) {
	t.Helper()
	probeSrc, err := os.ReadFile(filepath.Join("testdata", "flowconform", "probe.go"))
	if err != nil {
		t.Fatal(err)
	}
	hooked := map[string]int{}
	lib := codegentest.LibCopyWith(t, func(name, src string) string {
		if mut.lib != nil {
			src = mut.lib(name, src)
		}
		return flowFactHooks(name, src, hooked)
	}, map[string]string{"zz_probe.go": string(probeSrc)})
	for _, h := range flowFactHookList {
		if hooked[h.file+"\x00"+h.old] != h.n {
			t.Fatalf("the hook of %s found %d places for %q in %s, not %d", h.what, hooked[h.file+"\x00"+h.old], h.old, h.file, h.n)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoModAt("flowconf", lib)), 0o644); err != nil {
		t.Fatal(err)
	}
	probes := map[string]*flowProbe{}
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		pr := &flowProbe{empty: map[string]bool{}, mutate: mut.print}
		probes[c.name] = pr
		opts := c.opts
		opts.Lib, opts.NoLimitControls, opts.KeepComments, opts.Expectations = common.LibImport, c.noLimits, true, true
		opts.Package, opts.Module = c.name, "flowconf/"+c.name
		files, err := generateGo(g, opts, driver.Options{}, &print.Hooks{PrimTarget: pr.target})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, f := range files {
			path := filepath.Join(dir, c.name, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			content := f.Content
			if f.Path == "parser.go" {
				content = flowBodyProbes.ReplaceAll(content, []byte("$0\trt.ConfPre(p, -1)\n\tdefer rt.ConfPost()\n"))
			}
			if err := os.WriteFile(path, content, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		inputs, _ := json.Marshal(c.inputs)
		entries := c.entries
		if entries == nil {
			entries = []string{flowConformParse}
		}
		var cases strings.Builder
		for i, e := range entries {
			fmt.Fprintf(&cases, "\t\t\tcase %d:\n\t\t\t\t%s\n", i, e)
		}
		test := fmt.Sprintf(`package %s

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	rt "syntaxir.dev/sxir/pkg/runtime/libgo"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"
)

func TestProbe(t *testing.T) {
	var inputs []string
	if err := json.Unmarshal([]byte(%q), &inputs); err != nil {
		t.Fatal(err)
	}
	type skip struct {
		Rule string
		Pos  int
	}
	type run struct {
		Input  string
		Err    string
		Events []*rt.ConfEvent
		Skips  []skip
	}
	var runs []run
	var skips []skip
	opts := []ParseOption{sxirruntime.WithExpectations(), sxirruntime.WithLimits(sxirruntime.Limits{Compute: 1 << 40})}
	ordinary := []ParseOption{sxirruntime.WithExpectations()}
	observe := sxirruntime.WithTrace(func(e sxirinstrument.TraceEvent) {
		if e.Kind == sxirinstrument.TraceSkipped {
			skips = append(skips, skip{e.Rule, e.Pos})
		}
	})
	for _, in := range inputs {
		for entry := range %d {
			rt.ConfReset()
			skips = nil
			var err error
			switch entry {
%s			}
			r := run{Input: in, Events: rt.ConfEvents, Skips: skips}
			if err != nil {
				r.Err = err.Error()
			}
			runs = append(runs, r)
		}
	}
	_, _, _, _ = strings.NewReader, opts, ordinary, observe
	data, err := json.Marshal(runs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(%q, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
`, c.name, inputs, len(entries), cases.String(), filepath.Join(dir, c.name+".json"))
		if err := os.WriteFile(filepath.Join(dir, c.name, "probe_test.go"), []byte(test), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probed parsers: %v\n%s", err, out)
	}
	covered = map[string]int{}
	for _, c := range cases {
		pr := probes[c.name]
		for name := range pr.empty {
			covered[name]++
		}
		for _, call := range pr.calls {
			if name := execlower.FlowOp(call.Callee); flowOtherTargets[name] != "" {
				covered[name]++
			}
		}
		data, err := os.ReadFile(filepath.Join(dir, c.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var runs []flowRun
		if err := json.Unmarshal(data, &runs); err != nil {
			t.Fatal(err)
		}
		entries := c.entries
		if entries == nil {
			entries = []string{flowConformParse}
		}
		for i, r := range runs {
			in := ref.NewInput(r.Input)
			var isolated []flowEvent
			observed := entries[i%len(entries)] == flowConformObserved
			if observed {
				msgs = append(msgs, checkObserved(c.name, pr, runs[i-i%len(entries)], r, entries[0])...)
			}
			flowBodyUnits(r.Events)
			for _, e := range r.Events {
				if e.K < 0 {
					continue
				}
				call := pr.calls[e.K]
				name := execlower.FlowOp(call.Callee)
				if !e.Done {
					continue
				}
				covered[name]++

				covered["case:"+c.name+":"+name]++
				for _, f := range e.Facts {
					if flowFactUnits(&flowEvent{Facts: []flowFact{f}}) > 0 {
						covered["fact:"+name+":"+f.Kind]++
					}
				}
				for _, ch := range flowChanged(&e) {
					covered["chg:"+name+":"+ch]++
				}
				// An isolation ends with the record that it began in.
				switch name {
				case flow.OpIsolate:
					isolated = append(isolated, e)
				case flow.OpUnisolate:
					if n := len(isolated); n > 0 {
						if b := isolated[n-1]; e.FarAfter != b.Far || e.ExpsAfter != b.Exps {
							msgs = append(msgs, fmt.Sprintf("%s %q: %s at %d: restored the record at %d with %d expectations, not that at %d with %d before the isolation", c.name, r.Input, name, e.Pos, e.FarAfter, e.ExpsAfter, b.Far, b.Exps))
						}
						isolated = isolated[:n-1]
					}
				}
				for _, msg := range checkFlowCall(in, pr.tables[e.K], flowOps()[name], call, &e, !observed) {
					msgs = append(msgs, fmt.Sprintf("%s %q: %s at %d: %s", c.name, r.Input, name, e.Pos, msg))
				}
			}
		}
	}
	return msgs, covered
}

func TestFlowOperationsConform(t *testing.T) {
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	msgs, covered := flowConformRun(t, flowConformCases, flowMutation{})
	for _, m := range msgs {
		t.Error(m)
	}
	for _, f := range flowFactsCovered {
		if covered["fact:"+f] == 0 {
			t.Errorf("no case counts the units of a fact %s", f)
		}
	}
	for _, o := range flow.Ops() {
		if why, other := flowOtherTargets[o.Name]; other {
			if covered[o.Name] != 0 {
				t.Errorf("the Go printer printed %s: %s", o.Name, why)
			}
			continue
		}
		if covered[o.Name] == 0 {
			t.Errorf("no case calls %s", o.Name)
		}
	}
}

// flowFactsCovered are the facts that flowFactHookList records, each under an operation that the
// cases must make it count units under: the operation, then the kind of the fact.
var flowFactsCovered = []string{
	flow.OpCallRule + ":key", flow.OpCallRule + ":step", flow.OpCallRule + ":taken", flow.OpCallRule + ":insertion",
	flow.OpCallRule + ":replayedError",
	flow.OpSkippedCalls + ":call", flow.OpSkippedCalls + ":key",
	flow.OpRunBegin + ":walkStart", flow.OpSkipHit + ":walkLookup", flow.OpSkipKids + ":suffix",
}

// checkObserved returns what the observed parse r of the case name breaks, given the parse plain of the
// same input through first, the case's first entry: the observed parse fails or succeeds as plain does
// and counts the compute units in total that it counts, and the trace is told of a skipped call for
// each flow.observeSkip that the parse runs, in order, with its rule and position.
func checkObserved(name string, pr *flowProbe, plain, r flowRun, first string) []string {
	var msgs []string
	fail := func(format string, args ...any) {
		msgs = append(msgs, fmt.Sprintf("%s %q observed: ", name, r.Input)+fmt.Sprintf(format, args...))
	}
	if first != flowConformParse {
		fail("the first entry of the case is %q, not the parse that the observed parse is compared with", first)
		return msgs
	}
	if r.Err != plain.Err {
		fail("ended with %q, the parse that is not observed with %q", r.Err, plain.Err)
	}
	if got, want := flowTotalUnits(r.Events), flowTotalUnits(plain.Events); got != want {
		fail("counted %d compute units, the parse that is not observed %d", got, want)
	}
	var skips []flowSkip
	for _, e := range r.Events {
		if e.K < 0 || !e.Done {
			continue
		}
		if c := pr.calls[e.K]; execlower.FlowOp(c.Callee) == flow.OpObserveSkip {
			skips = append(skips, flowSkip{Rule: c.Imm.(prim.ObservedSkip).Rule, Pos: e.Pos})
		}
	}
	if len(skips) == 0 {
		fail("ran no %s", flow.OpObserveSkip)
	}
	if !slices.Equal(skips, r.Skips) {
		fail("ran %s for %v, but the trace was told of %v", flow.OpObserveSkip, skips, r.Skips)
	}
	return msgs
}

// flowTotalUnits returns the compute units that a parse counted, as its recorded calls saw the count.
func flowTotalUnits(events []flowEvent) uint64 {
	var n uint64
	for _, e := range events {
		n = max(n, e.Used, e.UsedAfter)
	}
	return n
}

// flowBodyUnits gives the units that the body of a rule, a frame, a line or an outlined node counts
// itself, outside the probed calls in it, to the call that runs the body, with the facts recorded in
// it: the probe of the body (K -1) only keeps its units apart from the calls' own. A rule function
// counts the unit of its call as it starts, which is the event of the call (flow.callRule) that runs
// it.
func flowBodyUnits(events []flowEvent) {
	for _, e := range events {
		if e.K >= 0 || !e.Done || e.Up < 0 {
			continue
		}
		events[e.Up].Nested -= e.UsedAfter - e.Used - e.Nested
		events[e.Up].Facts = append(events[e.Up].Facts, e.Facts...)
	}
}

// flowFact is an event of the event table that the library made inside a recorded call, as the hooks
// of flowFactHookList record it (ConfFact of testdata/flowconform/probe.go).
type flowFact struct {
	Kind string
	N    int
	Vals []any
}

// flowFactHook adds a record of the facts of an event to the library copy: the text new replaces old,
// which the file holds n times.
type flowFactHook struct {
	what, file, old, new string
	n                    int
}

// flowFactHookList are the hooks that record, where the library makes them, the events of the event
// table whose units depend on state that the probes of the printed calls do not see. Each one records
// what the event is made of (the values of a key, the entries a stored result takes, a growth step,
// the elements a copy makes), next to the work itself rather than to the count of its units, so that
// the check derives the units from the specification and a count that is missing, misplaced or of the
// wrong size shows. The key of a call of a memoized rule is recorded where every such call, of the
// library or printed, begins its memo protocol (FirstCall), not where the call counts the key.
var flowFactHookList = []flowFactHook{
	{"the key of a call of a memoized rule", "call_exec.go", "func (p *Parser) FirstCall(r *Rule) bool {\n",
		"func (p *Parser) FirstCall(r *Rule) bool {\n\tp.confKeyFact(r.varSlots)\n", 1},
	{"the key of a call of a memoized typed rule", "call_exec.go", "func (p *TParser) firstCall(r *TRule) bool {\n",
		"func (p *TParser) firstCall(r *TRule) bool {\n\tp.confKeyFact(r.varSlots)\n", 1},
	{"a growth step", "call_exec.go", "v, ok := p.invoke(r, min)\n", "confFact(\"step\", 1, nil)\nv, ok := p.invoke(r, min)\n", 4},
	{"the calls of a skip of rules that the parse does not memoize", "units.go", "func (p *Parser) SkipUnits(n uint64) {\n",
		"func (p *Parser) SkipUnits(n uint64) {\n\tconfFact(\"call\", int(n), nil)\n", 1},
	{"a call of a skip", "units.go", "\tr := rules[0]\n", "\tr := rules[0]\n\tconfFact(\"call\", 1, nil)\n", 2},
	{"a suffix copy", "skip_vals.go", "copySpend(p, a[slack+n:], tail.buf.a[tail.i:])",
		"confFact(\"suffix\", len(tail.buf.a)-tail.i, nil)\ncopySpend(p, a[slack+n:], tail.buf.a[tail.i:])", 1},
	{"the key of a walk as it starts", "skip_walks.go", "\t\tw.key = p.runKeyOf(site)\n",
		"\t\tw.key = p.runKeyOf(site)\n\t\tconfFact(\"walkStart\", confBool(len(w.key.bind) > 0), p.envSlotValues(w.key.vars))\n", 1},
	{"the key of a walk's lookup", "skip_walks.go", "\tk := p.walkKey()\n",
		"\tk := p.walkKey()\n\tif w.key != nil {\n\t\tconfFact(\"walkLookup\", confBool(len(w.key.bind) > 0), p.envSlotValues(w.key.vars))\n\t}\n", 1},
	{"a copy of the insertions of a result", "missing.go", "\treturn missingSeed(append([]missingToken(nil), s.path[n:]...))\n",
		"\tconfFact(\"insertion\", len(s.path)-n, nil)\n\treturn missingSeed(append([]missingToken(nil), s.path[n:]...))\n", 1},
	{"the insertions that a reused result records again", "missing.go", "\tfor _, t := range seed {\n\t\tif !p.installMissing(t, charge) {\n",
		"\tconfFact(\"insertion\", len(seed), nil)\n\tfor _, t := range seed {\n\t\tif !p.installMissing(t, charge) {\n", 1},
	{"the errors that a reused result records one by one", "missing.go", "\tall := appendRecErrors(nil, []recEntry{errs})\n",
		"\tall := appendRecErrors(nil, []recEntry{errs})\n\tconfFact(\"replayedError\", len(all), nil)\n", 1},
}

// flowFactHooks adds the hooks of flowFactHookList to the library file name, and records the entries a
// stored result takes (recSince, which every store of a result calls) in every file but that of the
// walks, whose records of iterations take entries that no unit counts. hooked counts the places found.
func flowFactHooks(name, src string, hooked map[string]int) string {
	for _, h := range flowFactHookList {
		if h.file == name {
			hooked[h.file+"\x00"+h.old] += strings.Count(src, h.old)
			src = strings.ReplaceAll(src, h.old, h.new)
		}
	}
	if name != "skip_walks.go" && name != "zz_probe.go" {
		src = strings.ReplaceAll(src, "p.recSince(", "p.confRecSince(")
	}
	return src
}

// flowKeyUnits returns the units of a key with the recorded values vals: one for each variable, and
// one for each byte of each string among them.
func flowKeyUnits(vals []any) int {
	n := len(vals)
	for _, v := range vals {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "s:") {
			n += len(s) - len("s:")
		}
	}
	return n
}

// flowFactUnits returns the units that the event table assigns to the facts of e: a memo key and a walk
// key their size, the key of a walk where the walk starts if the element binds none of its variables
// and at each lookup if it binds one, an entry taken, a growth step and a call one each, and a suffix
// copy one per element.
func flowFactUnits(e *flowEvent) int {
	n := 0
	for _, f := range e.Facts {
		switch f.Kind {
		case "key":
			n += flowKeyUnits(f.Vals)
		case "walkStart":
			if f.N == 0 {
				n += flowKeyUnits(f.Vals)
			}
		case "walkLookup":
			if f.N == 1 {
				n += flowKeyUnits(f.Vals)
			}
		case "taken", "step", "call", "suffix", "insertion", "replayedError":
			n += f.N
		default:
			panic("flow probe: fact " + f.Kind)
		}
	}
	return n
}

// flowUnbounded is the most units of a range that the check does not bound.
const flowUnbounded = 1 << 30

// flowUnits returns the range of the logical compute units that the recorded call e of the operation
// o counts itself, the probed calls inside it aside, as the event table of spec/runtime/parsing.md
// (Compute Units) assigns them to the operation's events. A parser that counts no units counts none.
// The range is one value, the units of the facts that the library recorded inside the call
// (flowFactUnits) and those of the operation's own events:
//
//   - a terminal (flow.advance, flow.matchLit, flow.matchClass, flow.skipTerminal and
//     flow.skipClasses) counts the position units it advances over, none when it fails;
//   - flow.makeError counts the syntax error that it builds for a recovery, one unit, and so does
//     flow.insertMissing for the error of an insertion, where it inserts or the limit of missing
//     tokens ends the parse;
//   - flow.callRule counts its call, one unit, and the facts of the protocol of the call: the key of
//     a memoized rule, the steps of a growing left-recursive rule, the entries of recovered errors
//     that a stored result takes, the insertions that it copies and that a reused result records
//     again, and the errors that a reused result records one by one;
//   - flow.skippedCalls counts the calls that the skip makes, and their keys;
//   - flow.runBegin, flow.skipHit and flow.skipKids count the keys and the suffix copies of run walks;
//   - every other operation makes no event and counts nothing.
//
// flow.pred, flow.bind and flow.eval run action code that the probes do not wrap, whose events
// action_charges_test.go checks: they count at least their facts.
func flowUnits(o *code.Op, e *flowEvent) (lo, hi int) {
	if !e.Counting {
		return 0, 0
	}
	n := flowFactUnits(e)
	switch o.Name {
	case flow.OpAdvance, flow.OpMatchLit, flow.OpMatchClass, flow.OpSkipTerminal, flow.OpSkipClasses:
		n += e.PosAfter - e.Pos
	case flow.OpMakeError, flow.OpCallRule:
		n++
	case flow.OpInsertMissing:
		// The error counts where the call inserts, and where the limit of missing tokens, which it
		// checks after the count, ends the parse: the panic unwinds the call with no results.
		if ok, known := resBool(e, 1); ok || !known {
			n++
		}
	case flow.OpPred, flow.OpBind, flow.OpEval:
		return n, flowUnbounded
	}
	return n, n
}

// checkFlowCall returns what the recorded call e of the operation o, the call c, breaks; with units,
// also the compute units it counts itself.
func checkFlowCall(in *ref.Input, tab *prim.Tables, o *code.Op, c *code.Call, e *flowEvent, units bool) []string {
	var errs []string
	fail := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	if !slices.Contains(o.Writes, prim.Pos) && e.PosAfter != e.Pos {
		fail("moved the position to %d without writing it", e.PosAfter)
	}
	self := int(e.UsedAfter-e.Used) - int(e.Nested)
	switch lo, hi := flowUnits(o, e); {
	case !units:
	case lo == hi && self != lo:
		fail("counted %d compute units, not %d", self, lo)
	case self < lo || self > hi:
		fail("counted %d compute units, outside %d to %d", self, lo, hi)
	}
	if check := flowConformChecks[o.Name]; check != nil {
		check(in, flowCall{c, tab}, e, fail)
	}
	return errs
}

func argInt(e *flowEvent, i int) (int, bool) {
	v, ok := e.Args[i].(float64)
	return int(v), ok
}

func resBool(e *flowEvent, i int) (bool, bool) {
	if i >= len(e.Results) {
		return false, false
	}
	v, ok := e.Results[i].(bool)
	return v, ok
}

func resInt(e *flowEvent, i int) (int, bool) {
	if i >= len(e.Results) {
		return 0, false
	}
	v, ok := e.Results[i].(float64)
	return int(v), ok
}

func markPos(x any) (int, bool) {
	s, ok := x.(string)
	if !ok {
		return 0, false
	}
	var n int
	_, err := fmt.Sscanf(s, "mark:%d", &n)
	return n, err == nil
}

// markTrail returns the length of the trail that the mark x (confVal) is at.
func markTrail(x any) (int, bool) {
	s, ok := x.(string)
	if !ok {
		return 0, false
	}
	var pos, n int
	_, err := fmt.Sscanf(s, "mark:%d:%d", &pos, &n)
	return n, err == nil
}

func listLen(x any) (int, bool) {
	s, ok := x.(string)
	if !ok {
		return 0, false
	}
	var n int
	_, err := fmt.Sscanf(s, "len:%d", &n)
	return n, err == nil
}

// flowCall is a call of a Flow operation with the tables of its module, which its immediate indexes.
type flowCall struct {
	*code.Call
	tab *prim.Tables
}

func classOf(tab *prim.Tables, id prim.ClassID) ref.Ranges {
	c := tab.Class(id)
	r := ref.Ranges{Negated: c.Negated}
	for _, x := range c.Ranges {
		r.Ranges = append(r.Ranges, [2]rune{x.Lo, x.Hi})
	}
	return r
}

type failFunc func(format string, args ...any)

// flowConformChecks are the checks of the Flow operations beyond their position and charges, by
// operation.
var flowConformChecks = map[string]func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc){
	flow.OpShapeSingle: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		sh := c.Imm.(prim.Shape)
		ok, known := resBool(e, 1)
		if known && ok != (sh.Mode != prim.SingleNone) {
			fail("reported %v for a shape of mode %d", ok, sh.Mode)
		}
		if known && ok {
			checkShapeValue(e, fail, sh, false)
		} else if known && e.Results[0] != nil {
			fail("returned %v where the expression has no value", e.Results[0])
		}
	},
	flow.OpShapeCapture: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		sh := c.Imm.(prim.Shape)
		if sh.Mode == prim.SingleNone {
			// The unit node of the range matched.
			start, ok := argInt(e, 1)
			if v, isNode := valOf(firstResult(e)); isNode && ok && (!isTuple(v) || v.Start != start || v.End != e.Pos || v.Kids != 0) {
				fail("returned %v for no value, not the empty tuple of %d to %d", firstResult(e), start, e.Pos)
			} else if !isNode {
				fail("returned %v for no value, not the empty tuple", firstResult(e))
			}
			return
		}
		checkShapeValue(e, fail, sh, false)
	},
	flow.OpScopeValue: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		checkShapeValue(e, fail, c.Imm.(prim.Shape), true)
	},
	flow.OpAttachCaptures: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		attached := false
		for i := 2; i < len(e.Args); i++ {
			if e.Args[i] != nil {
				attached = true
			}
		}
		checkAttached(e, fail, attached)
	},
	flow.OpAttachFrame: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		// The captures are the values of the frame argument.
		attached := e.ArgFrameSet
		checkAttached(e, fail, attached)
	},
	flow.OpDropCreated: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		var base int
		if _, err := fmt.Sscanf(fmt.Sprint(e.Args[0]), "ctx:%d", &base); err != nil {
			return
		}
		if e.CreatedAfter != base {
			fail("left %d made structs, not the %d of before the context", e.CreatedAfter, base)
		}
	},
	flow.OpUnfresh: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		if e.Fresh != "" && e.FreshAfter != "false" {
			fail("left the node owned by its maker (%s)", e.FreshAfter)
		}
	},
	flow.OpAppend: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		before, ok1 := listLen(e.Args[0])
		after, ok2 := listLen(firstResult(e))
		if !ok1 || !ok2 {
			return
		}
		want := 1
		if bool(c.Imm.(prim.Spread)) {
			x, ok := valOf(e.Args[1])
			if !ok {
				return
			}
			want = x.Kids
		}
		if after != before+want {
			fail("appended %d elements to the list of %d, to make %d, not %d", after-before, before, after, before+want)
		}
	},
	flow.OpCtxFinish: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		arg, res := e.Args[1], firstResult(e)
		if (arg == nil) != (res == nil) {
			fail("returned %v for the value %v: nil only for nil", res, arg)
			return
		}
		a, ok1 := valOf(arg)
		r, ok2 := valOf(res)
		if !ok1 || !ok2 {
			return
		}
		if a.Type != r.Type || a.Kids != r.Kids || a.Text != r.Text {
			fail("returned %v for the value %v: it changes only the span", res, arg)
		}
		if r.Start > r.End || r.End > e.Pos {
			fail("gave the value the span %d to %d, the position is %d", r.Start, r.End, e.Pos)
		}
		if (r.Start != a.Start || r.End != a.End) && r.End != e.Pos {
			fail("changed the span of the value from %d-%d to %d-%d, which does not end at the position %d", a.Start, a.End, r.Start, r.End, e.Pos)
		}
	},
	flow.OpFreshNode: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		if res := firstResult(e); res != nil && fmt.Sprint(res) != fmt.Sprint(e.Args[0]) {
			fail("returned %v, neither the node %v nor nil", res, e.Args[0])
		}
	},
	flow.OpPred:          func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpBind:          func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpEval:          func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpUseCtx:        func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpSetWhere:      func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpDeclareNames:  func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpStep:          func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpObserveSkip:   func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpConvertResult: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkOnlyAction(e, fail) },
	flow.OpSkipHit: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		hit, known := resBool(e, 0)
		switch {
		case !known:
		case !hit && (e.PosAfter != e.Pos || e.Kids != e.KidsAfter || e.Rec != e.RecAfter || e.Frame != e.FrameAfter ||
			fmt.Sprint(e.Vals) != fmt.Sprint(e.ValsAfter)):
			// Ending the iteration before may merge its expectations, which is not a change of a record.
			fail("found no record and changed the position or the state (%v)", flowChanged(e))
		case hit && (e.PosAfter < e.Pos || e.FarAfter < e.Far):
			fail("took a record that moved the position from %d to %d and the farthest failure from %d to %d", e.Pos, e.PosAfter, e.Far, e.FarAfter)
		}
	},
	flow.OpRunHot: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if _, known := resBool(e, 0); !known && len(e.Results) != 0 {
			fail("returned %v, not a bool", e.Results[0])
		}
		if e.PosAfter != e.Pos || len(flowChanged(e)) != 0 {
			fail("changed the state (%v), it is pure", flowChanged(e))
		}
	},
	flow.OpSkipKids: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if _, ok := listLen(firstResult(e)); !ok {
			fail("returned %v, not a list", firstResult(e))
		}
		if e.PosAfter != e.Pos {
			fail("moved the position from %d to %d", e.Pos, e.PosAfter)
		}
	},
	flow.OpScanReach: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		start, ok1 := argInt(e, 0)
		count, ok2 := argInt(e, 1)
		reach, ok3 := firstResult(e).(float64)
		if !ok1 || !ok2 || !ok3 {
			return
		}
		switch {
		case int(reach) < count:
			fail("returned %d characters, fewer than the %d matched", int(reach), count)
		case e.PosAfter-start != int(reach):
			fail("returned %d characters from %d and left the position at %d", int(reach), start, e.PosAfter)
		case int(reach) == count && e.PosAfter != e.Pos:
			fail("returned the count and moved the position from %d to %d", e.Pos, e.PosAfter)
		}
	},
	flow.OpScanKids: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		base, ok := argInt(e, 1)
		if _, isList := listLen(firstResult(e)); !isList {
			fail("returned %v, not a list", firstResult(e))
		}
		if ok && e.KidsAfter != base {
			fail("left %d gathered values, not %d", e.KidsAfter, base)
		}
		if e.PosAfter != e.Pos {
			fail("moved the position from %d to %d", e.Pos, e.PosAfter)
		}
	},
	flow.OpSkipBegin: checkWalkOnly,
	flow.OpRunBegin:  checkWalkOnly,
	flow.OpSkipVisit: checkWalkOnly,
	flow.OpSkipValue: checkWalkOnly,
	flow.OpSkipEnd:   checkWalkOnly,
	flow.OpRunWalked: checkWalkOnly,
	flow.OpScanEnd:   checkWalkOnly,
	flow.OpPeek: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		r, size, ok := in.At(e.Pos)
		if got, known := resBool(e, 2); known && got != ok {
			fail("reports a code point %v, the input %v", got, ok)
		}
		if !ok {
			return
		}
		if got, known := resInt(e, 0); known && rune(got) != r {
			fail("read %q, the input has %q", rune(got), r)
		}
		if got, known := resInt(e, 1); known && got != size {
			fail("read size %d, want %d", got, size)
		}
	},
	flow.OpMatchLit: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		lit := c.Imm.(prim.Lit)
		want := in.HasPrefix(e.Pos, lit.Text, lit.Fold)
		if got, known := resBool(e, 1); known && got != want {
			fail("matched %v, the input %v", got, want)
		}
		end := e.Pos
		if want {
			end += len([]rune(lit.Text))
		}
		if e.PosAfter != end {
			fail("ended at %d, want %d", e.PosAfter, end)
		}
	},
	flow.OpHasLit: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		lit := c.Imm.(prim.Lit)
		if got, known := resBool(e, 0); known && got && !in.HasPrefix(e.Pos, lit.Text, lit.Fold) {
			fail("holds where the input does not begin with %q", lit.Text)
		}
	},
	flow.OpMatchClass: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		want := in.MatchClass(e.Pos, classOf(c.tab, c.Imm.(prim.ClassID)))
		if got, known := resBool(e, 0); known && got != want {
			fail("matched %v, the input %v", got, want)
		}
		end := e.Pos
		if want {
			end++
		}
		if e.PosAfter != end {
			fail("ended at %d, want %d", e.PosAfter, end)
		}
	},
	flow.OpInClass: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		ch, ok := argInt(e, 0)
		got, known := resBool(e, 0)
		if ok && known && got != classOf(c.tab, c.Imm.(prim.ClassID)).Contains(rune(ch)) {
			fail("reports %v for %q", got, rune(ch))
		}
	},
	flow.OpGuardFails: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		g := c.Imm.(prim.Guard)
		got, known := resBool(e, 0)
		more, mok := e.Args[1].(bool)
		if !known || !mok {
			return
		}
		admits := false
		if ch, ok := argInt(e, 0); more && ok {
			admits = classOf(c.tab, g.Class).Contains(rune(ch))
		} else if more {
			return
		}
		if got && admits {
			fail("skips an alternative that the character can begin")
		}
		if !got && !admits && g.Depth == 0 {
			fail("keeps an alternative that the character cannot begin")
		}
	},
	flow.OpGuardLitFails: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		g := c.Imm.(prim.Guard)
		got, known := resBool(e, 0)
		if !known {
			return
		}
		admits := in.HasPrefix(e.Pos, string(g.Rune), g.Fold)
		if got && admits {
			fail("skips an alternative that the character can begin")
		}
		if !got && !admits && g.Depth == 0 {
			fail("keeps an alternative that the character cannot begin")
		}
		if e.PosAfter != e.Pos {
			fail("moved the position from %d to %d", e.Pos, e.PosAfter)
		}
	},
	flow.OpAtAnchor: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		want := in.AtAnchor(e.Pos, int(c.Imm.(prim.Anchor)))
		if got, known := resBool(e, 0); known && got != want {
			fail("reports %v, the input %v", got, want)
		}
	},
	flow.OpSkipClasses: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		run := c.Imm.(prim.ClassRun)
		matched := 0
		for matched < len(run) && in.MatchClass(e.Pos+matched, classOf(c.tab, run[matched])) {
			matched++
		}
		want := matched == len(run)
		if got, known := resBool(e, 0); known && got != want {
			fail("matched %v, the input %v", got, want)
		}
		// On failure the position is at the class that did not match.
		end := e.Pos + matched
		if e.PosAfter != end {
			fail("ended at %d, want %d", e.PosAfter, end)
		}
	},
	flow.OpSkipTerminal: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if ok, known := resBool(e, 0); known && !ok && e.PosAfter != e.Pos {
			fail("failed and moved the position")
		}
		// The terminal program charges a unit for each code point it reads, at most one past its match.
		if self := int(e.UsedAfter-e.Used) - int(e.Nested); self > e.PosAfter-e.Pos+1 {
			fail("charged %d units for a match of %d", self, e.PosAfter-e.Pos)
		}
	},
	flow.OpText: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		start, ok := argInt(e, 0)
		if s, isStr := firstString(e); ok && isStr && s != in.Slice(start, e.Pos) {
			fail("returned %q, the input %q", s, in.Slice(start, e.Pos))
		}
	},
	flow.OpLiteralText: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		start, ok := argInt(e, 0)
		if s, isStr := firstString(e); ok && isStr && s != in.Slice(start, e.Pos) {
			fail("returned %q, the input %q", s, in.Slice(start, e.Pos))
		}
	},
	flow.OpStr: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if s, isStr := firstString(e); isStr && s != string(c.Imm.(prim.Chars)) {
			fail("returned %q", s)
		}
	},
	flow.OpAdvance: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if n, ok := c.Args[0].(*code.Const); ok && e.PosAfter-e.Pos != int(n.Value.(int64)) {
			fail("moved by %d, not %v", e.PosAfter-e.Pos, n.Value)
		}
		if e.PosAfter < e.Pos || e.PosAfter > in.Len() {
			fail("moved to %d", e.PosAfter)
		}
	},
	flow.OpRestore: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if pos, ok := argInt(e, 0); ok && e.PosAfter != pos {
			fail("moved to %d, not %d", e.PosAfter, pos)
		}
		if rec, ok := argInt(e, 1); ok && e.RecAfter != min(rec, e.Rec) {
			fail("kept %d recovered errors of %d, not the first %d", e.RecAfter, e.Rec, rec)
		}
	},
	flow.OpMarkState: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if len(e.Results) > 0 {
			if pos, ok := markPos(e.Results[0]); ok && pos != e.Pos {
				fail("marked %d", pos)
			}
		}
	},
	flow.OpMarkPos: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		pos, ok := markPos(e.Args[0])
		if got, known := resInt(e, 0); ok && known && got != pos {
			fail("returned %d for a mark at %d", got, pos)
		}
	},
	flow.OpReset: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if pos, ok := markPos(e.Args[0]); ok && e.PosAfter != pos {
			fail("returned to %d for a mark at %d", e.PosAfter, pos)
		}
	},
	flow.OpPushKid: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if e.KidsAfter != e.Kids+1 {
			fail("left %d gathered values of %d", e.KidsAfter, e.Kids)
		}
	},
	flow.OpDropKids: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if base, ok := argInt(e, 0); ok && e.KidsAfter != base {
			fail("left %d gathered values, not %d", e.KidsAfter, base)
		}
	},
	flow.OpKidsLen: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if got, known := resInt(e, 0); known && got != e.Kids {
			fail("returned %d of %d", got, e.Kids)
		}
	},
	flow.OpKids: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		base, ok := argInt(e, 0)
		if !ok {
			return
		}
		if e.KidsAfter != base {
			fail("left %d gathered values, not %d", e.KidsAfter, base)
		}
		if n, ok := listLen(firstResult(e)); ok && n != e.Kids-base {
			fail("returned %d values of %d above %d", n, e.Kids, base)
		}
	},
	flow.OpNewVals: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if n, ok := listLen(firstResult(e)); ok && n != 0 {
			fail("returned %d values", n)
		}
	},
	flow.OpRecLen: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if got, known := resInt(e, 0); known && got != e.Rec {
			fail("returned %d of %d", got, e.Rec)
		}
	},
	flow.OpAppendError: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if e.RecAfter != e.Rec+1 {
			fail("left %d recovered errors of %d", e.RecAfter, e.Rec)
		}
	},
	flow.OpSilent: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if d := c.Imm.(prim.Silence).Delta; e.SilentAfter-e.Silent != d {
			fail("changed the silent depth by %d, not %d", e.SilentAfter-e.Silent, d)
		}
	},
	flow.OpNewMatch: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if v, ok := checkNodeBuilt(e, fail); ok {
			if s, isStr := e.Args[1].(string); isStr && strings.HasPrefix(s, "s:") && v.Text != s[2:] {
				fail("built a match with the text %q, not %q", v.Text, s[2:])
			}
		}
	},
	flow.OpNewTerminalNode: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if v, ok := checkNodeBuilt(e, fail); ok {
			if s, isStr := e.Args[1].(string); isStr && strings.HasPrefix(s, "s:") && v.Text != s[2:] {
				fail("built a terminal with the text %q, not %q", v.Text, s[2:])
			}
		}
	},
	flow.OpNewBranchNode: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		v, ok := checkNodeBuilt(e, fail)
		if n, isLen := listLen(e.Args[1]); ok && isLen && v.Kids != n {
			fail("built a branch of %d children from a list of %d", v.Kids, n)
		}
	},
	flow.OpNewNode: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		v, ok := checkNodeBuilt(e, fail)
		if n, isLen := listLen(e.Args[1]); ok && isLen && v.Kids != n {
			fail("built a node of %d children from a list of %d", v.Kids, n)
		}
	},
	flow.OpErrorValue: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkNodeBuilt(e, fail, "error")
	},
	flow.OpNewErrorNode: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkNodeBuilt(e, fail, "error")
	},
	flow.OpExpect: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		at, ok := argInt(e, 0)
		if !ok {
			return
		}
		switch {
		case e.Silent > 0 || at < e.Far:
			if e.FarAfter != e.Far || e.ExpsAfter != e.Exps {
				fail("recorded an expectation at %d that the silent depth %d or the farthest failure %d drops", at, e.Silent, e.Far)
			}
		case at > e.Far:
			if e.FarAfter != at || e.ExpsAfter != 1 {
				fail("left the record at %d with %d expectations after one at %d, past the farthest failure %d", e.FarAfter, e.ExpsAfter, at, e.Far)
			}
		default:
			if e.FarAfter != at || e.ExpsAfter < e.Exps || e.ExpsAfter > e.Exps+1 {
				fail("left the record at %d with %d expectations of %d after one at the farthest failure %d", e.FarAfter, e.ExpsAfter, e.Exps, at)
			}
		}
	},
	flow.OpMergeExpected: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkMerge(e, fail) },
	flow.OpMergeRenamed:  func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) { checkMerge(e, fail) },
	flow.OpIsolate: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if e.ExpsAfter != 0 || e.FarAfter != e.Pos {
			fail("began a record at %d with %d expectations, not an empty one at the position %d", e.FarAfter, e.ExpsAfter, e.Pos)
		}
	},
	flow.OpUnisolate: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if far, ok := resInt(e, 0); ok && far != e.Far {
			fail("returned the farthest failure %d of a record at %d", far, e.Far)
		}
		if len(e.Results) < 2 {
			return
		}
		if n, ok := listLen(e.Results[1]); ok && n != e.Exps {
			fail("returned %d expectations of a record of %d", n, e.Exps)
		}
	},
	flow.OpKeep: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		n, ok := listLen(e.Args[0])
		if !ok || n == 0 {
			return
		}
		if got, ok := listLen(firstResult(e)); !ok || got != n {
			fail("kept %v of %d expectations", firstResult(e), n)
		}
	},
	flow.OpMakeError: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if firstResult(e) == nil {
			fail("returned no error")
		}
	},
	flow.OpSetFrame: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if want := idOf(e.Args[0]); e.FrameAfter != want {
			fail("made %s the current frame, not %s", e.FrameAfter, want)
		}
	},
	flow.OpFrame: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if got := idOf(firstResult(e)); got != e.Frame {
			fail("returned the frame %s, the current one is %s", got, e.Frame)
		}
	},
	flow.OpClearVals: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		if e.ArgFrameSetAfter {
			fail("left a slot of the frame set")
		}
	},
	flow.OpDropTrail: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		if want, ok := markTrail(e.Args[0]); ok && e.TrailAfter != want {
			fail("left a trail of %d entries for a mark at %d (it was %d)", e.TrailAfter, want, e.Trail)
		}
	},
	flow.OpFreeFrame: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		if idOf(e.Args[0]) == e.Frame {
			fail("freed the current frame %s", e.Frame)
		}
	},
	flow.OpStreamOn: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		got, known := resBool(e, 0)
		switch {
		case !known:
		case got && !e.Streaming:
			fail("reported delivery in a parse that has no callback")
		case !got && e.Streaming && e.Depth <= 1:
			fail("reported no delivery at the top call of a stream parse")
		}
	},
	flow.OpStreamStart: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		checkOnlyAction(e, fail)
		if !e.Streaming {
			fail("began a stream in a parse that has no callback")
		}
		if e.MemKeptAfter+e.MemUsedAfter != e.MemKept+e.MemUsed || e.MemUsedAfter != 0 {
			fail("accounted %d+%d bytes after %d+%d (the bytes in use are kept for the stream)", e.MemKeptAfter, e.MemUsedAfter, e.MemKept, e.MemUsed)
		}
	},
	flow.OpEmitStream: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if e.PosAfter != e.Pos {
			fail("moved the position from %d to %d", e.Pos, e.PosAfter)
		}
		if e.EmitsAfter != e.Emits+1 {
			fail("delivered %d elements, not 1", e.EmitsAfter-e.Emits)
		} else if fmt.Sprint(e.EmitLastAfter) != fmt.Sprint(e.Args[0]) {
			fail("delivered %v, not %v", e.EmitLastAfter, e.Args[0])
		}
	},
	flow.OpNewFrame: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if got := idOf(firstResult(e)); got == "nil" || got == e.Frame {
			fail("returned the frame %s, not a new one (the current one is %s)", got, e.Frame)
		}
		if e.FrameAfter != e.Frame {
			fail("changed the current frame to %s", e.FrameAfter)
		}
		if e.ResultFrameSet {
			fail("returned a frame with a slot set")
		}
	},
	flow.OpSetEnv: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if want := idOf(e.Args[0]); e.EnvAfter != want {
			fail("made %s the environment, not %s", e.EnvAfter, want)
		}
	},
	flow.OpEnv: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if got := idOf(firstResult(e)); got != e.Env {
			fail("returned the environment %s, the current one is %s", got, e.Env)
		}
	},
	flow.OpSetCapture: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		slot := int(c.Imm.(prim.Slot))
		if slot >= len(e.Vals) || len(e.ValsAfter) != len(e.Vals) {
			fail("the frame has %d slots before and %d after, for slot %d", len(e.Vals), len(e.ValsAfter), slot)
			return
		}
		if want, got := fmt.Sprint(e.Args[0]), fmt.Sprint(e.ValsAfter[slot]); got != want {
			fail("set slot %d to %s, not %s", slot, got, want)
		}
		for i := range e.Vals {
			if i != slot && fmt.Sprint(e.Vals[i]) != fmt.Sprint(e.ValsAfter[i]) {
				fail("changed slot %d, not only slot %d", i, slot)
			}
		}
	},
	flow.OpFrameVal: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		slot := int(c.Imm.(prim.Slot))
		if idOf(e.Args[0]) != e.Frame || slot >= len(e.Vals) {
			return
		}
		if want, got := fmt.Sprint(e.Vals[slot]), fmt.Sprint(firstResult(e)); got != want {
			fail("returned %s for slot %d, the frame holds %s", got, slot, want)
		}
	},
	flow.OpCallRule: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if ok, known := resBool(e, 1); known && !ok && e.PosAfter != e.Pos {
			fail("failed and moved the position")
		}
	},
	flow.OpCallOutline: func(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
		if ok, known := resBool(e, 1); known && !ok && e.PosAfter != e.Pos {
			fail("failed and moved the position")
		}
	},
}

// flowVal is a node value as the probes record it (confVal).
type flowVal struct {
	Type       string
	Start, End int
	Kids       int
	Text       string
}

// valOf parses the recorded form of a node value.
func valOf(x any) (v flowVal, ok bool) {
	s, isStr := x.(string)
	if !isStr || !strings.HasPrefix(s, "val:") {
		return v, false
	}
	parts := strings.SplitN(s[len("val:"):], ":", 5)
	if len(parts) != 5 {
		return v, false
	}
	v.Type, v.Text = parts[0], parts[4]
	for i, dst := range []*int{&v.Start, &v.End, &v.Kids} {
		n, err := strconv.Atoi(parts[1+i])
		if err != nil {
			return v, false
		}
		*dst = n
	}
	return v, true
}

// checkNodeBuilt checks the node value that an operation returns: the span from the argument start to
// the position, and the type kinds that the operation builds (any of types; none for any type).
func checkNodeBuilt(e *flowEvent, fail failFunc, types ...string) (flowVal, bool) {
	v, ok := valOf(firstResult(e))
	if !ok {
		return v, false
	}
	if start, ok := argInt(e, 0); ok && v.Start != start {
		fail("built a value from %d, not from %d", v.Start, start)
	}
	if v.End != e.Pos {
		fail("built a value to %d, not to the position %d", v.End, e.Pos)
	}
	if len(types) > 0 && !slices.Contains(types, v.Type) {
		fail("built a value of type %s, not of %s", v.Type, strings.Join(types, " or "))
	}
	return v, true
}

// isTuple reports that the value is a tuple node.
func isTuple(v flowVal) bool { return strings.Trim(v.Type, "()") == "tuple" }

// checkShapeValue checks the value that an operation makes of the result x (its argument 0) for the
// shape sh, where the expression takes one element (scope false) or the body of a scope matched (scope
// true): the tuple node of the elements, or the element itself. It compares what is strictly
// comparable on the recorded values: the kind of the value and, for a tuple that the operation makes,
// its range and number of children.
func checkShapeValue(e *flowEvent, fail failFunc, sh prim.Shape, scope bool) {
	tuple := sh.Mode == prim.SingleTuple
	if scope {
		tuple = sh.Tuple
	}
	got, isNode := valOf(firstResult(e))
	start, _ := argInt(e, 1)
	switch {
	case tuple && !isNode:
		fail("returned %v, not a tuple node", firstResult(e))
	case tuple && !isTuple(got):
		fail("returned %v, not a tuple node", firstResult(e))
	case tuple && !sh.Elems && (got.Start != start || got.End != e.Pos || got.Kids != sh.K):
		fail("made the tuple %d to %d of %d children, not %d to %d of %d", got.Start, got.End, got.Kids, start, e.Pos, sh.K)
	case tuple && sh.Elems && !sh.Shared:
		if want, ok := valOf(e.Args[0]); ok && (got.Kids != want.Kids || got.Start != want.Start || got.End != want.End) {
			fail("returned the tuple %v, not the result %v itself", firstResult(e), e.Args[0])
		}
	case !tuple && !sh.Elems && fmt.Sprint(firstResult(e)) != fmt.Sprint(e.Args[0]):
		fail("returned %v, not the element %v itself", firstResult(e), e.Args[0])
	}
}

// checkAttached checks the value that an operation attaches captures to: the value itself where no
// capture is set, and otherwise a node.
func checkAttached(e *flowEvent, fail failFunc, attached bool) {
	switch res := firstResult(e); {
	case !attached && fmt.Sprint(res) != fmt.Sprint(e.Args[0]):
		fail("returned %v with no capture to attach, not %v itself", res, e.Args[0])
	case attached && res == nil:
		fail("returned no value with captures to attach")
	}
}

// checkOnlyAction checks an operation that evaluates an action, a predicate or a binding, or builds
// a value: it leaves the position, the gathered values, the recovered errors, the silent depth and
// the record of expectations as they were. (Bind sets a variable of the environment, which the
// environment operations check.)
func checkOnlyAction(e *flowEvent, fail failFunc) {
	if e.PosAfter != e.Pos {
		fail("moved the position from %d to %d", e.Pos, e.PosAfter)
	}
	if e.Kids != e.KidsAfter || e.Rec != e.RecAfter || e.Silent != e.SilentAfter || e.Far != e.FarAfter || e.Exps != e.ExpsAfter {
		fail("changed the state (%v)", flowChanged(e))
	}
}

// checkWalkOnly checks an operation of a walk of a run or scan site, which writes only the state of
// the walk: it leaves the position, the gathered values and the recovered errors as they were.
func checkWalkOnly(in *ref.Input, c flowCall, e *flowEvent, fail failFunc) {
	if e.PosAfter != e.Pos {
		fail("moved the position from %d to %d", e.Pos, e.PosAfter)
	}
	if e.Kids != e.KidsAfter || e.Rec != e.RecAfter || e.Silent != e.SilentAfter {
		fail("changed the state (%v)", flowChanged(e))
	}
}

// flowChanged lists the recorded components of the parser state that the call changed: the gathered
// values (kids), the recovered errors (rec), the silent depth (silent), the record of expectations
// (exp), the current frame (frame), the values of the current frame (vals) and the environment (env).
func flowChanged(e *flowEvent) []string {
	var ch []string
	if e.Kids != e.KidsAfter {
		ch = append(ch, "kids")
	}
	if e.Rec != e.RecAfter {
		ch = append(ch, "rec")
	}
	if e.Silent != e.SilentAfter {
		ch = append(ch, "silent")
	}
	if e.Far != e.FarAfter || e.Exps != e.ExpsAfter {
		ch = append(ch, "exp")
	}
	if e.Frame != e.FrameAfter {
		ch = append(ch, "frame")
	} else if fmt.Sprint(e.Vals) != fmt.Sprint(e.ValsAfter) {
		ch = append(ch, "vals")
	}
	if e.Env != e.EnvAfter {
		ch = append(ch, "env")
	}
	return ch
}

// idOf is the identity of a recorded frame or environment: "nil" or "ptr:" and the address.
func idOf(x any) string {
	if x == nil {
		return "nil"
	}
	return fmt.Sprint(x)
}

// checkMerge checks the merge of the expectations of a call, as the composition expect does for each
// of them: nothing under silence or before the farthest failure, and otherwise the record at the
// position of the merge.
func checkMerge(e *flowEvent, fail failFunc) {
	far, ok := argInt(e, 0)
	n, isLen := listLen(e.Args[1])
	if !ok || !isLen {
		return
	}
	switch {
	case e.Silent > 0 || far < e.Far || n == 0:
		if e.FarAfter != e.Far || e.ExpsAfter != e.Exps {
			fail("merged %d expectations at %d into the record at %d with silent depth %d and changed it", n, far, e.Far, e.Silent)
		}
	case far > e.Far:
		if e.FarAfter != far || e.ExpsAfter < 1 {
			fail("left the record at %d with %d expectations after a merge at %d, past the farthest failure %d", e.FarAfter, e.ExpsAfter, far, e.Far)
		}
	default:
		if e.FarAfter != far || e.ExpsAfter < e.Exps {
			fail("left the record at %d with %d expectations of %d after a merge at the farthest failure", e.FarAfter, e.ExpsAfter, e.Exps)
		}
	}
}

func firstResult(e *flowEvent) any {
	if len(e.Results) == 0 {
		return nil
	}
	return e.Results[0]
}

func firstString(e *flowEvent) (string, bool) {
	s, ok := firstResult(e).(string)
	if !ok || !strings.HasPrefix(s, "s:") {
		return "", false
	}
	return s[2:], true
}
