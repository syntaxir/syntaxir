package check

import (
	"fmt"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

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
	Model   *Model
	proj    map[string]string // projected captures of the rule being compiled (ruleProjections)
	// facts are the rule facts the program is checked with, or nil if the rules take the flags of
	// a compiled grammar, which was checked when it was compiled.
	facts   *RuleFacts
	opts    Options
	errs    diagnostic.ErrorList
	rule    *Rule
	descIDs map[string]program.ExpID
}

// desc returns the index of an expectation string (adding it to Model.DescTable).
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
	id := program.ExpID(len(c.Model.DescTable))
	c.Model.DescTable = append(c.Model.DescTable, s)
	c.descIDs[s] = id
	return id
}

func (c *Compiler) errorf(pos ast.Pos, format string, args ...any) {
	c.errs = append(c.errs, &diagnostic.Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// Flags returns the rule flags of the model's rules, with which a variant of the program (its
// recognizer) is checked again from its grammar so that its rules keep them.
func (m *Model) Flags() []RuleFlags {
	flags := make([]RuleFlags, len(m.RuleList))
	for i, r := range m.RuleList {
		flags[i] = RuleFlags{Memo: r.Memo, Leader: r.Leader, Member: r.Member, Cycle: r.Cycle, Positional: r.Positional, Twin: r.TwinOK}
	}
	return flags
}

// insertionFlags returns the rule flags of prog under the facts with token insertion enabled,
// which the compilation sets as it sets the rules' own from the facts without insertion, or nil if
// they equal the rules' own.
func insertionFlags(prog *Model, facts *RuleFacts, opts Options, control *cancel.Control) []RuleFlags {
	out := make([]RuleFlags, len(prog.RuleList))
	differ := false
	for i, r := range prog.RuleList {
		control.Poll()
		rf := &facts.Rules[i]
		f := RuleFlags{Leader: rf.LeaderIns, Member: rf.MemberIns, Cycle: rf.CycleIns, Positional: r.Positional}
		f.Memo = !opts.DisableMemo && !f.Leader
		_, isOperatorsRule := r.Def.Expr.(*ast.OperatorsRule)
		f.Twin = r.Action == nil && !isOperatorsRule && rf.CycleIns == 0 && !hasStream(r.Def.Expr, control)
		out[i] = f
		differ = differ || f != (RuleFlags{Memo: r.Memo, Leader: r.Leader, Member: r.Member, Positional: r.Positional, Cycle: r.Cycle, Twin: r.TwinOK})
	}
	if !differ {
		return nil
	}
	return out
}

// CheckCanonical checks the canonical grammar g (frontend.Canonicalize) into its semantic model:
// the rules with their flags, scopes and value-free twins, the types and the expectation table,
// everything a backend lowers and a generator reads, without building any backend's form (the
// front end's Result holds it). normalized is the grammar g is the lowering of, or nil if g is not
// a lowering (Model.Normalized), and starts and defaultStart are its start rules. The rule flags
// come from facts, the facts of g's rules. names receives the hidden rules that the lowering of
// inline actions generates. If flags is not nil, it skips type checking and the checks of the
// facts and takes the flags of a compiled grammar instead; facts still give the other rule facts.
// The front end reports the errors of the start declarations of a grammar that checks, and
// numbers its sites (frontend.NewResult).
func CheckCanonical(g, normalized *ast.Grammar, starts []string, defaultStart string, opts Options, flags []RuleFlags, names desugar.InlineNames, facts *RuleFacts, control *cancel.Control) (*Model, error) {
	prog := NewModel(g, normalized)
	prog.DescTable = append([]string(nil), core.FixedDescs...)
	c := &Compiler{Control: control, Model: prog, opts: opts}
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
		case !StartsUpper(td.DeclName()):
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
		r := &Rule{ID: len(prog.RuleList), Name: rd.Name, Def: rd, Scope: NewScope(), Action: rd.Action, Splice: astinspect.SpliceRule(rd)}
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
		r.Transient, r.Lexical, r.Rec, r.Vars = rf.Transient, rf.Lexical, rf.Rec, rf.Vars
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
	if opts.Recognition != BuildValues {
		// Only rules whose values are referenced by predicates, and the rules needed to build those
		// values, are evaluated as usual; so are the rules with a #stream repetition, whose elements
		// a stream hands to its caller, unless the program is the recognizer, which only recognizes.
		needed := ValueNeededRules(rules, control, opts.Recognition == RecognizeWithStreamValues)
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
	prog.TypeKinds, prog.NodeKinds = kinds.Types, kinds.Nodes
	for _, rs := range [][]*Rule{prog.RuleList, prog.Twins} {
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
		CheckTypesControlled(prog.Checker(), &c.errs, control)
		control.Check()
		if len(c.errs) > 0 {
			return nil, c.errs
		}
	}
	control.Check()
	return prog, nil
}

// ValidateGrammar returns the validation error of g as a compile error.
func ValidateGrammar(g *ast.Grammar, control *cancel.Control) error {
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
func leanBody(r *Rule) bool {
	if r.Stream {
		return false
	}
	return r.Action != nil
}

// NumberSeen numbers the rules whose memoization firstCall may defer (Rule.Seen) and returns
// their count.
func NumberSeen(lists ...[]*Rule) int {
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

// NumberLeaders numbers the leaders of each cycle of rules, in order (Rule.LeaderIndex).
func NumberLeaders(rules []*Rule) {
	n := map[int]int{}
	for _, r := range rules {
		r.LeaderIndex = 0
		if r.Leader {
			r.LeaderIndex = n[r.Cycle]
			n[r.Cycle]++
		}
	}
}

// defsByName returns the rule definitions of the program by name.
func (m *Model) defsByName() map[string]*ast.RuleDef {
	defs := make(map[string]*ast.RuleDef, len(m.RuleList))
	for _, r := range m.RuleList {
		defs[r.Name] = r.Def
	}
	return defs
}

// twinOf returns the value-free twin of rule r (creating it if needed; it is assembled later).
func (c *Compiler) twinOf(r *Rule) *Rule {
	if r.Twin == nil {
		prog := c.Model
		r.Twin = &Rule{ID: len(prog.RuleList) + len(prog.Twins), Name: r.Name, Def: r.Def, Scope: NewScope(),
			Memo: r.Memo, Missing: r.Missing, Transient: r.Transient, Lexical: r.Lexical, Rec: r.Rec, Vars: r.Vars, Positional: r.Positional, Novalue: true}
		prog.Twins = append(prog.Twins, r.Twin)
	}
	return r.Twin
}

// ValueNeededRules returns the rules that must build values even in a program that builds no
// tree: the rules called within captures that predicates can read, and every rule they call. With
// streams, also the rules with a #stream repetition (see the engine's Program.ParseStream) and every rule they call.
func ValueNeededRules(rules []*ast.RuleDef, control *cancel.Control, streams bool) map[string]bool {
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
		for _, callee := range astinspect.AllCallsControlled(byName[name].Expr, nil, control) {
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
			for _, callee := range astinspect.AllCallsControlled(cp.Expr, nil, control) {
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
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		if IsStream(x) {
			found = true
		}
	}, control)
	return found
}

// noCaptures reports captures inside value-free expressions (#atomic, -, !, skip) as errors.
func (c *Compiler) noCaptures(e ast.Expr) {
	control := c.Control
	c.Control.Poll()
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
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
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		if a, ok := x.(*ast.And); ok {
			astinspect.WalkExprControlled(a.Expr, func(y ast.Expr) { nested[y] = true }, control)
		}
	}, control)
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
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
			if r := c.Model.ByName[name]; r != nil {
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
func (c *Compiler) levelMin(e *ast.Ref, r *Rule) int {
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
	top := StreamTops(body)
	n := 0
	var stream ast.Expr
	astinspect.WalkExprControlled(body, func(e ast.Expr) {
		if !IsStream(e) {
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
		} else {
			stream = e
		}
	}, control)
	if stream != nil && n == 1 {
		c.checkStreamReads(body, stream)
	}
	return n > 0
}

// checkStreamReads checks that the predicates of a rule that run after its #stream repetition read
// nothing that a stream parse does not keep, so that a stream parse decides its outcome as the batch
// parse does: a stream hands the elements to the caller instead of putting them in the repetition's
// list, and does not hold the input before the end of the last element it delivered, which the
// text of a node other than a string node reads. So the captures that hold the repetition cannot be
// read after it, and text cannot be taken of a capture made before it, unless the capture holds a
// string node, which holds its text.
// A text of a lambda parameter counts as a text of every capture that its statement reads. A
// variable bound before the repetition carries a text across it. The rule's value action may read
// all of these: a stream parse builds no value and does not evaluate it. Inline actions have
// scopes of their own; predicates are the only other expressions after the repetition that read
// captures.
func (c *Compiler) checkStreamReads(body, stream ast.Expr) {
	control := c.Control
	held := map[string]bool{} // the captures that hold the repetition
	before := map[string]bool{}
	var after []ast.Expr
	// path walks e, the expression on the way from the body to the repetition, and reports
	// whether it holds the repetition.
	var path func(e ast.Expr) bool
	path = func(e ast.Expr) bool {
		control.Poll()
		if e == stream {
			for x := e; x != nil; {
				switch y := x.(type) {
				case *ast.Group:
					x = y.Expr
				case *ast.Attributed:
					x = y.Expr
				case *ast.Capture:
					held[y.Name] = true
					x = y.Expr
				default:
					x = nil
				}
			}
			return true
		}
		switch x := e.(type) {
		case *ast.Group:
			return path(x.Expr)
		case *ast.Attributed:
			return path(x.Expr)
		case *ast.Capture:
			if path(x.Expr) {
				held[x.Name] = true
				return true
			}
		case *ast.Seq:
			for i, it := range x.Items {
				if path(it) {
					for _, b := range x.Items[:i] {
						c.streamCutCaptures(b, before)
					}
					after = append(after, x.Items[i+1:]...)
					return true
				}
			}
		}
		return false
	}
	if !path(body) {
		return
	}
	// cut returns the first capture made before the repetition, not of a string node, that t
	// refers to, and whether t refers to a lambda parameter.
	cut := func(t ast.ActionExpr) (*ast.CaptureRef, bool) {
		var first *ast.CaptureRef
		param := false
		astinspect.WalkAction(t, func(x ast.ActionExpr) {
			if y, ok := x.(*ast.CaptureRef); ok {
				if y.Param {
					param = true
				} else if before[y.Name] && first == nil {
					first = y
				}
			}
		})
		return first, param
	}
	check := func(a ast.ActionExpr) {
		if a == nil {
			return
		}
		stmts := []ast.ActionExpr{a}
		if b, ok := a.(*ast.Block); ok {
			stmts = b.Statements
		}
		for _, st := range stmts {
			stmtCut, _ := cut(st)
			reported := false
			astinspect.WalkAction(st, func(x ast.ActionExpr) {
				switch y := x.(type) {
				case *ast.CaptureRef:
					if !y.Param && held[y.Name] {
						c.errorf(y.Diagnostic(), "capture %s holds a #stream repetition and cannot be read after it: a stream parse hands the elements to the caller instead of keeping them in its list", y.Name)
					}
				case *ast.Call:
					if y.Func != "text" || reported {
						return
					}
					var arg ast.ActionExpr
					if len(y.Args) > 0 {
						arg = y.Args[0]
					}
					ref, param := cut(arg)
					if ref == nil && param {
						ref = stmtCut
					}
					if ref != nil {
						reported = true
						c.errorf(y.Diagnostic(), "text of capture %s, made before a #stream repetition, cannot be taken after it: a stream parse does not hold the input before the end of the last element it delivered; bind a variable to the text before the repetition", ref.Name)
					}
				}
			})
		}
	}
	for _, e := range after {
		walkOwnPredicates(e, check, control)
	}
}

// streamCutCaptures adds to names the captures that e makes and the expressions after it in the
// same rule can see, those outside inline actions, which have scopes of their own, unless the
// capture holds a string node, whose text a stream keeps.
func (c *Compiler) streamCutCaptures(e ast.Expr, names map[string]bool) {
	control := c.Control
	inline := map[ast.Expr]bool{}
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		if a, ok := x.(*ast.InlineAction); ok {
			astinspect.WalkExprControlled(a.Expr, func(y ast.Expr) { inline[y] = true }, control)
		}
	}, control)
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		if cp, ok := x.(*ast.Capture); ok && !inline[x] && !c.stringNode(cp.Expr, map[string]bool{}) {
			names[cp.Name] = true
		}
	}, control)
}

// stringNode reports whether the value of e is always a string node: a literal, a character class,
// any character, an #atomic expression, or a call of a rule without an action or a declared type
// whose body is one, through groups and attributes other than #recover and #missing, whose recovered
// values are not string nodes.
func (c *Compiler) stringNode(e ast.Expr, seen map[string]bool) bool {
	c.Control.Poll()
	switch x := e.(type) {
	case *ast.Literal, *ast.CharClass, *ast.Any, *ast.Atomic:
		return true
	case *ast.Group:
		return c.stringNode(x.Expr, seen)
	case *ast.Attributed:
		return !Recovers(x) && c.stringNode(x.Expr, seen)
	case *ast.Ref:
		r := c.Model.ByName[x.Name]
		if r == nil || seen[x.Name] || r.Action != nil || r.Def.Type != nil || x.Level != "" {
			return false
		}
		seen[x.Name] = true
		return c.stringNode(r.BodyExpr(), seen)
	}
	return false
}

// walkOwnPredicates calls f with the action of each predicate of e outside its inline actions,
// whose actions and predicates see only their own captures.
func walkOwnPredicates(e ast.Expr, f func(ast.ActionExpr), control *cancel.Control) {
	inline := map[ast.Expr]bool{}
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		if a, ok := x.(*ast.InlineAction); ok {
			astinspect.WalkExprControlled(a.Expr, func(y ast.Expr) { inline[y] = true }, control)
		}
	}, control)
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		if p, ok := x.(*ast.Predicate); ok && !inline[x] {
			f(p.Action)
		}
	}, control)
}

// TypeKind returns the kind of nodes of the grammar's type typ without a rule.
func (m *Model) TypeKind(typ string) *valueimpl.Kind {
	if k := m.TypeKinds[typ]; k != nil {
		return k
	}
	return valueimpl.KindOf(typ, "")
}
