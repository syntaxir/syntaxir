package bench

import (
	"context"
	"testing"

	gjsonkeep "syntaxir.dev/sxir/internal/bench/parsers/jsonkeepguards"
	gxmlkeep "syntaxir.dev/sxir/internal/bench/parsers/xmlkeepguards"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// BenchmarkLogicalUnits measures logical compute units (design record 076)
// against the parsers without limit controls and the limit-controlled parsers of today, on the
// full JSON and XML inputs. Configurations:
//
//   - plain: generated with -no-limits; plain-aa runs it again, for the A/A spread;
//   - today-finite, today-unlimited, today-ctx: generated with the default limit controls (finite)
//     compute limit, Compute Unlimited, and a cancelable context with Compute Unlimited;
//   - logical-finite, logical-unlimited, logical-ctx: the same with logical units;
//   - keepguards-finite: logical units with every dispatch guard kept without the effects of the
//     calls it skips (inexact), which measures what the guards that are left out and the effects
//     cost.
func BenchmarkLogicalUnits(b *testing.B) {
	keep := map[string][2]func(string, ...sxirruntime.ParseOption) error{
		"JSON": {
			func(s string, o ...sxirruntime.ParseOption) error { _, err := gjsonkeep.Parse(s, o...); return err },
			func(s string, o ...sxirruntime.ParseOption) error {
				_, err := gjsonkeep.ParseValue(s, o...)
				return err
			},
		},
		"XML": {
			func(s string, o ...sxirruntime.ParseOption) error { _, err := gxmlkeep.Parse(s, o...); return err },
			func(s string, o ...sxirruntime.ParseOption) error { _, err := gxmlkeep.ParseValue(s, o...); return err },
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unlimited := sxirruntime.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited})
	for _, c := range logicalCases(1) {
		for _, un := range []string{"Bytes", "CodePoints"} {
			unit := sxirruntime.WithUnit(logicalUnitsOf[un])
			for i, api := range []string{"Parse", "ParseValue"} {
				pick := func(n func(string, ...sxirruntime.ParseOption) (*value.Node, error), v func(string, ...sxirruntime.ParseOption) (any, error)) func(string, ...sxirruntime.ParseOption) error {
					if i == 0 {
						return func(s string, o ...sxirruntime.ParseOption) error { _, err := n(s, o...); return err }
					}
					return func(s string, o ...sxirruntime.ParseOption) error { _, err := v(s, o...); return err }
				}
				plain, today, logical := pick(c.plain, c.plainV), pick(c.today, c.todayV), pick(c.parse, c.value)
				configs := []struct {
					name string
					run  func(string, ...sxirruntime.ParseOption) error
					opts []sxirruntime.ParseOption
				}{
					{"plain", plain, []sxirruntime.ParseOption{unit}},
					{"plain-aa", plain, []sxirruntime.ParseOption{unit}},
					{"today-finite", today, []sxirruntime.ParseOption{unit}},
					{"today-unlimited", today, []sxirruntime.ParseOption{unit, unlimited}},
					{"today-ctx", today, []sxirruntime.ParseOption{unit, unlimited, sxirruntime.WithContext(ctx)}},
					{"logical-finite", logical, []sxirruntime.ParseOption{unit}},
					{"logical-unlimited", logical, []sxirruntime.ParseOption{unit, unlimited}},
					{"logical-ctx", logical, []sxirruntime.ParseOption{unit, unlimited, sxirruntime.WithContext(ctx)}},
					{"keepguards-finite", keep[c.name][i], []sxirruntime.ParseOption{unit}},
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
