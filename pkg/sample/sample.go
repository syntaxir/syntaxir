// Package sample generates inputs that a grammar accepts, for testing code that consumes parse results
// and for seeding fuzz tests.
//
// Its audience is Go programs that test their own grammars and parse results, such as fuzz tests and
// table tests; the sxir command uses it for the same purpose. At v0.x its API is unstable and may change
// in any release.
//
// A Generator walks the grammar AST of a parser from its start rule and writes candidate inputs, making
// random decisions at ordered choices, repetitions, optional expressions and Operators Rule expressions. The
// decisions are reproducible: the same parser, options and seed give the same inputs with the same
// version of this package, whichever Go release built it (the generator's algorithm may change between
// versions; the package draws its random numbers with its own generator, as math/rand/v2 does not
// promise to keep its algorithms across Go releases). Recursion and length are bounded softly
// (WithMaxDepth, WithMaxLen): past the bounds, the generator prefers the shortest way to finish the input. The search itself has hard limits
// (WithBudget, and the limits on nesting and iterations described with WithMaxDepth and WithMaxRepeat).
//
// Every input that the generator returns has been parsed with the parser and accepted without errors
// (also without errors recovered with #recover). Generating a candidate by walking the grammar is not
// enough under PEG semantics: an ordered choice may never reach a later alternative for some inputs,
// a greedy repetition consumes as much as it can (no input generated for a* a parses), and lookaheads,
// predicates and variables depend on the input around them. The generator steers away from such
// candidates where it can tell, evaluating predicates on the text it generated and checking what the
// parser would match as the text grows, and retries otherwise; what it cannot evaluate, such as
// predicates over the results of actions, is handled only by generating and checking. The design is
// described in docs/design/records/015-input-generation.md.
//
// With WithCoverage, the generator prefers rules and alternatives that no accepted input has exercised
// yet, and Coverage reports those that remain, each with what became of the attempts that tried it
// (Why). Invalid generates near-miss invalid inputs: single
// mutations of valid inputs that the parser rejects. Seed adds generated inputs to the seed corpus of a
// fuzz test.
package sample

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/runtime/state"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/pkg/runtime"
)

// Defaults of the options.
const (
	DefaultMaxDepth  = 5
	DefaultMaxRepeat = 3
	DefaultMaxLen    = 512
	DefaultAttempts  = 100
	DefaultBudget    = 20000
	defaultRetry     = 4096
)

type config struct {
	seed      uint64
	maxDepth  int
	maxRepeat int
	maxLen    int
	attempts  int
	budget    int
	retry     int
	coverage  bool
}

// Option configures a Generator.
type Option func(*config)

// WithSeed sets the seed of the random decisions (default 0). The same parser, options and seed give
// the same inputs with the same version of this package (see the package documentation).
func WithSeed(seed uint64) Option { return func(c *config) { c.seed = seed } }

// WithMaxDepth bounds recursion: once rule calls are nested d deep inside calls of the same rules, the
// generator prefers the shortest way to finish (default DefaultMaxDepth). Calls of rules that are not
// already active do not count. The bound is soft: when the input requires deeper nesting (a predicate
// that counts it, for example), the search goes deeper, but never beyond d + 8: raise d for inputs that
// must nest deeper.
func WithMaxDepth(d int) Option { return func(c *config) { c.maxDepth = max(d, 0) } }

// WithMaxRepeat sets how many iterations an unbounded repetition aims for beyond its minimum, at most
// (default DefaultMaxRepeat). It also bounds the operators of an Operators Rule expression chain. The search may
// take more iterations when the input requires them (a predicate after the repetition wants a longer
// match), but never more than 8 × n + 4 beyond the minimum (28 by default): raise n for inputs with
// longer repetitions.
func WithMaxRepeat(n int) Option { return func(c *config) { c.maxRepeat = max(n, 0) } }

// WithMaxLen sets a soft limit on the length of an input in bytes (default DefaultMaxLen): once the input
// is that long, the generator prefers the alternatives with the shortest text to finish it.
func WithMaxLen(n int) Option { return func(c *config) { c.maxLen = max(n, 0) } }

