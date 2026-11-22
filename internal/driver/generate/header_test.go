package generate

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// TestHeaderWritesEveryGenerationOption is the reflection test of the generation options: each
// exported field of common.GenOptions, and of the structs inside it such as common.Debug, is
// written by exactly one header word or is omitted, and only the fields without a text form are
// omitted.
func TestHeaderWritesEveryGenerationOption(t *testing.T) {
	if err := Header.Check(); err != nil {
		t.Fatal(err)
	}
	for _, n := range Header.Omit {
		ft := reflect.TypeFor[common.GenOptions]()
		for _, name := range strings.Split(n, ".") {
			f, ok := ft.FieldByName(name)
			if !ok {
				t.Fatalf("%s is omitted, but it is no field", n)
			}
			ft = f.Type
		}
		if k := ft.Kind(); k != reflect.Interface {
			t.Errorf("%s is omitted, but its type %v could be written", n, ft)
		}
	}
}

// TestHeaderCoversTheCheckOptions is the reflection test of the options of the check: every field
// of common.CompileOptions, which the check of the grammar reads, is written by the options word,
// which every target shares.
func TestHeaderCoversTheCheckOptions(t *testing.T) {
	var options []string
	for _, w := range Header.Words {
		if w.Word == "options" {
			for _, f := range w.Fields {
				options = append(options, f.Field)
			}
		}
	}
	for _, f := range reflect.VisibleFields(reflect.TypeFor[common.CompileOptions]()) {
		if f.IsExported() && !slices.Contains(options, f.Name) {
			t.Errorf("the check option %s is not written by the options header", f.Name)
		}
	}
}

// TestHeaderRoundTrip checks that every written generation option reads back equal through a
// pipeline text: each field, the fields of the structs inside the options included, is set to a
// value other than its zero value, by reflection, so a new field is covered without a change of
// the test. A pointer reads back both set and nil.
func TestHeaderRoundTrip(t *testing.T) {
	var o common.GenOptions
	var pointers []reflect.Value
	var set func(v reflect.Value, prefix string)
	set = func(v reflect.Value, prefix string) {
		for _, f := range reflect.VisibleFields(v.Type()) {
			path := prefix + f.Name
			if slices.Contains(Header.Omit, path) {
				continue
			}
			fv := v.FieldByIndex(f.Index)
			if fv.Kind() == reflect.Pointer {
				pointers = append(pointers, fv)
				fv.Set(reflect.New(fv.Type().Elem()))
				fv = fv.Elem()
			}
			switch fv.Kind() {
			case reflect.Bool:
				fv.SetBool(true)
			case reflect.String:
				fv.SetString(f.Name + " a=b,c%")
			case reflect.Int:
				fv.SetInt(2) // LibOnly for Lib
			case reflect.Slice:
				fv.Set(reflect.ValueOf([]string{"x", "y,z"}))
			case reflect.Map:
				fv.Set(reflect.ValueOf(map[string]int{"p": 3, "q": 0}))
			case reflect.Struct:
				set(fv, path+".")
			default:
				t.Fatalf("the test cannot set %s of kind %v", path, fv.Kind())
			}
		}
	}
	set(reflect.ValueOf(&o).Elem(), "")
	if len(pointers) == 0 {
		t.Fatal("no option is a pointer: the nil case reads back nothing")
	}
	roundTrip := func(o common.GenOptions) string {
		var lines []pipe.Line
		for _, w := range Header.Words {
			label := ""
			if w.Word == "target" {
				label = "go"
			}
			lines = append(lines, Header.Line(w.Word, label, o))
		}
		text := (&pipe.Description{Version: pipe.Format, Header: lines, In: "program", Out: "output"}).String()
		d, err := pipe.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		var back common.GenOptions
		for _, l := range d.Header {
			if err := Header.Read(l, &back); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(back, o) {
			t.Errorf("read back\n%+v\nwant\n%+v", back, o)
		}
		return text
	}
	text := roundTrip(o)
	for _, want := range []string{" lib=only ", "settings profile=", " no-limits=true ", "debug verify-each=true opt-bisect-limit=2 "} {
		if !strings.Contains(text, want) {
			t.Errorf("the header text has no %q:\n%s", want, text)
		}
	}
	for _, p := range pointers {
		p.Set(reflect.Zero(p.Type()))
	}
	if text := roundTrip(o); strings.Contains(text, "opt-bisect-limit") {
		t.Errorf("a nil limit is written:\n%s", text)
	}
}

func TestHeaderRejectsAnUnknownLibraryMode(t *testing.T) {
	l := Header.Line("target", "go", common.GenOptions{})
	for i, f := range l.Fields {
		if f.Key == "lib" {
			l.Fields[i].Value = "vendored"
		}
	}
	var o common.GenOptions
	if err := Header.Read(l, &o); err == nil || !strings.Contains(err.Error(), `"vendored" is no library mode`) {
		t.Errorf("%v", err)
	}
}
