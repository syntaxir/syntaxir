package corepass

import (
	"fmt"
	"io"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// IR is the kind of a Core IR module, which the Core passes take and return: it is dumped as the
// listing of the module and verified by Verify, which checks the attributes of its nodes and never
// writes them.
var IR = pipe.RegisterIR(&pipe.IR[*core.Module]{Name: "core", Verify: Verify, Print: Print, Copy: (*core.Module).Clone})

// Print writes the dump of the Core IR module m: its listing.
func Print(w io.Writer, m *core.Module) {
	fmt.Fprintf(w, "%s\n", core.Print(m, true))
}
