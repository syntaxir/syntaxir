//go:build sxir_long && !sxir_budget_controls_reference

package engine

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestLimitErrorAgreementMutants checks that TestLimitErrorAgreement notices a backend that reports a
// run of units as one reservation: generated Go whose Take reserves its units at once, and generated
// TypeScript whose run of units is charged as one reservation; one that splits the reservation of the
// take-off of a term's list: generated Go and TypeScript that reserve the copy and the take-off apart,
// and generated Go that reserves it chunk by chunk; and generated Go that charges for taking off the
// list of a repetition, which charges nothing.
func TestLimitErrorAgreementMutants(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	cases := map[string]limitAgreementCase{}
	for _, c := range limitAgreementCases {
		cases[c.name] = c
	}
	t.Run("atomic Take", func(t *testing.T) {
		t.Parallel()
		codegentest.NeedsBudgetControls(t)
		const unit = "func (c *BudgetControl) takeSlow(n uint64) {\n\tc.settle()\n"
		runtime := codegentest.LibCopyWith(t, func(name, src string) string {
			if name != "budget.go" {
				return src
			}
			if !strings.Contains(src, unit) {
				t.Fatal("the unit reservations of Take are not in budget.go")
			}
			return strings.Replace(src, unit, "func (c *BudgetControl) takeSlow(n uint64) {\n\tc.chargeSlow(n)\n\treturn\n\tc.settle()\n", 1)
		}, nil)
		c := cases["literal"]
		want := engineLimitReports(t, c, Closure)
		node, typed := generatedGoLimitReports(t, c, runtime)
		if len(limitDisagreements(want, node, c.window)) == 0 || len(limitDisagreements(want, typed, c.window)) == 0 {
			t.Error("generated Go with an atomic Take agrees with the engine")
		}
	})
	// A list take-off reserved as a copy and a take-off, or chunk by chunk, is not one reservation of two
	// units for each element.
	kids := "\t\tif n := len(p.KidStack) - base; n > 0 {\n\t\t\tp.Control.Charge(2 * uint64(n))\n\t\t}\n"
	for name, split := range map[string]string{
		"list take-off as two reservations": "\t\tif n := len(p.KidStack) - base; n > 0 {\n\t\t\tp.Control.Charge(uint64(n))\n\t\t\tp.Control.Charge(uint64(n))\n\t\t}\n",
		"list take-off by chunks":           "\t\tfor n := len(p.KidStack) - base; n > 0; n -= min(n, 128) {\n\t\t\tp.Control.Charge(2 * uint64(min(n, 128)))\n\t\t}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			codegentest.NeedsBudgetControls(t)
			runtime := codegentest.LibCopyWith(t, func(file, src string) string {
				if file != "lib.go" && file != "typed.go" {
					return src
				}
				if strings.Count(src, kids) != 1 {
					t.Fatalf("the reservation of a list take-off is not in %s", file)
				}
				return strings.Replace(src, kids, split, 1)
			}, nil)
			// A term's list; only the long list is longer than a chunk.
			cs := []limitAgreementCase{cases["list"]}
			if name == "list take-off by chunks" {
				cs = []limitAgreementCase{cases["long list"]}
			}
			for _, c := range cs {
				node, typed := generatedGoLimitReports(t, c, runtime)
				caught := false
				for _, size := range c.lists {
					caught = caught || listReservationMissing(node, size) && listReservationMissing(typed, size)
				}
				if !caught {
					t.Errorf("%s: generated Go with the %s still makes each list take-off one reservation", c.name, name)
				}
			}
		})
	}
	// The list of a repetition is taken off without a charge: generated Go that charges a unit for each
	// element it takes off fails limits at which generated Go without the charge, which agrees with the
	// engine in the window (TestLimitErrorAgreement), succeeds. The take-off ends the parse, after the
	// last limit of the window, so the reports are compared at every limit.
	t.Run("charged repetition list take-off", func(t *testing.T) {
		t.Parallel()
		codegentest.NeedsBudgetControls(t)
		const takeOff = "\tks := p.Nodes(len(p.KidStack) - base)\n"
		runtime := codegentest.LibCopyWith(t, func(file, src string) string {
			if file != "lib.go" {
				return src
			}
			if strings.Count(src, takeOff) != 1 {
				t.Fatal("the take-off of a list is not in lib.go")
			}
			return strings.Replace(src, takeOff, "\tp.Control.Charge(uint64(len(p.KidStack) - base))\n"+takeOff, 1)
		}, nil)
		c := cases["repetition"]
		free, _ := generatedGoLimitReports(t, c, codegentest.LibCopy())
		node, _ := generatedGoLimitReports(t, c, runtime)
		if len(limitDisagreements(free, node, [2]uint64{1, uint64(len(free) - 1)})) == 0 {
			t.Error("generated Go that charges for taking off the list of a repetition reports what generated Go without the charge does")
		}
	})
	t.Run("TypeScript list take-off as two reservations", func(t *testing.T) {
		t.Parallel()
		codegentest.NeedsBudgetControls(t)
		const kids = "if (n > 0) this.control?.charge(2 * n);"
		for _, c := range []limitAgreementCase{cases["list"]} {
			ts := generatedTSLimitReports(t, c, func(b string) string {
				if !strings.Contains(b, kids) {
					t.Fatal("the reservation of a list take-off is not in the TypeScript runtime")
				}
				return strings.Replace(b, kids, "if (n > 0) { this.control?.charge(n); this.control?.charge(n); }", 1)
			})
			if !listReservationMissing(ts, c.lists[0]) {
				t.Errorf("%s: generated TypeScript with a list take-off as two reservations still makes it one", c.name)
			}
		}
	})
	t.Run("TypeScript run as one reservation", func(t *testing.T) {
		t.Parallel()
		codegentest.NeedsBudgetControls(t)
		const units = "spendUnits(n: number): void { this.control?.chargeUnits(n); }"
		c := cases["operators"]
		want := engineLimitReports(t, c, Closure)
		ts := generatedTSLimitReports(t, c, func(b string) string {
			if !strings.Contains(b, units) {
				t.Fatal("the run of units is not in the TypeScript runtime")
			}
			return strings.Replace(b, units, "spendUnits(n: number): void { this.control?.charge(n); }", 1)
		})
		if len(limitDisagreements(want, ts, c.tsWindow)) == 0 {
			t.Error("generated TypeScript that charges a run of units at once agrees with the engine")
		}
	})
}
