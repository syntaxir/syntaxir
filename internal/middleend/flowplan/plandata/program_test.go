package plandata

import (
	"maps"
	"testing"
)

// TestReachText checks that the text of a reach (ReachFeature), with which a pipeline text provides
// it, reads back as the reach it was written from: none, an empty reach and entries of slots and
// wrappers in tables whose names hold no dot.
func TestReachText(t *testing.T) {
	for _, r := range []map[Entry]bool{
		nil,
		{},
		{{Table: "rules", ID: 0}: true, {Table: "rules", ID: 12, Wrapper: true}: true, {Table: "recRules", ID: 3}: true},
	} {
		text := formatReach(r)
		got, err := parseReach(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if (got == nil) != (r == nil) || !maps.Equal(got, r) {
			t.Errorf("%q reads as %v, not %v", text, got, r)
		}
	}
	if got := formatReach(map[Entry]bool{{Table: "rules", ID: 2}: true, {Table: "rules", ID: 10, Wrapper: true}: true}); got != "rules.10/wrapper rules.2" {
		t.Errorf("the reach is written %q", got)
	}
	for _, bad := range []string{"rules", "rules.x", ".1", "rules.-1"} {
		if _, err := parseReach(bad); err == nil {
			t.Errorf("%q reads as a reach", bad)
		}
	}
}
