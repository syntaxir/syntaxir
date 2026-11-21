package archtest

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// checkedProgramPackage holds the checked program of the compiler (compile.Program): the grammar
// AST, the semantic model and the rule facts that the generators and analyses read.
const checkedProgramPackage = modulePath + "/internal/compile"

// TestRuntimeReadsNoCheckedProgram checks, in every build, that no package of the internal runtime
// (internal/runtime/** and internal/runtime/engine, the run-time entries) reaches the checked program of the
// compiler, directly or through another package. The runtime runs the run-time program
// (internal/runtime/program) and reaches what the compiler builds only through state.Source. Rule 1
// forbids such an edge too, but the allowlist can hold one; this check holds no exceptions.
func TestRuntimeReadsNoCheckedProgram(t *testing.T) {
	for _, g := range loadAll(t) {
		var runtime []string
		for path := range g.pkgs {
			rel, ok := strings.CutPrefix(path, modulePath+"/")
			if ok && (strings.HasPrefix(rel, "internal/runtime/") || rel == "internal/runtime/engine") {
				runtime = append(runtime, path)
			}
		}
		if len(runtime) < 2 {
			t.Fatalf("build %q: found %d runtime packages, want the internal runtime", g.tags, len(runtime))
		}
		slices.Sort(runtime)
		for _, from := range runtime {
			if chain := g.shortestChain([]string{from}, checkedProgramPackage, func(a, b string) bool { return inModule(b) }); chain != nil {
				t.Errorf("build %q: %s reaches the checked program: %s", g.tags, from, strings.Join(chain, " -> "))
			}
		}
	}
}

// modelReaders are the packages outside the compiler that read the checked grammar through its
// semantic model (check.Model), its checker and the facts of its Core IR, not through the checked
// program. Only the drivers (internal/driver/checked) and the bytecode compiler
// (internal/backend/vmlower) are to keep reading it; the list grows as the other readers move.
var modelReaders = []string{
	"internal/backend/actionlower",
	"internal/backend/closure",
	"internal/frontend/schemaexport",
}

// TestModelReadersReadNoCheckedProgram checks, in every build, that no package of modelReaders
// reaches the checked program of the compiler, directly or through another package.
func TestModelReadersReadNoCheckedProgram(t *testing.T) {
	for _, g := range loadAll(t) {
		for _, rel := range modelReaders {
			from := modulePath + "/" + rel
			if _, ok := g.pkgs[from]; !ok {
				t.Fatalf("build %q: no package %s", g.tags, rel)
			}
			if chain := g.shortestChain([]string{from}, checkedProgramPackage, func(a, b string) bool { return inModule(b) }); chain != nil {
				t.Errorf("build %q: %s reaches the checked program: %s", g.tags, rel, strings.Join(chain, " -> "))
			}
		}
	}
}

// generatorReaders are the generator packages that read a checked grammar only through the view of
// package common (common.Program), which internal/driver/checked builds.
var generatorReaders = []string{
	"internal/backend/codegen/common",
	"internal/backend/codegen/golang/print",
	"internal/backend/codegen/typescript/print",
}

// TestGeneratorsReadNoCheckedProgram checks, in every build, that no package of generatorReaders
// reaches the checked program of the compiler, directly or through another package.
func TestGeneratorsReadNoCheckedProgram(t *testing.T) {
	for _, g := range loadAll(t) {
		for _, msg := range generatorsReachingCheckedProgram(t, g) {
			t.Error(msg)
		}
	}
}

// TestGeneratorsReadNoCheckedProgramControl checks that the check fires for a generator that
// imports internal/middleend/checkfacts, which hands the front end the rule facts in the types of
// the checked program: the facts reach the generators through corepass, never through checkfacts.
func TestGeneratorsReadNoCheckedProgramControl(t *testing.T) {
	g := loadAll(t)[0].clone()
	from, via := modulePath+"/internal/backend/codegen/golang/print", modulePath+"/internal/middleend/checkfacts"
	if _, ok := g.pkgs[via]; !ok {
		t.Fatalf("no package %s", via)
	}
	g.pkgs[from].Imports = append(g.pkgs[from].Imports, via)
	msgs := generatorsReachingCheckedProgram(t, g)
	if !slices.ContainsFunc(msgs, func(m string) bool {
		return strings.Contains(m, "internal/backend/codegen/golang/print reaches the checked program: "+from+" -> "+via+" -> ")
	}) {
		t.Errorf("a generator that imports checkfacts passes the check: %q", msgs)
	}
}

// generatorsReachingCheckedProgram reports each package of generatorReaders that reaches the
// checked program in g, with the shortest chain of imports.
func generatorsReachingCheckedProgram(t *testing.T, g *graph) []string {
	t.Helper()
	var msgs []string
	for _, rel := range generatorReaders {
		from := modulePath + "/" + rel
		if _, ok := g.pkgs[from]; !ok {
			t.Fatalf("build %q: no package %s", g.tags, rel)
		}
		if chain := g.shortestChain([]string{from}, checkedProgramPackage, func(a, b string) bool { return inModule(b) }); chain != nil {
			msgs = append(msgs, fmt.Sprintf("build %q: %s reaches the checked program: %s", g.tags, rel, strings.Join(chain, " -> ")))
		}
	}
	return msgs
}

// analysisPackages are the analyses and tools that check a grammar through the front end
// (frontend.Run, frontend.CheckTyped) and read the facts of its Core IR, not the checked program.
// The front end itself still imports the checked program, so the check is on direct imports.
var analysisPackages = []string{
	"internal/analysis/complexity",
	"internal/analysis/lint",
}

// TestAnalysesImportNoCheckedProgram checks, in every build, that no package of analysisPackages
// imports the checked program of the compiler directly.
func TestAnalysesImportNoCheckedProgram(t *testing.T) {
	for _, g := range loadAll(t) {
		for _, rel := range analysisPackages {
			p, ok := g.pkgs[modulePath+"/"+rel]
			if !ok {
				t.Fatalf("build %q: no package %s", g.tags, rel)
			}
			if slices.Contains(p.Imports, checkedProgramPackage) {
				t.Errorf("build %q: %s imports the checked program (%s) directly; take the checked grammar through internal/frontend", g.tags, rel, checkedProgramPackage)
			}
		}
		// Negative control: the front end imports the checked program, so the same test applied to
		// it must fire.
		if p, ok := g.pkgs[modulePath+"/internal/frontend"]; !ok || !slices.Contains(p.Imports, checkedProgramPackage) {
			t.Errorf("build %q: negative control: internal/frontend is expected to import the checked program", g.tags)
		}
	}
}
