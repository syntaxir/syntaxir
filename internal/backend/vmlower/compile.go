package vmlower

import (
	"fmt"
	"maps"
	"slices"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// compile compiles a checked grammar to a bytecode module: the type, variable, rule and Operators
// Rule tables from the Core IR of its Node table, the Flow body of each function of the table
// selected by the selection table (selectFlows), and the action code of the actions and predicates.
// Every rule runs its Flow body, except a rule whose body is the expression of its Operators Rule,
// whose skip and lines run theirs. It returns the module and the number of matches of each pattern
// of statements of the selection table by name, which depends only on the bodies. A failure is an internal
// error: the grammar checked, so its table builds and every function of it has a Flow body.
func compile(in Input) (*bytecode.Module, map[string]int, error) {
	actions := in.Table.IR.Actions.Module()
	if actions == nil {
		return nil, nil, fmt.Errorf("internal error: the table of the bytecode module has no Action module")
	}
	funcs, _ := action.SiteFuncs(actions)
	c := &compiler{in: in, core: in.Table.IR.M, sites: ir.ActionSitesOf(in.Table.IR.M), actions: actions, funcs: funcs,
		tab:  prim.TablesOf(in.Table.Module.Module()),
		m:    &bytecode.Module{SourceInfo: in.Run.RecordsSourceInfo, DefaultValue: in.Run.MakesDefaultValue},
		strs: map[string]int{}, counts: map[string]int{}}
	c.compile()
	if c.err != nil {
		return nil, nil, c.err
	}
	// The action sites of the tables come first, so the strings they add follow those of the tables.
	for i, s := range c.actionSites {
		var err error
		if c.m.Actions[i], err = c.actionCode(s); err != nil {
			return nil, nil, err
		}
	}
	if err := c.selectFlows(); err != nil {
		return nil, nil, err
	}
	if err := c.complete(); err != nil {
		return nil, nil, err
	}
	return c.m, c.counts, nil
}

// complete checks that every rule, Operators Rule skip and line of the module has the body it runs:
// a rule its Flow body, unless its body is the expression of its Operators Rule, and a skip and a
// line theirs.
func (c *compiler) complete() error {
	for i, ri := range c.m.Rules {
		if ri.OperatorsRule < 0 && c.m.Flows[i] == nil {
			return fmt.Errorf("internal error: rule %s has no Flow body", c.m.Strings[ri.Name])
		}
	}
	for i := range c.m.OperatorsRules {
		pr := &c.m.OperatorsRules[i]
		var core *ir.OperatorsRule
		for j, ri := range c.m.Rules {
			if ri.OperatorsRule == i {
				core = c.core.Rules[j].Body.(*ir.OperatorsRule)
			}
		}
		if core.Skip != nil && pr.SkipFlow == nil {
			return fmt.Errorf("internal error: the skip of Operators Rule %d has no Flow body", i)
		}
		missing := false
		for j := range pr.Operands {
			missing = missing || pr.Operands[j].Flow == nil
		}
		for _, ops := range [][]bytecode.OperatorInfo{pr.Prefix, pr.InfixPostfix} {
			for j := range ops {
				missing = missing || ops[j].Line.Flow == nil
			}
		}
		if missing {
			return fmt.Errorf("internal error: a line of Operators Rule %d has no Flow body", i)
		}
	}
	return nil
}

// actionCode returns the action code of the site of s, translated from its function of the table's
// Action module, adding the strings it uses to the module.
func (c *compiler) actionCode(s actionSite) (bytecode.Action, error) {
	f := c.funcs[s.site]
	if f == nil {
		return bytecode.Action{}, fmt.Errorf("internal error: the Action module has no function of the action site %d", s.site)
	}
	tp, err := ActionProgram(c.actions, f, ActionEnv{Checker: c.in.Elab.Checker(), Vars: c.in.Elab.Checker().Vars, Scope: scopeOf(s.scope)})
	if err != nil {
		return bytecode.Action{}, fmt.Errorf("internal error: %w", err)
	}
	t, err := tp.Export(func(s string) int { return int(c.str(s)) })
	if err != nil {
		return bytecode.Action{}, fmt.Errorf("internal error: %w", err)
	}
	return t, nil
}

// compiler builds a bytecode module (Compile): m is the module it fills and strs indexes the
// module's string table.
type compiler struct {
	in   Input
	core *ir.Module
	// sites numbers the action sites of core, which the operations of predicates and bindings name.
	sites *ir.ActionSites
	// actions is the Action module of the table, and funcs its function of each site by number.
	actions *code.Module
	funcs   map[int]*code.Func
	// tab holds the tables of the Flow module of the table, which its immediates index.
	tab  *prim.Tables
	m    *bytecode.Module
	strs map[string]int
	// actionSites are the action sites of the tables, by index of Module.Actions, whose code Compile
	// makes once the tables are built.
	actionSites []actionSite
	// scope holds the capture slots of the frame of the action site being added (actionSite).
	scope []ir.Slot
	err   error
	// counts holds the number of matches of each pattern of statements of the selection table, by
	// name.
	counts map[string]int
}

// actionSite is an action site of the module: its number among the action sites of the table's Core
// IR and the slots of the frame it reads.
type actionSite struct {
	site  int
	scope []ir.Slot
}

// scopeOf returns the capture scope of the Core IR slots s.
func scopeOf(s []ir.Slot) *program.Scope {
	sc := program.NewScope()
	for i, x := range s {
		sc.Names = append(sc.Names, x.Name)
		sc.Slots[x.Name] = i
	}
	return sc
}

func (c *compiler) failf(format string, args ...any) {
	if c.err == nil {
		c.err = fmt.Errorf("internal error: "+format, args...)
	}
}

func (c *compiler) str(s string) int32 {
	if i, ok := c.strs[s]; ok {
		return int32(i)
	}
	c.strs[s] = len(c.m.Strings)
	c.m.Strings = append(c.m.Strings, s)
	return int32(len(c.m.Strings) - 1)
}

func (c *compiler) strList(names []string) []int {
	out := make([]int, len(names))
	for i, n := range names {
		out[i] = int(c.str(n))
	}
	return out
}

// exp returns the string of the expectation id of the module.
func (c *compiler) exp(id prim.ExpID) int32 { return c.str(c.tab.Exp(id)) }

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// compile fills the module with the type, variable, rule and Operators Rule tables.
func (c *compiler) compile() {
	chk := c.in.Elab.Checker()
	for _, decl := range chk.Grammar.Types() {
		td, ok := decl.(*ast.TypeDef)
		if !ok {
			continue // an alias is not a type of the module
		}
		ti := bytecode.TypeInfo{Name: int(c.str(td.DeclName())), Node: -1}
		if n, ok := chk.NodeTypeOf(td.DeclName()); ok {
			ti.Node = int(c.str(types.NewFieldType(n.Underlying()).Text))
		}
		for _, f := range check.DefFields(td) {
			ti.Fields = append(ti.Fields, int(c.str(f.Name)))
		}
		if z := c.in.Run.StructZero(td.DeclName()); z != nil {
			for _, f := range z.Names {
				ti.Zeros = append(ti.Zeros, int(c.str(z.Zeros[f])))
			}
		}
		c.m.Types = append(c.m.Types, ti)
	}
	if t := c.in.Run.Vars; t != nil {
		for _, d := range t.List {
			vi := bytecode.VarInfo{Name: int(c.str(d.Name)), Str: -1}
			switch v := d.Init.(type) {
			case int:
				vi.Type, vi.Int = bytecode.VarInt, v
			case string:
				vi.Type, vi.Str = bytecode.VarString, int(c.str(v))
			case bool:
				vi.Type, vi.Int = bytecode.VarBool, int(b2i(v))
			}
			c.m.Vars = append(c.m.Vars, vi)
		}
	}
	// The rule table.
	all := append(append([]*program.Rule(nil), c.in.Run.RuleList...), c.in.Run.Twins...)
	if len(all) != len(c.core.Rules) {
		c.failf("%d rules in Core IR, %d in the program", len(c.core.Rules), len(all))
		return
	}
	for _, r := range all {
		ri := bytecode.RuleInfo{Name: int(c.str(r.Name)), Action: -1, NodeType: -1, OperatorsRule: -1,
			Memo: r.Memo, Leader: r.Leader, Member: r.Member, Cycle: r.Cycle, PositionDependent: r.Positional, ValueFreeTwin: r.Novalue,
			Transient: r.Transient, Rec: r.Rec, Where: -1, Splice: r.Splice, Vars: c.strList(r.Vars)}
		if r.Where != "" && r.Where != r.Name {
			ri.Where = int(c.str(r.Where))
		}
		if r.NodeType != "" {
			ri.NodeType = int(c.str(r.NodeType))
		} else if r.OpNodeType != "" {
			ri.NodeType = int(c.str(r.OpNodeType)) // that of the operator nodes of an Operators Rule
		}
		c.m.Rules = append(c.m.Rules, ri)
	}
	keys := c.in.Run.RunKeys
	sites := slices.Sorted(maps.Keys(keys))
	for _, site := range sites {
		k := keys[site]
		c.m.RunKeys = append(c.m.RunKeys, bytecode.RunKeyInfo{Site: site, Vars: c.strList(k.Vars), Bind: c.strList(k.Bind)})
	}
	for i, r := range c.in.Run.RuleList {
		if slices.Contains(c.in.Run.StartRules, r.Name) {
			c.m.Starts = append(c.m.Starts, i)
		}
		if r.Name == c.in.Run.DefaultStartRule {
			c.m.DefaultStart = i
		}
	}
	for i, cr := range c.core.Rules {
		ri := &c.m.Rules[i]
		c.scope = cr.Scope
		o, ok := cr.Body.(*ir.OperatorsRule)
		if ok {
			ri.OperatorsRule = c.operatorsRule(o)
			c.scope = nil
		}
		ri.Scope = c.strList(ir.SlotNames(cr.Scope))
		if cr.Finish.Kind == ir.FinishScope && !ok && !ri.Splice && !ri.ValueFreeTwin {
			// The Flow bodies of the rule are its framed bodies, which leave the rule's value to the
			// call, as the generated parsers' rule tables do.
			sh := coreShapeOf(cr.Finish.Shape)
			ri.Value = &sh
		}
		if cr.Finish.Kind == ir.FinishAction {
			ri.Action = c.actionSite(c.sites.OfRule(cr.ID))
		}
	}
}

// actionSite adds the action site i, which reads the frame of the current scope, to the module and
// returns its index.
func (c *compiler) actionSite(i int) int {
	c.actionSites = append(c.actionSites, actionSite{site: i, scope: c.scope})
	c.m.Actions = append(c.m.Actions, bytecode.Action{})
	return len(c.m.Actions) - 1
}

// class adds the character class id of the module to the character class table and returns its
// index.
func (c *compiler) class(id prim.ClassID) int32 {
	cl := c.tab.Class(id)
	bc := bytecode.Class{Negated: cl.Negated, Expectation: int(c.exp(cl.Exp))}
	for _, r := range cl.Ranges {
		bc.Ranges = append(bc.Ranges, r.Lo, r.Hi)
	}
	c.m.Classes = append(c.m.Classes, bc)
	return int32(len(c.m.Classes) - 1)
}

// litDesc returns the string of the expectation of the literal n. The empty literal, which
// cannot fail, records none; its operand is the quoted empty text.
func (c *compiler) litDesc(n prim.Lit) int32 {
	if n.Text == "" {
		return c.str(ir.LiteralDesc(&ast.Literal{}))
	}
	return c.exp(n.Exp)
}

// operatorsRule adds the table of the Operators Rule o to the module and returns its index. The Flow
// bodies of its skip and lines are selected later (selectFlows).
func (c *compiler) operatorsRule(o *ir.OperatorsRule) int {
	var pi bytecode.OperatorsRuleInfo
	for _, l := range o.Operands {
		pi.Operands = append(pi.Operands, c.line(l))
	}
	for _, op := range o.Ops {
		oi := bytecode.OperatorInfo{ID: op.ID, Level: op.Level}
		switch op.Kind {
		case ast.Prefix:
			oi.Kind = bytecode.OperatorPrefix
		case ast.Postfix:
			oi.Kind = bytecode.OperatorPostfix
		default:
			oi.Kind = bytecode.OperatorInfix
		}
		switch op.Assoc {
		case ast.AssocRight:
			oi.Assoc = bytecode.AssocRight
		case ast.AssocNone:
			oi.Assoc = bytecode.AssocNone
		}
		oi.Line = c.line(op.Line)
		if oi.Kind == bytecode.OperatorPrefix {
			pi.Prefix = append(pi.Prefix, oi)
		} else {
			pi.InfixPostfix = append(pi.InfixPostfix, oi)
		}
	}
	c.m.OperatorsRules = append(c.m.OperatorsRules, pi)
	return len(c.m.OperatorsRules) - 1
}

// line returns the table entry of a line of an Operators Rule: its scope and the action site of its action.
func (c *compiler) line(l *ir.OpLine) bytecode.LineInfo {
	c.scope = l.Scope
	li := bytecode.LineInfo{Action: -1}
	li.Scope = c.strList(ir.SlotNames(l.Scope))
	if l.Action != nil {
		li.Action = c.actionSite(c.sites.Of(l))
	}
	return li
}

// foldedRunes returns s with the ASCII folding of #ignorecase (state.FoldASCIIRune) applied to each
// rune: the text a folded literal is compared with.
func foldedRunes(s string) string {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = state.FoldASCIIRune(r)
	}
	return string(rs)
}
