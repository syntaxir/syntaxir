package bench

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"syntaxir.dev/sxir/internal/bench/inputs"
	gjson "syntaxir.dev/sxir/internal/bench/parsers/json"
	gjsonlogical "syntaxir.dev/sxir/internal/bench/parsers/jsonlogical"
	gjsonplain "syntaxir.dev/sxir/internal/bench/parsers/jsonplain"
	gxml "syntaxir.dev/sxir/internal/bench/parsers/xml"
	gxmllogical "syntaxir.dev/sxir/internal/bench/parsers/xmllogical"
	gxmlplain "syntaxir.dev/sxir/internal/bench/parsers/xmlplain"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	rt "syntaxir.dev/sxir/pkg/runtime/libgo"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// The prototype of logical compute units (design record 076) against the parsers without limit
// controls: the same results, a count of units for each parse, and a compute limit that accepts at
// the count and rejects one below it.

type logicalCase struct {
	name   string
	input  string
	parse  func(string, ...rt.ParseOption) (*value.Node, error)
	value  func(string, ...rt.ParseOption) (any, error)
	plain  func(string, ...rt.ParseOption) (*value.Node, error)
	plainV func(string, ...rt.ParseOption) (any, error)
	today  func(string, ...rt.ParseOption) (*value.Node, error)
	todayV func(string, ...rt.ParseOption) (any, error)
}

func logicalCases(scale int) []logicalCase {
	return []logicalCase{
		{"JSON", inputs.JSONInput(256 << 10 / scale), gjsonlogical.Parse, func(s string, o ...rt.ParseOption) (any, error) { return gjsonlogical.ParseValue(s, o...) },
			gjsonplain.Parse, func(s string, o ...rt.ParseOption) (any, error) { return gjsonplain.ParseValue(s, o...) },
			gjson.Parse, func(s string, o ...rt.ParseOption) (any, error) { return gjson.ParseValue(s, o...) }},
		{"XML", inputs.XMLInput(256 << 10 / scale), gxmllogical.Parse, func(s string, o ...rt.ParseOption) (any, error) { return gxmllogical.ParseValue(s, o...) },
			gxmlplain.Parse, func(s string, o ...rt.ParseOption) (any, error) { return gxmlplain.ParseValue(s, o...) },
			gxml.Parse, func(s string, o ...rt.ParseOption) (any, error) { return gxml.ParseValue(s, o...) }},
	}
}

var logicalUnitsOf = map[string]sxirinput.PositionUnit{"Bytes": sxirinput.Bytes, "CodePoints": sxirinput.CodePoints}

func TestLogicalUnitsAgree(t *testing.T) {
	for _, c := range logicalCases(16) {
		for un, unit := range logicalUnitsOf {
			var nu, vu uint64
			n, err := c.parse(c.input, rt.WithUnit(unit), rt.WithUnitsReport(&nu), rt.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited}))
			if err != nil {
				t.Fatal(c.name, un, err)
			}
			want, err := c.plain(c.input, rt.WithUnit(unit))
			if err != nil || n.String() != want.String() {
				t.Fatal(c.name, un, "Parse differs from the parser without limit controls", err)
			}
			v, err := c.value(c.input, rt.WithUnit(unit), rt.WithUnitsReport(&vu), rt.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited}))
			if err != nil {
				t.Fatal(c.name, un, err)
			}
			wantV, err := c.plainV(c.input, rt.WithUnit(unit))
			if err != nil || !sameJSON(t, v, wantV) {
				t.Fatal(c.name, un, "ParseValue differs from the parser without limit controls", err)
			}
			t.Logf("%s %s: %d bytes, Parse %d units (%.2f/byte), ParseValue %d units (%.2f/byte)", c.name, un, len(c.input), nu, float64(nu)/float64(len(c.input)), vu, float64(vu)/float64(len(c.input)))
			if nu == 0 || vu == 0 {
				t.Fatal("no units counted")
			}
			// The limit accepts at the count and rejects one below it, with the count at the check.
			for _, f := range []struct {
				name  string
				run   func(...rt.ParseOption) error
				total uint64
			}{
				{"Parse", func(o ...rt.ParseOption) error { _, err := c.parse(c.input, o...); return err }, nu},
				{"ParseValue", func(o ...rt.ParseOption) error { _, err := c.value(c.input, o...); return err }, vu},
			} {
				if err := f.run(rt.WithUnit(unit), rt.WithLimits(sxirruntime.Limits{Compute: f.total})); err != nil {
					t.Fatal(c.name, un, f.name, "rejected at its count", err)
				}
				var used uint64
				err := f.run(rt.WithUnit(unit), rt.WithLimits(sxirruntime.Limits{Compute: f.total - 1}), rt.WithUnitsReport(&used))
				var le *sxirruntime.LimitError
				if !errors.As(err, &le) || le.Kind != sxirruntime.LimitCompute || le.Used <= le.Limit {
					t.Fatal(c.name, un, f.name, "not rejected one below its count", err)
				}
				t.Logf("  %s one below: stopped at %d, overshoot %d", f.name, le.Used, le.Used-le.Limit)
			}
			// A canceled context stops the parse.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := c.parse(c.input, rt.WithUnit(unit), rt.WithContext(ctx)); !errors.Is(err, context.Canceled) {
				t.Fatal(c.name, un, "canceled context not seen", err)
			}
		}
	}
}

// sameJSON compares typed values of parsers generated into different packages by their JSON form.
func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(ja) < 3 {
		t.Fatal("empty JSON form", string(ja))
	}
	return string(ja) == string(jb)
}
