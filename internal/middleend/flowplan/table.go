package flowplan

import (
	"zeroparse.dev/zp/internal/frontend/elaborate"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/flowlower"
)

// TableConfig says how the functions of a Node or recognizer table run: what the parser that runs
// them charges and records.
type TableConfig struct {
	// Values selects the Node table, whose Core IR gathers the values of projected repetitions as its
	// actions read them; false selects the recognizer, whose actions read no projections.
	Values bool
	// Budget charges the budget controls of a parse (flowlower.Options.Budget).
	Budget bool
	// Observe reports the rules a parse enters and the alternatives that guards skip.
	Observe bool
	// Document records how far each call examines the input, as a build with Documents and a traced
	// parse do.
	Document bool
	// Traced selects the traced bodies.
	Traced bool
}

// TableIR is the Core IR of a Node or recognizer table after its Core passes, and the options that
// every function of the table is lowered with.
type TableIR struct {
	// M is built with its origin map (core.Module.Origin), which the passes keep.
	M    *core.Module
	Opts flowlower.Options
}

// BuildTable builds the Core IR of the Node or recognizer table of the program prog, as c selects,
// through coreStep, the Core step of the caller's driver (driver.Driver.Core for the caller's table):
// coreStep runs build, the elaboration of the table, and then the Core passes, and returns the module
// after them. BuildTable returns that module with the lowering options of the table. Every backend
// that runs the Node or recognizer table from Flow IR builds it here, so that, with the same
// optimizations, they lower the same Core IR with the same options. Classes are inline tests in
// these tables; the forms that only typed rules print are the Flow passes' (flowpass.Target).
func BuildTable(prog elaborate.Program, c TableConfig, coreStep func(build func() (*core.Module, error)) (*core.Module, error)) (*TableIR, error) {
	m, err := coreStep(func() (*core.Module, error) { return elaborate.CoreProjecting(prog, true, c.Values) })
	if err != nil {
		return nil, err
	}
	opts := flowlower.Options{Budget: c.Budget, Recovers: flowlower.Recovers(m), Observe: c.Observe, Node: true,
		Document: c.Document, Traced: c.Traced}
	return &TableIR{M: m, Opts: opts}, nil
}
