package vm

import (
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Program is a module prepared for execution. Rule calls, memoization, left recursion, and
// end-of-rule processing use the runtime shared with the closure engine (state/runtime.go), while rule
// bodies and Operators Rule lines and skips run as Flow bodies, and actions and predicates as action
// code.
type Program struct {
	m        *bytecode.Module
	rules    []*state.Rule
	byName   map[string]*state.Rule
	nseen    int        // number of rules with rule.seen set
	runes    [][]rune   // code points of each string in the string table
	bytes    [][]byte   // UTF-8 of the decoded literal strings
	literals []string   // matching text; aliases m.Strings unless normalization is needed
	scopes   [][]string // names in the scope table
	ascii    []asciiSet // the ASCII part of each class, as a bitmap
	descs    []string   // expectation table: fixedDescs followed by the string table (string i has index numFixedDescs+i)
	// scopeOf holds the scope of each entry of the scope table, which the Flow bodies' ATTACHFRAME
	// attaches.
	scopeOf []*program.Scope
	// action expressions holds the programs of the module's action expressions (Module.Actions).
	actionExprs []*state.ActionProgram
	// nodeKinds holds the kinds of the module's named node types by the string index of their
	// names, at every index of the name (nil when the module has none).
	nodeKinds []*nodes.NodeTypeKinds
	// vars is the module's variable table (nil when it declares none).
	vars *program.VarTable
	// direct reports, by rule ID, that a call of the rule invokes it with state.Parser.InvokePlain,
	// leaving out the call protocol of state.Parser.Call: the rule is plain (core.Rule.Plain) and the
	// program runs the inlined family (state.Inlined).
	direct []bool
	// flows holds, by rule ID, the Flow body of the rule (Module.Flows), nil for a rule whose body is
	// the expression of its Operators Rule.
	flows []*bytecode.Flow
	// streams holds, by rule ID, the stream body of the rule (Module.Streams), which a call that
	// delivers the elements of the rule's #stream repetition (state.Parser.StreamsIn) runs in place of
	// its body; nil for a rule without one.
	streams []*bytecode.Flow
	// lines holds the Flow body of each line and skip of the Operators Rules, which the iterative
	// model runs: state.OperatorsRuleLine.Entry and state.OperatorsRule.SkipEntry index it.
	lines []*bytecode.Flow
}

// addLine adds the Flow body f of a line or skip to vm.lines and returns its index.
func (vm *Program) addLine(f *bytecode.Flow) int {
	vm.lines = append(vm.lines, f)
	return len(vm.lines) - 1
}

// lineMatcher returns a matcher that runs the Flow body f of a line or skip with the recursive
// model.
func (vm *Program) lineMatcher(f *bytecode.Flow) state.Matcher {
	return func(p *state.Parser) (*value.Node, bool) { return execFlow(vm, p, f) }
}

// vmBodies returns the body of the rule r in vm. Each driver of the VM passes its own to
// buildVMProgram, deciding how a body runs the rule's Flow body (Program.flows) or the expression
// of its Operators Rule: the recursive VM's execBody recurses for each rule call.
type vmBodies func(vm *Program, r *state.Rule) func(p *state.Parser, min int) (*value.Node, bool)

// New prepares a module for execution with the recursive model, in the family f; action expressions are the
// programs of its action expressions (LoadActions).
func New(m *bytecode.Module, actionExprs []*state.ActionProgram, f state.Family) *Program {
	return buildVMProgram(m, actionExprs, f, execBody)
}

// Rule returns the rule called name, or nil when the module has none.
func (vm *Program) Rule(name string) *state.Rule { return vm.byName[name] }

// Descs returns the expectation table of the program: program.FixedDescs followed by the string
// table.
func (vm *Program) Descs() []string { return vm.descs }

// NumSeen returns the number of rules whose memoization a first call may defer
// (program.NumberSeen), which a parse uses as the stride of its memo table.
func (vm *Program) NumSeen() int { return vm.nseen }

// execBody runs a rule body with the recursive model: each call recurses into the Flow body that
// it runs (Program.ruleFlow), or into the expression of the rule's Operators Rule.
func execBody(vm *Program, r *state.Rule) func(p *state.Parser, min int) (*value.Node, bool) {
	if vm.streams[r.ID] != nil {
		// Which body a call runs depends on whether it delivers the elements of the stream.
		return func(p *state.Parser, min int) (*value.Node, bool) { return execFlow(vm, p, vm.ruleFlow(p, r)) }
	}
	if f := vm.flows[r.ID]; f != nil {
		return flowBody(vm, f)
	}
	return func(p *state.Parser, min int) (*value.Node, bool) { return p.OperatorsRuleParse(r, min) }
}

// buildVMProgram prepares a module for execution in the family f. bodies makes the rule bodies.
func buildVMProgram(m *bytecode.Module, actionExprs []*state.ActionProgram, f state.Family, bodies vmBodies) *Program {
	vm := &Program{m: m, byName: map[string]*state.Rule{}, descs: append(append([]string(nil), program.FixedDescs...), m.Strings...)}
	vm.literals = m.Strings
	normalized := false
	for i, s := range m.Strings {
		val := nodes.LiteralValue(s)
		if val != s {
			if !normalized {
				vm.literals = append([]string(nil), m.Strings...)
				normalized = true
			}
			vm.literals[i] = val
		}
		vm.runes = append(vm.runes, []rune(val))
		vm.bytes = append(vm.bytes, []byte(val))
	}
	for _, sc := range m.Scopes {
		vm.scopes = append(vm.scopes, vm.names(sc))
		vm.scopeOf = append(vm.scopeOf, &program.Scope{Names: vm.scopes[len(vm.scopes)-1]})
	}
	for i := range m.Classes {
		vm.ascii = append(vm.ascii, newASCIISet(&m.Classes[i]))
	}
	var nodeTypes map[string]*nodes.NodeTypeKinds
	for _, t := range m.Types {
		if t.Node >= 0 {
			if nodeTypes == nil {
				nodeTypes = map[string]*nodes.NodeTypeKinds{}
			}
			nodeTypes[m.Strings[t.Name]] = nodes.NewNodeTypeKinds(m.Strings[t.Name])
		}
	}
	if nodeTypes != nil {
		// The module file checks the type of an ENODE by its name, which the string table may hold at
		// more than one index.
		vm.nodeKinds = make([]*nodes.NodeTypeKinds, len(m.Strings))
		for i, s := range m.Strings {
			vm.nodeKinds[i] = nodeTypes[s]
		}
	}
	vtable := program.ModuleVarTable(m)
	vm.vars = vtable
	vm.actionExprs = actionExprs
	for i := range m.Rules {
		ri := &m.Rules[i]
		r := &state.Rule{
			ID: i, Name: m.Strings[ri.Name], Scope: vm.scope(ri.Scope),
			Memo: ri.Memo, Leader: ri.Leader, Member: ri.Member, Cycle: ri.Cycle, Positional: ri.PositionDependent, Novalue: ri.ValueFreeTwin, Transient: ri.Transient, Rec: ri.Rec, Splice: ri.Splice, Missing: ri.Missing, Vars: vm.names(ri.Vars),
		}
		if ri.Where >= 0 {
			r.Where = m.Strings[ri.Where]
		}
		r.InitVals = vtable.ValuesOf(r.Vars)
		r.VarSlots = vtable.Slots(r.Vars)
		if ri.NodeType >= 0 {
			if ri.OperatorsRule >= 0 && ri.Action < 0 {
				// Without a whole-rule action, the node type of an Operators Rule is that of its
				// operators; with one, it is that of the action's result.
				r.OpNodeType, r.OpKinds = m.Strings[ri.NodeType], vm.nodeKinds[ri.NodeType]
			} else {
				r.NodeType, r.NodeKinds = m.Strings[ri.NodeType], vm.nodeKinds[ri.NodeType]
			}
		}
		r.Kinds = valueimpl.NewRuleKinds(r.Name)
		switch x := ri.Value; {
		case x == nil:
			r.Value = func(_ *state.Parser, v *value.Node, _ int) *value.Node { return v }
		case x.Tuple:
			x := *x
			r.Value = func(p *state.Parser, v *value.Node, start int) *value.Node { return flowShapeTuple(p, x, v, start) }
		default:
			sh := flowShaper(*x)
			r.Value = func(_ *state.Parser, v *value.Node, _ int) *value.Node { return sh.One(v) }
		}
		if ri.Action >= 0 {
			r.Act = vm.actionExprs[ri.Action].Eval
		}
		vm.rules = append(vm.rules, r)
		if _, twin := vm.byName[r.Name]; !twin { // value-free twins follow the original rules
			vm.byName[r.Name] = r
		}
	}
	statics := make([]*program.Rule, len(vm.rules))
	for i, r := range vm.rules {
		statics[i] = state.StaticRuleOf(r)
	}
	vm.nseen = program.NumberSeen(statics)
	program.NumberLeaders(statics)
	vm.flows = make([]*bytecode.Flow, len(vm.rules))
	for i, f := range m.Flows {
		vm.flows[i] = f
	}
	vm.streams = make([]*bytecode.Flow, len(vm.rules))
	for i, f := range m.Streams {
		vm.streams[i] = f
	}
	vm.direct = make([]bool, len(vm.rules))
	for i, r := range vm.rules {
		r.Plain = !r.Leader && (!r.Memo || r.Transient) && len(r.Scope.Names) == 0
		vm.direct[i] = r.Plain && f == state.Inlined
	}
	for i := range m.Rules {
		ri, r := &m.Rules[i], vm.rules[i]
		if ri.OperatorsRule >= 0 {
			r.OperatorsRule = vm.operatorsRule(&m.OperatorsRules[ri.OperatorsRule])
		}
		r.Stream = m.RuleStreams(i)
		r.Body = bodies(vm, r)
	}
	return vm
}

// ruleFlow returns the Flow body that a call of r runs in p: its stream body if the call delivers the
// elements of its #stream repetition, otherwise its Flow body; nil for a rule whose body is the
// expression of its Operators Rule.
func (vm *Program) ruleFlow(p *state.Parser, r *state.Rule) *bytecode.Flow {
	if f := vm.streams[r.ID]; f != nil && p.StreamsIn(r.ID, r.Rec) {
		return f
	}
	return vm.flows[r.ID]
}

func (vm *Program) names(ids []int) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = vm.m.Strings[id]
	}
	return out
}

