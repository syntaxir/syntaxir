package complexity_test

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/analysis/complexity"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/complexitytest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
	"syntaxir.dev/sxir/pkg/sample"
)

// TestViewEquivalence checks that the views of a grammar match what the grammar matches: ≡E of
// spec/proofs/grammar-laws.md (Equivalences), which every rule of the normalization preserves. For
// each subject (equivSubjects) it compares the grammar with each of its views (equivViews) that
// rewrites something: the view that the classifier reads, the view for analyses of matching, and
// the view of each law alone, so that a difference names the law that made it. A rule of a view
// and the rule of the grammar it is built from are each entered as start rules, on the same inputs,
// and must give the same observation (equivObservation): whether the call matches, and where it
// ends. That is what ≡E keeps of a call, since the environment and the captures of a rule
// invocation are not visible to its caller and a cut does not leave a rule.
//
// The inputs of the default start rule are the inputs written for the grammar and inputs that
// package sample generates for it with coverage of its alternatives. The inputs of every rule are
// the inputs from the places where parses of those call it, whether the call matches or fails
// (harvest), and inputs generated for a rule that no call matches; with changes of each by one
// character, each twice in a row, and the rule's literals before and after the shortest (derived);
// and the literals of the rule (literalInputs). equivTierOf sizes them.
//
// The test is held to what it checks by TestViewEquivalenceMutants, in the long tier: a mutant of
// each law that changes what the law's rewrites match must fail the subtest of that law on every
// subject where a parse reaches a rewrite of the law.
func TestViewEquivalence(t *testing.T) {
	tier := equivTierOf()
	for _, s := range equivSubjects(t) {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			c := newEquivCheck(t, s, tier)
			for _, v := range equivViews() {
				t.Run(v.name, func(t *testing.T) { c.check(t, v.opts) })
			}
		})
	}
}

// equivTier sizes the inputs of TestViewEquivalence: the inputs generated per rule, the changes made
// of each, and the changes made of each written input.
type equivTier struct {
	// startInputs are the inputs generated for the default start rule, perRule the inputs of each
	// rule taken from parses of the start rule's inputs, and unreached the inputs generated for a
	// rule that no such parse matches.
	startInputs, perRule, unreached int
	// prefixed is how many inputs of a rule, the shortest, each literal of the rule is written
	// before (derived).
	prefixed int
	// changes and writtenChanges are the changes made of each input of a rule and of each input of
	// the default start rule.
	changes, writtenChanges int
	// attempts bounds the candidates that package sample tries for one input of a rule.
	attempts int
}

// equivLaws are the laws of the normalization, in order.
var equivLaws = []complexity.Law{complexity.N1, complexity.N2, complexity.N3, complexity.N4, complexity.N5,
	complexity.N6, complexity.N7, complexity.N8, complexity.N9, complexity.N10, complexity.N11,
	complexity.N12, complexity.N13}

// An equivSubject is a grammar whose views TestViewEquivalence compares with it, and the inputs
// written for it.
type equivSubject struct {
	name    string
	src     string
	written []string
}

// An equivView is a view that TestViewEquivalence checks: its subtest name and its options.
type equivView struct {
	name string
	opts complexity.Options
}

// equivViews returns the views that TestViewEquivalence checks: the view that the classifier
// reads, the view for analyses of matching, and the view of each law alone, named by the law.
func equivViews() []equivView {
	vs := []equivView{
		{"classifier", complexity.Options{}},
		{"matching", complexity.Options{Labels: true, Inline: true}},
	}
	for _, l := range equivLaws {
		vs = append(vs, equivView{string(l), complexity.Options{Laws: []complexity.Law{l},
			Labels: l == complexity.N7, Inline: l == complexity.N13}})
	}
	return vs
}

