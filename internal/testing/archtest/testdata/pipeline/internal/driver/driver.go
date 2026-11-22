// Package driver is a fake driver, which may execute pipelines and exports one identifier too many.
package driver

import "example.com/pipe/internal/pass/pipe"

// Compile is the compilation API.
func Compile() { pipe.Execute() }

// RunStage is an export that the allowlist lacks.
func RunStage() {}
