package flowplan

import (
	"slices"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Outlining.
//
// A framed function that is printed in one piece grows with the grammar: a rule of many alternatives or a
// body that nests the same few matchers many times prints as thousands of lines that no other rule can
// share, and a function that large is neither compact nor fast for the JavaScript engine to compile. A
// backend whose runtime wants small functions, the TypeScript one, therefore runs the pass outline
// (Outlining) before it plans the program: the Node and recognizer tables then run some of their nodes
// as functions of their own (flow.CallOutline), which the TypeScript printer shares wherever their
// printed text is equal once the constants it names are replaced by their declarations.
//
// The nodes are chosen by the structure of the Core IR: a node whose structure occurs in more than one
// place of the module (outlineDuplicate), and a node that is large (outlineLarge) so that no function
// grows beyond a bound.

// PassOutline is the name of the pass outline (Outlining), and of the flag that turns it off.
const PassOutline = "outline"

// CounterOutlined is the counter of outline: the nodes that it outlined.
const CounterOutlined = "outlined"

func init() {
	pass.Register(pass.Flag{Name: PassOutline, Kind: pass.KindCorePass, Default: 1,
		Doc: "large and repeated parts of a rule run as functions of their own (TypeScript)"})
}

// Outlining is the pass outline: it chooses the nodes of each Node and recognizer table of the
// program (lowerdata.Options.Node) that run as functions of their own (outlineNodes), and sets them
// as the outlined nodes of the lowering options of the table (lowerdata.Options.Outlined). The units
// that share a table keep sharing it. The tables it is given are unchanged.
var Outlining = pipe.New(pipe.Def[*plandata.Cores, *plandata.Cores]{
	Decl: pipe.Decl{Name: PassOutline, Counters: []string{CounterOutlined},
		Obligation: "Every outlined node is a framed body that runs as a function of its own as it runs in place."},
	In: plandata.CoresIR, Out: plandata.CoresIR,
	Run: func(x *pipe.Ctx[*plandata.Cores], c *plandata.Cores) (*plandata.Cores, pass.PreservedSet, error) {
		out := *c
		out.Units = slices.Clone(c.Units)
		tables := map[*plandata.TableIR]*plandata.TableIR{}
		for i, u := range out.Units {
			if !u.IR.Opts.Node {
				continue
			}
			ir, ok := tables[u.IR]
			if !ok {
				ir = u.IR
				if chosen := outlineNodes(u.IR.M, u.IR.Values, cancel.New(x.Context())); len(chosen) > 0 {
					outlined := *u.IR
					outlined.Opts.Outlined = chosen
					ir = &outlined
					x.Count(CounterOutlined, len(chosen))
				}
				tables[u.IR] = ir
			}
			out.Units[i].IR = ir
		}
		return &out, pass.PreserveNone(), nil
	},
})

const (
	// outlineLarge is the number of Core nodes from which a node runs as a function of its own.
	outlineLarge = 120
	// outlineChild is the number of Core nodes from which a child of a large node runs as a function of its
	// own.
	outlineChild = 16
	// outlineDuplicate is the number of Core nodes from which a structure that occurs more than once runs
	// as a function of its own in the Node table.
	outlineDuplicate = 1
)

// outlineNodes returns the nodes of the module m that run as functions of their own: a node that is
// Outlinable is chosen if it has at least outlineLarge Core nodes, or if build is set and
// its structure occurs at least twice where its function would not already be a shared body. It polls
// control, which may be nil, for each node it visits, in every round.
func outlineNodes(m *core.Module, build bool, control *cancel.Control) map[core.Node]bool {
	type info struct {
		n    core.Node
		size int
		key  string
		kids []*info
	}
	var all []*info
	byNode := map[core.Node]*info{}
	var visit func(n core.Node) *info
	visit = func(n core.Node) *info {
		control.Poll()
		in := &info{n: n, size: 1}
		byNode[n] = in
		all = append(all, in)
		var kids []core.Node
		if o, ok := n.(*core.OperatorsRule); ok {
			for _, l := range o.Lines() {
				kids = append(kids, l.Body)
			}
		} else {
			kids = core.Children(n)
		}
		for _, c := range kids {
			if c != nil {
				k := visit(c)
				in.kids = append(in.kids, k)
				in.size += k.size
			}
		}
		return in
	}
	for _, r := range m.Rules {
		if r != nil {
			visit(r.Body)
		}
	}
	chosen := map[core.Node]bool{}
	candidate := func(in *info) bool { return flowlower.Outlinable(in.n) }
	var dup map[string]bool
	if build {
		count := map[string]int{}
		for _, in := range all {
			control.Poll()
			if in.size >= outlineDuplicate && in.size < outlineLarge && candidate(in) {
				in.key = core.PrintNodeGen(m, in.n)
				count[in.key]++
			}
		}
		// Count the structures again as the program that runs them as functions of their own: the
		// copies inside a structure that is shared are one copy.
		dup = map[string]bool{}
		for k, c := range count {
			if c >= 2 {
				dup[k] = true
			}
		}
		for {
			eff := map[string]int{}
			seen := map[string]bool{}
			var walk func(n core.Node)
			walk = func(n core.Node) {
				control.Poll()
				in := byNode[n]
				if in.key != "" {
					eff[in.key]++
				}
				if in.key != "" && dup[in.key] {
					if seen[in.key] {
						return // a copy of a shared structure
					}
					seen[in.key] = true
				}
				var kids []core.Node
				if o, ok := n.(*core.OperatorsRule); ok {
					for _, l := range o.Lines() {
						kids = append(kids, l.Body)
					}
				} else {
					kids = core.Children(n)
				}
				for _, c := range kids {
					if c != nil {
						walk(c)
					}
				}
			}
			for _, r := range m.Rules {
				if r != nil {
					walk(r.Body)
				}
			}
			changed := false
			for k := range dup {
				if eff[k] < 2 {
					delete(dup, k)
					changed = true
				}
			}
			if !changed {
				break
			}
		}
	}
	for _, in := range all {
		control.Poll()
		if candidate(in) && (in.size >= outlineLarge || in.key != "" && dup[in.key]) {
			chosen[in.n] = true
		}
		if in.size >= outlineLarge {
			// The function of a large node keeps calls in place of its large parts.
			for _, k := range in.kids {
				if k.size >= outlineChild && candidate(k) {
					chosen[k.n] = true
				}
			}
		}
	}
	return chosen
}
