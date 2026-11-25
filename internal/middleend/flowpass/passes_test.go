package flowpass

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/pass"
)

func class(lo, hi rune) *core.ClassNode {
	return &core.ClassNode{Class: &core.Class{Ranges: []core.Range{{Lo: lo, Hi: hi}}}}
}

func lit(s string) *core.Lit { return &core.Lit{Text: s} }

func seq(items ...core.Node) *core.Seq { return &core.Seq{Items: items} }

func choice(bodies ...core.Node) *core.Choice {
	c := &core.Choice{}
	for _, b := range bodies {
		c.Alts = append(c.Alts, &core.Alt{Body: b})
	}
	return c
}

// keywords returns a choice of MinTerminalLeaves literals, a terminal region.
func keywords() *core.Choice {
	var alts []core.Node
	for i := range corepass.MinTerminalLeaves {
		alts = append(alts, lit(fmt.Sprintf("k%02d", i)))
	}
	return choice(alts...)
}

// forms returns a value-free rule with every form that a pass replaces, after the Core passes, and its
// module: a terminal region (a negative lookahead of keywords; any character after it is no terminal
// form, so the region ends there), a choice whose first alternative begins with a guarded class
// followed by two more, a class of its own, and literals of one code point.
func forms(t *testing.T) (*core.Module, *core.Rule) {
	t.Helper()
	body := seq(
		&core.Look{Body: keywords(), Neg: true},
		&core.Any{},
		choice(seq(class('a', 'z'), class('0', '9'), class('x', 'y'), lit("q")), seq(lit("r"), class('a', 'z'), class('0', '9'))),
		class('m', 'n'),
	)
	m := &core.Module{Exps: []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}}
	core.Walk(body, func(n core.Node) {
		switch n := n.(type) {
		case *core.Lit:
			n.Exp = core.ExpID(len(m.Exps))
			m.Exps = append(m.Exps, `"`+n.Text+`"`)
		case *core.ClassNode:
			n.Class.Exp = core.ExpID(len(m.Exps))
			m.Exps = append(m.Exps, "class")
			m.Classes = append(m.Classes, n.Class)
		}
	})
	r := &core.Rule{ID: 0, Name: "r", Body: body, Policy: core.Policy{Seen: -1}}
	m.Rules = []*core.Rule{r}
	core.CallGraph(m)
	core.Analyze(m)
	m, _, err := corepass.Run(m, corepass.RunOptions{VerifyEach: true})
	if err != nil {
		t.Fatal(err)
	}
	return m, m.Rules[0]
}

// all is a target with every form.
var all = Target{Terminals: true, ClassRuns: true, ClassTables: true, GuardedClasses: true, InlineLiterals: true}

// values gives class-tables one range, so that every class is a candidate, and inline-literals its
// default.
func values(name string) int {
	switch name {
	case PassClassTables:
		return 1
	case PassInlineLiterals:
		return 4
	}
	return 0
}

// lowered is a function lowered from the forms rule, the passes that ran on it, and its listing
// after Finish.
type lowered struct {
	ran  []string
	text string
	m    *code.Module
}

// testEnv is what a test runs the passes under: the target, the observation of the parser, the
// flags that are on (all when Flag is nil) and the numbers of the passes that take one (values when
// Value is nil).
type testEnv struct {
	Target  Target
	Observe bool
	Flag    func(name string) bool
	Value   func(name string) int
	// Canonicalize runs canonicalize after the passes of forms, which the tests of the forms leave
	// out, so that a function shows what the passes of forms made.
	Canonicalize bool
}

