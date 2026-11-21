package print_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A join of struct types, node types and error has an interface whatever the values of its node
// members: each member has its own Go type and implements the interface.
func TestGeneratedTypedJoinMemberTypes(t *testing.T) {
	t.Parallel()
	types, err := codegentest.GoTypesOf(t, codegentest.JoinMemberGrammar)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type J interface{ isJ() }",
		"type K interface{ isK() }",
		"A []J",
		"C []K",
		"B ItemOrItemListNode",
		"func (*TNode[T]) isJ() {}",
		"func (*TNode[T]) isK() {}",
		"func (*Item) isJ() {}",
		"func (*Error) isJ() {}",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("types.go lacks %q:\n%s", want, types)
		}
	}
}

// The values of each member convert alike on the direct and the converting path, and as the engine
// gives them.
func TestGeneratedTypedJoinMemberValues(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedTypedJoinMemberValues)
}

func testGeneratedTypedJoinMemberValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := []codegentest.Case{{Name: "join members", Src: codegentest.JoinMemberGrammar, Inputs: codegentest.JoinMemberInputs}}
	codegentest.GeneratedTypesCorpus(t, goBin, cases, common.GenOptions{ConvertTypes: true}, len(cases))
}
