package compile_test

import (
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend"
)

func init() { compile.CheckGrammar = frontend.Check }
