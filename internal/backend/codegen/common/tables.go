package common

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Tables is the unit tables@exec, which the printers of the generating backends take: the tables of
// one target at the Exec stage, by name in the order of the backend's split, with what they were
// planned from and what the printer reads besides them: the program and its recognizer, the start
// rule, the target's options and the settings of the compilation, the plan of the program and the
// expectation table numbered from the Node and recognizer tables.
type Tables struct {
	Prog  *Program
	Rec   *Program // the recognizer, or nil if the target has no recognizer table
	Start *Rule
	// Opts are the options of the target, as the backend decided them from the program (a typed-only
	// Go parser whose typed entry points convert Nodes keeps its Node rules).
	Opts     GenOptions
	Settings pass.Settings
	// Plan is the plan of the program, or nil for a backend that does not plan it.
	Plan   *plandata.Program
	Names  []string
	Tables []*plandata.Table[code.Exec]
	// Descs is the expectation table that the Node and recognizer tables record, after the fixed
	// expectations, numbered from their lowered tables; nil for a backend that numbers it as it prints.
	Descs []string
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
	if t.Prog == nil || t.Start == nil {
		return fmt.Errorf("tables@exec has no program")
	}
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
