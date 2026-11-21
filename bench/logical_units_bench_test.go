package bench

import (
	"context"
	"testing"

	gjsonkeep "syntaxir.dev/sxir/internal/bench/parsers/jsonkeepguards"
	gxmlkeep "syntaxir.dev/sxir/internal/bench/parsers/xmlkeepguards"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	rt "syntaxir.dev/sxir/pkg/runtime/libgo"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// BenchmarkLogicalUnits measures the prototype of logical compute units (design record 076)
// against the parsers without limit controls and the limit-controlled parsers of today, on the
// full JSON and XML inputs. Configurations:
//
//   - plain: generated without -limits (no options); plain-aa runs it again, for the A/A spread;
//   - today-finite, today-unlimited, today-ctx: generated with -limits, the default (finite)
//     compute limit, Compute Unlimited, and a cancelable context with Compute Unlimited;
//   - logical-finite, logical-unlimited, logical-ctx: the same with logical units;
//   - keepguards-finite: logical units with every dispatch guard kept uncharged (inexact), which
//     measures what disabling the guards costs.
func BenchmarkLogicalUnits(b *testing.B) {
	keep := map[string][2]func(string, ...rt.ParseOption) error{
		"JSON": {
			func(s string, o ...rt.ParseOption) error { _, err := gjsonkeep.Parse(s, o...); return err },
			func(s string, o ...rt.ParseOption) error { _, err := gjsonkeep.ParseValue(s, o...); return err },
		},
		"XML": {
			func(s string, o ...rt.ParseOption) error { _, err := gxmlkeep.Parse(s, o...); return err },
			func(s string, o ...rt.ParseOption) error { _, err := gxmlkeep.ParseValue(s, o...); return err },
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unlimited := rt.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited})
	for _, c := range logicalCases(1) {
		for _, un := range []string{"Bytes", "CodePoints"} {
			unit := rt.WithUnit(logicalUnitsOf[un])
			for i, api := range []string{"Parse", "ParseValue"} {
				pick := func(n func(string, ...rt.ParseOption) (*value.Node, error), v func(string, ...rt.ParseOption) (any, error)) func(string, ...rt.ParseOption) error {
					if i == 0 {
						return func(s string, o ...rt.ParseOption) error { _, err := n(s, o...); return err }
					}
					return func(s string, o ...rt.ParseOption) error { _, err := v(s, o...); return err }
				}
				plain, today, logical := pick(c.plain, c.plainV), pick(c.today, c.todayV), pick(c.parse, c.value)
				configs := []struct {
					name string
					run  func(string, ...rt.ParseOption) error
					opts []rt.ParseOption
				}{
					{"plain", plain, []rt.ParseOption{unit}},
					{"plain-aa", plain, []rt.ParseOption{unit}},
					{"today-finite", today, []rt.ParseOption{unit}},
					{"today-unlimited", today, []rt.ParseOption{unit, unlimited}},
					{"today-ctx", today, []rt.ParseOption{unit, unlimited, rt.WithContext(ctx)}},
					{"logical-finite", logical, []rt.ParseOption{unit}},
					{"logical-unlimited", logical, []rt.ParseOption{unit, unlimited}},
					{"logical-ctx", logical, []rt.ParseOption{unit, unlimited, rt.WithContext(ctx)}},
					{"keepguards-finite", keep[c.name][i], []rt.ParseOption{unit}},
				}
				for _, cf := range configs {
					b.Run(c.name+"/"+api+"/"+un+"/"+cf.name, func(b *testing.B) {
						if err := cf.run(c.input, cf.opts...); err != nil {
							b.Fatal(err)
						}
						b.SetBytes(int64(len(c.input)))
						b.ReportAllocs()
						for b.Loop() {
							if err := cf.run(c.input, cf.opts...); err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			}
		}
	}
}
