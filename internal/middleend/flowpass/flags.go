package flowpass

import "syntaxir.dev/sxir/internal/pass"

// The Flow passes register their flags under the names of the passes.
func init() {
	pass.Register(
		pass.Flag{Name: PassTerminals, Kind: pass.KindFlowPass, Default: 1, Doc: "value-free choices of many literals and classes run as one program (lean typed Go)",
			Tradeoff:  "Data against code: the choice becomes a table in the parser instead of nested alternatives",
			Mechanism: "M140", Record: 136, Backends: pass.On(pass.BackendTypedGo),
			Witnesses: []string{"terminals"}},
		pass.Flag{Name: PassGuardedClasses, Kind: pass.KindFlowPass, Default: 1, Doc: "a leading class of a guarded alternative consumes the character its guard read (ordinary Go parsers)",
			Tradeoff:  "Saves a second read and test of that character",
			Mechanism: "M010", Record: 136, Backends: pass.GeneratedGo,
			Witnesses: []string{"outlined keywords"}},
		pass.Flag{Name: PassClassRuns, Kind: pass.KindFlowPass, Default: 1, Doc: "consecutive classes of a value-free sequence run as one (lean typed Go)",
			Tradeoff:  "One run against one test and call per class",
			Mechanism: "M150", Record: 136, Backends: pass.On(pass.BackendTypedGo),
			Witnesses: []string{"class runs"}},
		pass.Flag{Name: PassClassTables, Kind: pass.KindFlowPass, Int: true, Default: 0, Doc: "match a class of at least this many ranges through a membership table (lean typed Go)",
			Tradeoff:  "`-opt class-tables=5` is the measured choice, and 0 turns the tables off. Table data against code: about 3% smaller source, and about 1% slower parsing on the language grammars (3.7% on TypeScript)",
			Mechanism: "M160", Record: 136, Backends: pass.On(pass.BackendTypedGo),
			Witnesses: []string{"class tables"}},
		pass.Flag{Name: PassInlineLiterals, Kind: pass.KindFlowPass, Int: true, Default: 4, Doc: "compare literals of at most this many code points in place first; 0 turns it off",
			Tradeoff:  "Code against calls: faster, with more code",
			Mechanism: "M130", Record: 136, Backends: pass.GeneratedGo,
			Witnesses: []string{"literals"}},
	)
}
