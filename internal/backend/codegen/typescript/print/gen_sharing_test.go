package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

func TestGeneratedUnsharedTSParsersMatchEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node := tooltest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, codegentest.Representative(codegentest.GenCorpus(t)), common.GenOptions{NoOpt: []string{"method-sharing", "literal-sharing"}})
}
