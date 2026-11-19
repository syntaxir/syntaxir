package flow

import (
	"fmt"
	"io"

	"zeroparse.dev/zp/internal/ir/code"
)

// Stage is the name of the Flow stage of the pipeline, which the steps and passes on Flow functions
// take and return (pass.Step).
const Stage = "flow"

// Print writes the dump of the Flow module m of the table named table after the step after: each
// function under a header line of its own. The stages that take Flow modules print with it
// (pass.IR.Print).
func Print(w io.Writer, table, after string, m *code.Module) {
	for _, f := range m.Funcs {
		fmt.Fprintf(w, "-- Flow IR of %s in the %s table after %s --\n%s\n", f.Name, table, after, code.FuncStringIn(m, f))
	}
}
