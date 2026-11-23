package print

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// tsLibMarker starts the part of lib.ts in the TypeScript library that is copied into generated code.
const tsLibMarker = "// --- Library ---\n"

// A TypeScript parser (the step print-ts of the TypeScript backend) behaves exactly like the engine (and the generated Go
// parsers): lib.ts, with the library (LibEmbed), parser.ts, with the code generated from the grammar,
// and with opts.Types types.ts, with the declarations of the grammar's types. LibImport imports the
// library from opts.LibModule instead of writing it, and LibOnly writes only lib.ts (LibFiles).
// common.TypeScript describes the options.
//
// Each parsing expression becomes a function and actions and predicates become TypeScript
// expressions, as in the Go generator; the library (internal/backend/codegen/typescript/lib) mirrors
// the Go library. opts.Package is not used. opts.Types adds declarations and Node-to-value
// conversion entry points.

// maxLeaderBits is the number of leaders of each left-recursion cycle that have a bit in the present
// sets of the library's dependency memo (Rule.leaderBit), as in the Go library. The sign bit stays
// clear: a negative present set is the identity of a larger one (Parser.presentOf).
const maxLeaderBits = 31

// Hooks change how print-ts prints, for tests only: a test sets them on its target
// (driver.Target.TSHooks), and production leaves them nil.
type Hooks struct {
	// Generator, if not nil, is called with the generator once it has printed the parser.
	Generator func(*Generator)
	// Stream prints the parser as with -stream (common.GenOptions.Stream), for the tests that
	// generate through a target without the option: the table of the stream and the resumable
	// functions of its parse, printed as generators after the batch code, and createStream.
	Stream bool
}

// Validate checks the options of a TypeScript parser that do not depend on the grammar, other than
// those of the library alone (common.LibOnly), which the caller handles, and returns them.
func Validate(opts common.GenOptions) (common.GenOptions, error) {
	if opts.Lib == common.LibImport && opts.LibModule == "" {
		return opts, fmt.Errorf("the TypeScript library module (the specifier the parser imports the library from) is required with the import library mode")
	}
	if opts.Lib != common.LibImport && opts.LibModule != "" {
		return opts, fmt.Errorf("the TypeScript library module can only be set with the import library mode")
	}
	if opts.Stream && opts.Document {
		return opts, fmt.Errorf("stream generation cannot be combined with document generation in TypeScript")
	}
	if opts.Stream && opts.Observability {
		return opts, fmt.Errorf("stream generation cannot be combined with observability in TypeScript")
	}
	if opts.RecognizeOnly {
		switch {
		case opts.Types || opts.TypedOnly || opts.Document || opts.Stream:
			return opts, fmt.Errorf("recognize-only output generates only Recognize: it cannot have typed output, typed-only output, a Document or a stream")
		}
		opts.Recognize = true
	}
	if opts.TypedOnly {
		switch {
		case !opts.Types:
			return opts, fmt.Errorf("typed-only output needs typed output (-types)")
		case opts.Document || opts.Stream:
			return opts, fmt.Errorf("typed-only output cannot have a Document or a stream, which use the untyped Node (Recognize is allowed)")
		}
	}
	if opts.LogicalVariant != "" {
		return opts, fmt.Errorf("the variants of logical units are only supported for Go")
	}
	if err := opts.ValidateMaxDepth(true); err != nil {
		return opts, err
	}
	if err := opts.ValidateDefaultValue(); err != nil {
		return opts, err
	}
	return opts, nil
}

// newGenerator returns the generator of the TypeScript parser of prog with the options opts, which
// prints the rules that live holds, starting with the Node table.
func newGenerator(prog *common.Program, opts common.GenOptions, live *tsLive) *Generator {
	tsg := &Generator{Generator: common.Generator{Prog: prog, Table: "rules", Observability: opts.Observability, Document: opts.Document}, plainUsed: map[*common.Rule]bool{}, live: live, printed: map[string]map[int][]int{}, recognizeOnly: opts.RecognizeOnly, parseEntries: !opts.RecognizeOnly && !opts.TypedOnly,
		streamParser: opts.Stream, plainGenUsed: map[*common.Rule]bool{}, genBodies: map[*common.Rule]string{}}
	// Limit controls count logical compute units (spec/runtime/parsing.md, Compute Units) in every
	// form of the functions, a Document's included: no form charges a budget.
	tsg.limits = !opts.NoLimitControls
	tsg.Logical = tsg.limits
	tsg.optionsType = "ParseOptions"
	if opts.NoLimitControls {
		tsg.optionsType = "PlainParseOptions"
	}
	return tsg
}