// equivSubjects returns the subjects of TestViewEquivalence: the bundled grammars, with the inputs
// of their directories; grammars of shapes that the bundled grammars lack (equivShapes); and the
// grammar of each case of TestNormalView in which a law applies, which gives every law a subject,
// as the bundled grammars apply only some of them.
func equivSubjects(t testing.TB) []equivSubject {
	var out []equivSubject
	for _, tg := range append(slices.Clone(complexitytest.TriageGrammars), complexitytest.LinearGrammars...) {
		src, err := os.ReadFile(tg.Path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, equivSubject{tg.Name, string(src), writtenInputs(t, filepath.Dir(tg.Path))})
	}
	for _, s := range equivShapes {
		out = append(out, equivSubject{"shape_" + subtestName(s.name), "@start\n" + s.src + "\n", s.inputs})
	}
	for _, c := range complexity.LawCases() {
		out = append(out, equivSubject{"case_" + subtestName(c.Name), c.Src, nil})
	}
	return out
}

// subtestName returns name as the testing package writes it in the name of a subtest.
func subtestName(name string) string { return strings.ReplaceAll(name, " ", "_") }

// equivShapes are grammars of shapes that the bundled grammars lack, with inputs: cuts nested in
// the only alternative that a choice keeps, which commit the choice and nothing around it, and
// captures that predicates read, which the view must not move or merge.
var equivShapes = []struct {
	name, src string
	inputs    []string
}{
	{"cut in a discard in a repetition", `rule main = (-("a" #cut "b") / !"q" "q")* "ac"`, []string{"ababac", "ac", "abac", "aac", "abq"}},
	{"cut in a capture in an option", `rule main = (x:("a" #cut "b") / !"q" "q")? "ac"`, []string{"abac", "ac", "aac"}},
	{"cut in an atomic in a repetition", `rule main = (("a" #cut "b") #atomic / !"q" "q")* "ac"`, []string{"ababac", "ac", "abq"}},
	{"cut in a labeled expression in a repetition", `rule main = (("a" #cut "b") @error(message="m") / !"q" "q")* "ac"`, []string{"ababac", "ac"}},
	{"cut in a choice spliced into a choice", "rule main = ((-(\"x\" #cut e \"q\") / !\"z\" \"z\") / \"x\")* $$\nrule e = e \"x\" / \"x\"", []string{"xxq", "x", "xxxq", "xxqx", "xx"}},
	{"capture in a lookahead of a called rule", "rule main = y:r {len($y.$z) == 2} (e \"q\" / \"x\")* $$\nrule r = &(z:s) \"a\"*\nrule s = \"a\"* \"a\"*\nrule e = e \"x\" / \"x\"", []string{"x", "axq", "aaxq", "aax", "xxq", "aaaxq"}},
	{"read capture merged by a repetition", "rule main = x:\"a\" (x:\"a\")* {text($x) == \"a\"} (e \"q\" / \"x\")* $$\nrule e = e \"x\" / \"x\"", []string{"axxq", "aaxq", "ax", "aax", "a"}},
	{"read capture merged by an optional", "rule main = (x:\"a\")? (x:\"a\")* {text($x) == \"a\"} (e \"q\" / \"x\")* $$\nrule e = e \"x\" / \"x\"", []string{"axxq", "aaxq", "ax", "aax", "x"}},
	{"cut that ends an alternative of a read capture", "rule main = x:(\"abc\" #cut / \"z\") {len($x) == 1} (e \"q\" / \"x\")* $$\nrule e = e \"x\" / \"x\"", []string{"abcxxq", "abcx", "zxq", "zx", "abc", "abxq"}},
	{"cut of the last alternative around a read capture", "rule main = (\"w\" / x:(\"abc\" #cut)) {len($x) == 1} (e \"q\" / \"x\")* $$\nrule e = e \"x\" / \"x\"", []string{"abcxxq", "abcx", "wxq", "wx", "abc"}},
	{"cut in a rule that a read capture reaches", "rule main = y:r {len($y) == 1} (e \"q\" / \"x\")* $$\nrule r = \"abc\" #cut / \"z\"\nrule e = e \"x\" / \"x\"", []string{"abcxxq", "abcx", "zxq", "zx", "abc"}},
	{"choice of one sequence in a read capture", "rule main = x:(\"a\" (\"b\" \"c\" / _|_)) {len($x) == 2} (e \"q\" / \"x\")* $$\nrule e = e \"x\" / \"x\"", []string{"abcxxq", "abcx", "abxq", "ab"}},
	{"call of a rule with several items in a read capture", "rule main = x:(\"a\" r) {len($x) == 2} (e \"q\" / \"x\")* $$\nrule r = \"b\" \"c\"\nrule e = e \"x\" / \"x\"", []string{"abcxxq", "abcx", "abxq", "abc"}},
	{"cut in a skip", `rule main = ("a" "b" #recover(skip=#cut [^;]*) / !"q" "q")* "ac"`, []string{"abac", "ac", "aac", "a;ac"}},
}

