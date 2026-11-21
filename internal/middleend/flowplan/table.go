package flowplan

import (
	"zeroparse.dev/zp/internal/frontend/elaborate"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/internal/middleend/flowlower/lowerdata"
	"zeroparse.dev/zp/internal/middleend/flowplan/plandata"
)

// BuildTable builds the Core IR of the Node or recognizer table of the program prog, as c selects,
// through coreStep, the Core step of the caller's driver (driver.Driver.Core for the caller's table):
// coreStep runs build, the elaboration of the table, and then the Core passes, and returns the module
// after them. BuildTable returns that module with the lowering options of the table. Every backend
// that runs the Node or recognizer table from Flow IR builds it here, so that, with the same
// optimizations, they lower the same Core IR with the same options. Classes are inline tests in
// these tables; the forms that only typed rules print are the Flow passes' (flowpass.Target).
func BuildTable(prog elaborate.Program, c plandata.TableConfig, coreStep func(build func() (*core.Module, error)) (*core.Module, error)) (*plandata.TableIR, error) {
	m, err := coreStep(func() (*core.Module, error) { return elaborate.CoreProjecting(prog, true, c.Values) })
	if err != nil {
		return nil, err
	}
	opts := lowerdata.Options{Budget: c.Budget, Recovers: flowlower.Recovers(m), Observe: c.Observe, Node: true,
		Document: c.Document, Traced: c.Traced}
	return &plandata.TableIR{M: m, Opts: opts}, nil
}
