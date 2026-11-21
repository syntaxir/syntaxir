package archtest

import (
	"strings"
	"testing"
)

// vmlowerPackage is the bytecode lowering, which reads its input (vmlower.Input) and not the
// checked program of the compiler.
const vmlowerPackage = modulePath + "/internal/backend/vmlower"

// TestVMLowerReadsNoCompiler checks, in every build, that the bytecode lowering does not reach the
// compiler (internal/compile), directly or through another package. The driver adapts the checked
// program to the lowering's input (internal/driver/checked.VMInput), so the lowering stays free of
// the compiler and its edge cannot come back by way of an import.
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
