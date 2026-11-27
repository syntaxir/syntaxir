package pipe

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"syntaxir.dev/sxir/internal/pass"
)

// A toy compilation for the tests of the manager: a program of named tables, each lowered to a
// module of functions, transforms on modules and on functions, and the joined modules.

type prog struct{ tables []string }

type table struct{ name string }

type fn struct{ name, body string }

type mod struct{ fns []*fn }

type mods struct {
	names []string
	list  []*mod
}

func (m *mod) String() string {
	var b strings.Builder
	for _, f := range m.fns {
		fmt.Fprintf(&b, "%s: %s\n", f.name, f.body)
	}
	return b.String()
}

func (m *mods) String() string {
	var b strings.Builder
	for i, x := range m.list {
		fmt.Fprintf(&b, "[%s]\n%s", m.names[i], x)
	}
	return b.String()
}

func verifyFn(f *fn) error {
	if strings.Contains(f.body, "bad") {
		return fmt.Errorf("function %s is bad", f.name)
	}
	return nil
}

var (
	progIR  = RegisterIR(&IR[*prog]{Name: "t-program"})
	tableIR = RegisterIR(&IR[table]{Name: "t-table"})
	fnIR    = RegisterIR(&IR[*fn]{Name: "t-fn", Verify: verifyFn,
		Print: func(w io.Writer, f *fn) { fmt.Fprintf(w, "%s: %s\n", f.name, f.body) },
		Copy:  func(f *fn) *fn { c := *f; return &c }})
	modIR = RegisterIR(&IR[*mod]{Name: "t-mod",
		Verify: func(m *mod) error {
			verifies++
			for _, f := range m.fns {
				if err := verifyFn(f); err != nil {
					return err
				}
			}
			return nil
		},
		Print: func(w io.Writer, m *mod) { prints++; io.WriteString(w, m.String()) },
		Copy: func(m *mod) *mod {
			c := &mod{}
			for _, f := range m.fns {
				x := *f
				c.fns = append(c.fns, &x)
			}
			return c
		}})
	modsIR    = RegisterIR(&IR[*mods]{Name: "t-mods"})
	modListIR = RegisterIR(&IR[[]*mod]{Name: "t-mod-list"})

	funcsView = RegisterFuncs(modIR, fnIR, &Funcs[*mod, *fn]{Name: "t-funcs",
		Len:  func(m *mod) int { return len(m.fns) },
		Func: func(m *mod, i int) string { return m.fns[i].name },
		Get:  func(m *mod, i int) *fn { return m.fns[i] },
		Put:  func(m *mod, i int, f *fn) (*mod, error) { m.fns[i] = f; return m, nil }})

	tablesSplit = RegisterSplit(progIR, tableIR, modIR, modsIR, &Split[*prog, table, *mod, *mods]{Name: "t-tables",
		Labels: []string{"a", "b", "c"},
		Parts: func(_ SplitCtx, p *prog) ([]table, []string, error) {
			var ts []table
			for _, n := range p.tables {
				ts = append(ts, table{n})
			}
			return ts, p.tables, nil
		},
		Join: func(_ SplitCtx, p *prog, ms []*mod) (*mods, error) { return &mods{names: p.tables, list: ms}, nil }})

	tableKey = RegisterKey(tableIR, &Key[table]{Name: "t-key", Of: func(t table) string { return t.name }})
	modKey   = RegisterKey(modIR, &Key[*mod]{Name: "t-mod-key", Of: func(m *mod) string { return m.String() }})

	targetFeature = NewFeature("t-target", func(s string) string { return s }, func(s string) (string, error) { return s, nil })

	modSlot = NewSlot("t-mod-passes", modIR)
	fnSlot  = NewSlot("t-fn-passes", fnIR)
)

func init() {
	for _, n := range []string{"t-upper", "t-append", "t-fn-upper", "t-noop", "t-canon", "t-feat", "t-query"} {
		pass.Register(pass.Flag{Name: n, Kind: pass.KindCorePass, Doc: "A pass of the tests of the manager.", Default: 1})
	}
}

// build! lowers a table to a module of two functions.
var build = New(Def[table, *mod]{Decl: Decl{Name: "t-build", Required: true, Obligation: "lowers a table"},
	In: tableIR, Out: modIR,
	Run: func(c *Ctx[table], t table) (*mod, pass.PreservedSet, error) {
		builds++
		return &mod{fns: []*fn{{t.name + "1", "x"}, {t.name + "2", "y"}}}, pass.PreserveNone(), nil
	}})

var builds, prints, verifies int

// break-required! is a required transform that leaves an invalid unit.
var requiredBreaker = modPass("t-break-required", func(_ *Ctx[*mod], m *mod) error { m.fns[0].body = "bad"; return nil }, func(d *Decl) { d.Required = true })

// pick! takes the first result of a Fork.
var pick = New(Def[[]*mod, *mod]{Decl: Decl{Name: "t-pick", Required: true}, In: modListIR, Out: modIR,
	Run: func(_ *Ctx[[]*mod], l []*mod) (*mod, pass.PreservedSet, error) { return l[0], pass.PreserveNone(), nil }})