// generate generates the rules of the Node table and, if rec is not nil, of the recognizer rec, and
// returns the first error of their generation.
func (tsg *Generator) generate(rec *common.Program) error {
	tsg.Desc(core.FixedDescs[0]) // the fixed expectations come first
	tsg.rules()
	tsg.runKeys()
	if tsg.PrintErr != nil {
		return tsg.PrintErr
	}
	if tsg.reachErr != nil {
		return tsg.reachErr
	}
	if tsg.streamParser {
		tsg.streamRules()
		if tsg.PrintErr != nil {
			return tsg.PrintErr
		}
	}
	if rec != nil {
		prog := tsg.Prog
		tsg.Prog, tsg.Table = rec, "recRules"
		tsg.rules()
		tsg.runKeys()
		tsg.Prog, tsg.Table = prog, "rules"
		if tsg.PrintErr != nil {
			return tsg.PrintErr
		}
		if tsg.reachErr != nil {
			return tsg.reachErr
		}
	}
	return nil
}

// print returns the files of the parser whose rules tsg generated, with the options opts, the
// recognizer rec (nil if there is none) and the start rule start.
func (tsg *Generator) print(opts common.GenOptions, rec *common.Program, start *common.Rule) ([]common.GeneratedFile, error) {
	prog := tsg.Prog
	var out strings.Builder
	fmt.Fprintf(&out, "const G = mkGrammar(%d, %v, %v, %v);\n\n", common.GeneratedMaxDepth(opts), tsg.useSparseMemo(prog, rec), opts.Observability, !opts.NoLimitControls)
	// A recognize-only or typed-only parser has no parse entry points: it prints only the entries it keeps.
	if tsg.parseEntries {
		fmt.Fprintf(&out, "/**\n * Parses the whole input with the rule %s. unit selects the position unit (CodePoints by default).\n", start.Name)
		out.WriteString(" * A string is parsed as its UTF-8 encoding would be (lone surrogates as U+FFFD), and bytes are\n")
		out.WriteString(" * decoded as UTF-8 (invalid sequences as U+FFFD, one per byte). If the parse recovered from errors\n")
		out.WriteString(" * with #recover, the result has both the node and SyntaxErrors.\n */\n")
		// With limit controls every parse is controlled: the entry points without options use the default
		// compute limit of the input, as parseWithOptions does with no limits.
		parseBody, ruleBody := fmt.Sprintf("run(G, R%d, nseen, input, unit)", start.ID), "run(G, r, nseen, input, unit)"
		if !opts.NoLimitControls {
			parseBody, ruleBody = "parseWithOptions(input, { unit: unit ?? CodePoints })", "parseRuleWithOptions(name, input, { unit: unit ?? CodePoints })"
		}
		fmt.Fprintf(&out, "export function parse(input: string | Uint8Array, unit?: Unit): ParseResult {\n  return %s;\n}\n\n", parseBody)
		if !opts.NoLimitControls {
			fmt.Fprintf(&out, `/** Parses the whole input with the start rule name, a rule marked @start; "" selects the default start rule. */
export function parseRule(name: string, input: string | Uint8Array, unit?: Unit): ParseResult {
  return %s;
}

`, ruleBody)
		} else {
			fmt.Fprintf(&out, `/** Parses the whole input with the start rule name, a rule marked @start; "" selects the default start rule. */
export function parseRule(name: string, input: string | Uint8Array, unit?: Unit): ParseResult {
  const r = startIn(rules, ruleNames, starts, defaultStart, name);
  if (r instanceof Error) return { node: null, error: r };
  return %s;
}

`, ruleBody)
		}
	}
	if rec != nil || tsg.recognizeOnly {
		ref, seen := tsg.recognizerEntry(rec, start)
		fmt.Fprintf(&out, "/**\n * Checks that the whole input matches the rule %s without building a tree, and returns the syntax\n", start.Name)
		out.WriteString(" * error parse would return (SyntaxErrors for errors recovered with #recover), or null. Actions needed by\n")
		out.WriteString(" * predicates are evaluated and can report runtime errors; other actions are skipped.\n */\n")
		recBody := fmt.Sprintf("run(G, %s, %s, input, unit).error", ref, seen)
		if !opts.NoLimitControls {
			recBody = "recognizeWithOptions(input, { unit: unit ?? CodePoints })"
		}
		fmt.Fprintf(&out, "export function recognize(input: string | Uint8Array, unit?: Unit): Error | null {\n  return %s;\n}\n\n", recBody)
	}
	if tsg.parseEntries {
		fmt.Fprintf(&out, `/** Parses with invocation-local position and depth options. */
export function parseWithOptions(input: string | Uint8Array, options?: %[1]s): ParseResult {
  const o = resolveOptions(G, options, input);
  if (o instanceof Error) return { node: null, error: o };
  return withObservedOptions(o, control => run(G, R%[2]d, nseen, input, o.unit, o.maxDepth, control, o), error => ({ node: null, error }));
}

/** Parses a named start rule ("" for the default) with invocation-local options, validated before rule lookup. */
export function parseRuleWithOptions(name: string, input: string | Uint8Array, options?: %[1]s): ParseResult {
  const o = resolveOptions(G, options, input);
  if (o instanceof Error) return { node: null, error: o };
  const r = startIn(rules, ruleNames, starts, defaultStart, name);
  if (r instanceof Error) return { node: null, error: r };
  return withObservedOptions(o, control => run(G, r, nseen, input, o.unit, o.maxDepth, control, o), error => ({ node: null, error }));
}

`, tsg.optionsType, start.ID)
	}
	if rec != nil || tsg.recognizeOnly {
		ref, seen := tsg.recognizerEntry(rec, start)
		fmt.Fprintf(&out, `/** Recognizes with invocation-local options; predicate-dependent actions may run. */
export function recognizeWithOptions(input: string | Uint8Array, options?: %[1]s): Error | null {
  const o = resolveOptions(G, options, input);
  if (o instanceof Error) return o;
  return withObservedOptions<Error | null>(o, control => run(G, %[2]s, %[3]s, input, o.unit, o.maxDepth, control, o).error, error => error);
}

`, tsg.optionsType, ref, seen)
	}
	var types string
	if opts.Types {
		var typed string
		var err error
		if types, typed, err = tsg.typedTS(start, opts.Stream); err != nil {
			return nil, err
		}
		out.WriteString(typed)
	}
	if opts.Document {
		tsg.document(&out, start, opts.Types)
	}
	if opts.Stream {
		tsg.streamEntry(&out, start)
	}
	if len(tsg.FieldTypes) > 0 {
		out.WriteString(tsg.fieldCheckSource())
	}
	out.WriteString(tsg.Vars.String())
	if opts.Expectations {
		fmt.Fprintf(&out, "\nG.descs = %s;\nG.expectations = true;\n", TsStrings(tsg.Descs))
	}
	if prog.Run.RecordsSourceInfo {
		out.WriteString("G.sourceInfo = true;\n")
	}
	for _, v := range prog.Vars.All() {
		switch x := v.Init.(type) {
		case int:
			fmt.Fprintf(&out, "G.initialVars.push([%s, %s]);\n", TsString(v.Name), tsInt(x))
		case string:
			fmt.Fprintf(&out, "G.initialVars.push([%s, %s]);\n", TsString(v.Name), TsString(x))
		default:
			fmt.Fprintf(&out, "G.initialVars.push([%s, %v]);\n", TsString(v.Name), x)
		}
	}
	out.WriteString("\n")
	out.WriteString(tsg.Init.String())
	out.WriteString("\n")
	out.WriteString(tsg.Methods.String())
	features, err := common.ParserFeatures(prog, rec, opts, tsg.Params, tsg.tables.Plan)
	if err != nil {
		return nil, err
	}
	return tsFiles(opts, features, tsg.Params.UsedFeatures, tsg.tables.Library, out.String(), types)
}

