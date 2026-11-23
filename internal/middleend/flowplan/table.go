package flowplan

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// TableBuild returns the build of the Core IR of the Node or recognizer table of the program prog, as
// c selects: the elaboration of the table, whose rules build values only if c.Values is set, with its
// origin map if origin (common.TableSource).
func TableBuild(prog elaborate.Program, c plandata.TableConfig) func(*cancel.Control, bool) (*core.Module, error) {
	return func(control *cancel.Control, origin bool) (*core.Module, error) {
		return elaborate.CoreControlled(prog, origin, c.Values, control)
	}
}

// TableOf returns the Node or recognizer table whose Core IR m the Core passes made of the build of
// TableBuild, with the lowering options that c selects. Every backend that runs the Node or recognizer
// table from Flow IR builds it with TableBuild and TableOf, so that, with the same optimizations, they
// lower the same Core IR with the same options. Classes are inline tests in these tables; the forms
// that only typed rules print are the Flow passes' (flowpass.Target). The table outlines no node; the
// pass outline chooses the outlined nodes of a backend that runs it (Outlining).
func TableOf(m *core.Module, c plandata.TableConfig) *plandata.TableIR {
	opts := lowerdata.Options{Logical: c.Logical, Guards: c.Guards, Recovers: flowlower.Recovers(m), Inserts: flowlower.Inserts(m), Observe: c.Observe, Node: true,
		Document: c.Document, Resume: c.Resume, Traced: c.Traced}
	return &plandata.TableIR{M: m, Values: c.Values, Opts: opts}
}
