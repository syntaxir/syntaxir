// Package nolowering composes a pipeline that runs passes on a module at the Exec stage right after
// passes on a module at the Flow stage, with no lowering between them. It must not compile: the stage
// of a module is part of the type of its unit (TestNoLoweringDoesNotCompile).
package nolowering

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

var _ = pipe.Then(pipe.Seq(code.FlowStage), pipe.Seq(code.ExecStage))
