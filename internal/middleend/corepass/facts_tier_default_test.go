//go:build !sxir_long

package corepass_test

// factsSeeds is the number of random grammars TestFactsRandom checks: few in the default
// tier, many in the long tier (go test -tags sxir_long).
const factsSeeds = 300
