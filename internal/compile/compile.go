package compile

import (
	"fmt"
	"sync"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Program is the checked program: what the compiler found out about a grammar, its rules with
// their static facts, its types and expectations. It keeps no output of a backend; the compiler
// side that holds a program builds those (internal/driver/checked.Source). Every backend and
// generator reads it, and its run-time program (Runtime) is what the parser runs.
type Program struct {
	// Model is the semantic model of the grammar: the grammar, its types, variables and start
	// rules. Check always sets it.
	*check.Model
	HasMissing bool // immutable syntax metadata; bytecode/generation are unsupported
	// chk checks the types of the program (Checker), made on first use.
	chk           *check.Checker
	checkerOnce   sync.Once
	RuleList      []*check.Rule
	DescTable     []string        // expectation table (starting with fixedDescs)
	Twins         []*check.Rule   // value-free twins (Rule.Twin); ids start at len(RuleList)
	NSeen         int             // number of rules with Rule.Seen set
	Typed         *check.TypeInfo // types found by the type checker (nil if it did not run)
	NoProjections bool
	// RecordsSourceInfo reports that nodes record source information (Options.SourceInfo).
	RecordsSourceInfo bool
	// MakesDefaultValue reports that value actions and declared rule types were dropped
	// (Options.DefaultValue).
	MakesDefaultValue bool
	// InlineNames are the hidden rules that the inline actions of the grammar as written were
	// lowered to, with the inline actions they stand for; empty if it has none.
	InlineNames desugar.InlineNames
	ByName      map[string]*check.Rule
	// Opts are the options the program was compiled with. The engine builds further programs from the
	// same grammar later (the controlled closure graph and the recognizer) and derives their options
	// from these, so that none is lost (engine.Program.lazyOptions).
	Opts check.Options

	// typeKinds holds the node kinds of the grammar's types (TypeKind), made with the program from
	// the checker's (check.Checker.Kinds).
	typeKinds map[string]*valueimpl.Kind
	// NodeKinds holds the kinds of the grammar's named node types, also under the names of the
	// aliases of them.
	NodeKinds map[string]*nodes.NodeTypeKinds
	// sites numbers the skip-run, run and scan sites of the program's repetitions (SetSites).
	sites check.Sites
	// NumSkipSites, NumRunSites and NumScanSites count the sites (Sites).
	NumSkipSites, NumRunSites, NumScanSites int
	// InsertionFlags are the rule flags of the parses with missing-token insertion enabled, in
	// which left recursion through an insertion forms cycles of its own (RuleFact.CycleIns), when
	// they differ from the flags of the rules; nil otherwise. Such parses run a program built with
	// them.
	InsertionFlags []check.RuleFlags
	// runtime is the run-time program (Runtime), made once under runtimeOnce.
	runtimeOnce sync.Once
	runtime     *program.Program
}

// Reserved type names.
var builtinTypes = map[string]bool{
	"int": true, "string": true, "bool": true, "node": true, "terminal": true,
	"Match": true, "Seq": true, "List": true, "Operator": true, "Error": true, "Missing": true, valueimpl.TypeMissing: true,
}

// Compiler holds the state of compiling one program: the program being built, its analysis,
// options, expectation table and errors. The walker embeds it to check the rule bodies.
// A nil Control never cancels.
type Compiler struct {
	Control *cancel.Control
	Prog    *Program
	proj    map[string]string // projected captures of the rule being compiled (ruleProjections)
	// facts are the rule facts the program is checked with, or nil if the rules take the flags of
	// a compiled grammar, which was checked when it was compiled.
	facts   *check.RuleFacts
	opts    check.Options
	errs    diagnostic.ErrorList
	rule    *check.Rule
	descIDs map[string]program.ExpID
}

// desc returns the index of an expectation string (adding it to Program.DescTable).
func (c *Compiler) desc(s string) program.ExpID {
	if id, ok := c.descIDs[s]; ok {
		return id
	}
	if c.descIDs == nil {
		c.descIDs = map[string]program.ExpID{}
		for i, d := range core.FixedDescs {
			c.descIDs[d] = program.ExpID(i)
		}
		if id, ok := c.descIDs[s]; ok {
			return id
		}
	}
	id := program.ExpID(len(c.Prog.DescTable))
	c.Prog.DescTable = append(c.Prog.DescTable, s)
	c.descIDs[s] = id
	return id
}

func (c *Compiler) errorf(pos ast.Pos, format string, args ...any) {
	c.errs = append(c.errs, &diagnostic.Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// Flags returns the rule flags of the program's rules, with which a variant of the program (its
// recognizer) is checked again from its grammar so that its rules keep them.
func (p *Program) Flags() []check.RuleFlags {
	flags := make([]check.RuleFlags, len(p.RuleList))
	for i, r := range p.RuleList {
		flags[i] = check.RuleFlags{Memo: r.Memo, Leader: r.Leader, Member: r.Member, Cycle: r.Cycle, Positional: r.Positional, Twin: r.TwinOK}
	}
	return flags
}

// insertionFlags returns the rule flags of prog under the facts with token insertion enabled,
// which the compilation sets as it sets the rules' own from the facts without insertion, or nil if
// they equal the rules' own.
func insertionFlags(prog *Program, facts *check.RuleFacts, opts check.Options, control *cancel.Control) []check.RuleFlags {
	out := make([]check.RuleFlags, len(prog.RuleList))
	differ := false
	for i, r := range prog.RuleList {
		control.Poll()
		rf := &facts.Rules[i]
		f := check.RuleFlags{Leader: rf.LeaderIns, Member: rf.MemberIns, Cycle: rf.CycleIns, Positional: r.Positional}
		f.Memo = !opts.DisableMemo && !f.Leader
		_, isOperatorsRule := r.Def.Expr.(*ast.OperatorsRule)
		f.Twin = r.Action == nil && !isOperatorsRule && rf.CycleIns == 0 && !hasStream(r.Def.Expr, control)
		out[i] = f
		differ = differ || f != (check.RuleFlags{Memo: r.Memo, Leader: r.Leader, Member: r.Member, Positional: r.Positional, Cycle: r.Cycle, Twin: r.TwinOK})
	}
	if !differ {
		return nil
	}
	return out
}

// Canonical is the grammar that a program is compiled from: the grammar after its inline actions
// are lowered, its default-value form (if the options ask for it) and its attributes are normalized.
type Canonical struct {
	// Grammar is the grammar the program runs.
	Grammar *ast.Grammar
	// Source is the grammar as written, normalized, or nil if Grammar is the one as written.
	Source *ast.Grammar
	// Starts are the start rules in order, and DefaultStart is the one used when none is named.
	Starts       []string
	DefaultStart string
	// StartErrors are the errors of the start declarations; compilation reports them.
	StartErrors diagnostic.ErrorList
}

// Canonicalize validates the grammar and returns the canonical form that Check checks.
// names receives the hidden rules that the lowering of inline actions generates.
func Canonicalize(g *ast.Grammar, opts check.Options, names desugar.InlineNames, control *cancel.Control) (*Canonical, error) {
	control.Check()
	if !opts.DefaultValue {
		if err := validateGrammar(g, control); err != nil {
			return nil, err
		}
	}
	// The inline actions are checked as written, then lowered to hidden rules. A program keeps
	// the grammar as written, whatever its options, for checked.Source.Grammar.
	source := g
	if desugar.HasInlineAction(g, control) {
		if errs := desugar.UndeclaredRules(g, control); errs != nil {
			return nil, errs
		}
		lowered, errs := desugar.LowerInlineActionsControlled(g, names, control)
		if errs != nil {
			return nil, errs
		}
		g = lowered
		control.Check()
	}
	if opts.DefaultValue {
		lowered := g
		g = desugar.DefaultValueGrammar(g, valueNeededRules(control.Rules(g), control, false), control)
		control.Check()
		// A defect is reported where the grammar as written has it.
		valid := g
		if lowered != source {
			valid = desugar.SourceDefaultValueGrammar(source, lowered, g, names, control)
		}
		if err := validateGrammar(valid, control); err != nil {
			return nil, err
		}
	}
	keepSource := source != g
	var unsupported diagnostic.ErrorList
	for _, comment := range g.File.HeaderComments {
		control.Poll()
		unsupported = append(unsupported, &diagnostic.Error{Pos: comment.Pos, Msg: "comments are not allowed inside statement headers"})
	}
	checkBlocks := func(actionExpr ast.ActionExpr) {
		astfacts.WalkAction(actionExpr, func(t ast.ActionExpr) {
			control.Poll()
			if block, ok := t.(*ast.Block); ok {
				unsupported = append(unsupported, &diagnostic.Error{Pos: block.Diagnostic(), Msg: "action statement blocks are not implemented"})
			}
		})
	}
	for _, rd := range control.Rules(g) {
		control.Poll()
		checkBlocks(rd.Action)
		astfacts.WalkExprControlled(rd.Expr, func(e ast.Expr) {
			control.Poll()
			switch e := e.(type) {
			case *ast.Predicate:
				if block, ok := e.Action.(*ast.Block); ok && len(block.Statements) > 0 {
					for _, statement := range block.Statements {
						control.Poll()
						checkBlocks(statement)
					}
				} else {
					checkBlocks(e.Action)
				}
			case *ast.InlineAction:
				checkBlocks(e.Action)
			case *ast.OperatorsRule:
				for _, operand := range e.Operands {
					control.Poll()
					checkBlocks(operand.Action)
				}
				for _, level := range e.Levels {
					control.Poll()
					for _, operator := range level.Operators {
						control.Poll()
						checkBlocks(operator.Action)
					}
				}
			}
		}, control)
	}
	starts, defaultStart, startErrors := desugar.ResolveStarts(control.Rules(g), control)
	_, _, linearErrors := desugar.LinearDeclarations(g, control)
	startErrors = append(startErrors, linearErrors...)
	control.Check()
	if len(unsupported) != 0 {
		return nil, unsupported
	}
	var normalizationError error
	g, normalizationError = desugar.NormalizeAttributesControlled(g, opts.EagerAttributeCopies, control)
	control.Check()
	if normalizationError != nil {
		return nil, normalizationError
	}
	if !keepSource {
		source = nil // the grammar as written is the one the program runs
	} else {
		// The grammar as written is kept beside the one the program runs.
		if source, normalizationError = desugar.NormalizeAttributesControlled(source, opts.EagerAttributeCopies, control); normalizationError != nil {
			return nil, normalizationError
		}
	}
	return &Canonical{Grammar: g, Source: source, Starts: starts, DefaultStart: defaultStart, StartErrors: startErrors}, nil
}

// CheckCanonical checks the canonical grammar canon into its static program: the rules with their
// flags, scopes and value-free twins, the types and the expectation table, everything a backend
// lowers and a generator reads, without building any backend's form (the front end's Result holds
// it). The rule flags come from facts, the facts of canon's rules. names receives the hidden rules
// that the lowering of inline actions generates. If flags is not nil, it skips type checking and
// the checks of the facts and takes the flags of a compiled grammar instead; facts still give the
// other rule facts. The site numbers are set afterwards (SetSites).
func CheckCanonical(canon *Canonical, opts check.Options, flags []check.RuleFlags, names desugar.InlineNames, facts *check.RuleFacts, control *cancel.Control) (*Program, error) {
	g, source, starts, defaultStart, startErrors := canon.Grammar, canon.Source, canon.Starts, canon.DefaultStart, canon.StartErrors
	prog := &Program{Model: check.NewModel(g, source), ByName: map[string]*check.Rule{},
		DescTable: append([]string(nil), core.FixedDescs...)}
	c := &Compiler{Control: control, Prog: prog, opts: opts}
	if control != nil {
		prog.Checker().SetShapeControl(control)
		defer func() {
			prog.Checker().SetShapeControl(nil)
			c.Control = nil
		}()
	}
	prog.StartRules, prog.DefaultStartRule = starts, defaultStart
	prog.Opts = opts
	prog.NoProjections = opts.NoProjections
	prog.RecordsSourceInfo = opts.SourceInfo
	prog.MakesDefaultValue = opts.DefaultValue
	prog.InlineNames = names

	for _, td := range control.Types(g) {
		control.Poll()
		switch {
		case builtinTypes[td.DeclName()]:
			c.errorf(td.Diagnostic(), "type %s is reserved", td.DeclName())
		case !check.StartsUpper(td.DeclName()):
			c.errorf(td.Diagnostic(), "type %s must start with an uppercase letter", td.DeclName())
		case prog.Types[td.DeclName()] != nil:
			c.errorf(td.Diagnostic(), "type %s is already defined", td.DeclName())
		default:
			prog.Types[td.DeclName()] = td
		}
	}
	// A rule declared again is reported and then left out: the analyses below key rules by name
	// and take each name once.
	rules, redeclared := desugar.UniqueRules(g, control)
	c.errs = append(c.errs, redeclared...)
	for _, rd := range rules {
		control.Poll()
		r := &check.Rule{ID: len(prog.RuleList), Name: rd.Name, Def: rd, Scope: check.NewScope(), Action: rd.Action, Splice: astfacts.SpliceRule(rd)}
		if r.Splice && rd.Type != nil {
			c.errorf(rd.Diagnostic(), "rule %s makes no node of its own, so it cannot have a result type", rd.Name)
		}
		if w := names.Where(rd.Name); w != rd.Name {
			r.Where = w
		}
		prog.RuleList = append(prog.RuleList, r)
		prog.ByName[rd.Name] = r
	}

	c.declareVars(g)
	prog.markMissingRules(control)
	if facts == nil || len(facts.Rules) != len(prog.RuleList) {
		return nil, fmt.Errorf("internal error: no rule facts for the %d rules of the grammar", len(prog.RuleList))
	}
	if flags != nil && len(flags) != len(prog.RuleList) {
		return nil, fmt.Errorf("compiled grammar has %d rule flags for %d rules", len(flags), len(prog.RuleList))
	}
	if flags == nil {
		c.facts = facts
	}
	for i, r := range prog.RuleList {
		control.Poll()
		rf := &facts.Rules[i]
		twin := false
		if flags != nil {
			r.Memo, r.Leader, r.Member, r.Positional, twin = flags[i].Memo, flags[i].Leader, flags[i].Member, flags[i].Positional, flags[i].Twin
			r.Cycle = flags[i].Cycle
		} else {
			r.Leader, r.Member, r.Cycle, r.Positional = rf.Leader, rf.Member, rf.Cycle, rf.Positional
			// Leaders are always memoized (Leader); the other members of a cycle memoize the results
			// that read no growing seed.
			r.Memo = !opts.DisableMemo && !r.Leader
			_, isOperatorsRule := r.Def.Expr.(*ast.OperatorsRule)
			twin = r.Action == nil && !isOperatorsRule && rf.Cycle == 0 && !hasStream(r.Def.Expr, control)
		}
		r.Transient, r.Rec, r.Vars = rf.Transient, rf.Rec, rf.Vars
		r.TwinOK = twin
	}
	if flags == nil && prog.HasMissing {
		prog.InsertionFlags = insertionFlags(prog, facts, opts, control)
	}
	if flags == nil && !opts.LeftRecursionBindings {
		// A grammar must be valid whether or not insertion is enabled, so the bindings are checked
		// against the cycles of both.
		c.errs = append(c.errs, facts.BindingErrors...)
	}
	if opts.Recognition != check.BuildValues {
		// Only rules whose values are referenced by predicates, and the rules needed to build those
		// values, are evaluated as usual; so are the rules with a #stream repetition, whose elements
		// a stream hands to its caller, unless the program is the recognizer, which only recognizes.
		needed := valueNeededRules(rules, control, opts.Recognition == check.RecognizeWithStreamValues)
		for _, r := range prog.RuleList {
			control.Poll()
			if !needed[r.Name] {
				r.Novalue, r.Action, r.TwinOK = true, nil, false
				r.PredCaps = predicateCaptures(r.Def.Expr, control)
			}
		}
	}
	w := &walker{Compiler: c}
	for _, r := range prog.RuleList {
		control.Poll()
		w.compileRule(r)
	}
	// Value-free twins can be assembled the same way if the original rules have no errors. New twins
	// may be added during assembly.
	for i := 0; i < len(prog.Twins) && len(c.errs) == 0; i++ {
		control.Poll()
		w.compileRule(prog.Twins[i])
	}
	control.Check()
	if len(c.errs) > 0 {
		return nil, c.errs
	}
	prog.NSeen = NumberSeen(prog.RuleList, prog.Twins)
	NumberLeaders(prog.RuleList)
	kinds := prog.Checker().Kinds(control.Poll)
	prog.typeKinds, prog.NodeKinds = kinds.Types, kinds.Nodes
	for _, rs := range [][]*check.Rule{prog.RuleList, prog.Twins} {
		control.Poll()
		for _, r := range rs {
			control.Poll()
			r.Plain = !r.Leader && (!r.Memo || r.Transient) && len(r.Scope.Names) == 0
			if n, ok := prog.Checker().RuleNodeType(r.Def); ok && !r.Novalue {
				r.NodeType = n.Name
			}
			if n, ok := prog.Checker().OperatorsNodeType(r.Def); ok && !r.Novalue {
				r.OpNodeType = n.Name
			}
		}
	}
	if flags == nil {
		CheckTypesControlled(prog, &c.errs, control)
		control.Check()
		if len(c.errs) > 0 {
			return nil, c.errs
		}
	}
	// The start rules and the other declaration attributes are reported once the grammar is
	// otherwise valid.
	if len(startErrors) > 0 {
		return nil, startErrors
	}

	control.Check()
	return prog, nil
}

// validateGrammar returns the validation error of g as a compile error.
func validateGrammar(g *ast.Grammar, control *cancel.Control) error {
	var validationError error
	if control == nil {
		validationError = ast.Validate(g)
	} else {
		validationError = ast.ValidateContext(control.Ctx, g)
	}
	control.Check()
	if validationError != nil {
		e := validationError.(*ast.ValidationError)
		return diagnostic.ErrorList{&diagnostic.Error{Pos: e.Pos, Msg: e.Path + ": " + e.Msg}}
	}
	return nil
}

// leanBody reports whether the value of the rule body is unused. It is unused when there is an
// action, which reads captures only, provided the rule has no #stream. Every backend and the
// code generator use this test to compile the body value-free.
func leanBody(r *check.Rule) bool {
	if r.Stream {
		return false
	}
	return r.Action != nil
}

// NumberSeen numbers the rules whose memoization firstCall may defer (Rule.Seen) and returns
// their count.
func NumberSeen(lists ...[]*check.Rule) int {
	n := 0
	for _, rules := range lists {
		for _, r := range rules {
			r.Seen = -1
			if program.Defers(r.Memo, r.Transient, r.Leader) {
				r.Seen = n
				n++
			}
		}
	}
	return n
}

// NumberLeaders numbers the leaders of each cycle of rules, in order (Rule.LeaderIndex), and gives
// the first MaxLeaderBits of each a bit (Rule.LeaderBit).
func NumberLeaders(rules []*check.Rule) {
	n := map[int]int{}
	for _, r := range rules {
		r.LeaderIndex, r.LeaderBit = 0, 0
		if r.Leader {
			k := n[r.Cycle]
			n[r.Cycle]++
			r.LeaderIndex, r.LeaderBit = k, program.LeaderBitOf(k)
		}
	}
}

// defsByName returns the rule definitions of the program by name.
func (p *Program) defsByName() map[string]*ast.RuleDef {
	m := make(map[string]*ast.RuleDef, len(p.RuleList))
	for _, r := range p.RuleList {
		m[r.Name] = r.Def
	}
	return m
}

// twinOf returns the value-free twin of rule r (creating it if needed; it is assembled later).
func (c *Compiler) twinOf(r *check.Rule) *check.Rule {
	if r.Twin == nil {
		prog := c.Prog
		r.Twin = &check.Rule{ID: len(prog.RuleList) + len(prog.Twins), Name: r.Name, Def: r.Def, Scope: check.NewScope(),
			Memo: r.Memo, Missing: r.Missing, Transient: r.Transient, Rec: r.Rec, Vars: r.Vars, Positional: r.Positional, Novalue: true}
		prog.Twins = append(prog.Twins, r.Twin)
	}
	return r.Twin
}

// valueNeededRules returns the rules that must build values even in a program that builds no
// tree: the rules called within captures that predicates can read, and every rule they call. With
// streams, also the rules with a #stream repetition (see Program.ParseStream) and every rule they call.
func valueNeededRules(rules []*ast.RuleDef, control *cancel.Control, streams bool) map[string]bool {
	control.Poll()
	byName := map[string]*ast.RuleDef{}
	for _, rd := range rules {
		control.Poll()
		byName[rd.Name] = rd
	}
	needed := map[string]bool{}
	var mark func(name string)
	mark = func(name string) {
		if needed[name] || byName[name] == nil {
			return
		}
		needed[name] = true
		for _, callee := range astfacts.AllCallsControlled(byName[name].Expr, nil, control) {
			control.Poll()
			mark(callee)
		}
	}
	for _, rd := range rules {
		control.Poll()
		if streams && hasStream(rd.Expr, control) {
			mark(rd.Name)
		}
		for cp := range predicateCaptures(rd.Expr, control).Read {
			for _, callee := range astfacts.AllCallsControlled(cp.Expr, nil, control) {
				control.Poll()
				mark(callee)
			}
		}
	}
	return needed
}

// hasStream reports whether the expression contains #stream.
func hasStream(e ast.Expr, control *cancel.Control) bool {
	control.Poll()
	found := false
	astfacts.WalkExprControlled(e, func(x ast.Expr) {
		if check.IsStream(x) {
			found = true
		}
	}, control)
	return found
}

// noCaptures reports captures inside value-free expressions (#atomic, -, !, skip) as errors.
func (c *Compiler) noCaptures(e ast.Expr) {
	control := c.Control
	c.Control.Poll()
	astfacts.WalkExprControlled(e, func(x ast.Expr) {
		if cp, ok := x.(*ast.Capture); ok {
			c.errorf(cp.Diagnostic(), "capture %s inside #atomic, -, or ! has no effect", cp.Name)
		}
	}, control)
}

// noRecoveringCaptures reports, as errors, the captures inside the positive lookahead body e whose
// values can hold an error value of #recover. The end of the lookahead discards the recovered
// errors but keeps the captures, and an error value made inside a lookahead has no expected items
// and no farthest failure, so its position and message would differ from those of the same call
// outside one, and memoization could not answer both alike.
func (c *Compiler) noRecoveringCaptures(e ast.Expr) {
	if c.facts == nil {
		return // a compiled grammar, checked when it was compiled
	}
	control := c.Control
	c.Control.Poll()
	// A nested positive lookahead checks its own captures.
	nested := map[ast.Expr]bool{}
	astfacts.WalkExprControlled(e, func(x ast.Expr) {
		if a, ok := x.(*ast.And); ok {
			astfacts.WalkExprControlled(a.Expr, func(y ast.Expr) { nested[y] = true }, control)
		}
	}, control)
	astfacts.WalkExprControlled(e, func(x ast.Expr) {
		cp, ok := x.(*ast.Capture)
		if !ok || nested[x] {
			return
		}
		direct, calls := valueRecovery(cp.Expr, control)
		const what = "capture %s inside a positive lookahead can hold an error value of #recover"
		if direct {
			c.errorf(cp.Diagnostic(), what, cp.Name)
			return
		}
		for _, name := range calls {
			var w string
			if r := c.Prog.ByName[name]; r != nil {
				w = c.facts.Rules[r.ID].Recovering
			}
			switch w {
			case "":
				continue
			case name:
				c.errorf(cp.Diagnostic(), what+" in rule %s", cp.Name, name)
			default:
				c.errorf(cp.Diagnostic(), what+" in rule %s, through rule %s", cp.Name, w, name)
			}
			return
		}
	}, control)
}

// FoldASCIIRune maps A-Z to a-z and leaves every other rune as it is: the folding of #ignorecase
// and of the suffix i, which is ASCII only.
func FoldASCIIRune(r rune) rune {
	if 'A' <= r && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// FoldedRunes returns rs with foldASCIIRune applied to each rune.
func FoldedRunes(rs []rune) []rune {
	out := make([]rune, len(rs))
	for i, r := range rs {
		out[i] = FoldASCIIRune(r)
	}
	return out
}

// levelMin returns the minimum binding level of a call with a binding level.
func (c *Compiler) levelMin(e *ast.Ref, r *check.Rule) int {
	pr, ok := r.Def.Expr.(*ast.OperatorsRule)
	if !ok {
		c.errorf(e.Diagnostic(), "rule %s has no Operators Rule levels", e.Name)
		return 0
	}
	for i, l := range pr.Levels {
		if l.Name == e.Level {
			return i // parse binding levels i+1 and above
		}
	}
	c.errorf(e.Diagnostic(), "rule %s has no level %s", e.Name, e.Level)
	return 0
}

// checkStream checks that #stream appears only at the top level of a rule body (the body itself
// or an element of a sequence) of a rule outside every left-recursion cycle and reports whether it
// is present.
func (c *Compiler) checkStream(body ast.Expr) bool {
	control := c.Control
	top := check.StreamTops(body)
	n := 0
	astfacts.WalkExprControlled(body, func(e ast.Expr) {
		if !check.IsStream(e) {
			return
		}
		n++
		a := e.(*ast.Attributed)
		for _, attr := range a.Attrs {
			if attr.Name == "recover" {
				c.errorf(attr.Diagnostic(), "#recover cannot wrap a #stream repetition; recover inside each element instead")
			}
		}
		if !top[e] {
			c.errorf(a.Attrs[0].Diagnostic(), "#stream is only allowed at the top level of a rule body")
		} else if n > 1 {
			c.errorf(a.Attrs[0].Diagnostic(), "a rule body can have only one #stream")
		} else if c.rule != nil && c.rule.Cycle != 0 {
			// Growing a left-recursion cycle evaluates the bodies of its rules again from the same
			// position, which would deliver the elements a stream has already handed over.
			c.errorf(a.Attrs[0].Diagnostic(), "#stream cannot be used in rule %s, which is on a left-recursion cycle; growing the recursion would re-evaluate elements already delivered", c.rule.Name)
		}
	}, control)
	return n > 0
}

// TypeKind returns the kind of nodes of the grammar's type typ without a rule.
func (prog *Program) TypeKind(typ string) *valueimpl.Kind {
	if k := prog.typeKinds[typ]; k != nil {
		return k
	}
	return valueimpl.KindOf(typ, "")
}

// NewProgram returns a program of the grammar g with no rules or types yet, for the tests that fill
// in a program of their own.
func NewProgram(g *ast.Grammar) *Program {
	return &Program{Model: check.NewModel(g, nil), ByName: map[string]*check.Rule{}}
}
