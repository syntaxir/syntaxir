package plandata

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Entry names what of a rule of a table is entered: its slot in the rule table, which the runtime runs
// with its bookkeeping, or its wrapper, a function of its own that a call of the rule enters instead
// and that does not run the slot's body (core.Entry).
type Entry struct {
	Table   string
	ID      int
	Wrapper bool
}

// CallRule is what the route of a call of a rule depends on: whether the rule is plain (program.Rule),
// a left-recursion leader, or an Operators Rule.
type CallRule struct {
	Plain, Leader, Operators bool
}

// CoreTable is one rule table of a program as the planning of the program reads it: its name, its Core
// IR as elaborated, and what the route of a call of each rule depends on.
type CoreTable struct {
	Name string
	M    *core.Module
	// Rules holds the call facts of each rule of M by rule ID, value-free twins included.
	Rules []CallRule
	// Typed selects the typed table, which enters a rule that is neither an Operators Rule nor a
	// left-recursion leader through its own function.
	Typed bool
	// Slots says that every call enters the slot of its rule, as the Node table of a parser with a
	// Document does, whose slots memoize every rule.
	Slots bool
}

// Cores is what the planning of a program takes: the rule tables of the program and the entry points,
// the start rules of the tables and the typed rules that the typed entry points enter, and the tables
// that the backend lowers (Units), which the planning plans from the rules the entry points reach.
type Cores struct {
	Tables []CoreTable
	Roots  []Entry
	Units  []Unit
}

// Program is the plan of a program: the entry points, the route of a call of each rule of each table
// and the rules that the entry points reach, by the way they are entered. The functions of each table
// are planned per table (Plan), from the rules this plan reaches.
type Program struct {
	Roots []Entry
	// Reach holds every reachable entry.
	Reach map[Entry]bool
	// Tables holds the routes of the calls of the rules of each table, in the order of the tables of
	// the program.
	Tables []Routes
	// Units holds the planned tables that the backend lowers: one for each unit of the program, in
	// their order, then the unreachable part of each numbered unit that the plan prunes. Numbered
	// holds, for each numbered unit, the parts that the expectation table is numbered from.
	Units    []PlannedUnit
	Numbered []Numbered
	// Cores is what the program was planned from.
	Cores *Cores
}

// Routes are the routes of the calls of the rules of one table.
type Routes struct {
	Table string
	// Names holds the name of each rule by rule ID, and Wrappers whether a call of the rule enters
	// its wrapper rather than its slot.
	Names    []string
	Wrappers []bool
}

// Wrapper reports whether a call of the rule of the table enters the rule's wrapper rather than its
// slot. A table that the plan does not hold has no wrappers.
func (p *Program) Wrapper(table string, rule int) bool {
	for _, t := range p.Tables {
		if t.Table == table {
			return rule < len(t.Wrappers) && t.Wrappers[rule]
		}
	}
	return false
}

// ReachFeature is the reach of the plan of a program when the compilation gives it, the reach of the
// code that a printer printed in an earlier run (the fallback for planning): the planning takes it
// instead of following the calls of the Core IR. Nil, written none, has the planning compute it.
var ReachFeature = pipe.NewFeature("reach", formatReach, parseReach)

// formatReach writes the reach r as the entries it holds, sorted and separated by spaces, each as
// table.id or, for the entry of a wrapper, table.id/wrapper, or none for nil.
func formatReach(r map[Entry]bool) string {
	if r == nil {
		return "none"
	}
	var es []string
	for e, ok := range r {
		if ok {
			s := e.Table + "." + strconv.Itoa(e.ID)
			if e.Wrapper {
				s += "/wrapper"
			}
			es = append(es, s)
		}
	}
	slices.Sort(es)
	return strings.Join(es, " ")
}

