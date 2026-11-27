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

// A pass registered at two kinds is one pass: its instances are numbered across both, its flag
// governs both, and its statistics have the declared patterns at each.
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
	out, err = run(t, &Run{Settings: settings(t, "t-canon")}, p, &prog{tables: []string{"a"}})
	if err != nil || out.String() != "[a]\na1: x\na2: y\n" {
		t.Errorf("-no-opt t-canon does not turn off both instances: %v %v", out, err)
	}
	// Its text names it alike at both kinds, and the resolver takes the registration at the kind of
	// each place, so the pipeline read back runs as the typed one.
	d, err := Parse(Describe(p).String())
	if err != nil {
		t.Fatal(err)
	}
	back, err := Resolve(d, p)
	if err != nil {
		t.Fatal(err)
	}
	if out, err = run(t, &Run{}, back, &prog{tables: []string{"a"}}); err != nil || out.String() != "[a]\na1: xfg\na2: yfg\n" {
		t.Errorf("the pipeline read back: %v %v", out, err)
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
