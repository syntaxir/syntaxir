package engine

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// TestBackendsMatchOnMissingWithoutInsertion checks that the bytecode VMs parse and recognize the
// grammars that use #missing as the closure engine does when the parse inserts no token: a literal
// with #missing is then strict.
func TestBackendsMatchOnMissingWithoutInsertion(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.InsertionCorpus() {
		t.Run(c.Name, func(t *testing.T) {
			prog := mustCompile(t, c.Src)
			if !prog.HasMissing {
				t.Fatal("the grammar does not use #missing")
			}
			for _, in := range c.Inputs {
				checkBackends(t, prog, "main", in)
			}
		})
	}
}

// TestBackendsMatchOnInsertion checks that the bytecode VMs insert missing tokens as the closure
// engine does: the same results, errors, insertions and compute units, at several limits, in both
// units, with and without recognition and limit controls.
func TestBackendsMatchOnInsertion(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.InsertionCorpus() {
		t.Run(c.Name, func(t *testing.T) {
			prog := mustCompile(t, c.Src)
			for _, in := range c.Inputs {
				for _, u := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					for _, limit := range []int{1, 2, 64} {
						for _, recognize := range []bool{false, true} {
							for _, report := range []bool{false, true} {
								run := func(b Backend) string {
									var units uint64
									o := ParseOptions{Backend: b, Unit: u, Expectations: true, MissingTokens: limit, Recognize: recognize}
									if report {
										o.ComputeReport = &units
									}
									n, err := prog.ParseWith("main", in, o)
									return fmt.Sprintf("%s units=%d", codegentest.ResultJSON(n, err), units)
								}
								want := run(Closure)
								for _, b := range vmBackends {
									if got := run(b); got != want {
										t.Errorf("input %q (%v) limit %d recognize %t report %t: %v differs from closure\n closure %s\n %v %s", in, u, limit, recognize, report, b, want, b, got)
									}
								}
							}
						}
					}
				}
			}
		})
	}
}

// TestCompiledInsertion checks that a compiled grammar with #missing carries the module that the
// parses inserting missing tokens run, in each mode: the loaded program parses as the program it
// was saved from on both VMs, with the same results, errors and compute units, with and without
// insertion; it saves again to the same bytes; and a file whose insertion part is missing, or
// present for a grammar without #missing, does not load.
func TestCompiledInsertion(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.InsertionCorpus() {
		t.Run(c.Name, func(t *testing.T) {
			prog := mustCompile(t, c.Src)
			for _, mode := range allModes {
				data, err := prog.Marshal("main", mode)
				if err != nil {
					t.Fatalf("%s mode: %v", mode, err)
				}
				loaded, _, err := LoadProgram(data)
				if err != nil {
					t.Fatalf("%s mode: %v", mode, err)
				}
				if !loaded.HasMissing {
					t.Errorf("%s mode: the loaded program has no #missing", mode)
				}
				if again, err := loaded.Marshal("main", mode); err != nil || !bytes.Equal(again, data) {
					t.Errorf("%s mode: saved again, the data differs (%v)", mode, err)
				}
				for _, in := range c.Inputs {
					for _, limit := range []int{0, 1, 2, 64} {
						for _, b := range vmBackends {
							run := func(p *Program) string {
								var units uint64
								n, err := p.ParseWith("main", in, servedBy(mode, ParseOptions{Backend: b, Expectations: true, MissingTokens: limit, ComputeReport: &units}))
								return fmt.Sprintf("%s units=%d", codegentest.ResultJSON(n, err), units)
							}
							if want, got := run(prog), run(loaded); got != want {
								t.Errorf("%s mode %v: input %q limit %d\n source %s\n loaded %s", mode, b, in, limit, want, got)
							}
						}
					}
				}
			}
		})
	}
	// The insertion module comes with #missing, and only with it.
	withMissing, strict := mustCompile(t, `rule main = "a" ";" #missing`), mustCompile(t, `rule main = "a" ";"`)
	ins, err := withMissing.insertionProgram()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		name    string
		m, part *bytecode.Module
	}{
		{"a grammar with #missing without its insertion module", withMissing.Module(), nil},
		{"a grammar without #missing with an insertion module", strict.Module(), ins.Module()},
		{"an insertion module without #missing", withMissing.Module(), strict.Module()},
	} {
		if _, _, err := LoadProgram(encodeCompiled("main", CompiledParse, bad.m, bad.part)); err == nil || !strings.Contains(err.Error(), "insertion module") {
			t.Errorf("%s: loaded (%v)", bad.name, err)
		}
	}
	// A grammar that names only the type missing has nothing to insert; its compiled grammar keeps
	// the reference, which the entry points that refuse missing read.
	typeOnly := mustCompile(t, "type Item ~ missing\nrule main = \"a\"")
	data, err := typeOnly.Marshal("main", CompiledParse)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.HasMissing {
		t.Error("a grammar that names the type missing lost the reference")
	}
	if again, err := loaded.Marshal("main", CompiledParse); err != nil || !bytes.Equal(again, data) {
		t.Errorf("type only: saved again, the data differs (%v)", err)
	}
}
