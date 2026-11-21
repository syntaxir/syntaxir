package engine

import "testing"

// A name captured twice and read by the action of the rule has the value of the later capture, on
// every backend.
func TestDuplicateCaptureValueAcrossBackends(t *testing.T) {
	t.Parallel()
	check(t, `rule main = x:"a" x:"b" {text($x) == "b"} => $x`, ok("ab", `"b"`))
}
