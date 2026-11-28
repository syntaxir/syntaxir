//go:build !sxir_long

package print_test

// The sizes of the long inputs of the stream oracle cases. The default tier runs every mutant on
// these shrunk inputs: each cycle of a mutant is the cost of the input, and the discard rule of
// streamDiscard depends on the chunk size and not on the input length, so a shorter input still
// discards. The long tier (sxir_long) runs the full inputs in stream_oracle_sizes_long_test.go.
const (
	oracleLongLines     = 50  // lines of the lines case, twice
	oracleLongItems     = 75  // items of the items case, twice
	oracleLongLookahead = 250 // tokens of the lookahead case, except for memo prune
)

// oracleMemoPruneLookahead is the length of the lookahead input that the memo prune mutant runs in
// every tier. It is the only mutant that a shorter lookahead input does not kill: at 250 tokens the
// memo is never pruned with a stale entry, so the mutant survives. The other mutants keep the
// shrunk input.
const oracleMemoPruneLookahead = 2000
