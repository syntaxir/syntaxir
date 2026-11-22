// Package fake is test support, which may execute pipelines and set the printer hooks.
package fake

import (
	"example.com/pipe/internal/driver/generate"
	"example.com/pipe/internal/pass/pipe"
)

// Run executes a pipeline and sets the hooks.
func Run() generate.Target {
	pipe.Execute()
	var h int
	return generate.GoWith(&h)
}
