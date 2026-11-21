package libgo

import (
	"math"
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

func TestDefaultBudget(t *testing.T) {
	for _, tc := range []struct{ n, want uint64 }{
		{0, 1 << 20},
		{1024, 1 << 20},
		{1025, 1025 * 1024},
		{math.MaxUint64, math.MaxUint64 - 1},
	} {
		if got := defaultBudget(tc.n); got != tc.want {
			t.Errorf("defaultBudget(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
	if got := inputUnits(strings.Repeat("é", 2000), sxirinput.CodePoints); got != 2000 {
		t.Errorf("code points: %d", got)
	}
	if got := inputUnits(strings.Repeat("é", 2000), sxirinput.Bytes); got != 4000 {
		t.Errorf("bytes: %d", got)
	}
}

// A zero budget resolves per unit of input when the parser has controls; Unlimited leaves no control, and
// a stream starts at the floor and follows what the parser demands.
func TestPrepareOptionsDefault(t *testing.T) {
	o := prepareOptions(parseOptions{controls: true}, 5000, false)
	if o.control == nil || o.control.limits.Compute != 5000*1024 {
		t.Fatalf("default: %+v", o.control)
	}
	o = prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Compute: sxirruntime.Unlimited}}, 5000, false)
	if o.control != nil {
		t.Fatal("Unlimited made a control")
	}
	o = prepareOptions(parseOptions{limits: sxirruntime.Limits{Compute: sxirruntime.Unlimited}}, 5000, false)
	if o.control != nil {
		t.Fatal("Unlimited without controls made a control")
	}
	o = prepareOptions(parseOptions{}, 5000, false)
	if o.control != nil {
		t.Fatal("a parser without controls got a default")
	}
	o = prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Compute: 9}}, 5000, false)
	if o.control == nil || o.control.limits.Compute != 9 {
		t.Fatalf("explicit: %+v", o.control)
	}
	o = prepareOptions(parseOptions{controls: true}, 0, true)
	if o.control == nil || o.control.limits.Compute != 1<<20 || !o.control.adaptive {
		t.Fatalf("stream: %+v", o.control)
	}
	o.control.demand(3 << 20)
	if o.control.limits.Compute != 3<<30 {
		t.Fatalf("grown: %d", o.control.limits.Compute)
	}
}

// prepareText rejects a text longer than the Input limit before any work, counting code points only when
// the byte length passes the limit; zero and Unlimited set no bound and need no control.
func TestPrepareTextInput(t *testing.T) {
	text := strings.Repeat("é", 10) // 10 code points, 20 bytes
	over := func(o parseOptions) (le *sxirruntime.LimitError) {
		defer func() {
			if x := recover(); x != nil {
				f, ok := x.(fatal)
				if !ok {
					panic(x)
				}
				le, _ = f.err.(*sxirruntime.LimitError)
			}
		}()
		prepareText(o, text)
		return nil
	}
	for _, tc := range []struct {
		unit sxirinput.PositionUnit
		max  uint64
		want uint64 // the length reported, or zero when the text is accepted
	}{
		{sxirinput.CodePoints, 0, 0}, {sxirinput.CodePoints, sxirruntime.Unlimited, 0}, {sxirinput.CodePoints, 10, 0}, {sxirinput.CodePoints, 9, 10}, {sxirinput.CodePoints, 20, 0},
		{sxirinput.Bytes, 20, 0}, {sxirinput.Bytes, 19, 20}, {sxirinput.Bytes, 10, 20},
	} {
		le := over(parseOptions{unit: tc.unit, limits: sxirruntime.Limits{Input: tc.max}, controls: true})
		if tc.want == 0 {
			if le != nil {
				t.Errorf("unit %d, max %d: %v", tc.unit, tc.max, le)
			}
			continue
		}
		if le == nil || le.Kind != sxirruntime.LimitInput || le.Limit != tc.max || le.Requested != tc.want || le.Used != 0 {
			t.Errorf("unit %d, max %d: %+v, want length %d", tc.unit, tc.max, le, tc.want)
		}
	}
	// Input makes no control of its own: with the budget unlimited the batch check is the whole of it.
	o := prepareText(parseOptions{controls: true, limits: sxirruntime.Limits{Compute: sxirruntime.Unlimited, Input: 100}}, text)
	if o.control != nil {
		t.Fatal("a finite Input made a control")
	}
	// A stream keeps the resolved bound for the parser.
	o = prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Input: 100}}, 0, true)
	if o.maxInput != 100 {
		t.Errorf("stream bound %d", o.maxInput)
	}
	o = prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Input: sxirruntime.Unlimited}}, 0, true)
	if o.maxInput != 0 {
		t.Errorf("Unlimited stream bound %d", o.maxInput)
	}
	// Only a finite Compute needs a control: Input and Depth are checked without one.
	if !limitsDisabled(sxirruntime.Limits{}) || !limitsDisabled(sxirruntime.Limits{Compute: sxirruntime.Unlimited, Input: sxirruntime.Unlimited}) || !limitsDisabled(sxirruntime.Limits{Input: 1, Depth: 1}) || limitsDisabled(sxirruntime.Limits{Compute: 1}) {
		t.Error("disabled")
	}
}
