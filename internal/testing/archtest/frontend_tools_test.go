package archtest

import (
	"slices"
	"strings"
	"testing"
)

// frontEndTools are the packages that work on a grammar without running a parse of their own, and
// so link no part of the runtime that runs parses: the linter, the sampler (which runs the parses
// of the parser it is given through runtime.Parser, and reads its grammar through
// state.ParserSource), the language server with what the driver gives it, the checked program, and
// the entry of the generators. The generators of package compiler share that package with the
// parsers it makes on the VM, so their engine-free entry is internal/driver/generate.
var frontEndTools = []string{
	"internal/driver/checked",
	"internal/driver/generate",
	"internal/driver/tooling",
	"internal/lsp/sxirls",
	"pkg/lint",
	"pkg/sample",
}

// parseRuntime are the packages that run parses: the engine, the bytecode VM and the incremental
// Document.
var parseRuntime = []string{
	"internal/runtime/document",
	"internal/runtime/engine",
	"internal/runtime/vm",
}

// frontEndToolsReachingRuntime returns, for the build g, a message for each package of
// frontEndTools that reaches a package of parseRuntime, with the shortest chain that reaches it.
func frontEndToolsReachingRuntime(t *testing.T, g *graph) []string {
	t.Helper()
	var msgs []string
	for _, rel := range frontEndTools {
		from := modulePath + "/" + rel
		if _, ok := g.pkgs[from]; !ok {
			t.Fatalf("build %q: no package %s", g.tags, rel)
		}
		for _, r := range parseRuntime {
			if chain := g.shortestChain([]string{from}, modulePath+"/"+r, func(a, b string) bool { return inModule(b) }); chain != nil {
				msgs = append(msgs, rel+" links "+r+": "+strings.Join(chain, " -> "))
			}
		}
	}
	return msgs
}

// TestFrontEndToolsLinkNoParseRuntime checks, in every build, that no package of frontEndTools
// reaches the engine, the VM or the Document, directly or through another package. It holds no
// exceptions.
func TestFrontEndToolsLinkNoParseRuntime(t *testing.T) {
	for _, g := range loadAll(t) {
		for _, m := range frontEndToolsReachingRuntime(t, g) {
			t.Errorf("build %q: %s", g.tags, m)
		}
	}
	// Negative control: an import of package compiler by the linter, and of the engine by the
	// generator entry, must each be found with its chain.
	g := loadAll(t)[0].clone()
	lint, gen := modulePath+"/pkg/lint", modulePath+"/internal/driver/generate"
	g.pkgs[lint].Imports = append(g.pkgs[lint].Imports, modulePath+"/pkg/compiler")
	g.pkgs[gen].Imports = append(g.pkgs[gen].Imports, modulePath+"/internal/runtime/engine")
	msgs := frontEndToolsReachingRuntime(t, g)
	for _, want := range []string{
		"pkg/lint links internal/runtime/engine: " + lint + " -> " + modulePath + "/pkg/compiler -> ",
		"internal/driver/generate links internal/runtime/engine: " + gen + " -> " + modulePath + "/internal/runtime/engine",
	} {
		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.HasPrefix(m, want) }) {
			t.Errorf("negative control: no message starting %q in %q", want, msgs)
		}
	}
}