// useSparseMemo reports whether the parser keeps the representation selector of its first-call table:
// with the parameter SparseMemo on, when one of the rule tables of prog and rec, if not nil, can cross
// the sparse-storage threshold.
func (tsg *Generator) useSparseMemo(prog, rec *common.Program) bool {
	return tsg.sparseMemo && (prog.Run.NSeen > 64 || rec != nil && rec.Run.NSeen > 64)
}

// TsPrintRuns returns the code points that strconv.IsPrint accepts, for the library's quoteGo: the
// lengths of the alternating runs of code points that it rejects and accepts, from U+0000, in base
// 36 separated by commas (about 3 KB). Writing the table of the Go that generates the parser, rather
// than relying on the JavaScript library's Unicode properties, makes toString quote as the engine's
// Node.String does even where their Unicode versions differ.
var TsPrintRuns = sync.OnceValue(func() string {
	var b strings.Builder
	prev, start := false, 0
	for r := 0; r <= unicode.MaxRune; r++ {
		if p := strconv.IsPrint(rune(r)); p != prev {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatInt(int64(r-start), 36))
			prev, start = p, r
		}
	}
	return b.String()
})

// fieldCheckSource returns the run-time type check the parser uses: castFailure if called, which
// the library's fieldConforms serves.
func (g *Generator) fieldCheckSource() string {
	const f = "castFailure"
	if strings.Contains(g.Init.String(), f+"(") || strings.Contains(g.Vars.String(), f+"(") || strings.Contains(g.Methods.String(), f+"(") {
		return "\n" + tsCastFailure + "\n"
	}
	return ""
}