// writtenInputs returns the inputs written for the grammar in dir: the text files of dir and of its
// testdata directory, each cut to its first 4096 bytes.
func writtenInputs(t testing.TB, dir string) []string {
	var out []string
	for _, d := range []string{dir, filepath.Join(dir, "testdata")} {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(d, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if len(b) > 4096 {
				b = b[:4096]
			}
			out = append(out, string(b))
		}
	}
	return out
}

// An equivCheck compares the views of one subject with it.
type equivCheck struct {
	subject equivSubject
	g       *ast.Grammar
	// progs are the grammar compiled with its actions and with default values (compiled), each
	// with every rule that is not hidden entered as a start rule (entered).
	progs [2]*engine.Program
	// def is the default start rule, and inputs the inputs of each rule.
	def    string
	inputs map[string][]string
	rules  []string
	// observations are those of the grammar, compiled one way or the other, which every view is
	// compared with.
	observations [2]map[equivCall]equivResult
}

// An equivCall is a call of a rule on an input, and an equivResult what observe returns for it.
type equivCall struct{ rule, in string }

type equivResult struct {
	o   equivObservation
	err error
}

// observed returns what observe returns for the call of rule on in in the grammar compiled as mode
// gives, once for all the views of the subject.
func (c *equivCheck) observed(mode int, prog *engine.Program, rule, in string) (equivObservation, error) {
	k := equivCall{rule, in}
	if r, ok := c.observations[mode][k]; ok {
		return r.o, r.err
	}
	if c.observations[mode] == nil {
		c.observations[mode] = map[equivCall]equivResult{}
	}
	o, err := observe(prog, rule, in)
	c.observations[mode][k] = equivResult{o, err}
	return o, err
}

// compiled indexes equivCheck.progs: a grammar compiled with its actions, or with default values,
// which drops the actions and the declared types. A view that inlines calls (N13) or reshapes
// values can give a rule a value that its declared type does not accept; it is compared with
// default values, as is the grammar then. The view keeps the values that predicates read, which
// default values keep as well.
const (
	withActions = iota
	withDefaults
)

// newEquivCheck parses and compiles the subject and makes the inputs of its rules.
func newEquivCheck(t *testing.T, s equivSubject, tier equivTier) *equivCheck {
	begin := time.Now()
	g, err := parser.Parse(s.src)
	if err != nil {
		t.Fatal(err)
	}
	c := &equivCheck{subject: s, g: g, inputs: map[string][]string{}}
	for i, o := range []entry.Options{{}, {Compile: check.Options{DefaultValue: true}}} {
		if c.progs[i], err = entry.Compile(entered(g), o); err != nil {
			t.Fatalf("the grammar with every rule a start rule does not compile: %v", err)
		}
	}
	prog := c.progs[withActions]
	parserOf := func(rule string) runtime.Parser {
		p, err := engine.NewParser(prog, rule)
		if err != nil {
			t.Fatal(err)
		}
		return engine.Reference(p)
	}
	c.def = engine.Of(parserOf("")).Start()
	generate := func(rule string, n, attempts int) []string {
		gen, err := sample.Generate(parserOf(rule), n, sample.WithSeed(1), sample.WithCoverage(),
			sample.WithMaxLen(256), sample.WithAttempts(attempts))
		if err != nil && !errors.Is(err, sample.ErrNoInput) {
			// A rule that can never match has no input to generate; its literals are its inputs.
			t.Logf("no inputs generated for rule %s: %v", rule, err)
		}
		return gen
	}
	// The inputs of the default start rule are the written ones and inputs generated for it. Every
	// call in a parse of them gives the rule it calls inputs (harvest).
	whole := append(slices.Clone(s.written), generate(c.def, tier.startInputs, tier.attempts)...)
	found := map[string]*calledAt{}
	for _, in := range whole {
		harvest(prog, c.def, in, tier.perRule, found)
	}
	h := fnv.New64a()
	h.Write([]byte(s.name))
	rng := rand.New(rand.NewPCG(1, h.Sum64()))
	generated := 0
	for _, rd := range g.Rules() {
		if desugar.HiddenRule(rd.Name) {
			continue
		}
		c.rules = append(c.rules, rd.Name)
		var ins []string
		if f := found[rd.Name]; f != nil {
			ins = slices.Concat(f.texts, f.matched, f.failed)
		}
		if (found[rd.Name] == nil || len(found[rd.Name].matched) == 0) && tier.unreached > 0 {
			// A rule that no parse of the start rule's inputs matched has inputs of its own.
			gen := generate(rd.Name, tier.unreached, tier.attempts)
			if len(gen) > 0 {
				generated++
			}
			ins = append(ins, gen...)
		}
		c.inputs[rd.Name] = distinct(append(derived(rng, ins, literals(rd), tier), literalInputs(rd)...))
	}
	for _, in := range whole {
		c.inputs[c.def] = append(c.inputs[c.def], in)
		for range tier.writtenChanges {
			c.inputs[c.def] = append(c.inputs[c.def], changed(rng, in))
		}
	}
	c.inputs[c.def] = distinct(c.inputs[c.def])
	fromStart := countFound(c.rules, found)
	t.Logf("%d rules: %d with inputs from parses of the start rule, %d with generated inputs, %d with literals only; in %v",
		len(c.rules), fromStart, generated, len(c.rules)-fromStart-generated, time.Since(begin))
	return c
}

