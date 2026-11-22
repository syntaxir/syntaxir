package archtest

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// corePassPackage computes the rule facts and the site numbers of a grammar on its Core IR.
const corePassPackage = modulePath + "/internal/middleend/corepass"

// grammarASTPackage is the grammar AST, which the Core IR names as the origin of its nodes.
const grammarASTPackage = modulePath + "/pkg/grammar/ast"

// isFrontEnd reports whether p is a package of the front end.
func isFrontEnd(p string) bool {
	return p == modulePath+"/internal/frontend" || strings.HasPrefix(p, modulePath+"/internal/frontend/")
}

// TestCorePassReadsNoFrontEnd checks, in every build, that the Core passes read and produce Core
// IR only: they reach no package of the front end, directly or through another package, and do
// not import the grammar AST themselves (the Core IR and internal/cancel do, for the origins of
// nodes and the rules of a grammar). The facts reach the checker through
// internal/middleend/checkfacts, the one package that maps them to its types and to positions in
// the grammar.
func TestCorePassReadsNoFrontEnd(t *testing.T) {
	for _, g := range loadAll(t) {
		for _, msg := range corePassReachingFrontEnd(t, g) {
			t.Error(msg)
		}
	}
}

// TestCorePassReadsNoFrontEndControl checks that the check fires for Core passes that import the
// checker, and for Core passes that import the grammar AST.
func TestCorePassReadsNoFrontEndControl(t *testing.T) {
	for _, c := range []struct{ via, want string }{
		{modulePath + "/internal/frontend/check", "internal/middleend/corepass reaches the front end: " + corePassPackage + " -> " + modulePath + "/internal/frontend/check"},
		{grammarASTPackage, "internal/middleend/corepass imports the grammar AST"},
	} {
		g := loadAll(t)[0].clone()
		if _, ok := g.pkgs[c.via]; !ok {
			t.Fatalf("no package %s", c.via)
		}
		g.pkgs[corePassPackage].Imports = append(g.pkgs[corePassPackage].Imports, c.via)
		msgs := corePassReachingFrontEnd(t, g)
		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.Contains(m, c.want) }) {
			t.Errorf("Core passes that import %s pass the check: %q", c.via, msgs)
		}
	}
}

// corePassReachingFrontEnd reports each package of the front end that the Core passes reach in g,
// with the shortest chain of imports, and a direct import of the grammar AST.
func corePassReachingFrontEnd(t *testing.T, g *graph) []string {
	t.Helper()
	p, ok := g.pkgs[corePassPackage]
	if !ok {
		t.Fatalf("build %q: no package %s", g.tags, corePassPackage)
	}
	var msgs []string
	if slices.Contains(p.Imports, grammarASTPackage) {
		msgs = append(msgs, fmt.Sprintf("build %q: internal/middleend/corepass imports the grammar AST (%s)", g.tags, grammarASTPackage))
	}
	for q := range g.pkgs {
		if !isFrontEnd(q) {
			continue
		}
		if chain := g.shortestChain([]string{corePassPackage}, q, func(a, b string) bool { return inModule(b) }); chain != nil {
			msgs = append(msgs, fmt.Sprintf("build %q: internal/middleend/corepass reaches the front end: %s", g.tags, strings.Join(chain, " -> ")))
		}
	}
	slices.Sort(msgs)
	return msgs
}
