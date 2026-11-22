// Package pass holds the Go backend's Exec passes: the forms of a lowered function that the Go printer
// prints, made after the Exec passes that every backend runs. The Go printer prints each operation of
// the Exec stage as it is, so the pipeline of the rule bodies has no passes yet. The compositions of the
// Go library have a step of their own in the pipeline of the library (ColdPath).
package pass

import (
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Passes are the Go Exec passes, in the order they run on a lowered function with its module. There
// is none yet.
var Passes []*pipe.Pass[*execdata.Lowered, *execdata.Lowered]

// Pipeline is the pipeline of the Go Exec passes.
var Pipeline = pipe.Seq(execdata.LoweredIR, Passes...)
