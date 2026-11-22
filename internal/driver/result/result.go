// Package result holds the results of a compilation (record 074): the output of each target, which
// the last required step of its backend returns and the callers of the driver read. The backend
// packages import it to build their output; it imports no pass code, only the manager's kinds.
package result

import (
	"fmt"

	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Backend is the backend of a target.
type Backend uint8

const (
	Go         Backend = iota // a Go parser
	TypeScript                // a TypeScript parser
	VM                        // the bytecode of the VM
	Closure                   // the Core module that the closure backend builds from
)

// String returns the name of the backend as labels and the text form spell it.
func (b Backend) String() string {
	switch b {
	case Go:
		return "go"
	case TypeScript:
		return "ts"
	case VM:
		return "vm"
	case Closure:
		return "closure"
	}
	return fmt.Sprintf("Backend(%d)", int(b))
}

// Output is the result of one target. Each backend's result implements it; a caller that needs the
// concrete result asserts it once.
type Output interface {
	// Backend returns the backend that built the output.
	Backend() Backend
}

// File is a file of generated code. Path is relative to the directory of the generated package and
// uses slashes.
type File struct {
	Path    string
	Content []byte
}

// Files is the output of a generating backend: the files of the parser it generates.
type Files struct {
	Lang  Backend
	Files []File
}

// Backend returns the language of the files.
func (f *Files) Backend() Backend { return f.Lang }

// IR is the kind output: the result of one backend, which the last required step of every backend
// returns.
var IR = pipe.RegisterIR(&pipe.IR[Output]{Name: "output", Verify: verify})

// verify checks that an output is one: a nil output is no result of any backend.
func verify(o Output) error {
	if o == nil {
		return fmt.Errorf("no output")
	}
	if f, ok := o.(*Files); ok && f == nil {
		return fmt.Errorf("no files")
	}
	return nil
}