// parseReach reads the reach that formatReach writes.
func parseReach(s string) (map[Entry]bool, error) {
	if s == "none" {
		return nil, nil
	}
	r := map[Entry]bool{}
	for _, f := range strings.Fields(s) {
		var e Entry
		f, e.Wrapper = strings.CutSuffix(f, "/wrapper")
		i := strings.LastIndexByte(f, '.')
		if i <= 0 {
			return nil, fmt.Errorf("reach entry %q is not table.id", f)
		}
		id, err := strconv.Atoi(f[i+1:])
		if err != nil || id < 0 {
			return nil, fmt.Errorf("reach entry %q has no rule ID", f)
		}
		e.Table, e.ID = f[:i], id
		r[e] = true
	}
	return r, nil
}

// CoresIR is the kind of the input of the planning of a program, and ProgramIR that of its plan. The
// input has no dump: its Core IR is dumped as it is built and changed, and the plan shows its entry
// points and routes. The plan is dumped as printProgram writes it.
var (
	CoresIR = pipe.RegisterIR(&pipe.IR[*Cores]{Name: "cores", Verify: verifyCores})
	// ProgramIR is the kind of the plan of a program.
	ProgramIR = pipe.RegisterIR(&pipe.IR[*Program]{Name: "plan", Verify: verifyProgram, Print: printProgram})
)

// printProgram writes the plan p: its entry points, then each table and each rule of it by ID, with
// the route of a call of the rule and the ways the plan reaches it.
//
//	roots rules.main recRules.main trules.main/wrapper
//	table rules
//	  rule 0 main call=slot reach=slot
//	  rule 1 ws call=wrapper reach=wrapper
//	  rule 2 unused call=slot reach=none
func printProgram(w io.Writer, p *Program) {
	io.WriteString(w, "roots")
	for _, e := range p.Roots {
		fmt.Fprintf(w, " %s.%s", e.Table, p.name(e.Table, e.ID))
		if e.Wrapper {
			io.WriteString(w, "/wrapper")
		}
	}
	io.WriteString(w, "\n")
	for _, t := range p.Tables {
		fmt.Fprintf(w, "table %s\n", t.Table)
		for id, name := range t.Names {
			call := "slot"
			if t.Wrappers[id] {
				call = "wrapper"
			}
			var reach []string
			if p.Reach[Entry{Table: t.Table, ID: id}] {
				reach = append(reach, "slot")
			}
			if p.Reach[Entry{Table: t.Table, ID: id, Wrapper: true}] {
				reach = append(reach, "wrapper")
			}
			if reach == nil {
				reach = []string{"none"}
			}
			fmt.Fprintf(w, "  rule %d %s call=%s reach=%s\n", id, name, call, strings.Join(reach, ","))
		}
	}
}

// name returns the name of the rule of the table, or its ID if the plan has no such rule.
func (p *Program) name(table string, rule int) string {
	for _, t := range p.Tables {
		if t.Table == table && rule < len(t.Names) {
			return t.Names[rule]
		}
	}
	return strconv.Itoa(rule)
}

// verifyCores checks that every table has the call facts of each of its rules and that every entry
// point names a rule of a table.
func verifyCores(c *Cores) error {
	rules := map[string]int{}
	for _, t := range c.Tables {
		if len(t.Rules) != len(t.M.Rules) {
			return fmt.Errorf("the table %s has the call facts of %d rules, not of its %d rules", t.Name, len(t.Rules), len(t.M.Rules))
		}
		if _, dup := rules[t.Name]; dup {
			return fmt.Errorf("the table %s is given twice", t.Name)
		}
		rules[t.Name] = len(t.Rules)
	}
	for _, e := range c.Roots {
		if n, ok := rules[e.Table]; !ok || e.ID < 0 || e.ID >= n {
			return fmt.Errorf("the entry point %s[%d] names no rule of a table", e.Table, e.ID)
		}
	}
	return nil
}

// verifyProgram checks that the plan reaches every entry point.
func verifyProgram(p *Program) error {
	for _, e := range p.Roots {
		if !p.Reach[e] {
			return fmt.Errorf("the plan does not reach its entry point %s[%d]", e.Table, e.ID)
		}
	}
	return nil
}
