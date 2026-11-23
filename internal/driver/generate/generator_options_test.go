package generate_test

import (
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestGeneratorRecognizerCarriesEveryOption compiles a grammar for the generators with each
// compile option on in turn and checks that each derived program, built on first use, has the same
// options except Recognition, which the recognizers set. A new option fails the test until it is
// carried or listed here.
func TestGeneratorRecognizerCarriesEveryOption(t *testing.T) {
	g, err := compiletest.Parse(codegentest.LRBindingSource)
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeFor[check.Options]()
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		t.Run(name, func(t *testing.T) {
			var o check.Options
			f := reflect.ValueOf(&o).Elem().FieldByName(name)
			switch f.Kind() {
			case reflect.Bool:
				f.SetBool(true)
			case reflect.Uint8:
				f.SetUint(1)
			default:
				t.Fatalf("option %s is a %s: the test must say how to set it", name, f.Kind())
			}
			o.LeftRecursionBindings = true // the grammar needs it
			prog, derive, err := driver.CheckForGenerator(g, o)
			if err != nil {
				t.Fatal(err)
			}
			for _, role := range []driver.Role{driver.Recognizer, driver.Insertion, driver.InsertionRecognizer} {
				p, err := derive(role)
				if err != nil {
					t.Fatal(err)
				}
				got, want := reflect.ValueOf(p.Opts), reflect.ValueOf(prog.Opts)
				for j := 0; j < typ.NumField(); j++ {
					f := typ.Field(j).Name
					expected := want.Field(j).Interface()
					if f == "Recognition" && role != driver.Insertion {
						expected = check.Recognize
					}
					if got.Field(j).Interface() != expected {
						t.Errorf("%s: option %s is %v, want %v", role, f, got.Field(j).Interface(), expected)
					}
				}
			}
		})
	}
}