// WithBudget sets the number of steps one attempt may take before it gives up (default DefaultBudget).
// A step is a parsing expression generated or a OperatorsRule scope inspected; checks have a budget of their
// own in proportion. Past WithMaxDepth and WithMaxLen, the generator only prefers the shortest way to
// finish. The budget and the nesting/iteration limits (see WithMaxDepth and WithMaxRepeat) bound an
// attempt. Inputs that need many steps, such as an expression repeated 25,000 times, need a larger
// budget; the budget also bounds the search stack, which grows with the steps of an attempt.
func WithBudget(steps int) Option { return func(c *config) { c.budget = max(steps, 1) } }

// WithAttempts sets how many candidates the generator tries for one input before giving up (default
// DefaultAttempts).
func WithAttempts(n int) Option { return func(c *config) { c.attempts = max(n, 1) } }

// WithCoverage biases the generator toward rules, alternatives of ordered choices, and Operators Rule operands
// and operators that no accepted input has exercised yet.
func WithCoverage() Option { return func(c *config) { c.coverage = true } }

// ErrNoInput is returned when the generator finds no input within its attempts. The error returned
// wraps it with what became of the attempts (see Stats); test for it with errors.Is.
var ErrNoInput = errors.New("sample: no input found that the parser accepts")

// ErrNoInvalid is returned when the generator finds valid inputs but no mutation of them that the
// parser rejects.
var ErrNoInvalid = errors.New("sample: no mutation found that the parser rejects")

// Generator generates inputs for a parser. It is not safe for concurrent use.
type Generator struct {
	p     runtime.Parser
	cfg   config
	in    *info
	g     *gen
	stats Stats
	// names tell the rules of the inline actions, which the generator works on as hidden rules, from
	// the declared ones: reports show them as written.
	names *desugar.InlineActionNames
}

// Stats counts the work of a Generator.
type Stats struct {
	Candidates int // candidates passed to the parser
	Rejected   int // candidates that the parser rejected
	Failed     int // searches that ended without a candidate
	// Exhausted counts the failed searches that used up their budget (WithBudget). The others gave up
	// on every way to finish the input that they tried: the grammar may accept nothing within the
	// bounds, or the checks below may rule out the ways the search takes.
	Exhausted int
	// Duplicates counts the inputs that Generate and GenerateInvalid found again and skipped.
	Duplicates int
	// CheckCuts counts the ways that the search abandoned because a lookahead, the order of a choice
	// or $$ showed that the parser would not match them; PredicateCuts those abandoned because a
	// predicate would certainly fail. Many cuts in a failing search point at the checks that block it.
	CheckCuts     int
	PredicateCuts int
}

// RuleCuts counts the cuts of the checks and predicates in one rule's body (see Stats.CheckCuts and
// Generator.Cuts). A check belongs to the rule whose body holds the lookahead, choice, optional,
// repetition or $$ that installed it.
type RuleCuts struct {
	Rule       string `json:"rule"`
	Checks     int    `json:"checks"`
	Predicates int    `json:"predicates"`
}

// New returns a generator of inputs for p, starting at p's start rule. The parser must be compiled from
// the grammar source by package compiler: a parser loaded from a compiled grammar has no grammar AST and
// cannot be sampled.
func New(p runtime.Parser, opts ...Option) (*Generator, error) {
	var (
		src   state.Source
		start string
		ok    bool
	)
	if state.ParserSource != nil {
		src, start, ok = state.ParserSource(p)
	}
	if !ok {
		return nil, errors.New("sample: the parser is not a parser of package compiler or vm")
	}
	cs, _ := src.(*checked.Source)
	if cs == nil {
		return nil, errors.New("sample: the parser has no grammar AST")
	}
	// The sampler reads the attributes in their normalized form, such as #recover.
	g := cs.Normalized()
	// The sampler works on the rules the parser runs, where an inline action is an anonymous rule.
	g, names, err := desugar.LowerInlineActions(g)
	if err != nil {
		return nil, fmt.Errorf("sample: %w", err)
	}
	cfg := config{
		maxDepth:  DefaultMaxDepth,
		maxRepeat: DefaultMaxRepeat,
		maxLen:    DefaultMaxLen,
		attempts:  DefaultAttempts,
		budget:    DefaultBudget,
		retry:     defaultRetry,
	}
	for _, o := range opts {
		o(&cfg)
	}
	in := analyze(g, start)
	if in.start == nil {
		return nil, fmt.Errorf("sample: start rule %s is not defined", start)
	}
	if in.start.height >= inf {
		return nil, fmt.Errorf("sample: start rule %s can never match", start)
	}
	gen := &Generator{p: p, cfg: cfg, in: in, names: names}
	gen.g = newGen(in, &gen.cfg)
	return gen, nil
}

