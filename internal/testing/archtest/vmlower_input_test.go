package archtest

import (
	"strings"
	"testing"
)

// vmlowerPackage is the bytecode lowering, which reads its input (vmlower.Input) and not the
// checked program of the compiler.
const vmlowerPackage = modulePath + "/internal/backend/vmlower"

// TestVMLowerReadsNoCompiler checks, in every build, that the bytecode lowering does not reach the
// checked program of the compiler (internal/frontend), directly or through another package. The lowering reads the run-time
// program and the Core IR source of a checked grammar (vmlower.Input), so it stays free of the
// compiler and its edge cannot come back by way of an import.
func TestVMLowerReadsNoCompiler(t *testing.T) {
	for _, g := range loadAll(t) {
		if _, ok := g.pkgs[vmlowerPackage]; !ok {
			t.Fatalf("build %q: no package vmlower", g.tags)
		}
		if chain := g.shortestChain([]string{vmlowerPackage}, checkedProgramPackage, func(a, b string) bool { return inModule(b) }); chain != nil {
			t.Errorf("build %q: vmlower reaches the compiler: %s", g.tags, strings.Join(chain, " -> "))
		}
	}
}
