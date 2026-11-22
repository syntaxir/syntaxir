package common

import (
	"fmt"

	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Tables is the unit tables@exec, which the printers of the generating backends take: the tables of
// one target that the parser runs, at the Exec stage, by name in the order of the backend's split, with
// the plan of the program and the expectation table numbered from the
// Node and recognizer tables. The program and the options that the printer reads besides them are
// those of the target (TargetFeature).
type Tables struct {
	// Plan is the plan of the program, which holds what it was planned from, the routes of the calls
	// and the rules that the entry points reach (plandata.Program.Reach); nil for a backend that does
	// not plan the program.
	Plan   *plandata.Program
	Names  []string
	Tables []*plandata.Table[code.Exec]
	// Descs is the expectation table that the Node and recognizer tables record, after the fixed
	// expectations, numbered from their lowered tables; nil for a backend that numbers it as it prints.
	Descs []string
}

// Target is the target of a compilation as its printer reads it: the program, the options of the
// target and the settings of the compilation. Each generating backend has its own.
type Target interface {
	// Backend names the backend of the target, which is the text of the feature.
	Backend() string
}

// TargetFeature is the target of the pipeline of a backend (with target=go), which its printer reads.
// Its text names the backend; a pipeline text cannot provide it as a constant, since the target holds
// the program.
var TargetFeature = pipe.NewFeature("target", Target.Backend, nil)

// Replan is the output of a printer whose printed code enters other rules than the plan of the
// program reaches (plandata.Program.Reach), in place of its files: Reach is the reach of the printed
// code. Printing can leave out calls that the Core IR has, such as a call after an expression that
// never matches, so the plan reaches rules that nothing printed enters. The driver then runs the
// compilation again with Reach as the reach of the plan (plandata.ReachFeature), which is the fallback
// for planning, and returns the output of that run.
type Replan struct {
	Lang  result.Backend
	Reach map[plandata.Entry]bool
}

// Backend returns the backend of the printer.
func (r *Replan) Backend() result.Backend { return r.Lang }

// TableSource is the unit table: the build of the Core IR of one table of a program, which the step
// elaborate runs, and the key of the build, which is the same for the tables whose builds make the
// same module, so that they share one run of the Core part of the pipeline.
type TableSource struct {
	Build func() (*core.Module, error)
	Key   string
}

// Table returns the table named name, or nil if the target has none.
func (t *Tables) Table(name string) *plandata.Table[code.Exec] {
	for i, n := range t.Names {
		if n == name {
			return t.Tables[i]
		}
	}
	return nil
}

// TablesIR is the kind tables@exec.
var TablesIR = pipe.RegisterIR(&pipe.IR[*Tables]{Name: "tables@exec", Verify: verifyTables})

// verifyTables checks that every table of t is named once and is a table at the Exec stage.
func verifyTables(t *Tables) error {
	if len(t.Names) != len(t.Tables) {
		return fmt.Errorf("tables@exec names %d tables and holds %d", len(t.Names), len(t.Tables))
	}
	seen := map[string]bool{}
	for i, n := range t.Names {
		if seen[n] {
			return fmt.Errorf("tables@exec holds the table %s twice", n)
		}
		seen[n] = true
		if t.Tables[i] == nil {
			return fmt.Errorf("tables@exec has no module for the table %s", n)
		}
	}
	return nil
}

// JoinTables returns the tables at the Exec stage of the program planned as p, given the table
// of each unit of p at the Exec stage, in the order of p.Units: the tables that the parser runs, and
// the expectation table numbered from the numbered tables (execlower.NumberedExpectations), after the
// fixed expectations. The unreachable parts of the numbered tables, which only the numbering reads,
// are left out.
func JoinTables(p *plandata.Program, tables []*plandata.Table[code.Exec]) (*Tables, error) {
	if len(tables) != len(p.Units) {
		return nil, fmt.Errorf("internal error: %d tables are lowered for the %d planned tables", len(tables), len(p.Units))
	}
	descs, err := execlower.NumberedExpectations(p, tables)
	if err != nil {
		return nil, err
	}
	t := &Tables{Plan: p, Descs: descs[len(core.FixedDescs):]}
	for i, u := range p.Units {
		if u.Printed {
			t.Names = append(t.Names, u.Label)
			t.Tables = append(t.Tables, tables[i])
		}
	}
	return t, nil
}