// options returns the options of a run under e that verifies after each pass.
func (e *testEnv) options(t *testing.T) RunOptions {
	t.Helper()
	value := e.Value
	if value == nil {
		value = values
	}
	var s pass.Settings
	set := func(name string, v int) {
		var err error
		if s, err = s.With(name, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range append(Names(), corepass.PassGuards) {
		switch {
		case name == codepass.PassCanonicalize && !e.Canonicalize:
			set(name, 0)
		case e.Flag != nil && !e.Flag(name):
			set(name, 0)
		case name == PassClassTables || name == PassInlineLiterals:
			set(name, value(name))
		}
	}
	return RunOptions{Settings: s, Observe: e.Observe, Target: e.Target, VerifyEach: true}
}

// lower lowers the forms rule with opts, runs the passes under env, verifying after each, and ends
// them.
func lower(t *testing.T, opts lowerdata.Options, env *testEnv) lowered {
	t.Helper()
	m, r := forms(t)
	opts.Guarded = flowplan.Guarded(m, opts)
	f, err := flowlower.Lower(m, r, "r", opts)
	if err != nil {
		t.Fatal(err)
	}
	f, ran, err := Run(f, env.options(t))
	if err != nil {
		t.Fatal(err)
	}
	finishModule(f, nil)
	if err := flow.VerifyModule(f); err != nil {
		t.Fatalf("after Finish: %v\n%s", err, code.FuncStringIn(f, flow.FuncOf(f)))
	}
	return lowered{ran, code.FuncStringIn(f, flow.FuncOf(f)), f}
}

// marks are the operations by which each pass shows in a function, with the guarded classes shown by
// an advance by a variable the pass made.
var marks = map[string]func(l lowered) bool{
	PassTerminals:      func(l lowered) bool { return strings.Contains(l.text, string(flow.OpSkipTerminal)) },
	PassClassRuns:      func(l lowered) bool { return strings.Contains(l.text, string(flow.OpSkipClasses)) },
	PassClassTables:    func(l lowered) bool { return strings.Contains(l.text, string(flow.OpMatchClass)) },
	PassInlineLiterals: func(l lowered) bool { return strings.Contains(l.text, string(flow.OpHasLit)) },
	PassGuardedClasses: func(l lowered) bool { return guardedAdvance(l.m) },
}

// guardedAdvance reports whether the function of m advances by a variable other than size: the size of
// a guard's character that the class it guards consumes.
func guardedAdvance(m *code.Module) bool {
	found := false
	code.Walk(flow.FuncOf(m).Body, func(s code.Stmt) {
		if d, ok := s.(*code.Do); ok && d.Call.Callee == flow.OpAdvance && len(d.Call.Args) == 1 {
			if v, ok := d.Call.Args[0].(*code.Local); ok && v.Name != "size" {
				found = true
			}
		}
	}, nil)
	return found
}

// shows returns the passes whose marks the function has, in the order of the pipeline.
func shows(l lowered) []string {
	var out []string
	for _, name := range FormNames() {
		if marks[name](l) {
			out = append(out, name)
		}
	}
	return out
}

// Every pass changes the ordinary function of a target with every form, and a flag that is off
// removes exactly its pass and its mark.
func TestPassesDisable(t *testing.T) {
	names := FormNames()
	full := lower(t, lowerdata.Options{}, &testEnv{Target: all})
	if !slices.Equal(full.ran, names) || !slices.Equal(shows(full), names) {
		t.Fatalf("ran %v, shows %v, want %v:\n%s", full.ran, shows(full), names, full.text)
	}
	for _, off := range names {
		l := lower(t, lowerdata.Options{}, &testEnv{Target: all, Flag: func(name string) bool { return name != off }})
		want := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == off })
		if !slices.Equal(l.ran, want) {
			t.Errorf("without %s: ran %v, want %v", off, l.ran, want)
		}
		if marks[off](l) {
			t.Errorf("without %s: its mark is still in\n%s", off, l.text)
		}
		if l.text == full.text {
			t.Errorf("without %s: the same function", off)
		}
	}
	// A pass that takes a number is off at 0 too.
	for _, name := range []string{PassClassTables, PassInlineLiterals} {
		l := lower(t, lowerdata.Options{}, &testEnv{Target: all, Value: func(n string) int {
			if n == name {
				return 0
			}
			return values(n)
		}})
		if marks[name](l) {
			t.Errorf("%s at 0: its mark is still in\n%s", name, l.text)
		}
	}
	// guarded-classes needs the guards: without the Core pass it does not run.
	l := lower(t, lowerdata.Options{}, &testEnv{Target: all, Flag: func(name string) bool { return name != corepass.PassGuards }})
	if slices.Contains(l.ran, PassGuardedClasses) {
		t.Errorf("guarded-classes ran without guards")
	}
}

// A pass applies only to the targets that have its form, and none to the empty target, which keeps
// the function as the lowering wrote it.
func TestPassesTarget(t *testing.T) {
	plain := lower(t, lowerdata.Options{}, &testEnv{})
	if len(plain.ran) != 0 || len(shows(plain)) != 0 {
		t.Fatalf("empty target: ran %v, shows %v", plain.ran, shows(plain))
	}
	only := map[string]Target{
		PassTerminals:      {Terminals: true},
		PassGuardedClasses: {GuardedClasses: true},
		PassClassRuns:      {ClassRuns: true},
		PassClassTables:    {ClassTables: true},
		PassInlineLiterals: {InlineLiterals: true},
	}
	for name, target := range only {
		l := lower(t, lowerdata.Options{}, &testEnv{Target: target})
		if !slices.Equal(l.ran, []string{name}) || !slices.Equal(shows(l), []string{name}) {
			t.Errorf("target of %s: ran %v, shows %v", name, l.ran, shows(l))
		}
	}
}