// A calledAt holds the inputs of a rule taken from the places where a parse called it: the texts
// that calls matched, and the input from the start of a call that matched, and of one that failed,
// to some way past the input the call examined.
type calledAt struct{ texts, matched, failed []string }

// equivContext is how far past the input a call examined the input taken from its place goes, so
// that a view that examines more, or less, than the grammar sees the input that follows.
const equivContext = 32

// harvest parses in from the rule start and adds to found, for each rule that the parse calls, the
// text that a call matched and the input from where a call starts to equivContext bytes past the
// input it examined, up to per distinct texts and per distinct inputs of calls that matched and of
// calls that failed. A call that fails shows the rule an input that its context gives it and that it
// rejects, which is where an alternative, a lookahead or a label that a law rewrites decides; a text
// that a call matched ends where derived can write what may follow it.
func harvest(prog *engine.Program, start, in string, per int, found map[string]*calledAt) {
	opts := engine.ParseOptions{Recognize: true, Unit: input.Bytes, Limits: runtime.Limits{Compute: 50_000_000},
		Trace: func(e instrument.TraceEvent) {
			if e.Kind != instrument.TraceExit {
				return
			}
			f := found[e.Rule]
			if f == nil {
				f = &calledAt{}
				found[e.Rule] = f
			}
			texts := &f.failed
			if e.Matched {
				texts = &f.matched
				if text := in[e.Pos:e.End]; text != "" && len(f.texts) < per && !slices.Contains(f.texts, text) {
					f.texts = append(f.texts, text)
				}
			}
			if len(*texts) >= per {
				return
			}
			end := min(len(in), max(e.End, e.Examined)+equivContext)
			for end < len(in) && !utf8.RuneStart(in[end]) {
				end++
			}
			if text := in[e.Pos:end]; !slices.Contains(*texts, text) {
				*texts = append(*texts, text)
			}
		}}
	_, _ = prog.ParseWith(start, in, opts) // a parse that fails still gives the calls that it made
}

// derived returns the inputs of a rule made from ins, its inputs taken from parses or generated:
// each input, changes of it (changed), and the input twice, which repeats what a repetition
// matches; and each literal that the rule writes (lits) before and after each of the shortest
// prefixed inputs, which shows a rule a token where its inputs have another, or after what it
// matched, as a lookahead or a choice that a law rewrites would see it.
func derived(rng *rand.Rand, ins, lits []string, tier equivTier) []string {
	var out []string
	for _, in := range ins {
		out = append(out, in)
		if in == "" {
			continue
		}
		for range tier.changes {
			out = append(out, changed(rng, in))
		}
		out = append(out, in+in)
	}
	short := slices.Clone(ins)
	slices.SortStableFunc(short, func(a, b string) int { return len(a) - len(b) })
	short = slices.Compact(short)
	for _, in := range short[:min(len(short), tier.prefixed)] {
		for _, l := range lits {
			out = append(out, l+in, in+l)
		}
	}
	return out
}

