package codepass

import (
	"context"
	"slices"
	"testing"

	. "syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// patternSites returns a function with one site of each pattern of the Exec stage, where no pattern
// makes or removes a site of another: turning one pattern off leaves exactly its own site.
func patternSites(m *Module) *Func {
	return m.AddFunc("f", []Param{P("x", TBool)}, nil, nil, func(b *Builder) {
		b.Do("log.note", Add(Int(1), Int(2)))                            // fold-constants
		b.If(BoolC(true), func(b *Builder) { b.Do("log.note", Int(4)) }) // fold-branches
		b.Block("b", func(b *Builder) {                                  // drop-after-exit
			b.Exit("b")
			b.Do("log.note", Int(5))
		})
		b.Block("c", func(b *Builder) { // drop-unused-labels; the local keeps the block
			b.Let("z", Int(6))
			b.Do("log.note", L("z"))
		})
		b.Block("", func(b *Builder) { b.Do("log.note", Int(7)) }) // splice-blocks
	})
}

// canonicalized returns the listing of the function of patternSites after the patterns of the Exec
// stage, with those named off turned off, and the number of rewrites of each pattern by name.
func canonicalized(t *testing.T, off ...string) (string, map[string]int) {
	t.Helper()
	m := module()
	f := patternSites(m)
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	ps := PatternsAt[Exec]()
	counts := ApplyPatterns(f, ps, func(p string) bool { return !slices.Contains(off, p) })
	if err := Verify(m); err != nil {
		t.Fatalf("%v: %v\n%s", off, err, FuncString(f))
	}
	byName := map[string]int{}
	for i, p := range ps {
		byName[p.Name] = counts[i]
	}
	return FuncString(f), byName
}

// The patterns of the Exec stage, in the order they are tried.
func TestExecPatterns(t *testing.T) {
	var names []string
	for _, p := range PatternsAt[Exec]() {
		names = append(names, p.Name)
	}
	want := []string{"fold-constants", "fold-branches", "drop-after-exit", "drop-unused-labels", "splice-blocks"}
	if !slices.Equal(names, want) {
		t.Errorf("patterns %v, want %v", names, want)
	}
	for _, name := range append(want, PassCanonicalize) {
		if _, ok := pass.Lookup(name); !ok {
			t.Errorf("%s is not a registered optimization", name)
		}
	}
}

// Each pattern rewrites its site, and turning one pattern off changes only the rewrites of that
// pattern: the other patterns rewrite what they rewrote with every pattern on, and applying the
// pattern that was off afterwards gives the listing of every pattern on.
func TestCanonicalizePatternOff(t *testing.T) {
	all, counts := canonicalized(t)
	want := "func f(x bool) {\n  log.note(3)\n  log.note(4)\n  block b {\n    exit b\n  }\n  {\n    let z = 6\n    log.note(z)\n  }\n  log.note(7)\n}\n"
	if all != want {
		t.Errorf("every pattern on:\n%s\nwant\n%s", all, want)
	}
	for _, p := range PatternsAt[Exec]() {
		if counts[p.Name] != 1 {
			t.Errorf("%s rewrote %d sites, want 1", p.Name, counts[p.Name])
		}
	}
	for _, p := range PatternsAt[Exec]() {
		t.Run(p.Name, func(t *testing.T) {
			got, c := canonicalized(t, p.Name)
			if got == all {
				t.Errorf("turning %s off changes nothing", p.Name)
			}
			for _, q := range PatternsAt[Exec]() {
				if want := counts[q.Name]; q.Name != p.Name && c[q.Name] != want || q.Name == p.Name && c[q.Name] != 0 {
					t.Errorf("with %s off, %s rewrote %d sites", p.Name, q.Name, c[q.Name])
				}
			}
			m := module()
			f := patternSites(m)
			ApplyPatterns(f, PatternsAt[Exec](), func(q string) bool { return q != p.Name })
			ApplyPatterns(f, []Pattern{p}, func(string) bool { return true })
			if FuncString(f) != all {
				t.Errorf("%s applied after the others gives\n%s", p.Name, FuncString(f))
			}
		})
	}
}

// Each application asks on its own: admitting only the first of two applications of a pattern, as a
// bisection limit of 1 does, rewrites that site and keeps the other, whose application is refused again
// on the next sweep.
func TestCanonicalizeAsksEachApplication(t *testing.T) {
	m := module()
	f := m.AddFunc("f", nil, nil, nil, func(b *Builder) {
		b.Do("log.note", Add(Int(1), Int(2)))
		b.Do("log.note", Add(Int(3), Int(4)))
	})
	asked := 0
	ApplyPatterns(f, []Pattern{FoldConstants}, func(string) bool { asked++; return asked == 1 })
	if got, want := FuncString(f), "func f() {\n  log.note(3)\n  log.note((3 + 4))\n}\n"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if asked != 3 {
		t.Errorf("asked %d times, want 3: two in the first sweep and one in the second", asked)
	}
}

// The patterns of the Flow stage keep its invariant that every block has a label: they neither
// remove a label nor splice a block, and an If with a constant condition leaves the statements of
// its branch in place of itself.
func TestFlowPatterns(t *testing.T) {
	var names []string
	for _, p := range PatternsAt[Flow]() {
		names = append(names, p.Name)
	}
	if want := []string{"drop-after-exit"}; !slices.Equal(names, want) {
		t.Errorf("patterns %v, want %v", names, want)
	}
}

// runCanonicalize runs canonicalize of the Exec stage on the function of patternSites with the flags
// off turned off, and returns its listing and the statistics of the pass.
func runCanonicalize(t *testing.T, off ...string) (string, pipe.InstanceStats) {
	t.Helper()
	m := module()
	m.Dialects = []string{""} // the operations of the test module belong to no dialect
	patternSites(m)
	s, err := pass.Settings{}.Without(off...)
	if err != nil {
		t.Fatal(err)
	}
	st := pipe.NewStats()
	out, err := pipe.Execute(context.Background(), &pipe.Run{Settings: s, VerifyEach: true, Observers: []pipe.Observer{st}},
		OnFuncs(ExecCanonicalize), EnterExec(m))
	if err != nil {
		t.Fatal(err)
	}
	stats, _ := st.Instance("", PassCanonicalize)
	return FuncString(out.Module().Func("f")), stats
}

// The pass and each of its patterns turn off under their names: with the pass off the function is as
// the lowering made it, and with a pattern off its applications are skipped and its site stays.
func TestCanonicalizePassOff(t *testing.T) {
	all, _ := canonicalized(t)
	if got, st := runCanonicalize(t); got != all || st.Runs != 1 || st.Changed != 1 {
		t.Errorf("on: %+v\n%s", st, got)
	}
	m := module()
	asLowered := FuncString(patternSites(m))
	if got, st := runCanonicalize(t, PassCanonicalize); got != asLowered || st.Runs != 0 {
		t.Errorf("-no-opt canonicalize: %+v\n%s", st, got)
	}
	for _, p := range PatternsAt[Exec]() {
		want, _ := canonicalized(t, p.Name)
		got, st := runCanonicalize(t, p.Name)
		if got != want {
			t.Errorf("-no-opt %s:\n%s\nwant\n%s", p.Name, got, want)
		}
		for _, d := range st.Patterns {
			if d.Pattern == p.Name && (d.Applied != 0 || d.Skipped == 0) {
				t.Errorf("-no-opt %s: %+v", p.Name, d)
			}
		}
	}
}