// tsFieldTypeDecl declares the check type variable v.
func tsFieldTypeDecl(v string, t *types.FieldType) string {
	return fmt.Sprintf("const %s: FieldType = %s;\n", v, tsFieldType(t))
}

// tsFieldType writes a field type as a generated FieldType value.
func tsFieldType(t *types.FieldType) string {
	elem := "null"
	if t.Elem != nil {
		elem = tsFieldType(t.Elem)
	}
	alts := make([]string, len(t.Alts))
	for i, a := range t.Alts {
		alts[i] = tsFieldType(a)
	}
	return fmt.Sprintf("{ kind: %s, name: %s, elem: %s, alts: [%s], text: %s }", TsString(string(rune(t.Kind))), TsString(t.Name), elem, strings.Join(alts, ", "), TsString(t.Text))
}

// Generator generates TypeScript. It shares the expectation table, names and analyses of the Go
// generator through common.Generator.
type Generator struct {
	common.Generator
	// plainUsed holds the plain rules whose function (plainCall) is called.
	plainUsed map[*common.Rule]bool
	// streamParser reports a -stream parser, which prints the resumable functions of its stream after
	// the batch code (streamRules); stream reports that the printer prints them.
	streamParser, stream bool
	// plainGenUsed holds the plain rules whose resumable function (plainGenCall) is called, and
	// genBodies the resumable functions of the bodies of the rules of the stream, by rule.
	plainGenUsed map[*common.Rule]bool
	genBodies    map[*common.Rule]string
	// keyUsed holds the run sites of the rule table being generated whose key constant
	// (runKeyName) the emitted code refers to; runKeys writes only those.
	keyUsed map[int]bool
	// limits reports a parser with limit controls, which counts logical compute units (Logical) and
	// the storage that the memory limit counts.
	limits bool
	// optionsType is the name of the library's options type that the entry points take: ParseOptions
	// with limit controls, PlainParseOptions without.
	optionsType string
	// sparseMemo is the parameter SparseMemo of the printing (Params).
	sparseMemo bool
	// live holds the rules that a parse can enter (reach.go).
	live *tsLive
	// recognizeOnly is the option RecognizeOnly: the program printed is the recognizer itself, and
	// the parser has no parse entry point.
	recognizeOnly bool
	// parseEntries is false for a parser with no parse entry point (RecognizeOnly or TypedOnly): it
	// prints no parse, parseRule or parseWithOptions. The rule table stays for parseRuleValue, unless
	// the parser is recognize-only.
	parseEntries bool
	// table is the table being generated, planned and lowered to the Exec stage (lowerTable), whose
	// functions the printer prints; nil if its Core IR or its lowering failed (PrintErr).
	table *plandata.Table[code.Exec]
	// tables are the tables of the compilation at the Exec stage that the printer prints.
	tables   *execdata.Tables
	reachErr error
	// printed holds, for each table, the rules that the printed code of each rule calls (by ID).
	printed map[string]map[int][]int
	// startType and startRepr are, with types, the TypeScript type of the start rule's value and the
	// recipe that converts it (typedTS).
	startType string
	startRepr int
	// streamType and streamRepr are, with types and a stream, the TypeScript type of the elements of
	// the start rule's #stream repetition and the recipe that converts them (typedTS); streamType is
	// empty otherwise.
	streamType string
	streamRepr int
	// ExecFuncs counts the functions printed from Exec IR (execRoute).
	ExecFuncs int
	// LoweredActions counts the action expressions printed from Exec IR (actionStmts).
	LoweredActions int
}