// Next returns an input that the parser accepts. Inputs may repeat. It returns ErrNoInput if no
// candidate was accepted within the attempts.
func (g *Generator) Next() (string, error) {
	before := g.Stats()
	clear(g.g.recent)
	for range g.cfg.attempts {
		s, trail, ok := g.g.attempt()
		if !ok {
			g.stats.Failed++
			if g.g.exhausted() {
				g.stats.Exhausted++
				g.g.failed(outOfBudget)
			} else {
				g.g.failed(noWay)
			}
			continue
		}
		g.stats.Candidates++
		if _, err := g.p.Parse(s); err != nil {
			g.stats.Rejected++
			g.g.failed(rejectedCandidate)
			continue
		}
		g.g.accepted(trail)
		return s, nil
	}
	return "", noInput(g.cfg.attempts, before, g.Stats(), g.ruleCuts(g.g.recent))
}

// noInput returns ErrNoInput with what became of the attempts between the stats before and after.
// cuts lists the rules with the most cuts first.
func noInput(attempts int, before, after Stats, cuts []RuleCuts) error {
	failed, exhausted := after.Failed-before.Failed, after.Exhausted-before.Exhausted
	var most strings.Builder
	writeCuts(&most, cuts[:min(len(cuts), maxCuts)])
	return fmt.Errorf("%w in %s: %s rejected by the parser, %s out of budget, %s without a way to finish (%s cut by checks, %d by predicates%s)",
		ErrNoInput, count(attempts, "attempt", "attempts"),
		count(after.Rejected-before.Rejected, "candidate", "candidates"),
		count(exhausted, "search", "searches"), count(failed-exhausted, "search", "searches"),
		count(after.CheckCuts-before.CheckCuts, "way", "ways"), after.PredicateCuts-before.PredicateCuts, most.String())
}

// Cuts returns the cuts of each rule so far, for the rules with any, the rules with the most cuts
// first (ties in rule name order).
func (g *Generator) Cuts() []RuleCuts { return g.ruleCuts(g.g.cuts) }

func (g *Generator) ruleCuts(counts []ruleCuts) []RuleCuts {
	var out []RuleCuts
	for i, c := range counts {
		if c.checks+c.predicates > 0 {
			out = append(out, g.ruleCut(i, c))
		}
	}
	sortCuts(out)
	return out
}

func (g *Generator) ruleCut(rule int, c ruleCuts) RuleCuts {
	return RuleCuts{Rule: g.names.Rule(g.in.order[rule].def.Name), Checks: c.checks, Predicates: c.predicates}
}

// sortCuts sorts the rules with the most cuts first, ties in rule name order.
func sortCuts(out []RuleCuts) {
	slices.SortFunc(out, func(a, b RuleCuts) int {
		if n := cmp.Compare(b.Checks+b.Predicates, a.Checks+a.Predicates); n != 0 {
			return n
		}
		return strings.Compare(a.Rule, b.Rule)
	})
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Generate returns up to n distinct inputs that the parser accepts. It returns fewer if it cannot find
// more (a grammar may accept only a few inputs; it gives up after as many inputs in a row that it already
// returned as the attempts per input), and ErrNoInput if it finds none.
func (g *Generator) Generate(n int) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	var err error
	for dups := 0; len(out) < n && dups < g.cfg.attempts; {
		var s string
		if s, err = g.Next(); err != nil {
			break
		}
		if seen[s] {
			dups++
			g.stats.Duplicates++
			continue
		}
		seen[s] = true
		dups = 0
		out = append(out, s)
	}
	if len(out) == 0 && n > 0 {
		return nil, err
	}
	return out, nil
}

// Stats returns the counts of the work done so far.
func (g *Generator) Stats() Stats {
	s := g.stats
	s.CheckCuts, s.PredicateCuts = g.g.checkCuts, g.g.predicateCuts
	return s
}

// Generate returns up to n distinct inputs that p accepts (see Generator.Generate).
func Generate(p runtime.Parser, n int, opts ...Option) ([]string, error) {
	g, err := New(p, opts...)
	if err != nil {
		return nil, err
	}
	return g.Generate(n)
}

