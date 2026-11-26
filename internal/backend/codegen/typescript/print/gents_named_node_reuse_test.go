package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestGeneratedTSNamedNodeReuse checks that a generated TypeScript parser keeps the children and the
// fields of a node of a named node type that the rule body makes: the rule's own value is converted
// in place (ownedTyped takes over the children and fields of the fresh node). The named types are
// declared with `=`, the form that makes a rule's node type (a `~` declaration is an alias with no
// name). Each tree equals the engine's, with both labeled captures of every pair.
func TestGeneratedTSNamedNodeReuse(t *testing.T) {
	t.Parallel()
	node := tooltest.RequireNode(t)
	cases := []codegentest.Case{{Name: "named node reuse", Src: `type Kw = node[string]
type Pair = node[_]
rule main = k:kw p:pair "!"
rule kw Kw = "if"
rule pair Pair = a:[a-b] b:[c-d]`, Inputs: []string{"ifac!", "ifbd!", "ifa!", "if"}}}
	codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{Types: true, SourceInfo: true})
}
