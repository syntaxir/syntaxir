package pipe

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/pass"
)

// canonFn is t-canon registered again at the kind of a function, with the declaration of t-canon:
// one pass, one flag and one pattern at two kinds.
var canonFn = New(Def[*fn, *fn]{Decl: canon.Decl(), In: fnIR, Out: fnIR,
	Run: func(c *Ctx[*fn], f *fn) (*fn, pass.PreservedSet, error) {
		if c.Allow("t-fold") {
			f.body += "g"
		}
		return f, pass.PreserveNone(), nil
	}})

// A pass registered at two kinds is one pass: its instances are numbered across both, its flag and
// its pattern's flag govern both, and its statistics have the declared patterns at each.
func TestPassAtTwoKinds(t *testing.T) {
	p := tablePipe(Then(Seq(modIR, canon), PerFunc(funcsView, Seq(fnIR, canonFn))))
	rec := &recorder{}
	st := NewStats()
	out, err := run(t, &Run{Observers: []Observer{rec, st}}, p, &prog{tables: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "[a]\na1: xfg\na2: yfg\n" {
		t.Errorf("output %q", got)
	}
	if n := strings.Count(strings.Join(rec.lines, "\n"), " t-canon#2/t-fold "); n != 2 {
		t.Errorf("events:\n%s", strings.Join(rec.lines, "\n"))
	}
	if got, ok := st.Instance("", "t-canon#2"); !ok || len(got.Patterns) != 1 || got.Patterns[0].Applied != 2 {
		t.Errorf("statistics of the second instance: %+v", got)
	}
	for _, off := range []string{"t-canon", "t-fold"} {
		out, err = run(t, &Run{Settings: settings(t, off)}, p, &prog{tables: []string{"a"}})
		if err != nil || out.String() != "[a]\na1: x\na2: y\n" {
			t.Errorf("-no-opt %s does not turn off both instances: %v %v", off, out, err)
		}
	}
}

// A name registered again at a kind it has, or with another declaration, panics.
func TestPassRegisteredTwice(t *testing.T) {
	for name, register := range map[string]func(){
		"same kind": func() {
			New(Def[*mod, *mod]{Decl: canon.Decl(), In: modIR, Out: modIR, Run: func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) { return m, pass.PreserveNone(), nil }})
		},
		"other declaration": func() {
			d := canon.Decl()
			d.Obligation = "another"
			New(Def[table, table]{Decl: d, In: tableIR, Out: tableIR, Run: func(_ *Ctx[table], x table) (table, pass.PreservedSet, error) { return x, pass.PreserveNone(), nil }})
		},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			register()
		}()
	}
}
