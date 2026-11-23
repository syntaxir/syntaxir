package archtest

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// flowPassPackage holds the passes of the Flow stage.
const flowPassPackage = modulePath + "/internal/middleend/flowpass"

// coreIRPackage is Core IR.
const coreIRPackage = modulePath + "/internal/ir/core"

// TestFlowPassImportsNoCoreIR checks, in every build, that the Flow passes reach no Core IR through
// their imports: they rewrite Code IR from what it holds, the immediates of its operations, the tables
// of its module and the forms that the lowering recorded, as rule 2 holds Code IR
// (internal/ir/code/**) itself to.
func TestFlowPassImportsNoCoreIR(t *testing.T) {
	for _, g := range loadAll(t) {
		for _, msg := range flowPassImportingCoreIR(t, g) {
			t.Error(msg)
		}
	}
}

// TestFlowPassImportsNoCoreIRControl checks that the check fires for Flow passes that import Core IR,
// directly or through the Core passes.
func TestFlowPassImportsNoCoreIRControl(t *testing.T) {
	for _, imp := range []string{coreIRPackage, corePassPackage} {
		g := loadAll(t)[0].clone()
		g.pkgs[flowPassPackage].Imports = append(g.pkgs[flowPassPackage].Imports, imp)
		msgs := flowPassImportingCoreIR(t, g)
		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.Contains(m, "internal/middleend/flowpass reaches Core IR") }) {
			t.Errorf("Flow passes that import %s pass the check: %q", imp, msgs)
		}
	}
}

// flowPassImportingCoreIR reports the shortest chain of imports from the Flow passes to Core IR in g.
func flowPassImportingCoreIR(t *testing.T, g *graph) []string {
	t.Helper()
	if _, ok := g.pkgs[flowPassPackage]; !ok {
		t.Fatalf("build %q: no package %s", g.tags, flowPassPackage)
	}
	if chain := g.shortestChain([]string{flowPassPackage}, coreIRPackage, func(a, b string) bool { return inModule(b) }); chain != nil {
		return []string{fmt.Sprintf("build %q: internal/middleend/flowpass reaches Core IR: %s", g.tags, strings.Join(chain, " -> "))}
	}
	return nil
}