// TsString returns s as a TypeScript string literal (JSON is valid TypeScript).
func TsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TsStrings returns xs as a TypeScript array literal of strings.
func TsStrings(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = TsString(x)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// TsInts returns xs as a TypeScript array literal of numbers.
func TsInts[T ~int32 | ~byte](xs []T) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = strconv.Itoa(int(x))
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func tsJoinCondition(parts []string, op string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	mid := len(parts) / 2
	return "(" + tsJoinCondition(parts[:mid], op) + op + tsJoinCondition(parts[mid:], op) + ")"
}

// recognizerEntry returns the constant of the rule that Recognize runs and the seen count of its
// table: the recognizer rec's rule of start, or, for a recognize-only parser, the rule of its own table.
func (g *Generator) recognizerEntry(rec *common.Program, start *common.Rule) (ref, seen string) {
	if g.recognizeOnly {
		return fmt.Sprintf("R%d", start.ID), "nseen"
	}
	return fmt.Sprintf("Q%d", rec.ByName[start.Name].ID), "recNseen"
}

func (g *Generator) seenVar() string {
	if g.Table == "rules" {
		return "nseen"
	}
	return "recNseen"
}

// ruleVar returns the constant that holds the rule r of the table being generated.
func (g *Generator) ruleVar(r *common.Rule) string {
	if g.Table == "rules" {
		return fmt.Sprintf("R%d", r.ID)
	}
	return fmt.Sprintf("Q%d", r.ID)
}

func (g *Generator) plainPrefix() string {
	if g.Table == "rules" {
		return "r"
	}
	return "q"
}

// Method emits a function of an expression with the given body and returns its name.
//
// fn turns a call expression into a function.
func (g *Generator) Method(comment, body string) string {
	name, shared := g.MethodName("Parser/R", body)
	if shared {
		return name
	}
	if comment != "" {
		fmt.Fprintf(&g.Methods, "// %s\n", comment)
	}
	param := "p"
	if !strings.Contains(body, "p.") && !strings.Contains(body, "(p)") {
		param = "_p" // not reported as unused (the body is in generated code only, without user text)
	}
	fmt.Fprintf(&g.Methods, "function %s(%s: Parser): R {\n%s}\n\n", name, param, body)
	return name
}

// fn returns a function (a Matcher) that does what the call expression x does.
func (g *Generator) fn(x string) string {
	if name, ok := strings.CutSuffix(x, "(p)"); ok && !strings.ContainsAny(name, "(). ") {
		return name
	}
	return g.Method("", "  return "+x+";\n")
}

// tsActxUse matches a use of the evaluation context c in the text of an action expression.
var tsActxUse = regexp.MustCompile(`\bc\b`)

// tsActxParam returns the name of the evaluation context parameter of a function that returns the action expression
// body: c, or _c if the action expression reads no context (a leading _ keeps it from being reported as unused).
func tsActxParam(body string) string {
	if tsActxUse.MatchString(body) {
		return "c"
	}
	return "_c"
}

// action emits a function that evaluates the action site i (an action or a predicate) and returns its
// name.
func (g *Generator) action(i int, s *check.Scope, locals map[string]string) string {
	name := g.Name("a")
	g.Methods.WriteString(g.actionFunc(name, i, s, locals))
	return name
}

func (g *Generator) rules() {
	g.startTSTable()
	init := &g.Init
	bodies := map[*common.Rule]string{}
	scopes := map[*common.Rule]*check.Scope{}
	var all []*common.Rule
	for _, r := range append(append([]*common.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...) {
		if !g.pruned(r) {
			all = append(all, r)
		}
	}
	for _, r := range all {
		s := check.NewScope()
		scopes[r] = s
		if _, ok := r.Def.Expr.(*ast.OperatorsRule); !ok {
			g.Cur = r
			if call, ok := g.ruleBodyCall(r, s); ok {
				bodies[r] = call
			} else {
				g.noFunction("the body of rule " + r.Name)
				bodies[r] = "null" // the generation fails with the error g.noFunction records
			}
		}
	}
	// Each rule is a constant, which the generated code refers to.
	var vars []string
	leaders := map[int]int{}
	for _, r := range all {
		if !g.tableRow(r) {
			continue
		}
		value := tsShaper(g.Prog, r.BodyExpr())
		if _, ok := r.Def.Expr.(*ast.OperatorsRule); ok {
			value = 0 // the lines build the rule's value
		}
		fmt.Fprintf(init, "const %s = mkRule(%d, %s, %s, %d, %v, %v, %d, %v, %s);\n", g.ruleVar(r),
			r.ID, TsString(r.Name), TsStrings(scopes[r].Names), value, r.Memo && !r.Transient, r.Leader, r.Seen, r.Novalue, tsIntList(g.Prog.Vars.Slots(r.Vars)))
		vars = append(vars, g.ruleVar(r))
		if r.Splice {
			fmt.Fprintf(init, "%s.splice = true;\n", g.ruleVar(r))
		}
		if g.Document && (r.Leader || r.Memo) {
			fmt.Fprintf(init, "%s.docMemo = true;\n", g.ruleVar(r))
		}
		if g.Document && r.Positional {
			fmt.Fprintf(init, "%s.positional = true;\n", g.ruleVar(r))
		}
		if r.Rec {
			fmt.Fprintf(init, "%s.rec = true;\n", g.ruleVar(r))
		}
		if r.Cycle != 0 {
			fmt.Fprintf(init, "%s.cycle = %d;\n", g.ruleVar(r), r.Cycle)
			if r.Leader {
				// The leaders of each cycle are numbered, and the first maxLeaderBits have a bit in
				// present sets.
				k := leaders[r.Cycle]
				if k > 0 {
					fmt.Fprintf(init, "%s.leaderIndex = %d;\n", g.ruleVar(r), k)
				}
				if k < maxLeaderBits {
					fmt.Fprintf(init, "%s.leaderBit = %d;\n", g.ruleVar(r), 1<<k)
				}
				leaders[r.Cycle]++
			}
		}
		if w := r.WhereName(); w != r.Name {
			fmt.Fprintf(init, "%s.where = %s;\n", g.ruleVar(r), TsString(w))
		}
		if r.NodeType != "" {
			fmt.Fprintf(init, "%s.nodeType = %s;\n", g.ruleVar(r), TsString(r.NodeType))
		}
		if r.OpNodeType != "" {
			fmt.Fprintf(init, "%s.opNodeType = %s;\n", g.ruleVar(r), TsString(r.OpNodeType))
		}
	}
	if g.Table == "rules" && !g.recognizeOnly {
		// The table and the start rules, for parseRule and parseRuleValue (a recognize-only parser has none).
		fmt.Fprintf(init, "const rules: Rule[] = [%s];\n", strings.Join(vars, ", "))
		var names []string
		for _, r := range g.Prog.RuleList {
			names = append(names, r.Name)
		}
		fmt.Fprintf(init, "const ruleNames = %s;\n", TsStrings(names))
		fmt.Fprintf(init, "const starts = %s;\n", TsStrings(g.Prog.StartRules))
		fmt.Fprintf(init, "const defaultStart = %s;\n", TsString(g.Prog.DefaultStartRule))
	}
	fmt.Fprintf(init, "const %s = %d;\n", g.seenVar(), g.Prog.Run.NSeen)
	for _, r := range all {
		if pr, ok := r.Def.Expr.(*ast.OperatorsRule); ok {
			g.Cur = r
			g.operatorsRule(r, pr)
			if r.Action != nil && !r.Novalue {
				// The whole-rule action, which the library runs on the folded value after each call.
				locals := map[string]string{rprogram.OperatorsRuleValue: "c.value"}
				fmt.Fprintf(init, "%s.action = %s;\n", g.ruleVar(r), g.action(g.RuleSite(r), check.NewScope(), locals))
			}
			continue
		}
		if g.slotReached(r) {
			fmt.Fprintf(init, "%s.body = %s;\n", g.ruleVar(r), g.fn(bodies[r]))
		}
		if r.Action != nil && g.tableRow(r) {
			fmt.Fprintf(init, "%s.action = %s;\n", g.ruleVar(r), g.action(g.RuleSite(r), scopes[r], nil))
		}
	}
	// The functions of plain rules that are called (all calls are generated by now).
	for _, r := range all {
		if g.plainUsed[r] {
			g.plainCall(r, bodies[r])
		}
	}
	if g.Table != "rules" {
		return // the recognizer shares the struct fields
	}
	// Struct fields, used to detect references to undeclared fields at run time.
	var names []string
	for name, spec := range g.Prog.Types {
		if td, ok := spec.(*ast.TypeDef); ok && td.Struct() != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		sz := g.Prog.Run.StructZeroOf(name)
		var fields []string
		for _, f := range sz.Names {
			fields = append(fields, "["+TsString(f)+", "+TsString(sz.Zeros[f])+"]")
		}
		fmt.Fprintf(init, "G.structFields.set(%s, structFieldsOf([%s]));\n", TsString(name), strings.Join(fields, ", "))
	}
}

// plainCall writes the function r<id>, which does what invokePlain does for the rule r (a rule never
// memoized and without captures) with a direct call of its body: the template invokePlain (tsTemplates)
// printed for r.
func (g *Generator) plainCall(r *common.Rule, body string) {
	f, err := g.template("invokePlain")
	if err != nil {
		g.printErr(err)
		return
	}
	text, err := Statements(g.tables.Library.Module, f, tsTemplateTarget(g.ruleVar(r), body, r.Rec, r.Novalue), 1)
	if err != nil {
		g.printErr(err)
		return
	}
	name := g.tracePlainCall(r, fmt.Sprintf("%s%d", g.plainPrefix(), r.ID))
	fmt.Fprintf(&g.Methods, "// %s, called as by invokePlain\nfunction %s(p: Parser): R {\n%s}\n\n", r.Name, name, text)
}

// printErr records err as the error of the generation, unless one is recorded already.
func (g *Generator) printErr(err error) {
	if g.PrintErr == nil {
		g.PrintErr = err
	}
}

// tsShaper returns the library shaper of the expression e, its static shape (internal/frontend/check/value_shape.go), packed
// as the library's Shaper.
func tsShaper(prog *common.Program, e ast.Expr) int {
	s := int(check.SingleModeOf(prog.Checker(), e))<<2 | check.ValuedCount(prog.Checker(), e)<<5
	if check.IsElems(prog.Checker(), e) {
		s |= 1
	}
	if check.ScopeTuple(prog.Checker(), e) {
		s |= 2
	}
	if check.IsElems(prog.Checker(), e) && check.SharesTuple(prog.Checker(), e) {
		s |= 16
	}
	return s
}

func (g *Generator) operatorsRule(r *common.Rule, e *ast.OperatorsRule) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.operatorsRule = {\n", g.ruleVar(r))
	// The lines of the rule's Core IR, in the order of e, if the printer prints them from Exec IR.
	var co *core.OperatorsRule
	if g.Exec != nil {
		co, _ = g.Core().Rules[r.ID].Body.(*core.OperatorsRule)
	}
	if e.Skip != nil {
		var skip *core.OpLine
		if co != nil {
			skip = co.Skip
		}
		m, ok := g.lineFunc(plandata.Key{Use: plandata.OperatorsSkip, Rule: r.ID}, skip, check.NewScope())
		if !ok {
			g.noFunction("the skip of the Operators Rule " + r.Name)
			m = "null"
		}
		fmt.Fprintf(&b, "  skip: %s,\n", g.fn(m))
	} else {
		b.WriteString("  skip: null,\n")
	}
	b.WriteString("  operands: [\n")
	for i, o := range e.Operands {
		var l *core.OpLine
		if co != nil {
			l = co.Operands[i]
		}
		fmt.Fprintf(&b, "    %s,\n", g.operatorsRuleLine(o.Expr, o.Action, false, plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: i}, l))
	}
	b.WriteString("  ],\n")
	var prefix, led []string
	id := 0
	for i, l := range e.Levels {
		for _, op := range l.Operators {
			var line *core.OpLine
			if co != nil {
				line = co.Ops[id].Line
			}
			s := fmt.Sprintf("    { id: %d, kind: %s, assoc: %s, level: %d, line: %s },\n", id, TsString(op.Kind), TsString(op.Assoc), i+1, g.operatorsRuleLine(op.Expr, op.Action, true, plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: len(e.Operands) + id}, line))
			id++
			if op.Kind == ast.Prefix {
				prefix = append(prefix, s)
			} else {
				led = append(led, s)
			}
		}
	}
	fmt.Fprintf(&b, "  prefix: [\n%s  ],\n", strings.Join(prefix, ""))
	fmt.Fprintf(&b, "  led: [\n%s  ],\n", strings.Join(led, ""))
	b.WriteString("};\n")
	fmt.Fprintf(&b, "%s.body = (p: Parser, min: number): R => p.operatorsRuleParse(%s, min);\n", g.ruleVar(r), g.ruleVar(r))
	g.Init.WriteString(b.String())
}

