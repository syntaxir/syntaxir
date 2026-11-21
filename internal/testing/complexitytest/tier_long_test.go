//go:build sxir_long

package complexitytest

import "testing"

// reduced reports whether a test measures the reduced sizes and counts of its series: the long tier
// (go test -tags sxir_long) measures the full ones, unless -short is given.
func reduced() bool { return testing.Short() }