func modPass(name string, f func(c *Ctx[*mod], m *mod) error, d ...func(*Decl)) *Pass[*mod, *mod] {
	decl := Decl{Name: name, Obligation: "a test transform"}
	for _, x := range d {
		x(&decl)
	}
	return New(Def[*mod, *mod]{Decl: decl, In: modIR, Out: modIR,
		Run: func(c *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
			if err := f(c, m); err != nil {
				return nil, pass.PreservedSet{}, err
			}
			return m, pass.PreserveNone(), nil
		}})
}

func each(m *mod, f func(*fn)) {
	for _, x := range m.fns {
		f(x)
	}
}

var (
	upper = modPass("t-upper", func(_ *Ctx[*mod], m *mod) error {
		each(m, func(f *fn) { f.body = strings.ToUpper(f.body) })
		return nil
	})
	appnd  = modPass("t-append", func(_ *Ctx[*mod], m *mod) error { each(m, func(f *fn) { f.body += "+" }); return nil })
	mark   = modPass("t-mark", func(_ *Ctx[*mod], m *mod) error { each(m, func(f *fn) { f.body += "#" }); return nil }, func(d *Decl) { d.Required = true })
	failer = modPass("t-fail", func(_ *Ctx[*mod], m *mod) error {
		each(m, func(f *fn) { f.body = "half" })
		return errors.New("broken")
	}, func(d *Decl) { d.Test = true })
	noop = New(Def[*mod, *mod]{Decl: Decl{Name: "t-noop"}, In: modIR, Out: modIR,
		Run: func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) { return m, pass.PreserveNone(), nil }})
	liar = New(Def[*mod, *mod]{Decl: Decl{Name: "t-liar", Test: true}, In: modIR, Out: modIR,
		Run: func(_ *Ctx[*mod], m *mod) (*mod, pass.PreservedSet, error) {
			m.fns[0].body += "!"
			return m, pass.PreserveAll(), nil
		}})
	breaker = modPass("t-break", func(_ *Ctx[*mod], m *mod) error { m.fns[0].body = "bad"; return nil }, func(d *Decl) { d.Test = true })
	feat    = modPass("t-feat", func(c *Ctx[*mod], m *mod) error {
		each(m, func(f *fn) { f.body += "@" + Feature(c, targetFeature) })
		return nil
	}, func(d *Decl) { d.Features = []FeatureID{targetFeature.ID()} })
	canon = modPass("t-canon", func(c *Ctx[*mod], m *mod) error {
		each(m, func(f *fn) {
			if c.Allow("t-fold") {
				f.body += "f"
			}
		})
		return nil
	}, func(d *Decl) { d.Patterns = []string{"t-fold"} })
	needsAppend = modPass("t-after", func(_ *Ctx[*mod], m *mod) error { return nil }, func(d *Decl) { d.Test = true; d.After = []string{"t-append"} })

	fnUpper = New(Def[*fn, *fn]{Decl: Decl{Name: "t-fn-upper"}, In: fnIR, Out: fnIR,
		Run: func(_ *Ctx[*fn], f *fn) (*fn, pass.PreservedSet, error) {
			f.body = strings.ToUpper(f.body)
			return f, pass.PreserveNone(), nil
		}})
	fnFail = New(Def[*fn, *fn]{Decl: Decl{Name: "t-fn-fail", Test: true}, In: fnIR, Out: fnIR,
		Run: func(c *Ctx[*fn], f *fn) (*fn, pass.PreservedSet, error) {
			if strings.HasSuffix(f.name, "2") {
				f.body = "half"
				return nil, pass.PreservedSet{}, errors.New("broken")
			}
			f.body += "-"
			return f, pass.PreserveNone(), nil
		}})
)

// recorder is an observer that writes one line per event.
type recorder struct {
	needs Needs
	veto  func(Event) bool
	lines []string
	dumps []string
}

func (r *recorder) Needs() Needs { return r.needs }

func (r *recorder) Before(e Event, _ func(io.Writer)) bool {
	if r.veto != nil && e.Step.Required == false {
		return !r.veto(e)
	}
	return true
}

func (r *recorder) After(e Event, res *Result) {
	l := fmt.Sprintf("%d %s [%s] ran=%v changed=%v", e.Seq, e.Step.Instance, e.Label, res.Ran, res.Changed)
	if r.needs.TextChanged {
		l += fmt.Sprintf(" text=%v", res.TextChanged)
	}
	if e.Replayed {
		l += " replayed"
	}
	if res.Err != nil {
		l += " err"
	}
	r.lines = append(r.lines, l)
	if res.Print != nil {
		var b strings.Builder
		res.Print(&b)
		r.dumps = append(r.dumps, e.Step.Instance+"\n"+b.String())
	}
}

// copiesOf runs f and returns the copies that the runs inside it took.
func copiesOf(f func()) int {
	n := 0
	testHook = func(rs *runner) { n += rs.copies }
	defer func() { testHook = nil }()
	f()
	return n
}
