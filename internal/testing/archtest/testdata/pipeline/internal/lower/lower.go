// Package lower declares passes, executes a pipeline and exports what its passes run.
package lower

import "example.com/pipe/internal/pass/pipe"

// Def is an exported pass definition.
var Def = pipe.Def[int, int]{Run: func(i int) int { return Lower(i) }}

var named = pipe.Def[int, int]{Run: RunNamed, Applies: func() bool { return Exported() > 0 && helper(0) == 0 }}

// RunNamed is the exported run function of a pass.
func RunNamed(i int) int { return helper(i) }

// Lower is exported and named by a run function, and executes a pipeline.
func Lower(i int) int {
	pipe.Execute()
	return i
}

// Exported is exported and named by an Applies function only.
func Exported() int { return 1 }

func helper(i int) int { return i }