// distinct returns ins without the inputs that occur earlier in it.
func distinct(ins []string) []string {
	seen := map[string]bool{}
	return slices.DeleteFunc(ins, func(in string) bool {
		dup := seen[in]
		seen[in] = true
		return dup
	})
}

// countFound returns how many of rules have inputs from calls that matched in found.
func countFound(rules []string, found map[string]*calledAt) int {
	n := 0
	for _, r := range rules {
		if f := found[r]; f != nil && len(f.matched) > 0 {
			n++
		}
	}
	return n
}

// literalInputs returns inputs made of the literals of the rule rd, in the order it writes them:
// each literal alone, which an alternative that begins with it reads, and twice, which a repetition
// of it reads, and the first literals one after another, which a sequence of them reads. They reach the rewrites of a rule that the
// generated inputs do not, as in a rule that never matches, for which package sample generates
// nothing.
func literalInputs(rd *ast.RuleDef) []string {
	var lits []string
	astinspect.WalkExpr(rd.Expr, func(e ast.Expr) {
		if l, ok := e.(*ast.Literal); ok && l.Value != "" {
			lits = append(lits, l.Value)
		}
	})
	var out []string
	for i, l := range lits[:min(len(lits), 32)] {
		out = append(out, l, l+l)
		if i > 0 && i < 8 {
			out = append(out, strings.Join(lits[:i+1], ""))
		}
	}
	return out
}

// literals returns the distinct literals that rd writes, up to equivLiterals of them.
func literals(rd *ast.RuleDef) []string {
	var lits []string
	astinspect.WalkExpr(rd.Expr, func(e ast.Expr) {
		if l, ok := e.(*ast.Literal); ok && l.Value != "" && len(lits) < equivLiterals && !slices.Contains(lits, l.Value) {
			lits = append(lits, l.Value)
		}
	})
	return lits
}

// equivLiterals bounds the literals of a rule that derived writes before its inputs.
const equivLiterals = 16

