package archtest

import (
	"slices"
	"strings"
	"testing"
)

// vmPackage is the public entry of a loaded parser: it loads a compiled grammar file and runs it on
// the bytecode VM.
const vmPackage = modulePath + "/pkg/runtime/vm"

// vmDepsOutsideRuntime returns, for the build g, a message for each package that the transitive
// dependencies of the VM side (pkg/runtime/vm and every package under internal/runtime) reach
// outside the runtime side, with the chain that reaches it: a package of the module that the
// release layout does not place on the runtime side, or a package of another module. The
// dependencies are those that `go list -deps` lists, of non-test files.
func vmDepsOutsideRuntime(g *graph) []string {
	roots := []string{vmPackage}
	for path := range g.pkgs {
		if r, ok := strings.CutPrefix(path, modulePath+"/"); ok && strings.HasPrefix(r, "internal/runtime/") {
			roots = append(roots, path)
		}
	}
	slices.Sort(roots[1:])
	seen := map[string]bool{}
	for _, r := range roots {
		seen[r] = true
	}
	var msgs []string
	queue := slices.Clone(roots)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		p := g.pkgs[n]
		if p == nil {
			continue
		}
		for _, m := range p.Imports {
			if seen[m] {
				continue
			}
			seen[m] = true
			q := g.pkgs[m]
			if q != nil && q.Standard {
				continue
			}
			if inModule(m) {
				if pl, err := placeOf(rel(m)); err == nil && runtimeSide(pl.target) {
					queue = append(queue, m)
					continue
				}
			}
			chain := g.shortestChain(roots, m, func(a, b string) bool { return true })
			msgs = append(msgs, "the VM side links "+m+": "+strings.Join(chain, " -> "))
		}
	}
	slices.Sort(msgs)
	return msgs
}

// TestVMLinksOnlyRuntime checks, in every build, that a program that loads and runs a compiled
// grammar links no package of the compiler side: the transitive dependencies of pkg/runtime/vm and
// of internal/runtime/** are the standard library and the runtime side only, so neither the front
// end, the grammar AST (pkg/grammar/...), Core IR nor the lowerings are linked. Rule 1 holds the
// same for each edge, but its allowlist can hold one; this check holds no exceptions.
func TestVMLinksOnlyRuntime(t *testing.T) {
	for _, g := range loadAll(t) {
		if _, ok := g.pkgs[vmPackage]; !ok {
			t.Fatalf("build %q: no package %s", g.tags, vmPackage)
		}
		for _, m := range vmDepsOutsideRuntime(g) {
			t.Errorf("build %q: %s", g.tags, m)
		}
	}
	// Negative control: an import of the grammar AST by the VM's state, and of the compiler by the
	// run-time program, must each be found with its chain.
	g := loadAll(t)[0].clone()
	state, prog := modulePath+"/internal/runtime/state", modulePath+"/internal/runtime/program"
	g.pkgs[state].Imports = append(g.pkgs[state].Imports, modulePath+"/pkg/grammar/ast")
	g.pkgs[prog].Imports = append(g.pkgs[prog].Imports, checkedProgramPackage)
	msgs := vmDepsOutsideRuntime(g)
	for _, want := range []string{
		"the VM side links " + modulePath + "/pkg/grammar/ast: ",
		"the VM side links " + checkedProgramPackage + ": ",
	} {
		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.HasPrefix(m, want) }) {
			t.Errorf("negative control: no message starting %q in %q", want, msgs)
		}
	}
}
