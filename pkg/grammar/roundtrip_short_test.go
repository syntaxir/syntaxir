//go:build !sxir_long

package grammar_test

// The default tier checks a few thousand grammars of each kind in a few seconds.
var roundTripRuns = []roundTripRun{
	{name: "plain", seed: 1, n: 2000, depth: 4},
	{name: "blocks", seed: 2, n: 2000, depth: 4, blocks: true},
	{name: "wide", seed: 3, n: 5000, depth: 4, wide: true},
}

// nestingAgreementRules is the number of random rule bodies whose level count
// TestValidateNestingAgreesWithParser checks against the parser; each parses a
// text of about 500 levels twice.
const nestingAgreementRules = 6

// depthAllShapes selects every family of TestValidateBoundsDepth, rather than
// the chain in groups and the negative constant.
const depthAllShapes = false