// check compares the view of the subject with opts with the subject. It skips a view that rewrites
// nothing, which is the grammar itself.
func (c *equivCheck) check(t *testing.T, opts complexity.Options) {
	vg, view, err := complexity.NormalView(c.g, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(equivLaws, func(l complexity.Law) bool { return view.Applied[l] > 0 }) {
		t.Skip("the view rewrites nothing")
	}
	mode := withActions
	vprog, err := entry.Compile(entered(vg), entry.Options{})
	if err != nil {
		mode = withDefaults
		if vprog, err = entry.Compile(entered(vg), entry.Options{Compile: check.Options{DefaultValue: true}}); err != nil {
			// The grammar compiles with default values, so the view has lost a capture that a
			// predicate reads, or made one that a predicate cannot: a difference in what ≡E keeps.
			t.Fatalf("%s: the view does not compile with default values: %v", equivDifference, err)
		}
	}
	prog := c.progs[mode]
	runs, limited, diffs := 0, 0, 0
	for _, r := range c.rules {
		for _, in := range c.inputs[r] {
			want, werr := c.observed(mode, prog, r, in)
			got, gerr := observe(vprog, r, in)
			if errors.Is(werr, errEquivLimit) || errors.Is(gerr, errEquivLimit) {
				limited++
				continue
			}
			if werr != nil || gerr != nil {
				t.Fatalf("rule %s on %q: the grammar: %v; the view: %v", r, in, werr, gerr)
			}
			runs++
			if got != want {
				diffs++
				if diffs <= 5 {
					t.Errorf("%s: rule %s on %.80q: the view %v, the grammar %v", equivDifference, r, in, got, want)
				}
			}
		}
	}
	if diffs > 5 {
		t.Errorf("%s: %d more differences", equivDifference, diffs-5)
	}
	if runs == 0 {
		t.Fatalf("no input compared (%d over the compute limit)", limited)
	}
	names := []string{"actions", "default values"}
	t.Logf("rewrites %v, compared with %s: %d calls, %d over the compute limit", view.Applied, names[mode], runs, limited)
}

// entered returns a copy of g in which every rule that is not hidden is a start rule, so that a
// test can enter it, and the start rule that g marks, or else its first rule, is the default one.
// Only the declarations change, not the rules: the views are built from g itself, since they treat
// start rules apart (N13 inlines no call of one). A view is in the compiler's normalized form, so
// the copy is in the form the syntax writes (compiletest.Written), which the compiler takes.
func entered(g *ast.Grammar) *ast.Grammar {
	g = compiletest.Written(g)
	out := *g
	out.File.Decls = slices.Clone(g.File.Decls)
	var marked []*ast.RuleDef
	hasDefault := false
	for _, d := range out.File.Decls {
		if rd, ok := d.(*ast.RuleDef); ok && desugar.StartMarked(rd) {
			marked = append(marked, rd)
			hasDefault = hasDefault || slices.ContainsFunc(rd.Attrs, isDefaultStart)
		}
	}
	for i, d := range out.File.Decls {
		rd, ok := d.(*ast.RuleDef)
		if !ok || desugar.HiddenRule(rd.Name) {
			continue
		}
		start := &ast.Attribute{Name: "start"}
		switch {
		case desugar.StartMarked(rd) && (hasDefault || len(marked) > 1):
			continue
		case desugar.StartMarked(rd), len(marked) == 0 && !hasDefault:
			// The only start rule, or the first rule of a grammar without one, is the default.
			start.Args = []any{&ast.Ref{Name: "default"}}
			hasDefault = true
		}
		cp := *rd
		cp.Attrs = append(slices.DeleteFunc(slices.Clone(rd.Attrs), isStart), start)
		out.File.Decls[i] = &cp
	}
	return &out
}

func isStart(a *ast.Attribute) bool        { return !a.Pragma && a.Name == "start" }
func isDefaultStart(a *ast.Attribute) bool { return isStart(a) && len(a.Args) == 1 }

// equivObservation is what ≡E keeps of a call of a rule: whether it matches and where it ends.
type equivObservation struct {
	matched bool
	end     int
}

func (o equivObservation) String() string {
	if !o.matched {
		return "fails"
	}
	return fmt.Sprintf("matches to %d", o.end)
}

var errEquivLimit = errors.New("over the compute limit")

// equivDifference begins the report of a difference between a view and its grammar.
const equivDifference = "the view differs from the grammar"

// observe returns the observation of the call of rule start that a parse of in from it makes: the
// call at depth 1, whatever the parse does after it, such as reporting the input that is left.
func observe(prog *engine.Program, start, in string) (equivObservation, error) {
	var o equivObservation
	found := false
	opts := engine.ParseOptions{Recognize: true,
		TraceSelection: &instrument.TraceSelection{MaxDepth: 1},
		Trace: func(e instrument.TraceEvent) {
			if e.Kind == instrument.TraceExit && e.Depth == 1 {
				o, found = equivObservation{e.Matched, e.End}, true
			}
		}}
	// The reference build has no budget controls, so there a parse runs without a compute limit.
	if state.BudgetControlsEnabled {
		opts.Limits = runtime.Limits{Compute: 50_000_000}
	}
	_, err := prog.ParseWith(start, in, opts)
	if le := (*runtime.LimitError)(nil); errors.As(err, &le) {
		return o, errEquivLimit
	}
	if !found {
		return o, fmt.Errorf("the parse made no call of %s: %v", start, err)
	}
	return o, nil
}

// changed returns in cut short, or with one character deleted, inserted (a character of in) or
// swapped with the next.
func changed(rng *rand.Rand, in string) string {
	rs := []rune(in)
	if len(rs) == 0 {
		return in
	}
	i := rng.IntN(len(rs))
	switch rng.IntN(4) {
	case 0:
		return string(rs[:i])
	case 1:
		return string(rs[:i]) + string(rs[i+1:])
	case 2:
		return string(rs[:i]) + string(rs[rng.IntN(len(rs))]) + string(rs[i:])
	default:
		if i+1 < len(rs) {
			rs[i], rs[i+1] = rs[i+1], rs[i]
		}
		return string(rs)
	}
}
