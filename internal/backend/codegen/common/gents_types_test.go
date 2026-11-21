package common_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A join with the error member must not gain an open-node member, whether it is named by an alias
// or inferred for a field: both forms share one TypeScript union and one recipe.
func TestGeneratedTypedErrorJoinHasNoOpenMember(t *testing.T) {
	t.Parallel()
	a := types.NamedTy{Name: "A", Kind: 's'}
	open := func(alts ...types.Ty) bool {
		u := types.JoinTy{Alts: alts}
		_, open := common.JoinNodeMembers(u)
		return common.NodeJoin(u) && open
	}
	if open(a, types.TyErr) || !common.NodeJoin(types.JoinTy{Alts: []types.Ty{a, types.TyErr}}) {
		t.Error("the error member counts as an open node, or A | error has no union")
	}
	if common.NodeJoin(types.JoinTy{Alts: []types.Ty{a, types.TyInt}}) {
		t.Error("a join with a scalar member must keep its open type")
	}
	// The raw spelling of node[_] is an open member, with or without error next to it.
	if !open(a, types.NodeTy{Value: types.TyTop}) {
		t.Error("a raw node[_] member is no open node")
	}
	if !open(a, types.NodeTy{Value: types.TyTop}, types.TyErr) {
		t.Error("a raw node[_] member next to error is no open node")
	}
	src := `type Block = { Body []Stmt }
type Let = { N string }
type Stmt ~ Let | Block | error
rule main Block = body:stmts => Block{Body: $body}
rule stmts = ss:(s:stmt)* => map($ss, (x) => x.$s)
rule stmt Stmt = s:(let / block) #recover(skip=[^;{}]+ ";"?) => $s
rule block Block = "{" body:stmts "}" => Block{Body: $body}
rule let Let = n:([a-z]+) ";" => Let{N: text($n)}`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := codegentest.GenerateTS(g, common.GenOptions{Types: true, Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "types.ts" && strings.Contains(string(f.Content), "Block | TNode<unknown>") {
			t.Errorf("types.ts adds an open node to the join with error:\n%s", f.Content)
		}
		if f.Path == "parser.ts" && strings.Contains(string(f.Content), "fallback:") {
			t.Errorf("parser.ts has an open-node fallback for the join with error")
		}
	}
	goFiles, err := codegentest.Generate(t, g, common.GenOptions{Types: true, Start: "main", Module: "example.com/m", Package: "p"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range goFiles {
		if strings.Contains(string(f.Content), "TNode[any]) isStmt") {
			t.Errorf("%s makes the open node a member of the join with error", f.Path)
		}
	}
}