// operatorsRuleLine returns the line k of an Operators Rule with the expression e and the action; l is
// its Core IR, or nil if the table has none. The plan matches a line whose value is unused without
// values, as the engine does, and so every line of a rule without a value (in the recognizer).
func (g *Generator) operatorsRuleLine(e ast.Expr, action ast.ActionExpr, operator bool, k plandata.Key, l *core.OpLine) string {
	s := check.NewScope()
	m, ok := g.lineFunc(k, l, s)
	if !ok {
		g.noFunction(fmt.Sprintf("the line %d of the Operators Rule %s", k.Line, g.Cur.Name))
		m = "null"
	}
	m = g.fn(m)
	act := "null"
	if action != nil && !g.Cur.Novalue {
		var locals map[string]string
		if operator {
			locals = map[string]string{"lhs": "c.lhs", "rhs": "c.rhs", "op": "c.op"}
		}
		act = g.action(g.LineSite(l), s, locals)
	}
	return fmt.Sprintf("{ scope: %s, m: %s, action: %s, value: %d }", TsStrings(s.Names), m, act, tsShaper(g.Prog, e))
}

// tsInt returns the TypeScript value of the int v: a number when it is a safe integer, otherwise a
// bigint (see the library's ints).
func tsInt(v int) string {
	const maxSafe = 1<<53 - 1
	if int64(v) >= -maxSafe && int64(v) <= maxSafe {
		return strconv.Itoa(v)
	}
	return "BigInt(\"" + strconv.Itoa(v) + "\")"
}

