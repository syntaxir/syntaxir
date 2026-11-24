//go:build sxir_long

package grammar_test

// The long tier checks more and deeper grammars, the runs of the default tier among them.
var roundTripRuns = []roundTripRun{
	{name: "plain", seed: 1, n: 60000, depth: 4},
	{name: "blocks", seed: 2, n: 60000, depth: 4, blocks: true},
	{name: "wide", seed: 3, n: 60000, depth: 4, wide: true},
	{name: "wide shallow", seed: 4, n: 60000, depth: 3, wide: true},
	{name: "wide deep", seed: 5, n: 60000, depth: 6, wide: true},
}
