package print_test

import (
	"os/exec"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestMemberAccessOnJoinGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	codegentest.TSParsersCorpus(t, node, append(codegentest.JoinMemberCases(), codegentest.TypedJoinMemberCases()...), common.GenOptions{})
}