// The passes respect the function: a parse that reports its rules has no terminal programs, a
// function lowered for Documents records every read and keeps its classes, and a traced copy reads
// every literal through the operation that records it.
func TestPassesFunction(t *testing.T) {
	for _, c := range []struct {
		name string
		opts lowerdata.Options
		env  testEnv
		want []string
	}{
		{"observed", lowerdata.Options{Observe: true}, testEnv{Observe: true, Target: all},
			[]string{PassGuardedClasses, PassClassRuns, PassClassTables, PassInlineLiterals}},
		{"document", lowerdata.Options{Document: true}, testEnv{Target: all}, []string{PassInlineLiterals}},
		{"traced", lowerdata.Options{Traced: true, Observe: true}, testEnv{Observe: true, Target: all}, nil},
		{"node", lowerdata.Options{Node: true}, testEnv{Target: all}, []string{PassGuardedClasses, PassInlineLiterals}},
	} {
		env := c.env
		l := lower(t, c.opts, &env)
		if got := shows(l); !slices.Equal(got, c.want) {
			t.Errorf("%s: shows %v, want %v:\n%s", c.name, got, c.want, l.text)
		}
	}
}

// TestClassRunsGuarded checks the classes that class-runs joins: a class that consumes a guard's
// character is none of them, so the guarded alternative's run begins at its second class, and the
// other alternative's run is its two classes after the literal.
func TestClassRunsGuarded(t *testing.T) {
	l := lower(t, lowerdata.Options{}, &testEnv{Target: Target{ClassRuns: true, GuardedClasses: true}})
	var runs []string
	code.Walk(flow.FuncOf(l.m).Body, func(s code.Stmt) {
		if st, ok := s.(*code.If); ok {
			if n, ok := st.Cond.(*code.Unary); ok {
				if c, ok := n.X.(*code.Call); ok && c.Callee == flow.OpSkipClasses {
					runs = append(runs, fmt.Sprint(len(c.Imm.(prim.ClassRun))))
				}
			}
		}
	}, nil)
	if !slices.Equal(runs, []string{"2", "2"}) || !guardedAdvance(l.m) {
		t.Errorf("runs %v, guarded %v, want two runs of two and a guarded class:\n%s", runs, guardedAdvance(l.m), l.text)
	}
}

// Finish leaves no block of a form, no variable that only a replaced form named, and the
// variables a pass made among the others; it does nothing to a function without forms.
func TestFinish(t *testing.T) {
	l := lower(t, lowerdata.Options{}, &testEnv{Target: all})
	if strings.Contains(l.text, "form") {
		t.Errorf("a form block is left:\n%s", l.text)
	}
	fn := flow.FuncOf(l.m)
	a := flow.AttrsOf(fn)
	if a.Forms != nil {
		t.Error("forms after Finish")
	}
	named := flow.NamedVars(fn.Body)
	for _, v := range a.Vars {
		if !named[v] && v != "v" && v != "c" {
			t.Errorf("variable %s is declared and named nowhere:\n%s", v, l.text)
		}
	}
	if len(fn.Locals) < len(a.Vars) {
		t.Errorf("locals %v, vars %v", fn.Locals, a.Vars)
	}
	for i, v := range a.Vars {
		if fn.Locals[i].Name != v {
			t.Errorf("local %d is %s, var %s", i, fn.Locals[i].Name, v)
		}
	}
	// A second Finish, and one on a function without forms, change nothing.
	before := code.FuncStringIn(l.m, fn)
	finishModule(l.m, nil)
	if after := code.FuncStringIn(l.m, fn); after != before {
		t.Errorf("a second Finish changed the function:\n%s", after)
	}
}

// Canonicalize runs after the passes of forms and keeps what they made.
func TestCanonicalizeRunsLast(t *testing.T) {
	base := lower(t, lowerdata.Options{}, &testEnv{Target: all})
	l := lower(t, lowerdata.Options{}, &testEnv{Target: all, Canonicalize: true})
	if want := append(FormNames(), codepass.PassCanonicalize); !slices.Equal(l.ran, want) {
		t.Errorf("ran %v, want %v", l.ran, want)
	}
	if !slices.Equal(shows(l), shows(base)) {
		t.Errorf("canonicalize changed the forms: %v, want %v", shows(l), shows(base))
	}
	plain := lower(t, lowerdata.Options{}, &testEnv{Canonicalize: true})
	if !slices.Equal(plain.ran, []string{codepass.PassCanonicalize}) {
		t.Errorf("empty target: ran %v", plain.ran)
	}
}

// TestGuardsFlagNamesTheCorePass checks that the flag that guarded-classes reads is that of the Core
// pass guards, registered under that name.
func TestGuardsFlagNamesTheCorePass(t *testing.T) {
	if guardsFlag != corepass.PassGuards {
		t.Errorf("guarded-classes reads the flag %q, the Core pass guards is %q", guardsFlag, corepass.PassGuards)
	}
	if _, ok := pass.Lookup(guardsFlag); !ok {
		t.Errorf("no flag %q is registered", guardsFlag)
	}
}
