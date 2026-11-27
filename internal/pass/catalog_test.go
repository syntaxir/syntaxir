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
		{"unclassified mechanism", func(f *Flag) { f.Mechanism, f.Backends = "M120", Engines }, ""},
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
