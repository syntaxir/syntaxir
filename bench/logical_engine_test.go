//go:build !sxir_budget_controls_reference

package bench

import (
	"errors"
	"fmt"
	"os"
	"testing"

	ghand "syntaxir.dev/sxir/internal/bench/parsers/handlogical"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// The engine counts the logical compute units of a parse as the generated code does
// (spec/runtime/parsing.md#compute-units): its totals and its decisions are those of the logical
// parsers of TestLogicalExact, on the same grammars and inputs, though it may check at other points.

// engineGrammars maps the cases of exactCases to the grammars their parsers were generated from,
// with the start rule of the parsers.
var engineGrammars = map[string]struct{ path, start string }{
	"JSON":   {"../testdata/grammars/json/json.sxir", "main"},
	"XML":    {"../testdata/grammars/xml/xml.sxir", "main"},
	"policy": {"grammars/logical_policy.sxir", "doc"},
	"chain":  {"grammars/logical_chain.sxir", "doc"},
	"keyed":  {"grammars/logical_keyed.sxir", "doc"},
}

// loadStart compiles the grammar at path for the engine with the start rule start.
func loadStart(t *testing.T, path, start string) *engine.Parser {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := enginetest.CompileSource(string(src), start)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// engineUnitBackends are the engine's backends whose totals the test checks.
var engineUnitBackends = []struct {
	name string
	b    engine.Backend
}{
	{"closure", engine.Closure},
	{"bytecode", engine.Bytecode},
	{"bytecode iterative", engine.BytecodeIterative},
}

// engineTotal returns the total of the parse of in by p: the smallest compute limit that does not
// stop it, with the parse's error under that limit. It checks that every smaller limit it tries stops
// the parse with a LimitError whose Limit is the limit, whose Requested is zero and whose Used
// exceeds the limit by at most K + B, and that every larger one does not.
func engineTotal(t *testing.T, where string, parse func(sxirruntime.Limits) error) (uint64, error) {
	t.Helper()
	try := func(l uint64) (bool, error) {
		err := parse(sxirruntime.Limits{Compute: l})
		var le *sxirruntime.LimitError
		if !errors.As(err, &le) || le.Kind != sxirruntime.LimitCompute {
			return false, err
		}
		if le.Limit != l || le.Requested != 0 || le.Used <= l || le.Used-l > exactK {
			t.Errorf("%s limit %d: LimitError %+v outside (limit, limit+K+B] or with a request", where, l, le)
		}
		return true, err
	}
	hi := uint64(1)
	for {
		stopped, _ := try(hi)
		if !stopped {
			break
		}
		hi *= 2
	}
	lo := hi / 2 // stopped at lo (or lo is 0)
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		if stopped, _ := try(mid); stopped {
			lo = mid
		} else {
			hi = mid
		}
	}
	_, err := try(hi)
	return hi, err
}

// TestEngineLogicalUnits checks the engine's totals and outcomes against the logical parsers'.
func TestEngineLogicalUnits(t *testing.T) {
	unlimited := sxirruntime.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited})
	for _, c := range exactCases() {
		g := engineGrammars[c.name]
		p := loadStart(t, g.path, g.start)
		for _, bk := range engineUnitBackends {
			for i, in := range c.inputs {
				for _, unit := range exactUnits {
					want, werr := countUnits(c.logical[0], in, unit, unlimited)
					where := fmt.Sprintf("%s %s input %d (%v)", bk.name, c.name, i, unit)
					got, gerr := engineTotal(t, where, func(l sxirruntime.Limits) error {
						_, err := p.Parse(in, engine.WithBackend(bk.b), engine.WithUnit(unit), engine.WithLimits(l))
						return err
					})
					if got != want || (gerr == nil) != (werr == nil) {
						t.Errorf("%s: %d units (err %v), logical parser %d (err %v)", where, got, gerr, want, werr)
					}
				}
			}
		}
	}
}

// TestEngineLogicalUnitsActions checks the engine's totals of the actions, predicates and comparisons
// of bench/grammars/logical_hand.sxir against the logical parser's.
func TestEngineLogicalUnitsActions(t *testing.T) {
	p := loadStart(t, "grammars/logical_hand.sxir", "tag")
	for _, c := range []struct{ rule, in string }{
		{"tag", "<ab>xyz</ab>"}, {"tag", "<ab>xyz</ac>"}, {"nums", "[1,0,2]"}, {"nums", "[0,0,10,200,0]"}, {"nums", "[1,"},
	} {
		r, err := p.WithStart(c.rule)
		if err != nil {
			t.Fatal(err)
		}
		for _, bk := range engineUnitBackends {
			for _, unit := range []sxirinput.PositionUnit{sxirinput.Bytes, sxirinput.CodePoints} {
				var want uint64
				_, werr := ghand.ParseRule(c.rule, c.in, sxirruntime.WithUnit(unit), sxirruntime.WithComputeReport(&want))
				where := fmt.Sprintf("%s %s %q (%v)", bk.name, c.rule, c.in, unit)
				got, gerr := engineTotal(t, where, func(l sxirruntime.Limits) error {
					_, err := r.Parse(c.in, engine.WithBackend(bk.b), engine.WithUnit(unit), engine.WithLimits(l))
					return err
				})
				if got != want || (gerr == nil) != (werr == nil) {
					t.Errorf("%s: %d units (err %v), logical parser %d (err %v)", where, got, gerr, want, werr)
				}
			}
		}
	}
}
