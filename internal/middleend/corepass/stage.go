package corepass

import (
	"fmt"
	"io"

	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/pass"
)

// IR describes the Core IR module of a table to the manager of the pipeline of a compilation: it is
// dumped as the listing of the module. It has no Verify: the Pipeline verifies the module after its
// passes (Verify).
var IR = pass.IR[*core.Module]{Stage: Stage, Print: Print, Copy: (*core.Module).Clone}

// Print writes the dump of the Core IR module m: its listing.
func Print(w io.Writer, m *core.Module) {
	fmt.Fprintf(w, "%s\n", core.Print(m, true))
}
