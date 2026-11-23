package checked

import (
	"context"
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/parser"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A program holds the settings it was compiled with, and the recognizer and the insertion program
// that it derives hold them too, so the three programs of a grammar build under the parser's
// settings.
func TestDerivedProgramsHoldTheSettings(t *testing.T) {
	t.Parallel()
	settings, err := pass.Settings{}.With(codegen.MethodSharing, 0)
	if err != nil {
		t.Fatal(err)
	}
	g, err := parser.Parse(statetest.InsertionCycle)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Compile(starttest.MarkAll(g), Options{Settings: settings}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Settings(), settings) {
		t.Fatalf("the program holds the settings %+v, want %+v", s.Settings(), settings)
	}
	rec, err := s.Recognizer()
	if err != nil {
		t.Fatal(err)
	}
	ins, err := s.Insertion()
	if err != nil {
		t.Fatal(err)
	}
	for what, d := range map[string]any{"recognizer": rec, "insertion program": ins} {
		if got := d.(*Source).Settings(); !reflect.DeepEqual(got, settings) {
			t.Errorf("the %s holds the settings %+v, want the program's %+v", what, got, settings)
		}
	}
}

// A source wraps its program for the driver once, as the program of its role with the options of
// the grammar's parser: the parser itself, and the recognizer and the insertion program derived from
// it.
func TestProgramsCarryTheirRole(t *testing.T) {
	t.Parallel()
	g, err := parser.Parse(statetest.InsertionCycle)
	if err != nil {
		t.Fatal(err)
	}
	opts := check.Options{SourceInfo: true}
	s, err := Compile(starttest.MarkAll(g), Options{Compile: opts}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Program() != s.Program() {
		t.Error("the program is wrapped again on the second call")
	}
	if p := s.Program(); p.View().Run != s.Runtime() {
		t.Error("the program wraps another checked program")
	}
	rec, err := s.Recognizer()
	if err != nil {
		t.Fatal(err)
	}
	ins, err := s.Insertion()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		src  *Source
		role driver.Role
	}{{s, driver.Parser}, {rec.(*Source), driver.Recognizer}, {ins.(*Source), driver.Insertion}} {
		p := c.src.Program()
		if p.Role() != c.role || p.ParserOptions() != opts || p.Start() != nil {
			t.Errorf("%s: role %s, parser options %+v, start %v; want %s, %+v and no start rule", c.role, p.Role(), p.ParserOptions(), p.Start(), c.role, opts)
		}
	}
	if got := rec.(*Source).Checked().Opts.Recognition; got != check.Recognize {
		t.Errorf("the recognizer is checked with recognition %v", got)
	}
}

// runTimeBuilds returns the disassembled bytecode of s, or "" for a program that has none, and its
// printed Core module of the closures.
func runTimeBuilds(t *testing.T, s *Source, bytecode bool) (string, string) {
	t.Helper()
	var module string
	if bytecode {
		m, err := s.Module()
		if err != nil {
			t.Fatal(err)
		}
		module = m.Disassemble()
	}
	core, err := s.Core(nil)
	if err != nil {
		t.Fatal(err)
	}
	return module, ir.Print(core.(*closure.Module).Core, true)
}

// The settings of a program reach both of its run-time builds: turning a Core pass off changes the
// bytecode and the Core module of the closures, which are the builds of the driver under those
// settings; the recognizer derived from the parser builds as the recognizer compiled directly with
// the same flags and settings, and the insertion program as the insertion program compiled
// directly, each also changed by the settings.
func TestSettingsReachTheRunTimeBuilds(t *testing.T) {
	t.Parallel()
	off, err := pass.Settings{}.Without(corepass.PassGuards)
	if err != nil {
		t.Fatal(err)
	}
	g, err := parser.Parse("@start\nrule main = (word / num / -\" \")* $$\nrule word = \"if\" / \"else\" / [a-z]+\nrule num = [0-9]+\n")
	if err != nil {
		t.Fatal(err)
	}
	ig, err := parser.Parse(statetest.InsertionCycle)
	if err != nil {
		t.Fatal(err)
	}
	ig = starttest.MarkAll(ig)
	source := func(g *ast.Grammar, o check.Options, s pass.Settings, flags []check.RuleFlags) *Source {
		src, err := Check(g, Options{Compile: o, Settings: s}, flags, nil)
		if err != nil {
			t.Fatal(err)
		}
		return src
	}
	derived := func(d state.Source, err error) *Source {
		if err != nil {
			t.Fatal(err)
		}
		return d.(*Source)
	}
	base, tuned := source(g, check.Options{}, pass.Settings{}, nil), source(g, check.Options{}, off, nil)
	ibase, ituned := source(ig, check.Options{}, pass.Settings{}, nil), source(ig, check.Options{}, off, nil)
	for _, c := range []struct {
		name        string
		base, tuned *Source
		direct      func() *Source
		bytecode    bool
	}{
		{"parser", base, tuned, func() *Source { return source(g, check.Options{}, off, nil) }, true},
		{"recognizer", derived(base.Recognizer()), derived(tuned.Recognizer()),
			func() *Source { return source(g, check.Options{Recognition: check.Recognize}, off, tuned.prog.Flags()) }, true},
		{"insertion program", derived(ibase.Insertion()), derived(ituned.Insertion()),
			func() *Source { return source(ig, check.Options{}, off, ituned.prog.InsertionFlags) }, false},
	} {
		baseModule, baseCore := runTimeBuilds(t, c.base, c.bytecode)
		module, core := runTimeBuilds(t, c.tuned, c.bytecode)
		if c.bytecode && module == baseModule {
			t.Errorf("%s: turning %s off leaves the bytecode as it is", c.name, corepass.PassGuards)
		}
		if core == baseCore {
			t.Errorf("%s: turning %s off leaves the Core module of the closures as it is", c.name, corepass.PassGuards)
		}
		outs, err := driver.Compile(context.Background(), c.tuned.Program(), driver.Options{Settings: off}, driver.Target{Backend: result.Closure})
		if err != nil {
			t.Fatal(err)
		}
		if got := ir.Print(outs[0].(*closure.Module).Core, true); got != core {
			t.Errorf("%s: the Core module of the closures is not the driver's build under the settings", c.name)
		}
		if c.bytecode {
			outs, err := driver.Compile(context.Background(), c.tuned.Program(), driver.Options{Settings: off}, driver.Target{Backend: result.VM})
			if err != nil {
				t.Fatal(err)
			}
			if got := outs[0].(*vmlower.Bytecode).Module.Disassemble(); got != module {
				t.Errorf("%s: the bytecode is not the driver's build under the settings", c.name)
			}
		}
		directModule, directCore := runTimeBuilds(t, c.direct(), c.bytecode)
		if directModule != module || directCore != core {
			t.Errorf("%s: the derived program builds otherwise than the program compiled directly", c.name)
		}
	}
}
