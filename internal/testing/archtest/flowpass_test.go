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

// TestFlowPassImportsNoCoreIR checks, in every build, that the Flow passes do not import Core IR:
// they rewrite Code IR from what it holds, the immediates of its operations, the tables of its module
// and the forms that the lowering recorded, as rule 2 holds Code IR (internal/ir/code/**) itself to.
// They import internal/middleend/corepass for the name of the pass whose guards guarded-classes
// depends on, which links Core IR without reading it.
func TestFlowPassImportsNoCoreIR(t *testing.T) {
	for _, g := range loadAll(t) {
		for _, msg := range flowPassImportingCoreIR(t, g) {
			t.Error(msg)
		}
	}
}

// TestFlowPassImportsNoCoreIRControl checks that the check fires for Flow passes that import Core IR.
func TestFlowPassImportsNoCoreIRControl(t *testing.T) {
	g := loadAll(t)[0].clone()
	g.pkgs[flowPassPackage].Imports = append(g.pkgs[flowPassPackage].Imports, coreIRPackage)
	msgs := flowPassImportingCoreIR(t, g)
	if !slices.ContainsFunc(msgs, func(m string) bool { return strings.Contains(m, "internal/middleend/flowpass imports Core IR") }) {
		t.Errorf("Flow passes that import Core IR pass the check: %q", msgs)
	}
}

// flowPassImportingCoreIR reports a direct import of Core IR by the Flow passes in g.
func flowPassImportingCoreIR(t *testing.T, g *graph) []string {
	t.Helper()
	p, ok := g.pkgs[flowPassPackage]
	if !ok {
		t.Fatalf("build %q: no package %s", g.tags, flowPassPackage)
	}
	if slices.Contains(p.Imports, coreIRPackage) {
		return []string{fmt.Sprintf("build %q: internal/middleend/flowpass imports Core IR (%s)", g.tags, coreIRPackage)}
	}
	return nil
}
