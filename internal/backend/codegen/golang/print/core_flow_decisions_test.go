package print_test

import (
	"fmt"
	"strings"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/pipetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The terminal programs, the class runs and the guarded classes are chosen by the Flow passes of the
// target (internal/middleend/flowpass), not by the Core passes. The decisions golden freezes them with
// the decisions of Core IR: the generations of the tests record what the Flow passes replaced in each
// function the generator lowered, and coreDecisionLines encodes the records of the case's table.

// flowKey is the place of a decision: a rule of the table and an expression of its body.
type flowKey struct {
	rule int
	e    ast.Expr
}

// flowDecisions are the decisions of the Flow passes on the functions lowered from one table, in
// the copies of one kind (traced or not).
type flowDecisions struct {
	// terminals are the terminal regions matched as one program, by their root's expression.
	terminals map[flowKey]bool
	// runs are the class runs of each value-free sequence, as "position:length" in its items.
	runs map[flowKey]string
	// guarded are the alternatives, by their expression (coreGenCase.altOrigin), whose first class
	// consumes the character of the guard.
	guarded map[flowKey]bool
}

func newFlowDecisions() *flowDecisions {
	return &flowDecisions{terminals: map[flowKey]bool{}, runs: map[flowKey]string{}, guarded: map[flowKey]bool{}}
}

// clone returns a copy of d that a negative control may change.
func (d *flowDecisions) clone() *flowDecisions {
	c := newFlowDecisions()
	for k, v := range d.terminals {
		c.terminals[k] = v
	}
	for k, v := range d.runs {
		c.runs[k] = v
	}
	for k, v := range d.guarded {
		c.guarded[k] = v
	}
	return c
}

// flowScope says which functions a record holds: those of a rule table, traced or not.
type flowScope struct {
	table  string
	traced bool
}

// flowRecord holds the decisions of the Flow passes in one generation, with the origins and the nodes
// by ID of the Core modules of its tables, by their labels, which the decisions are keyed by.
type flowRecord struct {
	mu  sync.Mutex
	gen *print.Generator
	// settings are those of the generation, which the Core IR of a case is built with (typedCore), and
	// checked is the checked program that it compiled the grammar to.
	settings pass.Settings
	checked  *frontend.Result
	origins  map[string]core.Origin
	nodes    map[string]map[core.NodeID]core.Node
	by       map[flowScope]*flowDecisions
	// cores is the planned tables of the generation: the Go decision's tables, each with the Core IR
	// that the Core passes made and the lowering options the generator gives it (plandata.Cores).
	cores *plandata.Cores
}

// decisions returns the decisions recorded for the table, in the copies a case of gen reads: the
// traced ones in a build with observability (coreGenerate), the others otherwise.
func (r *flowRecord) decisions(table string, gen *print.Generator) *flowDecisions {
	if r == nil {
		return newFlowDecisions()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.by[flowScope{table, gen.Traced}]; ok {
		return d
	}
	return newFlowDecisions()
}

// flowRecords maps each generator of coreGenerate to the record of its generation.
var flowRecords sync.Map // *print.Generator -> *flowRecord

// settingsOf returns the settings of the generation of gen, or none for a generator that no
// generation recorded.
func settingsOf(gen *print.Generator) pass.Settings {
	if rec := flowRecordOf(gen); rec != nil {
		return rec.settings
	}
	return pass.Settings{}
}

// checkedOf returns the checked program of the generation of gen, which coreGenerate recorded.
func checkedOf(gen *print.Generator) *frontend.Result {
	rec := flowRecordOf(gen)
	if rec == nil || rec.checked == nil {
		panic("internal error: no generation of the test recorded the checked program of the generator")
	}
	return rec.checked
}

// coresOf returns the tables of the generation of gen, as the Go decision made them, or nil.
func coresOf(gen *print.Generator) *plandata.Cores {
	if rec := flowRecordOf(gen); rec != nil {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return rec.cores
	}
	return nil
}

// flowRecordOf returns the record of the generation of gen, or nil.
func flowRecordOf(gen *print.Generator) *flowRecord {
	r, _ := flowRecords.Load(gen)
	rec, _ := r.(*flowRecord)
	return rec
}

// options returns the options of a compilation whose observers keep the origin of the Core module of
// each table, which the functions are lowered from, and record in rec what the Flow passes replaced in
// each function of a table, after each pass, before flow-finish drops the forms. A pass leaves alone
// what an earlier one replaced, so the decisions recorded after a pass include those after the passes
// before it.
func (rec *flowRecord) options() driver.Options {
	return driver.Options{Observers: []pipe.Observer{&pipetest.Watch{Units: true, Fn: rec.core}, codegentest.FlowPasses(rec.record)}}
}

// hooks returns the hooks that keep the generator that prints in rec, as the generator of the case.
func (rec *flowRecord) hooks() *print.Hooks {
	return &print.Hooks{Generator: func(x *print.Generator) { rec.gen = x }}
}

// core keeps the origin and the nodes of the Core module of the table of the event, after each Core
// step.
func (rec *flowRecord) core(e pipe.Event, r *pipe.Result) {
	if c, ok := r.Unit.(*plandata.Cores); ok && r.Err == nil && e.Label.Table == "" {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.cores = c
		return
	}
	m, ok := r.Unit.(*core.Module)
	if !ok || r.Err != nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.origins == nil {
		rec.origins, rec.nodes = map[string]core.Origin{}, map[string]map[core.NodeID]core.Node{}
	}
	rec.origins[e.Label.Table] = m.Origin
	rec.nodes[e.Label.Table] = nodesByID(m)
}

// nodesByID returns the nodes of the rules of m that have an ID, by their IDs, the lines of Operators
// Rules included.
func nodesByID(m *core.Module) map[core.NodeID]core.Node {
	out := map[core.NodeID]core.Node{}
	var walk func(n core.Node)
	walk = func(n core.Node) {
		core.Walk(n, func(x core.Node) {
			if id := x.Attrs().ID; id != 0 {
				out[id] = x
			}
			if o, ok := x.(*core.OperatorsRule); ok {
				for _, l := range o.Lines() {
					walk(l.Body)
				}
			}
		})
	}
	for _, r := range m.Rules {
		if r != nil {
			walk(r.Body)
		}
	}
	return out
}

// record adds the decisions of the passes on the function of m, of the planned table labeled l.Table,
// before flow-finish drops its forms. A table of the Flow stage is lowered from the Core module of the
// same label; the typed table is the generator's trules. The functions that the plan leaves
// unreachable (flowplan.UnreachableSuffix) are not printed, so the decisions leave them out.
func (rec *flowRecord) record(l pipe.Label, m *code.Module) {
	a := flow.AttrsOf(flow.FuncOf(m))
	if a == nil || a.Forms == nil || strings.HasSuffix(l.Table, flowplan.UnreachableSuffix) {
		return
	}
	label := l.Table
	table := strings.TrimSuffix(label, "Traced")
	if table == "typed" {
		table = "trules"
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	origin, ok := rec.origins[label]
	if !ok {
		return
	}
	nodes := rec.nodes[label]
	if rec.by == nil {
		rec.by = map[flowScope]*flowDecisions{}
	}
	scope := flowScope{table, a.Traced}
	d := rec.by[scope]
	if d == nil {
		d = newFlowDecisions()
		rec.by[scope] = d
	}
	id := a.Rule
	for _, r := range a.Forms.Regions {
		if r.By != flowpass.PassTerminals {
			continue
		}
		if e, ok := origin.Exprs[core.NodeID(r.ID)]; ok {
			d.terminals[flowKey{id, e}] = true
		}
	}
	for _, s := range a.Forms.Seqs {
		e, ok := origin.Exprs[core.NodeID(s.ID)]
		if !ok {
			continue
		}
		var runs []string
		for i := 0; i < len(s.Items); i++ {
			n := 0
			for i+n < len(s.Items) && s.Items[i+n] != nil && s.Items[i+n].By == flowpass.PassClassRuns {
				n++
			}
			if n > 0 {
				runs = append(runs, fmt.Sprintf("%d:%d", i, n))
				i += n - 1
			}
		}
		if len(runs) > 0 {
			d.runs[flowKey{id, e}] = strings.Join(runs, " ")
		}
	}
	for _, cl := range a.Forms.Classes {
		if cl.By != flowpass.PassGuardedClasses || cl.Guard == nil {
			continue
		}
		x, ok := nodes[core.NodeID(cl.Guard.ID)].(*core.Choice)
		if !ok {
			continue
		}
		for i, alt := range x.Alts {
			if c := leadingClass(alt.Body); c == nil || cl.ID == 0 || int32(c.Attrs().ID) != cl.ID {
				continue
			}
			if e, ok := altOriginIn(origin, x, i); ok {
				d.guarded[flowKey{id, e}] = true
			}
		}
	}
}

// leadingClass returns the class that n is or that begins the sequence n, or nil.
func leadingClass(n core.Node) core.Node {
	if s, ok := n.(*core.Seq); ok && len(s.Items) > 0 {
		n = s.Items[0]
	}
	if c, ok := n.(*core.ClassNode); ok {
		return c
	}
	return nil
}

// altOriginIn returns the expression of the alternative i of the choice x under origin, as
// coreGenCase.altOrigin does.
func altOriginIn(origin core.Origin, x *core.Choice, i int) (ast.Expr, bool) {
	if ce, ok := origin.Of(x).(*ast.Choice); ok && len(ce.Alts) == len(x.Alts) {
		return ce.Alts[i], true
	}
	e, ok := origin.Exprs[x.Alts[i].Body.Attrs().ID]
	return e, ok
}