// tsIntList returns xs as a TypeScript array literal of numbers.
func tsIntList(xs []int) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = strconv.Itoa(x)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// document writes the Document of the start rule: createDocument and, with types, the Document's
// parseValue, which converts the Node result as parseValue does.
func (g *Generator) document(out *strings.Builder, start *common.Rule, types bool) {
	fmt.Fprintf(out, `/**
 * Document is an incremental Document of the start rule %[1]s: a text that edit changes, and parse,
 * which returns what parse of the whole current text returns. It is mutable and parses its whole
 * text on each parse.
 */
export class Document extends DocumentBase {
`, start.Name)
	if types {
		fmt.Fprintf(out, `  /** parseValue parses the current text and converts its completed Node result, as parseValue does. */
  parseValue(options?: %[1]s): Types.TypedParseResult<%[2]s> {
    return this.parseWith<Types.TypedParseResult<%[2]s>>(options, (r, control) => astResult<%[2]s>(r, %[3]d, control), error => ({ value: null, error }));
  }
`, g.optionsType, g.startType, g.startRepr)
	}
	fmt.Fprintf(out, `}

/**
 * createDocument makes a Document of input, which it copies, with options as the defaults of its
 * parses; document is null when the options are invalid, with the error.
 */
export function createDocument(input: string | Uint8Array, options?: %[1]s): { document: Document | null; error: Error | null } {
  return createDocumentBase(G, input, options, (bytes, defaults, o) => new Document(G, R%[2]d, nseen, bytes, defaults, o));
}

`, g.optionsType, start.ID)
}

