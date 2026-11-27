package pass

import (
	"strings"
	"testing"
)

// A flag that keeps the rules of the catalog, for the cases that break one rule each.
func catalogFlag() Flag {
	return Flag{Name: "t-catalog", Kind: KindCorePass, Doc: "x", Default: 1, Mechanism: "M140", Record: 136,
		Tradeoff: "y", Backends: On(BackendTypedGo), Witnesses: []string{"terminals"}}
}

// CheckCatalog accepts a flag that keeps its rules and reports each rule that a flag breaks.
func TestCheckCatalog(t *testing.T) {
	if err := CheckCatalog([]Flag{catalogFlag()}); err != nil {
		t.Errorf("a valid flag: %v", err)
	}
	for _, c := range []struct {
		name string
		edit func(*Flag)
		want string
	}{
		{"no mechanism", func(f *Flag) { f.Mechanism = "" }, "names no mechanism"},
		{"undeclared mechanism", func(f *Flag) { f.Mechanism = "M999" }, "neither declared nor unclassified"},
		{"former ID", func(f *Flag) { f.Mechanism = "R010" }, "neither declared nor unclassified"},
		{"part of the mechanism's backends", func(f *Flag) { f.Mechanism, f.Backends = "M120", Engines }, ""},
		{"no record", func(f *Flag) { f.Record = 0 }, "names no record"},
		{"no trade-off", func(f *Flag) { f.Tradeoff = "" }, "what it trades"},
		{"no backends", func(f *Flag) { f.Backends = 0 }, "no backend"},
		{"no witnesses", func(f *Flag) { f.Witnesses = nil }, "no witness"},
		{"backends beyond the mechanism", func(f *Flag) { f.Backends = On(BackendTypedGo, BackendTypeScript) }, "mechanism M140 acts on"},
		{"removed name", func(f *Flag) { f.Name = "fold-constants" }, "the removed optimization fold-constants is registered"},
	} {
		f := catalogFlag()
		c.edit(&f)
		err := CheckCatalog([]Flag{f})
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// Settings reject the name of a removed optimization with the record that removed it, and an unknown
// name with the valid ones.
func TestRemovedNames(t *testing.T) {
	if _, err := (Settings{}).Without("ts-spend-runs"); err == nil || !strings.Contains(err.Error(), "optimization record 145") {
		t.Errorf("-no-opt ts-spend-runs: %v", err)
	}
	if _, err := (Settings{}).With("canonicalize", 1); err == nil || !strings.Contains(err.Error(), "optimization record 142") {
		t.Errorf("-opt canonicalize: %v", err)
	}
	if _, err := (Settings{}).Without("t-nothing"); err == nil || !strings.Contains(err.Error(), "unknown optimization") {
		t.Errorf("-no-opt t-nothing: %v", err)
	}
}

// The control of a mechanism is a flag if a flag names it, a reference control if only a reference
// control does, and none otherwise.
func TestControlOf(t *testing.T) {
	flags := []Flag{catalogFlag()}
	if got := ControlOf("M140", flags); got != ControlFlag {
		t.Errorf("M140: %v", got)
	}
	if got := ControlOf("M150", flags); got != ControlNone {
		t.Errorf("M150: %v", got)
	}
}

// The check of the declarations: a flag may name a mechanism that is not classified yet, an ID
// belongs to one list only, and an experiment reaches some backends partly, none fully, and has no
// reference control.
func TestCheckCatalogDeclarations(t *testing.T) {
	m := Mechanism{ID: "M140", Title: "t", Page: "M140-t.md", Scope: "s", Partial: On(BackendTypedGo)}
	exp := Mechanism{ID: "R160", Title: "t", Page: "R160-t.md", Scope: "s", Partial: Engines, Experiment: "tag"}
	expFull, expNone := exp, exp
	expFull.Full, expFull.Partial = Engines, 0
	expNone.Partial = 0
	f := catalogFlag()
	for _, c := range []struct {
		name string
		cat  catalog
		flag Flag
		want string
	}{
		{"declared", catalog{mechanisms: []Mechanism{m}}, f, ""},
		{"unclassified", catalog{unclassified: []string{"M140"}}, f, ""},
		{"former", catalog{former: []string{"M140"}}, f, "neither declared nor unclassified"},
		{"declared and unclassified", catalog{mechanisms: []Mechanism{m}, unclassified: []string{"M140"}}, f, "M140 is both mechanism and unclassified mechanism"},
		{"unclassified and former", catalog{unclassified: []string{"M140"}, former: []string{"M140"}}, f, "M140 is both unclassified mechanism and former ID"},
		{"former twice", catalog{mechanisms: []Mechanism{m}, former: []string{"R010", "R010"}}, f, "former ID R010 is declared twice"},
		{"reference control of a former ID", catalog{mechanisms: []Mechanism{m}, former: []string{"R010"},
			refs: []ReferenceControl{{Mechanism: "R010", Tag: "t", Backends: Engines, Record: 1}}}, f, "neither declared nor unclassified"},
		{"experiment", catalog{mechanisms: []Mechanism{m, exp}}, f, ""},
		{"experiment with full reach", catalog{mechanisms: []Mechanism{m, expFull}}, f, "acts fully on no parser"},
		{"experiment without reach", catalog{mechanisms: []Mechanism{m, expNone}}, f, "names no backend"},
		{"reference control of an experiment", catalog{mechanisms: []Mechanism{m, exp},
			refs: []ReferenceControl{{Mechanism: "R160", Tag: "t", Backends: Engines, Record: 1}}}, f, "names the experiment R160"},
	} {
		err := c.cat.check([]Flag{c.flag})
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// Uncontrolled returns the backends of a mechanism that no control lists.
func TestUncontrolled(t *testing.T) {
	m, ok := LookupMechanism("M120")
	if !ok {
		t.Fatal("M120 is not declared")
	}
	f := catalogFlag()
	f.Mechanism, f.Backends = "M120", On(BackendTypedGo)
	if got, want := Uncontrolled(m, []Flag{f}), m.Reaches()&^On(BackendTypedGo); got != want {
		t.Errorf("M120 with a typed Go flag: %v, want %v", got, want)
	}
	f.Backends = m.Reaches()
	if got := Uncontrolled(m, []Flag{f}); got != 0 {
		t.Errorf("M120 with a flag on every backend: %v", got)
	}
}
