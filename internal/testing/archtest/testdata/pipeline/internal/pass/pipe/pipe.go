// Package pipe is a fake of the pass manager for the negative controls of the pipeline rules.
package pipe

// Def is the definition of a pass.
type Def[I, O any] struct {
	Run     func(I) O
	Applies func() bool
}

// Execute runs a pipeline.
func Execute() {}
