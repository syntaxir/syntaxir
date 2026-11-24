//go:build !sxir_long

package grammar_test

// The default tier checks a few thousand grammars of each kind in a few seconds.
var roundTripRuns = []roundTripRun{
	{name: "plain", seed: 1, n: 2000, depth: 4},
	{name: "blocks", seed: 2, n: 2000, depth: 4, blocks: true},
	{name: "wide", seed: 3, n: 5000, depth: 4, wide: true},
}