// streamEntry writes createStream, which makes the push stream of the start rule, whose #stream
// repetition delivers its elements as they are committed. It takes the stream options of the
// library, without the limit controls when the options of the parse have none.
func (g *Generator) streamEntry(out *strings.Builder, start *common.Rule) {
	options := "StreamOptions"
	if g.optionsType == "PlainParseOptions" {
		options = "PlainStreamOptions"
	}
	if g.streamType != "" {
		// With types, the elements are also converted as they are delivered, each on its own as parseValue
		// converts a result, so that values share nothing with later elements.
		fmt.Fprintf(out, `/** StreamValue is the typed value of an element of the #stream repetition of the rule %[1]s. */
export type StreamValue = %[2]s;

/**
 * createStreamValue makes a push stream like createStream whose feeds return each committed element as
 * a typed value, converted as parseValue converts a result, with the absent value of its type for an
 * element without a value. stream is null when the options are invalid, with the error.
 */
export function createStreamValue(options?: %[3]s): { stream: Stream<StreamValue> | null; error: Error | null } {
  return createStreamBase<StreamValue>(G, R%[4]d, nseen, bodyGens, options, (n, control) => astConvert(unboxNode(n), %[5]d, control) as StreamValue);
}

`, start.Name, g.streamType, options, start.ID, g.streamRepr)
	}
	fmt.Fprintf(out, `/**
 * createStream makes a push stream of the start rule %[1]s, whose #stream repetition delivers each
 * element as it is committed: feed adds the chunks of the input, the last one final, and returns the
 * elements committed during the feed. stream is null when the options are invalid, with the error.
 */
export function createStream(options?: %[2]s): { stream: Stream | null; error: Error | null } {
  return createStreamBase(G, R%[3]d, nseen, bodyGens, options);
}

`, start.Name, options, start.ID)
}

// runKeys writes the constants of the keys of the run sites of the program being generated that are
// not free of the environment (runKeyName), as the slots of their variables: those of the sites
// whose code the rules emitted refer to (keyUsed), so that no constant goes unread. It runs after
// the rules of the table, and the constants are initialized before a parse starts.
func (g *Generator) runKeys() {
	keys := g.Prog.Run.RunKeys
	for _, site := range slices.Sorted(maps.Keys(keys)) {
		if !g.keyUsed[site] {
			continue
		}
		k := keys[site]
		fmt.Fprintf(&g.Init, "const %s = runKey(%s, %s);\n", g.runKeyName(site), tsIntList(g.Prog.Vars.Slots(k.Vars)), tsIntList(g.Prog.Vars.Slots(k.Bind)))
	}
	g.keyUsed = nil
}

// runKeyName names the constant of the key of the run site in the rule table being generated, and
// records that the code refers to it.
func (g *Generator) runKeyName(site int) string {
	if g.keyUsed == nil {
		g.keyUsed = map[int]bool{}
	}
	g.keyUsed[site] = true
	return fmt.Sprintf("%sRunKey%d", g.Table, site)
}