// Coverage reports which parts of the grammar the accepted inputs exercised.
//
// Coverage is measured on the way the generator derived each accepted input. The parser takes the same
// way unless the generator could not evaluate a lookahead, a predicate or an earlier alternative of a
// choice (see the package documentation); counts are therefore close to, but not guaranteed to be, the
// parser's own coverage.
type Coverage struct {
	// Rules counts the rules that the start rule can reach and that can match; RulesCovered those that
	// an accepted input exercised.
	Rules        int `json:"rules"`
	RulesCovered int `json:"rulesCovered"`
	// Alternatives counts the alternatives of ordered choices and the operands and operators of OperatorsRule
	// expressions in those rules, leaving out those that can never match or that occur only inside an
	// expression that can never match, or in OperatorsRule tails that no reachable entry or prefix RHS permits.
	Alternatives        int `json:"alternatives"`
	AlternativesCovered int `json:"alternativesCovered"`
	// MissedRules and MissedAlternatives list what was not exercised, each with what the generator
	// knows about why.
	MissedRules        []MissedRule        `json:"missedRules,omitempty"`
	MissedAlternatives []MissedAlternative `json:"missedAlternatives,omitempty"`
	// Unreachable lists the rules that generation cannot exercise from the start rule: rules that are
	// not called, or called only inside negative lookaheads (!keyword), the skip of #recover, or
	// expressions that can never match or blocked Operators Rule operator parts. Impossible lists the rules
	// that the start rule calls (outside negative lookaheads and #recover) but that can never match,
	// such as rules that end in _|_ to report an error. Neither is counted in Rules.
	Unreachable []string `json:"unreachable,omitempty"`
	Impossible  []string `json:"impossible,omitempty"`
}

// Alternative identifies an alternative of an ordered choice, or an operand or operator of a OperatorsRule
// expression.
type Alternative struct {
	Rule string `json:"rule"`
	// Kind is "choice", "operand" or "operator".
	Kind string `json:"kind"`
	// Index is the position of the alternative in its choice, or of the operand or operator in the
	// Operators Rule expression (operators are counted across all levels).
	Index int `json:"index"`
	// Expr is the alternative in SyntaxIR syntax.
	Expr string `json:"expr"`
}

func (a Alternative) String() string {
	return fmt.Sprintf("%s: %s %d: %s", a.Rule, a.Kind, a.Index, a.Expr)
}

// MissedRule is a rule that no accepted input exercised.
type MissedRule struct {
	Rule string `json:"rule"`
	Why
}

// MissedAlternative is an alternative, operand or operator that no accepted input exercised.
type MissedAlternative struct {
	Alternative
	Why
}

// Why tells what the generator knows about why no accepted input exercised a target.
//
// The counts are of the attempts that tried the target, that is whose search generated it at some
// point, and that gave no accepted input that exercises it. Rejected, Failed and Exhausted have the
// meanings of the members of Stats with the same names. No attempt means that no search took the
// target: other options were preferred, or the bounds of the options kept the search away from it.
type Why struct {
	// Bypassed counts the attempts that left the target behind and found an accepted input without
	// it: the search backtracked out of every way through it that it tried.
	Bypassed  int `json:"bypassed"`
	Rejected  int `json:"rejected"`  // candidates that the parser rejected
	Failed    int `json:"failed"`    // searches that ended without a candidate
	Exhausted int `json:"exhausted"` // the failed searches that used up their budget
	// Cuts lists the rules whose checks and predicates cut the most ways in those attempts, at most
	// three, the rules with the most cuts first (ties in rule name order).
	Cuts []RuleCuts `json:"cuts,omitempty"`
	// MinLen is the length in bytes of the shortest input that exercises the target as far as the
	// structure of the grammar tells, leaving out lookaheads, predicates and the order of choices: a
	// lower bound. A MinLen beyond WithMaxLen means that the length bound steers the search away.
	MinLen int `json:"minLen"`
}

