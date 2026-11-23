package archtest

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// checkedProgramPackage holds the checked program of the compiler (frontend.Result): the semantic
// model of a checked grammar with its site numbers and run-time program, which the drivers hand
// the generators, the backends and the runtime only as the views and inputs they read.
const checkedProgramPackage = modulePath + "/internal/frontend"

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

// modelReaders are the packages outside the front end that read the checked grammar through its
// semantic model (check.Model), its checker and the facts of its Core IR, not through the checked
// program, which only the drivers read.
var modelReaders = []string{
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
// package common (common.Program), which internal/driver/checked builds from the checked program.
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
// imports internal/driver/checked, which builds the generators' view from the checked program: the
// generators take the view it builds, never the package that builds it.
func TestGeneratorsReadNoCheckedProgramControl(t *testing.T) {
	g := loadAll(t)[0].clone()
	from, via := modulePath+"/internal/backend/codegen/golang/print", modulePath+"/internal/driver/checked"
	if _, ok := g.pkgs[via]; !ok {
		t.Fatalf("no package %s", via)
	}
	g.pkgs[from].Imports = append(g.pkgs[from].Imports, via)
	msgs := generatorsReachingCheckedProgram(t, g)
	if !slices.ContainsFunc(msgs, func(m string) bool {
		return strings.Contains(m, "internal/backend/codegen/golang/print reaches the checked program: "+from+" -> "+via+" -> ")
	}) {
		t.Errorf("a generator that imports internal/driver/checked passes the check: %q", msgs)
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
