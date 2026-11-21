package pipe

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/pass"
)

func init() {
	pass.Register(pass.Flag{Name: "t-width", Kind: pass.KindCorePass, Doc: "A parameter of the tests of the text form.", Int: true, Default: 1})
}

// widen appends as many "~" to each function as its parameter t-width says.
var widen = modPass("t-widen", func(c *Ctx[*mod], m *mod) error {
	each(m, func(f *fn) { f.body += strings.Repeat("~", c.Value("t-width")) })
	return nil
}, func(d *Decl) { d.Test = true; d.Params = []string{"t-width"} })

// rich is a pipeline that uses every combinator: a fork, a constant and a derived feature, a split
// with a shared body, a slot with a body for one part, a per-function block and a pass that occurs
// twice on one path.
func rich(t *testing.T) Pipeline[*prog, []*mods] {
	t.Helper()
	table := Then3(Of(build),
		Slot(modSlot, Seq(modIR, upper, canon)),
		Then3(PerFunc(funcsView, Slot(fnSlot, Seq(fnIR, fnUpper))), Seq(modIR, mark, appnd), WithDerived(firstName, Seq(modIR, feat, appnd))))
	p := Fork(
		B("x", WithConst(targetFeature, "go dir", Each(tablesSplit, Shared(tableKey, table)))),
		B("y", Each(tablesSplit, Then(Of(build), Seq(modIR, mark)))))
	p, err := Apply(p, Fill(modSlot, "a", Seq(modIR, appnd, appnd)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// richText is the text of rich: the printer's canonical form.
const richText = `sxir-pipeline 1.0
sxir 0.9.0
target go module=example.com/a%20b print-after=
pipeline t-program -> []t-mods {
  fork {
    branch x {
      with t-target=go%20dir {
        each t-tables {
          shared t-key {
            t-build!
            slot t-mod-passes {
              t-upper
              t-canon[t-fold]
            } for a {
              t-append
              t-append#2
            }
            func t-funcs {
              slot t-fn-passes {
                t-fn-upper
              }
            }
            t-mark!
            t-append#3
            with t-first-name {
              t-feat
              t-append#4
            }
          }
        }
      }
    }
    branch y {
      each t-tables {
        t-build!
        t-mark!
      }
    }
  }
}
`

var richHeader = []Line{{Word: "sxir", Label: "0.9.0"},
	{Word: "target", Label: "go", Fields: []Field{{"module", "example.com/a b"}, {"print-after", ""}}}}

// TestTextRoundTrip checks the three properties of the text door: the printer writes the canonical
// text, parsing a text and printing it gives the same text, and resolving it gives a pipeline that
// prints the same text and runs as the typed pipeline runs: the same output, events and dumps.
func TestTextRoundTrip(t *testing.T) {
	p := rich(t)
	text := Describe(p, richHeader...).String()
	if text != richText {
		t.Fatalf("Describe:\n%s\nwant:\n%s", text, richText)
	}
	d, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.String(); got != text {
		t.Fatalf("Parse then String:\n%s\nwant:\n%s", got, text)
	}
	if v, _ := d.Header[1].Value("module"); v != "example.com/a b" || d.Header[0].Label != "0.9.0" {
		t.Errorf("headers read back as %+v", d.Header)
	}
	r, err := Resolve(d, p)
	if err != nil {
		t.Fatal(err)
	}
	if got := Describe(r, d.Header...).String(); got != text {
		t.Fatalf("Resolve then Describe:\n%s\nwant:\n%s", got, text)
	}
	runOf := func(q Pipeline[*prog, []*mods]) (string, []string, []string) {
		rec := &recorder{needs: Needs{Print: true}}
		out, err := run(t, &Run{Observers: []Observer{rec}}, q, &prog{tables: []string{"a", "b"}})
		if err != nil {
			t.Fatal(err)
		}
		var s strings.Builder
		for _, m := range out {
			s.WriteString(m.String())
		}
		return s.String(), rec.lines, rec.dumps
	}
	wo, we, wd := runOf(p)
	go_, ge, gd := runOf(r)
	if go_ != wo || strings.Join(ge, "\n") != strings.Join(we, "\n") || strings.Join(gd, "\n") != strings.Join(wd, "\n") {
		t.Errorf("the resolved pipeline runs otherwise:\noutput %q\nwant   %q\nevents\n%s\nwant\n%s", go_, wo, strings.Join(ge, "\n"), strings.Join(we, "\n"))
	}
	// Table a runs the part body of the slot, b its default; the derived feature hides the
	// constant; branch y runs only the required steps.
	if !strings.Contains(wo, "a1: X++#+@a1+") || !strings.Contains(wo, "b1: XF#+@b1+") || !strings.Contains(wo, "b1: x#") {
		t.Errorf("the pipeline under test did not run every part: %q", wo)
	}
}

// TestTextParameterOverride checks that a text sets the parameter of one instance, that the
// printer writes it, and that the resolver checks its flag and its value.
func TestTextParameterOverride(t *testing.T) {
	ref := Seq(modIR, widen, widen)
	text := "sxir-pipeline 1.0\npipeline t-mod -> t-mod {\n  t-widen(t-width=3)\n  t-widen#2\n}\n"
	d, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Resolve(d, ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := Describe(p).String(); got != text {
		t.Errorf("Describe:\n%s\nwant:\n%s", got, text)
	}
	out, err := run(t, &Run{}, p, &mod{fns: []*fn{{"f", "x"}}})
	if err != nil || out.String() != "f: x~~~~\n" {
		t.Errorf("the override and the setting: %q %v, want x~~~~", out, err)
	}
	for _, c := range []struct{ step, want string }{
		{"t-widen(t-width=-1)", "does not take the value -1"},
		{"t-widen(t-other=1)", "has no parameter t-other"},
		{"t-widen(t-width=1,t-width=2)", "given twice"},
	} {
		d, err := Parse("sxir-pipeline 1.0\npipeline t-mod -> t-mod {\n  " + c.step + "\n}\n")
		if err == nil {
			_, err = Resolve(d, ref)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", c.step, err, c.want)
		}
	}
}

// TestTextSharedKeyOfAnOverride checks that two uses of a shared body with different overrides do
// not share: the parameter is part of the effective key.
func TestTextSharedKeyOfAnOverride(t *testing.T) {
	ref := Fork(B("x", Shared(modKey, Seq(modIR, widen))), B("y", Shared(modKey, Seq(modIR, widen))))
	text := strings.Replace(Describe(ref).String(), "branch y {\n      shared t-mod-key {\n        t-widen", "branch y {\n      shared t-mod-key {\n        t-widen(t-width=2)", 1)
	d, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Resolve(d, ref)
	if err != nil {
		t.Fatal(err)
	}
	out, err := run(t, &Run{}, p, &mod{fns: []*fn{{"f", "x"}}})
	if err != nil || out[0].String() != "f: x~\n" || out[1].String() != "f: x~~\n" {
		t.Errorf("%v %v, want x~ and x~~", out, err)
	}
}

// TestResolveRejects checks every way a text can differ from its assembled pipeline that the
// resolver must refuse, each by an edit of the canonical text, before anything runs.
func TestResolveRejects(t *testing.T) {
	p := rich(t)
	for _, c := range []struct {
		name, old, new, want string
	}{
		{"missing required step", "            t-mark!\n", "", `where the pipeline has "t-mark;"`},
		{"extra required step", "            t-mark!\n", "            t-mark!\n            t-mark!\n", "the required steps of the text differ"},
		{"required step moved to another block", "            t-mark!\n            t-append#3\n", "            t-append#3\n", "the required steps of the text differ"},
		{"changed constant feature", "with t-target=go%20dir {", "with t-target=ts {", `"with t-target=ts{"`},
		{"required step marked optional", "        t-mark!\n      }\n    }\n  }", "        t-mark\n      }\n    }\n  }", "pass t-mark is required"},
		{"optional step marked required", "t-upper\n", "t-upper!\n", "pass t-upper is optional"},
		{"unknown pass", "t-upper\n", "t-uper\n", "no pass t-uper"},
		{"unknown split", "each t-tables {\n        t-build!", "each t-tablez {\n        t-build!", "no split t-tablez"},
		{"unknown view", "func t-funcs {", "func t-fncs {", "no view t-fncs"},
		{"unknown slot", "slot t-fn-passes {", "slot t-fn-pases {", "no slot t-fn-pases"},
		{"unknown key", "shared t-key {", "shared t-kee {", "no shared key t-kee"},
		{"unknown provider", "with t-first-name {", "with t-first-nam {", "no provider t-first-nam"},
		{"unknown feature", "with t-target=go%20dir {", "with t-targt=go {", "no feature t-targt"},
		{"kind mismatch", "              t-upper\n", "              t-fn-upper\n", "pass t-fn-upper takes t-fn, but it gets t-mod"},
		{"unprovided feature", "        t-mark!\n      }\n    }\n  }", "        t-mark!\n        t-feat\n      }\n    }\n  }", "reads the feature t-target, which no enclosing scope provides"},
		{"part label the split does not declare", "} for a {", "} for z {", "the part z, which the split does not give"},
		{"required step in a part body", "              t-append#2\n", "              t-append#2\n              t-mark!\n", "the body of slot t-mod-passes for a changes the required steps"},
		{"patterns that differ from the declaration", "t-canon[t-fold]", "t-canon", "lists the patterns [], but declares [t-fold]"},
		{"shared key on a block of another kind", "          shared t-key {\n            t-build!\n", "          t-build!\n          shared t-key {\n", "shared key t-key reads t-table, but its block starts on t-mod"},
		{"derived provider on a block of another kind", "          shared t-key {\n            t-build!\n", "          with t-first-name {\n            t-build!\n", "provider t-first-name reads t-mod, but its block starts on t-table"},
		{"a branch outside a fork", "    branch y {\n      each t-tables {\n        t-build!\n        t-mark!\n      }\n    }\n", "", "the required steps of the text differ"},
		{"other kinds", "pipeline t-program -> []t-mods {", "pipeline t-table -> []t-mods {", "the text takes t-table to []t-mods"},
	} {
		text := strings.Replace(richText, c.old, c.new, 1)
		if text == richText {
			t.Fatalf("%s: the edit does not apply", c.name)
		}
		d, err := Parse(text)
		if err == nil {
			_, err = Resolve(d, p)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", c.name, err, c.want)
		}
	}
}

// TestParseRejects checks the errors of the reader: the versions, a body for a part outside a
// slot, and a text that is not in the grammar.
func TestParseRejects(t *testing.T) {
	for _, c := range []struct {
		name, old, new, want string
	}{
		{"another major version", "sxir-pipeline 1.0", "sxir-pipeline 2.0", "this reader reads 1.0 to 1.0"},
		{"newer minor version", "sxir-pipeline 1.0", "sxir-pipeline 1.1", "version 1.1"},
		{"no version", "sxir-pipeline 1.0", "sxir-pipeline", "want sxir-pipeline <version>"},
		{"body for a part outside a slot", "      }\n    }\n    branch y {", "      } for a {\n      }\n    }\n    branch y {", "only a slot has bodies for parts"},
		{"unclosed block", "    }\n  }\n}\n", "    }\n  }\n", "a block is not closed"},
		{"pipeline closed by a part", "    }\n  }\n}\n", "    }\n  }\n} for a {\n}\n", "the pipeline block ends with"},
		{"text after the end", "  }\n}\n", "  }\n}\nt-upper\n", "text after the end"},
		{"unknown block", "fork {", "spoon {", "unknown block"},
		{"bad parameter", "t-upper\n", "t-upper(x)\n", "is not flag=integer"},
		{"bad header", "target go", "Target go", "is not a lower-case word"},
		{"bad escape", "a%20b", "a%zzb", "invalid escape"},
	} {
		text := strings.Replace(richText, c.old, c.new, 1)
		if text == richText {
			t.Fatalf("%s: the edit does not apply", c.name)
		}
		if _, err := Parse(text); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", c.name, err, c.want)
		}
	}
	d, err := Parse(richText)
	if err != nil || d.Version != Format {
		t.Errorf("the canonical text: %v", err)
	}
}

// TestFeatureWithoutTextForm checks that a feature without a parser prints but cannot be read
// back: only the typed door provides it.
func TestFeatureWithoutTextForm(t *testing.T) {
	k := NewFeature("t-opaque", func(func()) string { return "opaque" }, nil)
	ref := WithConst(k, func() {}, Seq(modIR))
	text := Describe(ref).String()
	if !strings.Contains(text, "with t-opaque=opaque {") {
		t.Fatalf("Describe:\n%s", text)
	}
	d, err := Parse(text)
	if err == nil {
		_, err = Resolve(d, ref)
	}
	if err == nil || !strings.Contains(err.Error(), "feature t-opaque has no text form") {
		t.Errorf("%v, want the feature named", err)
	}
}
