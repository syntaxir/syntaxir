//go:build sxir_long

package print_test

// The sizes of the long inputs of the stream oracle cases in the long tier; see
// stream_oracle_sizes_test.go.
const (
	oracleLongLines     = 400
	oracleLongItems     = 600
	oracleLongLookahead = 2000
)

// oracleMemoPruneLookahead is the length of the memo prune input; the long tier runs the full
// lookahead input for every mutant, so this is the same length. See stream_oracle_sizes_test.go.
const oracleMemoPruneLookahead = 2000
