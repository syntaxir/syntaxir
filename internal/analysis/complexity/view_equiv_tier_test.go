//go:build !sxir_long

package complexity_test

// equivTierOf returns the inputs of TestViewEquivalence in the default tier: enough to reach every
// rewrite of the views of the subjects (TestViewEquivalenceMutants checks that they do), with one
// input generated for a rule that parses of the start rule do not match, as generating costs the most.
func equivTierOf() equivTier {
	return equivTier{startInputs: 20, perRule: 3, unreached: 1, prefixed: 1, changes: 2, writtenChanges: 200, attempts: 5}
}