// String describes the explanation on one line.
func (w Why) String() string {
	var b strings.Builder
	if w.Bypassed+w.Rejected+w.Failed == 0 {
		b.WriteString("not tried")
	} else {
		fmt.Fprintf(&b, "tried in %s:", count(w.Bypassed+w.Rejected+w.Failed, "attempt", "attempts"))
		sep := " "
		for _, part := range []struct {
			n    int
			what string
		}{{w.Bypassed, "accepted without it"}, {w.Rejected, "rejected by the parser"},
			{w.Exhausted, "out of budget"}, {w.Failed - w.Exhausted, "without a way to finish"}} {
			if part.n > 0 {
				fmt.Fprintf(&b, "%s%d %s", sep, part.n, part.what)
				sep = ", "
			}
		}
		writeCuts(&b, w.Cuts)
	}
	fmt.Fprintf(&b, "; shortest input %s", count(w.MinLen, "byte", "bytes"))
	return b.String()
}

// writeCuts appends the rules with the most cuts.
func writeCuts(b *strings.Builder, cuts []RuleCuts) {
	for i, c := range cuts {
		if i == 0 {
			b.WriteString("; most cuts in ")
		} else {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "%s (%d by checks, %d by predicates)", c.Rule, c.Checks, c.Predicates)
	}
}

// maxCuts is the number of rules that Why.Cuts and the error of Next list at most.
const maxCuts = 3

// why explains why the target was not exercised; minLen is the length of the shortest input that
// exercises it.
func (g *Generator) why(t, minLen int) Why {
	l := &g.g.log[t]
	w := Why{Bypassed: l.bypassed, Rejected: l.rejected, Failed: l.failed, Exhausted: l.exhausted, MinLen: minLen}
	for r, c := range l.cuts {
		w.Cuts = append(w.Cuts, g.ruleCut(r, c))
	}
	sortCuts(w.Cuts)
	w.Cuts = w.Cuts[:min(len(w.Cuts), maxCuts)]
	return w
}

// Coverage returns the coverage of the inputs accepted so far.
func (g *Generator) Coverage() Coverage {
	in := g.in
	var c Coverage
	for _, ri := range in.order {
		switch {
		case ri.height >= inf && in.called[ri.index]:
			c.Impossible = append(c.Impossible, g.names.Rule(ri.def.Name))
		case !in.reachable[ri.index]:
			c.Unreachable = append(c.Unreachable, g.names.Rule(ri.def.Name))
		}
	}
	kinds := map[int]string{targetAlt: "choice", targetOperand: "operand", targetOperator: "operator"}
	minLens := in.minLens()
	for i, t := range in.targets {
		ri := in.rules[t.rule]
		if !in.reachable[ri.index] || ri.height >= inf || !t.possible {
			continue
		}
		hit := g.g.covered.has(i)
		if t.kind == targetRule {
			c.Rules++
			if hit {
				c.RulesCovered++
			} else {
				c.MissedRules = append(c.MissedRules, MissedRule{Rule: g.names.Rule(t.rule), Why: g.why(i, minLens[i])})
			}
			continue
		}
		c.Alternatives++
		if hit {
			c.AlternativesCovered++
		} else {
			a := Alternative{Rule: g.names.Rule(t.rule), Kind: kinds[t.kind], Index: t.index, Expr: g.names.Expr(syntaxfmt.FormatExpr(t.expr))}
			c.MissedAlternatives = append(c.MissedAlternatives, MissedAlternative{Alternative: a, Why: g.why(i, minLens[i])})
		}
	}
	return c
}

// String summarizes the coverage on one line.
func (c Coverage) String() string {
	return fmt.Sprintf("rules %d/%d (%s), alternatives %d/%d (%s)",
		c.RulesCovered, c.Rules, percent(c.RulesCovered, c.Rules),
		c.AlternativesCovered, c.Alternatives, percent(c.AlternativesCovered, c.Alternatives))
}

// Report describes the coverage in detail, one item per line.
func (c Coverage) Report() string {
	var b strings.Builder
	b.WriteString(c.String() + "\n")
	for _, r := range c.MissedRules {
		fmt.Fprintf(&b, "missed rule: %s (%s)\n", r.Rule, r.Why)
	}
	for _, a := range c.MissedAlternatives {
		fmt.Fprintf(&b, "missed %s (%s)\n", a.Alternative, a.Why)
	}
	if len(c.Unreachable) > 0 {
		fmt.Fprintf(&b, "unreachable: %s\n", strings.Join(c.Unreachable, ", "))
	}
	if len(c.Impossible) > 0 {
		fmt.Fprintf(&b, "never match: %s\n", strings.Join(c.Impossible, ", "))
	}
	return b.String()
}

func percent(a, b int) string {
	if b == 0 {
		return "100%"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(a)/float64(b))
}
