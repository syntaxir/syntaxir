package checked

import (
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/internal/testing/statetest"
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
