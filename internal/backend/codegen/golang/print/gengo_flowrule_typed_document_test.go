package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestGenerateDirectTypedDocument(t *testing.T) {
	t.Parallel()
	codegentest.DirectTypedDocument(t, common.GenOptions{})
}