func (vm *Program) scope(ids []int) *program.Scope {
	s := program.NewScope()
	for _, n := range vm.names(ids) {
		s.Slot(n)
	}
	return s
}

// line returns the line l of an Operators Rule. Its Flow body returns the value of the line's
// expression, which the shape of the line makes the value of the line: the scope value of an operand
// line, the single of an operator line.
func (vm *Program) line(l *bytecode.LineInfo) *state.OperatorsRuleLine {
	pl := &state.OperatorsRuleLine{Scope: vm.scope(l.Scope), M: vm.lineMatcher(l.Flow), Entry: vm.addLine(l.Flow)}
	x := l.Shape
	pl.Single, pl.Shape = program.SingleMode(x.Mode), flowShaper(x)
	if x.Tuple {
		pl.Value = func(p *state.Parser, v *value.Node, start int) *value.Node { return flowShapeTuple(p, x, v, start) }
	} else {
		sh := flowShaper(x)
		pl.Value = func(_ *state.Parser, v *value.Node, _ int) *value.Node { return sh.One(v) }
	}
	if l.Action >= 0 {
		pl.Act = vm.actionExprs[l.Action].Eval
	}
	return pl
}

func (vm *Program) operatorsRule(pi *bytecode.OperatorsRuleInfo) *state.OperatorsRule {
	pr := &state.OperatorsRule{SkipEntry: -1}
	if pi.SkipFlow != nil {
		pr.SkipEntry = vm.addLine(pi.SkipFlow)
		pr.Skip = vm.lineMatcher(pi.SkipFlow)
	}
	for i := range pi.Operands {
		pr.Operands = append(pr.Operands, vm.line(&pi.Operands[i]))
	}
	op := func(o *bytecode.OperatorInfo) *state.OperatorsRuleOp {
		po := &state.OperatorsRuleOp{Id: o.ID, Level: o.Level, Line: vm.line(&o.Line)}
		po.Kind = []string{"prefix", "postfix", "infix"}[o.Kind]
		if o.Kind == bytecode.OperatorInfix {
			po.Assoc = []string{"left", "right", "none"}[o.Assoc]
		}
		return po
	}
	for i := range pi.Prefix {
		pr.Prefix = append(pr.Prefix, op(&pi.Prefix[i]))
	}
	for i := range pi.InfixPostfix {
		pr.Led = append(pr.Led, op(&pi.InfixPostfix[i]))
	}
	return pr
}

