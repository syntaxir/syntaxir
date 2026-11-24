package print_test

import (
	"fmt"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/engine"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tests here check the Core IR of the typed Go rules, after the Core passes (typedCore), against the decisions frozen in testdata/decisions/typed
// (core_typed_decisions_test.go).

// coreGenMode is a set of generator options under which the Core passes are frozen.
type coreGenMode struct {
	name string
	opts common.GenOptions
}

var coreGenModes = []coreGenMode{
	{"lean", common.GenOptions{NoLimitControls: true, Types: true, TypedOnly: true}},
	{"lean observable", common.GenOptions{NoLimitControls: true, Types: true, TypedOnly: true, Observability: true}},
	{"logical", common.GenOptions{Types: true, TypedOnly: true}},
	{"controlled document", common.GenOptions{Types: true, Document: true}},
	{"lean boxed", common.GenOptions{NoLimitControls: true, Types: true, TypedOnly: true, NoOpt: []string{"text-dispatch", "field-compare"}}},
}

// coreGenCase is a corpus grammar generated with the options of a mode, and the Core IR of its
// rules under them, with the expression each node is built from.
type coreGenCase struct {
	name, mode string
	// table is the generator's rule table of the module: trules for the typed rules, recRules for the
	// recognizer.
	table  string
	golden *decisionsGolden
	gen    *print.Generator
	m      *core.Module
	origin core.Origin
	// rules holds the rules of the program by ID, value-free twins included.
	rules []*common.Rule
	// passErrs holds the violations Verify found after a pass.
	passErrs []string
	// broken is set where a negative control broke a pass of this case.
	broken bool
	// lines holds the decisions of the Core IR (coreDecisionLines), once compared.
	lines []string
	// flow holds the decisions of the Flow passes on the functions of the table (flowDecisions).
	flow *flowDecisions
}

func (c *coreGenCase) g() *print.Generator { return c.gen }

// altOrigin returns the expression of the alternative i of the choice x, which the decisions are
// keyed by: the alternative of the grammar choice x is built from. (The origin of an alternative's
// body is the innermost expression it is built from: inside a capture that builds nothing, for
// one.)
func (c *coreGenCase) altOrigin(x *core.Choice, i int) (ast.Expr, bool) {
	if ce, ok := c.origin.Of(x).(*ast.Choice); ok && len(ce.Alts) == len(x.Alts) {
		return ce.Alts[i], true
	}
	return altOriginIn(c.origin, x, i)
}

// coreGenerate generates the parser of the grammar source with the options and returns the
// generator that wrote it, for the Core IR of the same options (typedCore), in the
// state of its traced pass with observability, and records the decisions of its Flow passes
// (flowRecordOf). It reports false for a grammar that the generator rejects.
func coreGenerate(src string, opts common.GenOptions) (*print.Generator, bool) {
	g, err := parseTest(src)
	if err != nil {
		return nil, false
	}
	settings, err := generate.Settings(opts)
	if err != nil {
		return nil, false
	}
	rec := &flowRecord{settings: settings}
	opts.Module = "example.com/p"
	if _, rec.checked, err = generateGoChecked(g, opts, rec.options(), rec.hooks()); err != nil || rec.gen == nil {
		return nil, false
	}
	gen := rec.gen
	flowRecords.Store(gen, rec)
	if opts.Observability {
		// The bodies that observe are the traced copies, which the generator prints in its traced pass;
		// the other bodies are those of the build without observability.
		gen.Traced, gen.Observability, gen.Ordinary = true, true, false
	}
	return gen, true
}

// recognizerOf returns the view of the recognizer of the program that the generator g prints,
// compiled as Program.Recognizer compiles it. The view's elaboration input is the checked program
// that the test compiled.
func recognizerOf(g *print.Generator) (codegen.Checked, error) {
	rec, err := engine.New(checked.Of(checkedOf(g), pass.Settings{})).Recognizer()
	if err != nil {
		return codegen.Checked{}, err
	}
	return codegen.Checked{View: checked.Of(entry.Checked(rec), pass.Settings{}).Generator(), Elab: entry.Checked(rec)}, nil
}

// coreTypedCase builds the Core IR of the typed rules of the generated parser with the Core passes
// (typedCore), verifying the module after each pass. A scan has the ID, and so the
// origin, of the repetition it replaces. If breaks is not nil, it is called
// after each pass, before the module is verified: a negative control breaks a pass there.
func coreTypedCase(name, mode string, gen *print.Generator, breaks func(c *coreGenCase, pass string, m *core.Module)) (*coreGenCase, error) {
	c := &coreGenCase{name: name, mode: mode, table: "trules", golden: typedGolden, gen: gen}
	c.flow = flowRecordOf(gen).decisions(c.table, gen)
	c.rules = typedRules(gen)
	m, err := typedCore(gen, corepass.RunOptions{Dump: func(pass string, m *core.Module) {
		// The module is built, and so the origin known, before the first pass.
		if c.origin.Exprs == nil {
			c.origin = m.Origin
		}
		if breaks != nil {
			breaks(c, pass, m)
		}
		core.Analyze(m)
		if err := core.Verify(m); err != nil {
			c.passErrs = append(c.passErrs, fmt.Sprintf("after %s: %v", pass, err))
		}
	}})
	if err != nil {
		return nil, err
	}
	c.m, c.origin = m, m.Origin
	return c, nil
}

// coreGenerated holds the corpus grammars generated in each mode, by mode name. The tests that read
// it change its generators (Generator.Table), so they do not run in parallel.
var coreGenerated = map[string][]coreGenerated1{}

type coreGenerated1 struct {
	name string
	gen  *print.Generator
}

// coreGenerations generates every corpus grammar in the mode, once for all tests: those that the
// generator accepts and whose typed rules have a Core IR. The grammars are generated concurrently,
// since the tests that read them run before the parallel tests and would otherwise wait for each.
func coreGenerations(t *testing.T, mode coreGenMode) []coreGenerated1 {
	t.Helper()
	if out, ok := coreGenerated[mode.name]; ok {
		return out
	}
	cases := append(codegentest.CorpusWithParsers(t), codegentest.CoreExtraGrammars...)
	gens := make([]*print.Generator, len(cases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, gc := range cases {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			gen, ok := coreGenerate(gc.Src, mode.opts)
			if !ok {
				return
			}
			if m, err := typedCore(gen, corepass.RunOptions{}); err != nil || len(m.Rules) == 0 {
				return
			}
			gens[i] = gen
		})
	}
	wg.Wait()
	var out []coreGenerated1
	for i, gen := range gens {
		if gen != nil {
			out = append(out, coreGenerated1{cases[i].Name, gen})
		}
	}
	if len(out) < 100 {
		t.Fatalf("%s: %d grammars", mode.name, len(out))
	}
	coreGenerated[mode.name] = out
	return out
}

// coreGenCases builds the Core IR of every corpus grammar generated in the mode, breaking the
// passes with breaks if it is not nil (coreTypedCase).
func coreGenCases(t *testing.T, mode coreGenMode, breaks func(c *coreGenCase, pass string, m *core.Module)) []*coreGenCase {
	t.Helper()
	gens := coreGenerations(t, mode)
	// The cases are independent: each reads and changes the generator of its own grammar only.
	type built struct {
		c   *coreGenCase
		err error
	}
	cases := codegentest.MapParallel(len(gens), func(i int) built {
		c, err := coreTypedCase(gens[i].name, mode.name, gens[i].gen, breaks)
		return built{c, err}
	})
	out := make([]*coreGenCase, 0, len(cases))
	for i, b := range cases {
		if b.err != nil {
			t.Fatalf("%s: %s: %v", mode.name, gens[i].name, b.err)
		}
		out = append(out, b.c)
	}
	return out
}

// coreNodes calls f for every node of the rule with the alternative and the line it is in.
func coreNodes(r *core.Rule, f func(n core.Node)) { core.Walk(r.Body, f) }

// opLines returns the lines of an Operators Rule with their grammar lines, in order: the skip
// line, the operands and the operators.
func opLines(o *core.OperatorsRule, e *ast.OperatorsRule) ([]*core.OpLine, []ast.Expr, []ast.ActionExpr) {
	var exprs []ast.Expr
	var actions []ast.ActionExpr
	if e.Skip != nil {
		exprs, actions = append(exprs, e.Skip), append(actions, nil)
	}
	for _, x := range e.Operands {
		exprs, actions = append(exprs, x.Expr), append(actions, x.Action)
	}
	for _, l := range e.Levels {
		for _, x := range l.Operators {
			exprs, actions = append(exprs, x.Expr), append(actions, x.Action)
		}
	}
	return o.Lines(), exprs, actions
}

// guardString is the guard of a Core alternative as a Go condition: the condition on the
// next character and the expectation.
func guardString(m *core.Module, f *core.First) (string, string) {
	if f.Lit {
		if f.Fold {
			return fmt.Sprintf("ch == %d || ch == %d", f.Rune, f.Rune-('a'-'A')), m.Exps[f.Exp]
		}
		return fmt.Sprintf("ch == %d", f.Rune), m.Exps[f.Exp]
	}
	e := &ast.CharClass{Negated: f.Class.Negated}
	for _, r := range f.Class.Ranges {
		e.Ranges = append(e.Ranges, ast.CharRange{Lo: r.Lo, Hi: r.Hi})
	}
	return print.NegateCondition(print.ClassReject(e)), m.Exps[f.Exp]
}

// coreDecisionCounts counts the decisions compared.
type coreDecisionCounts struct {
	terminals, guards, guardedClasses, classRuns, scans, scopes int
}

// coreDecisionLines encodes the decisions of the Core IR of c as the lines of the decisions
// golden (core_typed_decisions_test.go), sorted, with the guard conditions as quoted text, and
// counts them in n. It returns the problems of nodes it cannot encode, each prefixed with its
// aspect. The decisions are those of every node of the module:
//
//   - the terminal regions that the Flow passes match as one program (c.flow);
//   - the guard of every alternative of a choice, and whether the Flow passes let its first class
//     consume the guard's character;
//   - the class runs that the Flow passes make in every value-free sequence;
//   - the scans;
//   - the scopes of repetition elements and Operators Rule lines.
func coreDecisionLines(c *coreGenCase, n *coreDecisionCounts) (lines, problems []string) {
	for _, cr := range c.m.Rules {
		r := c.rules[cr.ID]
		_, idx := typedDecisionsWalk(r)
		problem := func(aspect, format string, args ...any) {
			problems = append(problems, fmt.Sprintf("%s: rule %s: ", aspect, r.Name)+fmt.Sprintf(format, args...))
		}
		// at returns the index of the expression in the rule's body.
		at := func(aspect string, e ast.Expr) (int, bool) {
			i, ok := idx[e]
			if !ok {
				problem(aspect, "expression %s is not in the body", syntaxfmt.FormatExpr(e))
			}
			return i, ok
		}
		add := func(kind string, at int, format string, args ...any) {
			lines = append(lines, fmt.Sprintf("%s\t%d\t%d", kind, cr.ID, at)+fmt.Sprintf(format, args...))
		}
		names := func(s []core.Slot) string { return strings.Join(core.SlotNames(s), ",") }
		for k := range c.flow.terminals {
			if k.rule != cr.ID {
				continue
			}
			if i, ok := at("terminal", k.e); ok {
				add("terminal", i, "")
				n.terminals++
			}
		}
		for k, runs := range c.flow.runs {
			if k.rule != cr.ID {
				continue
			}
			if i, ok := at("class runs", k.e); ok {
				add("runs", i, "\t%s", runs)
				n.classRuns += len(strings.Fields(runs))
			}
		}
		coreNodes(cr, func(x core.Node) {
			switch x := x.(type) {
			case *core.Choice:
				for i, a := range x.Alts {
					e, ok := c.altOrigin(x, i)
					if !ok {
						problem("guard", "alternative %d without origin", i)
						continue
					}
					ei, ok := at("guard", e)
					if !ok {
						continue
					}
					guarded := c.flow.guarded[flowKey{cr.ID, e}]
					if a.Guard == nil {
						add("guard", ei, "\t0\t0\t%d\t%q\t%q", typedDecisionsBit(guarded), "", "")
						continue
					}
					cond, desc := guardString(c.m, a.Guard)
					add("guard", ei, "\t1\t%d\t%d\t%q\t%q", a.Guard.Depth, typedDecisionsBit(guarded), desc, cond)
					n.guards++
					if guarded {
						n.guardedClasses++
					}
				}
			case *core.Scan:
				e, ok := c.origin.Exprs[x.Attrs().ID]
				if !ok {
					problem("scan", "scan without origin")
					return
				}
				if i, ok := at("scan", e); ok {
					add("scan", i, "")
					n.scans++
				}
			case *core.Repeat:
				e, ok := c.origin.Of(x).(*ast.Repeat)
				if !ok || x.Scope == nil {
					return
				}
				if i, ok := at("scope", e.Expr); ok {
					add("scope", i, "\t%s", names(x.Scope))
					n.scopes++
				}
			case *core.OperatorsRule:
				lineList, exprs, _ := opLines(x, r.Def.Expr.(*ast.OperatorsRule))
				for i, l := range lineList {
					if ei, ok := at("scope", exprs[i]); ok {
						add("scope", ei, "\t%s", names(l.Scope))
						n.scopes++
					}
				}
			}
		})
	}
	sort.Strings(lines)
	return lines, problems
}

// decisionAspects maps the kind of a decision line to its aspect.
var decisionAspects = map[string]string{"terminal": "terminal", "guard": "guard", "runs": "class runs", "scan": "scan", "scope": "scope"}

// compareCoreDecisions compares the decisions of the Core IR of c with the golden's, and returns
// the differences, each prefixed with its aspect (terminal, guard, class runs, scan, scope;
// verify for a pass after which Verify fails). It sets c.lines. With -update only the problems of the
// encoding and of Verify are returned: the frozen-decisions tests write the golden from c.lines.
func compareCoreDecisions(c *coreGenCase, n *coreDecisionCounts) []string {
	lines, errs := coreDecisionLines(c, n)
	c.lines = lines
	for _, e := range c.passErrs {
		errs = append(errs, "verify: "+e)
	}
	if !*codegentest.UpdateGoldens {
		errs = append(errs, c.golden.differences(decisionsKey{c.mode, c.name}, lines, func(id int) string {
			if id >= 0 && id < len(c.rules) {
				return c.rules[id].Name
			}
			return "?"
		})...)
	}
	slices.Sort(errs)
	return errs
}

// coreAspectErrors returns the differences of the aspect among errs (compareCoreDecisions).
func coreAspectErrors(errs []string, aspect string) []string {
	var out []string
	for _, e := range errs {
		if strings.HasPrefix(e, aspect+": ") {
			out = append(out, e)
		}
	}
	return out
}

// TestCoreTypedDecisionsFrozen compares the decisions of the Core IR passes over the corpus in
// each mode with the typed decisions frozen in testdata/decisions/typed, and checks that
// every kind of decision is met. -update rewrites the golden (core_typed_decisions_test.go).
func TestCoreTypedDecisionsFrozen(t *testing.T) {
	typedGolden.freeze(t, coreGenModes, func(t *testing.T, mode coreGenMode) []*coreGenCase {
		return coreGenCases(t, mode, nil)
	}, func(t *testing.T, mode coreGenMode, n *coreDecisionCounts) {
		// The observing bodies of a build with observability are its traced copies, which record every
		// read and so are not lean: they have no terminal programs, class runs or guarded classes. Nor
		// has the form that charges budget steps, which only a Document or a stream with limit controls
		// still prints; outside them the controlled form is the lean one with a counter of compute units.
		lean := !budgetForm(mode.opts) && !mode.opts.Observability
		if n.guards == 0 || n.scans == 0 || n.scopes == 0 ||
			(n.terminals > 0) != lean || (n.classRuns > 0) != lean || (n.guardedClasses > 0) != lean {
			t.Errorf("counts %+v", n)
		}
	})
}

// budgetForm reports whether the options print the form that charges budget steps: limit controls in
// a Document or a stream. Elsewhere a parser with limit controls prints the ordinary form and counts
// logical compute units (spec/runtime/parsing.md, Compute Units).
func budgetForm(opts common.GenOptions) bool {
	return !opts.NoLimitControls && (opts.Document || opts.Stream)
}
