package engine

import (
	"reflect"
	"testing"

	"zeroparse.dev/zp/core"
	"zeroparse.dev/zp/internal/codegentest"
	"zeroparse.dev/zp/internal/compile"
	"zeroparse.dev/zp/internal/engine/state"
)

// lazyOverride gives, for an option the build sets itself, the value of the lazily built program:
// nil keeps the program's own.
type lazyOverride struct{ controlled, recognizer, insertion *bool }

// lazyOverrides are the options that a lazily built program sets itself, by the field of
// compile.Options. Every other option must be the program's own.
var lazyOverrides = map[string]lazyOverride{
	"Recognize":        {recognizer: ptr(true)},
	"DropStreamValues": {recognizer: ptr(true)},
}

func ptr[T any](v T) *T { return &v }

// TestLazyProgramsCarryEveryOption compiles a program with each compile option on in turn, builds
// its controlled closure graph, its recognizer and its program of missing-token insertion, and
// checks that each option reaches all three, except those that the build sets itself. A new option
// fails the test until it is carried or listed in lazyOverrides.
func TestLazyProgramsCarryEveryOption(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	typ := reflect.TypeFor[compile.Options]()
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if typ.Field(i).Type.Kind() != reflect.Bool {
			t.Fatalf("option %s is not a bool: the test must say how to set it", name)
		}
		t.Run(name, func(t *testing.T) {
			var o compile.Options
			reflect.ValueOf(&o).Elem().FieldByName(name).SetBool(true)
			o.Recognize = o.Recognize || o.DropStreamValues // DropStreamValues needs Recognize
			o.LeftRecursionBindings = true                  // the grammar below needs it
			prog := mustCompile(t, codegentest.LRBindingSource, o)
			if _, err := prog.ParseWith("main", "x", ParseOptions{Backend: Closure, Limits: core.Limits{Compute: 1 << 30}}); err != nil {
				t.Fatal(err)
			}
			rec, err := prog.Recognizer()
			if err != nil {
				t.Fatal(err)
			}
			if prog.budgetProgram == nil {
				t.Fatal("the controlled graph was not built")
			}
			// The insertion program is built from a grammar that has an insertion.
			withIns := mustCompile(t, insertionCycle, o)
			withIns.ParseWith("main", "yx", ParseOptions{MissingTokens: 1000}) // only builds the program
			if withIns.ins == nil {
				t.Fatal("the insertion program was not built")
			}
			for _, c := range []struct {
				what      string
				from, got compile.Options
				pick      func(lazyOverride) *bool
			}{
				{"controlled graph", prog.Opts, prog.budgetProgram.Opts, func(s lazyOverride) *bool { return s.controlled }},
				{"recognizer", prog.Opts, rec.Opts, func(s lazyOverride) *bool { return s.recognizer }},
				{"insertion program", withIns.Opts, withIns.ins.Opts, func(s lazyOverride) *bool { return s.insertion }},
			} {
				got, want := reflect.ValueOf(c.got), reflect.ValueOf(c.from)
				for j := 0; j < typ.NumField(); j++ {
					f := typ.Field(j).Name
					expected := want.Field(j).Bool()
					if ov, ok := lazyOverrides[f]; ok && c.pick(ov) != nil {
						expected = *c.pick(ov)
					}
					if got.Field(j).Bool() != expected {
						t.Errorf("%s: option %s is %v, want %v (the program's is %v)", c.what, f, got.Field(j).Bool(), expected, want.Field(j).Bool())
					}
				}
			}
		})
	}
	for f := range lazyOverrides {
		if _, ok := typ.FieldByName(f); !ok {
			t.Errorf("lazyOverrides names %s, which is not an option", f)
		}
	}
}

// TestLeftRecursionBindingsOnTheLazyClosureGraph parses a left-recursive rule that a binding
// re-enters with other variable values, which compiles only with LeftRecursionBindings, on every
// backend, with the default budget (the controlled closure graph) and through the recognizer. The
// results agree with the bytecode backends and with the program's own closure graph.
func TestLeftRecursionBindingsOnTheLazyClosureGraph(t *testing.T) {
	prog := mustCompile(t, codegentest.LRBindingSource, compile.Options{LeftRecursionBindings: true})
	rec, err := prog.Recognizer()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range codegentest.LRBindingInputs {
		want := codegentest.ResultJSON(prog.ParseWith("main", in, ParseOptions{Backend: Bytecode}))
		for _, b := range []Backend{Closure, BytecodeIterative} {
			if got := codegentest.ResultJSON(prog.ParseWith("main", in, ParseOptions{Backend: b})); got != want {
				t.Errorf("%s: input %q: %s, want %s", b, in, got, want)
			}
		}
		_, werr := prog.ParseWith("main", in, ParseOptions{Backend: Bytecode})
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			_, err := rec.ParseWith("main", in, ParseOptions{Backend: b})
			if (err == nil) != (werr == nil) {
				t.Errorf("recognizer, %s: input %q: error %v, the parse's is %v", b, in, err, werr)
			}
		}
	}
}

// TestDropStreamValuesNeedsRecognize checks that DropStreamValues, which only has a meaning with
// Recognize, is rejected without it instead of being ignored.
func TestDropStreamValuesNeedsRecognize(t *testing.T) {
	g := mustParse(`rule main = "x"`)
	if _, err := Compile(g, compile.Options{DropStreamValues: true}); err == nil {
		t.Fatal("DropStreamValues without Recognize was accepted")
	}
	if _, err := Compile(g, compile.Options{Recognize: true, DropStreamValues: true}); err != nil {
		t.Fatal(err)
	}
}
