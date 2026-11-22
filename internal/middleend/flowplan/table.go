package flowplan

import (
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// BuildTable builds the Core IR of the Node or recognizer table of the program prog, as c selects,
// through coreStep, the Core step of the caller's driver: coreStep runs build, the elaboration of the
// table (TableBuild), and then the Core passes, and returns the module after them. BuildTable returns
// that module with the lowering options of the table (TableOf).
func BuildTable(prog elaborate.Program, c plandata.TableConfig, coreStep func(build func() (*core.Module, error)) (*core.Module, error)) (*plandata.TableIR, error) {
	m, err := coreStep(TableBuild(prog, c))
	if err != nil {
		return nil, err
	}
	return TableOf(m, c), nil
}

// TableBuild returns the build of the Core IR of the Node or recognizer table of the program prog, as
// c selects: the elaboration of the table, whose rules build values only if c.Values is set.
func TableBuild(prog elaborate.Program, c plandata.TableConfig) func() (*core.Module, error) {
	return func() (*core.Module, error) { return elaborate.CoreProjecting(prog, true, c.Values) }
}

// TableOf returns the Node or recognizer table whose Core IR m the Core passes made of the build of
// TableBuild, with the lowering options that c selects. Every backend that runs the Node or recognizer
// table from Flow IR builds it with TableBuild and TableOf, so that, with the same optimizations, they
// lower the same Core IR with the same options. Classes are inline tests in these tables; the forms
// that only typed rules print are the Flow passes' (flowpass.Target). A table that outlines
// (plandata.TableConfig.Outline) chooses its outlined nodes here, from its Core IR after the Core
// passes; the Node table outlines its repeated structures too (outlineNodes).
func TableOf(m *core.Module, c plandata.TableConfig) *plandata.TableIR {
	opts := lowerdata.Options{Budget: c.Budget, Logical: c.Logical, Guards: c.Guards, Recovers: flowlower.Recovers(m), Observe: c.Observe, Node: true,
		Document: c.Document, Resume: c.Resume, Traced: c.Traced}
	if c.Outline {
		if chosen := outlineNodes(m, c.Values); len(chosen) > 0 {
			opts.Outline = func(n core.Node) bool { return chosen[n] }
		}
	}
	return &plandata.TableIR{M: m, Opts: opts}
}