// Machine is the state of the bytecode VM for one parse, beside the parser it runs on: the register
// files of the running Flow bodies. The parser holds it as its backend state (machineOf). A driver
// built on this VM that needs state of its own, such as frames, embeds Machine in that state and
// points driver at it.
type Machine struct {
	p      *state.Parser
	regs   regStack // the register files of the running Flow bodies
	driver vmDriverState
}

// vmDriverState is the state that a driver built on the VM keeps beside Machine.
type vmDriverState interface {
	// reset drops the state of the last parse. Machine keeps the driver state for the next
	// parse only through keep, which resets it.
	reset()
}

// machineOf returns the VM state of the parse that p runs, making it on first use.
func machineOf(p *state.Parser) *Machine {
	if mc, ok := p.Backend.(*Machine); ok {
		return mc
	}
	mc := &Machine{p: p}
	p.Backend = mc
	return mc
}

// SplitChunks does nothing: the VM allocates no values from chunks of its own.
func (mc *Machine) SplitChunks() {}

// ClearInactive clears the registers above those of the running Flow bodies, which may still hold
// the values of finished calls. It charges nothing: the registers are no item of the cost
// (spec/runtime/parsing.md#cost), and the backends must charge the same units.
func (mc *Machine) ClearInactive() { mc.regs.clearInactive() }

