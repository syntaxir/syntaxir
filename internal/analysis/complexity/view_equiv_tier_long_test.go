//go:build sxir_long

package complexity_test

import "testing"

// equivTierOf returns the inputs of TestViewEquivalence in the long tier (go test -tags sxir_long):
// more of every kind than the default tier, unless -short is given.
func equivTierOf() equivTier {
	if testing.Short() {
		return equivTier{startInputs: 20, perRule: 3, unreached: 1, prefixed: 1, changes: 2, writtenChanges: 200, attempts: 5}
	}
	return equivTier{startInputs: 100, perRule: 12, unreached: 12, prefixed: 3, changes: 8, writtenChanges: 200, attempts: 40}
}
