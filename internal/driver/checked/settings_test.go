package checked

import (
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen"
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
