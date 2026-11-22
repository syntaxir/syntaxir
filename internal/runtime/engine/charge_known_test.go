package engine

import "syntaxir.dev/sxir/pkg/bytecode"

// knownChargeDivergences lists the pairs of charge case and backend that diverge from the closure
// backend today, as "case|backend" ("case|*" for every backend), and as "case|table" the cases
// whose closure units differ from chargeTableUnits. The backends do not yet charge the
// abstract units that spec/runtime/parsing.md requires; each change that makes a backend charge
// them removes its pairs, and a new divergence fails the tests. The testdata grammars are not
// listed by backend yet.
var knownChargeDivergences = map[string]bool{
	"literal|table":                       true,
	"class repetition|table":              true,
	"class sequence|table":                true,
	"choice of sequences|table":           true,
	"repeat or class|table":               true,
	"rules|table":                         true,
	"lookahead|table":                     true,
	"cel|*":                               true,
	"csv|*":                               true,
	"cue|*":                               true,
	"duckdb|*":                            true,
	"golang|*":                            true,
	"json|*":                              true,
	"python|*":                            true,
	"typescript|*":                        true,
	"xml|*":                               true,
	"yaml|*":                              true,
	"captures|document edit":              true,
	"captures|document":                   true,
	"captures|generated Go default":       true,
	"captures|generated Go typed default": true,
	"captures|generated Go optimizations off":                    true,
	"captures|generated Go typed optimizations off":              true,
	"captures|generated TypeScript default":                      true,
	"captures|generated TypeScript optimizations off":            true,
	"choice of sequences|bytecode-iterative":                     true,
	"choice of sequences|bytecode":                               true,
	"choice of sequences|document edit":                          true,
	"choice of sequences|document":                               true,
	"choice of sequences|flow-iterative":                         true,
	"choice of sequences|flow":                                   true,
	"choice of sequences|generated Go default":                   true,
	"choice of sequences|generated Go typed default":             true,
	"choice of sequences|generated Go optimizations off":         true,
	"choice of sequences|generated Go typed optimizations off":   true,
	"choice of sequences|generated TypeScript default":           true,
	"choice of sequences|generated TypeScript optimizations off": true,
	"class repetition|document edit":                             true,
	"class repetition|document":                                  true,
	"class repetition|generated Go default":                      true,
	"class repetition|generated Go typed default":                true,
	"class repetition|generated Go optimizations off":            true,
	"class repetition|generated Go typed optimizations off":      true,
	"class repetition|generated TypeScript default":              true,
	"class repetition|generated TypeScript optimizations off":    true,
	"class sequence|document edit":                               true,
	"class sequence|document":                                    true,
	"class sequence|generated Go default":                        true,
	"class sequence|generated Go typed default":                  true,
	"class sequence|generated Go optimizations off":              true,
	"class sequence|generated Go typed optimizations off":        true,
	"class sequence|generated TypeScript default":                true,
	"class sequence|generated TypeScript optimizations off":      true,
	"cut in repetition|document edit":                            true,
	"cut in repetition|document":                                 true,
	"cut in repetition|generated Go default":                     true,
	"cut in repetition|generated Go typed default":               true,
	"cut in repetition|generated Go optimizations off":           true,
	"cut in repetition|generated Go typed optimizations off":     true,
	"cut in repetition|generated TypeScript default":             true,
	"cut in repetition|generated TypeScript optimizations off":   true,
	"left recursion|document edit":                               true,
	"left recursion|document":                                    true,
	"left recursion|generated Go default":                        true,
	"left recursion|generated Go typed default":                  true,
	"left recursion|generated Go optimizations off":              true,
	"left recursion|generated Go typed optimizations off":        true,
	"left recursion|generated TypeScript default":                true,
	"left recursion|generated TypeScript optimizations off":      true,
	"list action|generated Go default":                           true,
	"list action|generated Go typed default":                     true,
	"list action|generated Go optimizations off":                 true,
	"list action|generated Go typed optimizations off":           true,
	"list action|generated TypeScript default":                   true,
	"list action|generated TypeScript optimizations off":         true,
	"literal|document edit":                                      true,
	"literal|document":                                           true,
	"literal|generated Go default":                               true,
	"literal|generated Go typed default":                         true,
	"literal|generated Go optimizations off":                     true,
	"literal|generated Go typed optimizations off":               true,
	"literal|generated TypeScript default":                       true,
	"literal|generated TypeScript optimizations off":             true,
	"lookahead|document edit":                                    true,
	"lookahead|document":                                         true,
	"lookahead|generated Go default":                             true,
	"lookahead|generated Go typed default":                       true,
	"lookahead|generated Go optimizations off":                   true,
	"lookahead|generated Go typed optimizations off":             true,
	"lookahead|generated TypeScript default":                     true,
	"lookahead|generated TypeScript optimizations off":           true,
	"operators|generated Go default":                             true,
	"operators|generated Go typed default":                       true,
	"operators|generated Go optimizations off":                   true,
	"operators|generated Go typed optimizations off":             true,
	"operators|generated TypeScript default":                     true,
	"operators|generated TypeScript optimizations off":           true,
	"recover skip|generated Go default":                          true,
	"recover skip|generated Go typed default":                    true,
	"recover skip|generated Go optimizations off":                true,
	"recover skip|generated Go typed optimizations off":          true,
	"recover skip|generated TypeScript default":                  true,
	"recover skip|generated TypeScript optimizations off":        true,
	"recovery|document edit":                                     true,
	"recovery|document":                                          true,
	"recovery|generated Go default":                              true,
	"recovery|generated Go typed default":                        true,
	"recovery|generated Go optimizations off":                    true,
	"recovery|generated Go typed optimizations off":              true,
	"recovery|generated TypeScript default":                      true,
	"recovery|generated TypeScript optimizations off":            true,
	"repeat or class|document edit":                              true,
	"repeat or class|document":                                   true,
	"repeat or class|generated Go default":                       true,
	"repeat or class|generated Go typed default":                 true,
	"repeat or class|generated Go optimizations off":             true,
	"repeat or class|generated Go typed optimizations off":       true,
	"repeat or class|generated TypeScript default":               true,
	"repeat or class|generated TypeScript optimizations off":     true,
	"rules|document edit":                                        true,
	"rules|document":                                             true,
	"rules|generated Go default":                                 true,
	"rules|generated Go typed default":                           true,
	"rules|generated Go optimizations off":                       true,
	"rules|generated Go typed optimizations off":                 true,
	"rules|generated TypeScript default":                         true,
	"rules|generated TypeScript optimizations off":               true,
	"text action|document edit":                                  true,
	"text action|document":                                       true,
	"text action|generated Go default":                           true,
	"text action|generated Go typed default":                     true,
	"text action|generated Go optimizations off":                 true,
	"text action|generated Go typed optimizations off":           true,
	"text action|generated TypeScript default":                   true,
	"text action|generated TypeScript optimizations off":         true,
}

// knownGuardReadDivergence reports whether the units vm that the bytecode VM charged for a parse
// of a grammar whose module is m may differ from the units closure that the closure backend charged
// by the one divergence of the reads of a choice's first-character guards: the closure backend
// charges one read for the guards of a choice together (state.ChoiceRead), and the VM one for each
// guard it tests (GUARD, GUARDLIT and their jumps), so the VM charges more than the closure for a
// choice whose second guard or a later one is tested. It holds when a Flow body of m tests two
// guards or more and the VM charged more units than the closure backend, never fewer.
// TestKnownGuardReadDivergence pins the divergence on its smallest grammars.
//
// Owner: #310, whose contract counts a guard no unit on every backend; the change that makes the
// backends charge that removes this entry and its pins.
func knownGuardReadDivergence(m *bytecode.Module, closure, vm uint64) bool {
	if vm <= closure {
		return false
	}
	for _, f := range m.Flows {
		guards := 0
		for _, in := range f.Code {
			switch in.Op {
			case bytecode.FlowGuard, bytecode.FlowGuardLit, bytecode.FlowGuardJump, bytecode.FlowGuardLitJump:
				guards++
			}
		}
		if guards >= 2 {
			return true
		}
	}
	return false
}
