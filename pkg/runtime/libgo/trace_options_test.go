package libgo

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"
)

// observeCalls reports a fixed sequence of calls to the observation that opts select: main calls
// item, which calls pair (calling word twice) and skips word; main then calls item again, which
// calls word.
func observeCalls(opts ...sxirruntime.ParseOption) {
	var o parseOptions
	if err := o.applyOptions(opts); err != nil {
		panic(err)
	}
	silent := 0
	ob := newObservation(&o.observe, &silent)
	if ob == nil {
		return
	}
	defer ob.release()
	ob.Enter("main", 0, 0)
	ob.Enter("item", 0, 0)
	ob.Enter("pair", 0, 0)
	ob.Enter("word", 0, 0)
	ob.Exit(true, 1, 2)
	ob.Enter("word", 2, 0)
	ob.Exit(true, 3, 4)
	ob.Exit(true, 3, 4)
	ob.Skipped("word", 3, 0)
	ob.Exit(true, 3, 4)
	ob.Enter("item", 3, 0)
	ob.Enter("word", 3, 0)
	ob.Exit(false, 3, 4)
	ob.Exit(false, 3, 4)
	ob.Exit(true, 3, 4)
}

// eventLog records events as "kind rule depth".
type eventLog []string

func (l *eventLog) trace(e sxirinstrument.TraceEvent) {
	*l = append(*l, fmt.Sprintf("%v %s %d", e.Kind, e.Rule, e.Depth))
}

// TestTraceOptionSelection checks what the trace functions receive with WithTraceFilter and
// WithTraceMaxDepth, of which the last of each applies.
func TestTraceOptionSelection(t *testing.T) {
	for _, c := range []struct {
		name string
		opts []sxirruntime.ParseOption
		want string
	}{
		{"all", nil, "enter main 1,enter item 2,enter pair 3,enter word 4,exit word 4,enter word 4,exit word 4,exit pair 3,skipped word 3,exit item 2,enter item 2,enter word 3,exit word 3,exit item 2,exit main 1"},
		{"pair and nested", []sxirruntime.ParseOption{sxirruntime.WithTraceFilter("pair")},
			"enter pair 3,enter word 4,exit word 4,enter word 4,exit word 4,exit pair 3"},
		{"skipped and called", []sxirruntime.ParseOption{sxirruntime.WithTraceFilter("word", "no such rule")},
			"enter word 4,exit word 4,enter word 4,exit word 4,skipped word 3,enter word 3,exit word 3"},
		{"depth", []sxirruntime.ParseOption{sxirruntime.WithTraceMaxDepth(2)},
			"enter main 1,enter item 2,exit item 2,enter item 2,exit item 2,exit main 1"},
		{"filter and depth", []sxirruntime.ParseOption{sxirruntime.WithTraceFilter("item"), sxirruntime.WithTraceMaxDepth(3)},
			"enter item 2,enter pair 3,exit pair 3,skipped word 3,exit item 2,enter item 2,enter word 3,exit word 3,exit item 2"},
		{"last filter and depth", []sxirruntime.ParseOption{sxirruntime.WithTraceFilter("main"), sxirruntime.WithTraceMaxDepth(1), sxirruntime.WithTraceFilter("pair"), sxirruntime.WithTraceMaxDepth(3)},
			"enter pair 3,exit pair 3"},
		{"no depth limit", []sxirruntime.ParseOption{sxirruntime.WithTraceMaxDepth(1), sxirruntime.WithTraceMaxDepth(0), sxirruntime.WithTraceFilter("no such rule"), sxirruntime.WithTraceFilter()},
			"enter main 1,enter item 2,enter pair 3,enter word 4,exit word 4,enter word 4,exit word 4,exit pair 3,skipped word 3,exit item 2,enter item 2,enter word 3,exit word 3,exit item 2,exit main 1"},
		{"unknown rule", []sxirruntime.ParseOption{sxirruntime.WithTraceFilter("no such rule")}, ""},
	} {
		var got eventLog
		var prof sxirinstrument.Profile
		observeCalls(append(c.opts, sxirruntime.WithTrace(got.trace), sxirruntime.WithProfile(&prof))...)
		if s := strings.Join(got, ","); s != c.want {
			t.Errorf("%s: got\n%s\nwant\n%s", c.name, s, c.want)
		}
		if snap := prof.Snapshot(); snap.Entered != 7 || snap.Skipped != 1 {
			t.Errorf("%s: the profile counts %d calls and %d skipped, want 7 and 1", c.name, snap.Entered, snap.Skipped)
		}
	}
}

// TestTraceOptionComposition checks that several WithTrace and WithProfile options each receive the
// whole parse in order, that nil ones are ignored, and that the parses of a Document do not share the
// profiles they add to its defaults.
func TestTraceOptionComposition(t *testing.T) {
	var all, a, b eventLog
	observeCalls(sxirruntime.WithTrace(all.trace))
	var order []string
	var pa, pb sxirinstrument.Profile
	observeCalls(sxirruntime.WithTrace(a.trace), sxirruntime.WithTrace(nil), sxirruntime.WithProfile(&pa), sxirruntime.WithProfile(nil), sxirruntime.WithTrace(func(sxirinstrument.TraceEvent) { order = append(order, "a") }),
		sxirruntime.WithTrace(b.trace), sxirruntime.WithProfile(&pb), sxirruntime.WithTrace(func(sxirinstrument.TraceEvent) { order = append(order, "b") }))
	if !slices.Equal(a, all) || !slices.Equal(b, all) {
		t.Errorf("trace functions received\n%v\n%v\nwant\n%v", a, b, all)
	}
	if len(order) != 2*len(all) || order[0] != "a" || order[1] != "b" {
		t.Errorf("trace functions called in the order %v", order)
	}
	if sa, sb := pa.Snapshot(), pb.Snapshot(); sa.Parses != 1 || sb.Parses != 1 || sa.Entered != sb.Entered || sa.Entered != 7 {
		t.Errorf("profiles recorded %d and %d parses, %d and %d calls", sa.Parses, sb.Parses, sa.Entered, sb.Entered)
	}

	// The defaults of a Document are copied for each parse, which may add profiles.
	var defaults parseOptions
	var p1, p2, p3, p4, pc sxirinstrument.Profile
	for _, pr := range []*sxirinstrument.Profile{&p1, &p2, &p3, &p4} {
		if err := defaults.applyOptions([]sxirruntime.ParseOption{sxirruntime.WithProfile(pr)}); err != nil {
			t.Fatal(err)
		}
	}
	first, second := defaults, defaults
	if err := first.applyOptions([]sxirruntime.ParseOption{sxirruntime.WithProfile(&pb)}); err != nil {
		t.Fatal(err)
	}
	if err := second.applyOptions([]sxirruntime.ParseOption{sxirruntime.WithProfile(&pc)}); err != nil {
		t.Fatal(err)
	}
	if len(defaults.observe.extras.more) != 3 || len(first.observe.extras.more) != 4 || len(second.observe.extras.more) != 4 {
		t.Fatal("profiles added to a copy of the defaults")
	}
	if first.observe.extras.more[3] != &pb || second.observe.extras.more[3] != &pc {
		t.Error("a parse's profile was replaced by one that another parse added to the defaults")
	}
}