func (mc *Machine) Attach(p *state.Parser) { mc.p = p }

// Keep keeps the first chunk of the register stack, cleared, whose registers a Document reuses
// (reuseDocumentVMValues), and the reset driver state. The chunk holds the registers of the
// largest body of the grammar at least, which does not grow with the input, so n does not bound it.
func (mc *Machine) Keep(n int, clearUsed bool) {
	if mc.driver != nil {
		mc.driver.reset()
	}
	mc.regs.drop()
	*mc = Machine{regs: mc.regs, driver: mc.driver}
}

func (mc *Machine) Release() {
	if mc.driver != nil {
		mc.driver.reset()
	}
	mc.regs.drop()
	*mc = Machine{regs: mc.regs, driver: mc.driver}
}

// RetainedBytes returns the bytes of the register stack, all of which the state keeps.
func (mc *Machine) RetainedBytes() uint64 { return mc.regs.bytes() }

func (mc *Machine) StackBytes() uint64 { return mc.regs.bytes() }

// NewMachine returns VM state whose register stack starts with the chunk regs (its whole capacity),
// as a Document keeps it between parses.
func NewMachine(regs []any) *Machine {
	mc := &Machine{}
	if cap(regs) > 0 {
		mc.regs.chunks = [][]any{regs[:cap(regs)]}
	}
	return mc
}

// Registers returns the first chunk of the register stack of mc, not a copy, with the length that
// the running Flow bodies use of it; nil if there is none.
func (mc *Machine) Registers() []any {
	if len(mc.regs.chunks) == 0 {
		return nil
	}
	n := len(mc.regs.chunks[0])
	if mc.regs.cur == 0 {
		n = mc.regs.top
	}
	return mc.regs.chunks[0][:n]
}

// Parser returns the parser that mc serves, or nil when it serves none.
func (mc *Machine) Parser() *state.Parser { return mc.p }

// errorNode returns the error node of the recovered error se from start to the position, as the Go
// library's NewErrorNode builds it.
func errorNode(p *state.Parser, start int, se *value.SyntaxError) *value.Node {
	return p.NewNode(nodes.Data{Kind: valueimpl.ErrorKind, Start: int32(start), End: int32(p.Pos), Fields: valueimpl.Fields{{Name: "message", Value: p.RecoveryErrorText(se)}}, Fresh: true, Terminal: true})
}

// asciiSet is the membership of the characters below 128 in a class.
type asciiSet [2]uint64

func newASCIISet(cl *bytecode.Class) asciiSet {
	var s asciiSet
	for ch := rune(0); ch < 128; ch++ {
		if cl.Has(ch) {
			s[ch>>6] |= 1 << (ch & 63)
		}
	}
	return s
}

// has reports whether the character ch is in class a.
func (vm *Program) has(p *state.Parser, a int32, ch rune) bool {
	if uint32(ch) < 128 {
		return vm.ascii[a][ch>>6]&(1<<(ch&63)) != 0
	}
	return vm.m.Classes[a].Has(ch)
}

// --- Actions ---

// The VM implements the backend state of package state.
var _ state.BackendState = (*Machine)(nil)

// LoadActions makes the programs of the action expressions of the module m, checking each (state.LoadAction). A
// loader calls it to reject a module file whose action code is invalid.
func LoadActions(m *bytecode.Module) ([]*state.ActionProgram, error) {
	if len(m.Actions) == 0 {
		return nil, nil
	}
	return state.LoadActions(m.Actions, m.Strings, len(m.Vars), moduleActionTypes(m))
}

// moduleActionTypes resolves the types that the action code of m names from its type table.
func moduleActionTypes(m *bytecode.Module) state.ActionTypes {
	nodeTypes := map[string]*nodes.NodeTypeKinds{}
	structs := map[string]*valueimpl.Kind{}
	for _, t := range m.Types {
		name := m.Strings[t.Name]
		if t.Node >= 0 {
			nodeTypes[name] = nodes.NewNodeTypeKinds(name)
		} else {
			structs[name] = valueimpl.KindOf(name, "")
		}
	}
	var typeNames map[string]bool
	casts := map[string]*program.FieldType{}
	return state.ActionTypes{
		NodeType: func(name string) *nodes.NodeTypeKinds { return nodeTypes[name] },
		Struct:   func(name string) *valueimpl.Kind { return structs[name] },
		Cast: func(text string) (*program.FieldType, error) {
			if t, ok := casts[text]; ok {
				return t, nil
			}
			if typeNames == nil {
				typeNames = program.ModuleTypeNames(m)
			}
			t, err := program.ParseFieldType(text, typeNames)
			if err != nil {
				return nil, err
			}
			casts[text] = t
			return t, nil
		},
	}
}
