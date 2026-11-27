package corepass

import "syntaxir.dev/sxir/internal/pass"

// The Core passes register their flags under the names of the passes.
func init() {
	pass.Register(
		pass.Flag{Name: PassGuards, Kind: pass.KindCorePass, Default: 1, Doc: "skip an alternative when the next character cannot start it",
			Tradeoff:  "One read of the next character per choice against the attempts of alternatives that cannot match, and a little more code per choice",
			Mechanism: "M010", Record: 136, Backends: pass.Engines | pass.Generated,
			Witnesses: []string{"guards"}},
		pass.Flag{Name: PassScans, Kind: pass.KindCorePass, Default: 1, Doc: "a value-free repetition of a class or of any character runs as a scan",
			Tradeoff:  "A loop against the general repetition",
			Mechanism: "M040", Record: 136, Backends: pass.Engines | pass.Generated,
			Witnesses: []string{"scans"}},
	)
}
