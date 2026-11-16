package codegen

import (
	"reflect"
	"testing"

	"zeroparse.dev/zp/internal/codegentest"
	"zeroparse.dev/zp/internal/compile"
	"zeroparse.dev/zp/internal/compiletest"
)

// TestGeneratorRecognizerCarriesEveryOption compiles a grammar for the generators with each
// compile option on in turn and checks that the recognizer program, built on first use, has the
// same options except Recognize and DropStreamValues, which the recognizer sets. A new option fails the test until it
// is carried or listed here.
func TestGeneratorRecognizerCarriesEveryOption(t *testing.T) {
	g, err := compiletest.Parse(codegentest.LRBindingSource)
	if err != nil {
		t.Fatal(err)
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
			o.LeftRecursionBindings = true                  // the grammar needs it
			prog, recognizer, err := compileForGenerator(g, o)
			if err != nil {
				t.Fatal(err)
			}
			rec, err := recognizer()
			if err != nil {
				t.Fatal(err)
			}
			got, want := reflect.ValueOf(rec.Opts), reflect.ValueOf(prog.Opts)
			for j := 0; j < typ.NumField(); j++ {
				f := typ.Field(j).Name
				expected := want.Field(j).Bool()
				if f == "Recognize" || f == "DropStreamValues" {
					expected = true
				}
				if got.Field(j).Bool() != expected {
					t.Errorf("recognizer: option %s is %v, want %v", f, got.Field(j).Bool(), expected)
				}
			}
		})
	}
}
