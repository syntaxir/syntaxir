package vm

import (
	"fmt"
	"unsafe"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Program is a module prepared for execution. Rule calls, memoization, left recursion, and
// end-of-rule processing use the runtime shared with the closure engine (state/runtime.go), while rule
// bodies, Operators Rule sections, actions, and predicates run as bytecode.
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
	// runSites holds, at the index of each REPEAT that a Document can resume, how to resume it.
	runSites []*vmRunSite
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
	// flows holds, by rule ID, the Flow body that the rule runs in place of its match code
	// (Module.Flows), nil for a rule that runs its match code.
	flows []*bytecode.Flow
	// streams holds, by rule ID, the stream body of the rule (Module.Streams), which a call that
	// delivers the elements of the rule's #stream repetition (state.Parser.StreamsIn) runs in place of
	// its body; nil for a rule without one.
	streams []*bytecode.Flow
	// lines holds the code of each line and skip of the Operators Rules, which the iterative model
	// runs: state.OperatorsRuleLine.Entry and state.OperatorsRule.SkipEntry index it.
	lines []lineCode
}

// lineCode is the code of a line or a skip of an Operators Rule: its Flow body, or else the entry of
// its match code.
type lineCode struct {
	entry int
	flow  *bytecode.Flow
}

// addLine adds the code of a line or skip to vm.lines and returns its index.
func (vm *Program) addLine(entry int, f *bytecode.Flow) int {
	vm.lines = append(vm.lines, lineCode{entry: entry, flow: f})
	return len(vm.lines) - 1
}

// lineMatcher returns a matcher that runs the line or skip code c with the recursive model.
func (vm *Program) lineMatcher(c lineCode) state.Matcher {
	if c.flow == nil {
		return vm.block(c.entry)
	}
	body := flowBody(vm, c.flow)
	return func(p *state.Parser) (*value.Node, bool) { return body(p, 0) }
}

// vmRunSite is a repetition that a Document can resume (state/resume.go): the REPEAT at ip, whose ITER
// follows it, with NEXT just before its ENDREPEAT.
type vmRunSite struct {
	shiftable bool // the elements' values contain no position values
}

// runSite returns how to resume the repetition whose REPEAT is at ip, or nil if it cannot be
// resumed (as compiler.resumable): its element evaluates predicates or calls rules that read
// variables.
func (vm *Program) runSite(ip int) *vmRunSite {
	code := vm.m.Code
	if ip+1 >= len(code) || code[ip+1].Op != bytecode.OpIter {
		return nil
	}
	end := int(code[ip+1].A)
	if end <= ip+2 || end >= len(code) || code[end].Op != bytecode.OpEndRepeat || code[end-1].Op != bytecode.OpNext {
		return nil
	}
	site := &vmRunSite{shiftable: true}
	for _, in := range code[ip+2 : end-1] {
		switch in.Op {
		case bytecode.OpPred, bytecode.OpBind:
			return nil
		case bytecode.OpCapture:
			if code[ip].C < 0 { // the element writes captures to the enclosing scope (in &)
				return nil
			}
		case bytecode.OpCall:
			if in.A < 0 || int(in.A) >= len(vm.rules) {
				return nil
			}
			r := vm.rules[in.A]
			if len(r.Vars) > 0 {
				return nil
			}
			if r.Positional {
				site.shiftable = false
			}
		}
	}
	return site
}

// vmBodies returns the body of the rule r, whose code starts at entry in vm. Each driver of the
// VM passes its own to buildVMProgram, deciding how a body runs the code or the rule's Flow body
// (Program.flows): the recursive VM's execBody calls exec or flowBody, which recurse for each rule
// call.
type vmBodies func(vm *Program, r *state.Rule, entry int) func(p *state.Parser, min int) (*value.Node, bool)

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

// execBody runs a rule body with the recursive model: each call recurses into exec, or into the
// Flow body of a rule that has one (Program.ruleFlow).
func execBody(vm *Program, r *state.Rule, entry int) func(p *state.Parser, min int) (*value.Node, bool) {
	if vm.streams[r.ID] != nil {
		// Which body a call runs depends on whether it delivers the elements of the stream.
		return func(p *state.Parser, min int) (*value.Node, bool) {
			if f := vm.ruleFlow(p, r); f != nil {
				return execFlow(vm, p, f)
			}
			v, ok, _ := machineOf(p).exec(vm, entry, r, min)
			return v, ok
		}
	}
	if f := vm.flows[r.ID]; f != nil {
		return flowBody(vm, f)
	}
	return func(p *state.Parser, min int) (*value.Node, bool) {
		v, ok, _ := machineOf(p).exec(vm, entry, r, min)
		return v, ok
	}
}

// buildVMProgram prepares a module for execution in the family f. bodies makes the rule bodies; a
// rule with a Flow body (bytecode.Flow) runs it in place of its match code in both models.
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
			Memo: ri.Memo, Leader: ri.Leader, Member: ri.Member, Cycle: ri.Cycle, Positional: ri.PositionDependent, Novalue: ri.ValueFreeTwin, Transient: ri.Transient, Rec: ri.Rec, Splice: ri.Splice, Vars: vm.names(ri.Vars),
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
		r.Value = func(_ *state.Parser, v *value.Node, _ int) *value.Node { return v } // the code pushes the value of the body's scope
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
	vm.runSites = make([]*vmRunSite, len(m.Code))
	for ip, in := range m.Code {
		if in.Op == bytecode.OpRepeat || in.Op == bytecode.OpRepeatWide {
			vm.runSites[ip] = vm.runSite(ip)
		}
	}
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
		entry := ri.Entry
		r.Entry = entry
		r.Stream = m.RuleStreams(i)
		r.Body = bodies(vm, r, entry)
	}
	return vm
}

// ruleFlow returns the Flow body that a call of r runs in p: its stream body if the call delivers the
// elements of its #stream repetition, otherwise its Flow body; nil for a rule that runs its match
// code.
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

// block returns a matcher that runs the code of an Operators Rule section. It sets p.cut if a cut is passed
// within the section.
func (vm *Program) block(entry int) state.Matcher {
	return func(p *state.Parser) (*value.Node, bool) {
		v, ok, cut := machineOf(p).exec(vm, entry, nil, 0)
		if cut {
			p.Cut = true
		}
		return v, ok
	}
}

func (vm *Program) line(l *bytecode.LineInfo, operator bool) *state.OperatorsRuleLine {
	c := lineCode{entry: l.Entry, flow: l.Flow}
	pl := &state.OperatorsRuleLine{Scope: vm.scope(l.Scope), M: vm.lineMatcher(c), Entry: vm.addLine(l.Entry, l.Flow)}
	switch {
	case l.Flow != nil:
		// The body returns the value of the line's expression, which the shape of the line makes the
		// value of the line: the scope value of an operand line, the single of an operator line.
		x := l.Shape
		pl.Single, pl.Shape = program.SingleMode(x.Mode), flowShaper(x)
		if x.Tuple {
			pl.Value = func(p *state.Parser, v *value.Node, start int) *value.Node { return flowShapeTuple(p, x, v, start) }
		} else {
			sh := flowShaper(x)
			pl.Value = func(_ *state.Parser, v *value.Node, _ int) *value.Node { return sh.One(v) }
		}
	default:
		// The code pushes the line's value already: the value of an operand line's scope, or the
		// operator's value, which is the node of its tuple if IsSeq (its captures are attached to it).
		pl.Single = program.SinglePass
		pl.Value = func(_ *state.Parser, v *value.Node, _ int) *value.Node { return v }
		if operator && l.IsSeq {
			pl.Single, pl.Shape = program.SingleTuple, program.Shaper{Elems: true}
		}
	}
	if l.Action >= 0 {
		pl.Act = vm.actionExprs[l.Action].Eval
	}
	return pl
}

func (vm *Program) operatorsRule(pi *bytecode.OperatorsRuleInfo) *state.OperatorsRule {
	pr := &state.OperatorsRule{SkipEntry: -1}
	if pi.Skip >= 0 || pi.SkipFlow != nil {
		pr.SkipEntry = vm.addLine(pi.Skip, pi.SkipFlow)
		pr.Skip = vm.lineMatcher(vm.lines[pr.SkipEntry])
	}
	for i := range pi.Operands {
		pr.Operands = append(pr.Operands, vm.line(&pi.Operands[i], false))
	}
	op := func(o *bytecode.OperatorInfo) *state.OperatorsRuleOp {
		po := &state.OperatorsRuleOp{Id: o.ID, Level: o.Level, Line: vm.line(&o.Line, true)}
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

// Machine is the state of the bytecode VM for one parse, beside the parser it runs on: the
// value, entry, label and repetition stacks of the running code, the starts of the lists being
// gathered, the chunks that lambdas and pushed positions are allocated from, and the runStates of
// finished repetitions for reuse. The parser holds it as its backend state (machineOf). A
// driver built on this VM that needs state of its own, such as frames, embeds Machine in that
// state and points driver at it.
type Machine struct {
	p        *state.Parser
	vals     []any      // value stack
	ents     []vmEntry  // entry stack
	labs     []labState // state of the label, recovery and skip entries on ents
	reps     []repState
	scans    []scanWalk        // the walks of scan sites in progress that gather a list
	posSlab  []int             // positions pushed as values (newPos); stream splits keep it
	freeRuns []*state.RunState // runStates of finished repetitions (newRunState)
	regs     regStack          // the register files of the running Flow bodies
	driver   vmDriverState
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

// SplitChunks keeps the position chunk, so its bytes stay charged.
func (mc *Machine) SplitChunks() {
	if len(mc.posSlab) > 0 {
		mc.p.MemUsed += state.PtrChunk * uint64(unsafe.Sizeof(0))
	}
}

func (mc *Machine) ClearInactive() { state.ClearSpend(mc.p, mc.vals[len(mc.vals):cap(mc.vals)]) }

func (mc *Machine) Attach(p *state.Parser) { mc.p = p }

// Keep keeps only the value stack, whose capacity a Document reuses (reuseDocumentVMValues), and
// the reset driver state.
func (mc *Machine) Keep(n int, clearUsed bool) {
	vals := mc.vals[:0]
	// A large document may have been replaced with a small input. Bound the kept capacity by
	// the current size, with room for small grammars' fixed stack overhead. Division avoids int
	// overflow.
	if cap(vals)/2 > max(512, n) {
		vals = nil
	}
	if clearUsed {
		clear(vals[:cap(vals)])
	}
	if mc.driver != nil {
		mc.driver.reset()
	}
	mc.regs.drop()
	*mc = Machine{vals: vals, regs: mc.regs, driver: mc.driver}
}

func (mc *Machine) Release() {
	clear(mc.vals[:cap(mc.vals)])
	if mc.driver != nil {
		mc.driver.reset()
	}
	mc.regs.drop()
	*mc = Machine{regs: mc.regs, driver: mc.driver}
}

func (mc *Machine) RetainedBytes() uint64 {
	return uint64(cap(mc.vals)) * uint64(unsafe.Sizeof(any(nil)))
}

func (mc *Machine) StackBytes() uint64 {
	return mc.RetainedBytes() + uint64(cap(mc.ents))*uint64(unsafe.Sizeof(vmEntry{})) +
		uint64(cap(mc.labs))*uint64(unsafe.Sizeof(labState{})) + uint64(cap(mc.reps))*uint64(unsafe.Sizeof(repState{})) + uint64(cap(mc.scans))*uint64(unsafe.Sizeof(scanWalk{})) +
		uint64(cap(mc.freeRuns))*uint64(unsafe.Sizeof((*state.RunState)(nil))) + mc.regs.bytes()
}

// NewMachine returns VM state that holds vals as its value stack, as a Document keeps it between
// parses.
func NewMachine(vals []any) *Machine { return &Machine{vals: vals} }

// Values returns the live value stack of mc, not a copy.
func (mc *Machine) Values() []any { return mc.vals }

// Parser returns the parser that mc serves, or nil when it serves none.
func (mc *Machine) Parser() *state.Parser { return mc.p }

// --- Running matching code ---

// Entry kinds
const (
	eChoice = iota
	eIter
	eRunIter // an iteration of a repetition whose run is recorded (repState.run)
	eLook
	eNotLook
	eLabel
	eRecover
	eSkip
)

// vmEntry is an element of the entry stack (see the Failure section of spec/bytecode/README.md). It is
// kept small, since a choice or an iteration pushes one: the state of the rarer label, recovery
// and skip entries is on a separate stack (Machine.labs).
type vmEntry struct {
	kind uint8
	cut  bool
	ip   int32
	lab  int32 // eLabel, eRecover, eSkip: index of the entry's state in Machine.labs
	save vmSave
}

// labState is the state of a label, recovery or skip entry.
type labState struct {
	msg  int32         // eLabel: message
	mode int32         // eLabel: 0 for @error, 1 for @diagnostic
	far  int           // eLabel, eRecover: saved farthest failure. eSkip: recovered failure
	base int           // eLabel, eRecover: saved start of the expectation record (ExpMark)
	bits uint64        // eLabel, eRecover: saved membership filter
	exp  []state.ExpID // eSkip: expectations of the recovered failure
}

// pushLab pushes an entry of kind with the state l.
func (mc *Machine) pushLab(kind uint8, ip int32, save vmSave, l labState) {
	mc.labs = append(mc.labs, l)
	mc.ents = append(mc.ents, vmEntry{kind: kind, ip: ip, lab: int32(len(mc.labs) - 1), save: save})
}

// popLab pops the state of the entry e, which has just been popped.
func (mc *Machine) popLab(e *vmEntry) labState {
	l := mc.labs[e.lab]
	mc.labs = mc.labs[:e.lab]
	return l
}

// vmSave is a saved state. Stack heights are int32 to keep entries small.
type vmSave struct {
	pos                          int
	vals, reps, trail, recovered int32
	env                          *state.Env
	frame                        *state.Frame
}

type repState struct {
	min, max, count int
	scope           int
	base            int             // bottom of the element values
	run             *state.RunState // the run being recorded in a Document parse, or nil
	skip            bool            // a skip-run site (SKIPREPEAT) or a run site whose walk has begun
	build           bool            // a run site whose walk gathers the element values
	cold            bool            // a run site that is not hot: an ordinary repetition until its end
	scan, hot, hit  bool            // the walk of a scan site has reached ScanHotSpan elements; the site is hot; it took the rest from a record
	coldStart, site int             // where the walk of a cold run site or of a scan site began, and its number
}

// scanWalk is the state of the walk of a scan site that builds a list, beyond that of its repState
// (state/scan_sites.go). The walks in progress are a stack, in the order of their repetitions.
type scanWalk struct {
	hw0  int
	kids []*value.Node // the elements of the record that the walk took the rest from
}

func (mc *Machine) save() vmSave {
	p := mc.p
	return vmSave{pos: p.Pos, vals: int32(len(mc.vals)), reps: int32(len(mc.reps)), trail: int32(len(p.Trail)),
		recovered: int32(len(p.Recovered)), env: p.Env, frame: p.Frame}
}

func (mc *Machine) restore(s *vmSave) {
	p := mc.p
	p.Pos = s.pos
	mc.vals = mc.vals[:s.vals]
	mc.reps = mc.reps[:s.reps]
	p.UndoCaptures(int(s.trail))
	p.Trail = p.Trail[:state.Min2(int(s.trail), len(p.Trail))]
	p.Recovered = p.Recovered[:s.recovered]
	p.Env = s.env
	p.Frame = s.frame
}

func (mc *Machine) push(v any) { mc.vals = append(mc.vals, v) }
func (mc *Machine) pop() any {
	v := mc.vals[len(mc.vals)-1]
	mc.vals = mc.vals[:len(mc.vals)-1]
	return v
}

func asNodeValue(v any) *value.Node {
	if v == nil {
		return nil
	}
	return v.(*value.Node)
}

// exec runs matching code from ip until RETURN or END (the recursive model). On success it
// returns the pushed value. cut reports whether a cut belonging to no choice was passed (used
// in Operators Rule sections). Rule calls and Operators Rule expressions call the shared runtime (call,
// OperatorsRuleParse) through host recursion.
func (mc *Machine) exec(vm *Program, ip int, r *state.Rule, min int) (result *value.Node, ok bool, cut bool) {
	p := mc.p
	bv := mc.newBody(ip, r, min)
	b := &bv
	ev, v, ok := mc.step(vm, b, false, false, nil)
	for {
		switch ev {
		case evCall:
			in := &vm.m.Code[b.ip]
			if r := vm.rules[in.A]; vm.direct[in.A] {
				v, ok = p.CallPlain(r, int(in.B)) // what call does for such a rule
			} else {
				v, ok = p.Call(r, int(in.B))
			}
		case evOperatorsRule:
			v, ok = p.OperatorsRuleParse(b.r, b.min)
		default:
			return v, ok, b.cut
		}
		ev, v, ok = mc.step(vm, b, true, ok, v)
	}
}

// vmBody is the state of one run of code (a rule body or an Operators Rule section).
type vmBody struct {
	ip                  int
	vbase, ebase, rbase int
	r                   *state.Rule
	min                 int
	cut                 bool // a cut belonging to no choice was passed
}

func (mc *Machine) newBody(ip int, r *state.Rule, min int) vmBody {
	return vmBody{ip: ip, vbase: len(mc.vals), ebase: len(mc.ents), rbase: len(mc.reps), r: r, min: min}
}

// Reasons step suspends
const (
	evDone          = iota // finished by RETURN, END, or failure
	evCall                 // CALL (the instruction at b.ip); resumed with the result
	evOperatorsRule        // OPERATORS_RULE; resumed with the result
)

// step runs code until it finishes or reaches a rule call or an Operators Rule expression.
// If resume is set, it resumes with the result (ok, v) of the suspended instruction.
func (mc *Machine) step(vm *Program, b *vmBody, resume, rok bool, rv *value.Node) (ev int, result *value.Node, ok bool) {
	p := mc.p
	m := vm.m
	code := m.Code
	ip, ebase := b.ip, b.ebase
	if resume {
		if rok {
			if in := &code[ip]; in.Op == bytecode.OpOperatorsRule || in.C == 1 {
				mc.push(rv)
			}
			ip++
		} else if ip, ok = mc.unwind(m, ebase); !ok {
			mc.vals = mc.vals[:b.vbase]
			mc.reps = mc.reps[:b.rbase]
			return evDone, nil, false
		}
	}
	for {
		in := &code[ip]
		switch in.Op {
		case bytecode.OpStr:
			start := p.Pos
			if !p.MatchLiteral(vm.runes[in.A], vm.bytes[in.A]) {
				p.Expect(start, program.NumFixedDescs+state.ExpID(in.B))
				goto fail
			}
			if in.C == 1 {
				mc.push(p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.LiteralText(start, vm.literals[in.A]), Terminal: true, Fresh: true}))
			}
		case bytecode.OpStrFold:
			start := p.Pos
			if !p.MatchLiteralFold(vm.runes[in.A]) {
				p.Expect(start, program.NumFixedDescs+state.ExpID(in.B))
				goto fail
			}
			if in.C == 1 {
				mc.push(p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpClass:
			ch, size, ok := p.Peek()
			if !ok || !vm.has(p, in.A, ch) {
				p.Expect(p.Pos, program.NumFixedDescs+state.ExpID(in.B))
				goto fail
			}
			mc.single(size, in.C == 1)
		case bytecode.OpGuard:
			ch, _, ok := p.Peek()
			if !(ok && vm.has(p, in.A, ch)) && p.Depth+int(in.B) <= p.MaxDepth {
				p.Expect(p.Pos, program.NumFixedDescs+state.ExpID(m.Classes[in.A].Expectation))
				ip = int(in.C)
				continue
			}
		case bytecode.OpScan, bytecode.OpScanWide:
			min, max := int(in.B), int(in.C)
			if in.Op == bytecode.OpScanWide {
				min, max = int(m.Ints[in.B]), int(m.Ints[in.C])
			}
			count := 0
			for max < 0 || count < max {
				p.Spend()
				ch, size, ok := p.Peek()
				if !ok || in.A >= 0 && !vm.has(p, in.A, ch) {
					if in.A >= 0 {
						p.Expect(p.Pos, program.NumFixedDescs+state.ExpID(m.Classes[in.A].Expectation))
					} else {
						p.Expect(p.Pos, program.IDAny)
					}
					break
				}
				p.Pos += size
				count++
			}
			if count < min {
				goto fail
			}
		case bytecode.OpScanSite:
			// As OpScan without a maximum, and as closure's scanSite: the walk takes what is left of it
			// after ScanHotSpan characters from the record of its start, or records itself.
			start, site, count, hot, hit, hw0 := p.Pos, int(in.C), 0, false, false, 0
			for {
				if count == p.Tuning().ScanHotSpan() {
					hot, hw0 = p.ScanHot(site), p.Hw
					p.Hw = 0
					if hot {
						var rec state.ScanRecord
						if rec, hit = p.ScanLookup(site, start); hit {
							p.ScanJump(rec)
							count = rec.Count()
						}
					}
				}
				p.Spend()
				ch, size, ok := p.Peek()
				if !ok || in.A >= 0 && !vm.has(p, in.A, ch) {
					if in.A >= 0 {
						p.Expect(p.Pos, program.NumFixedDescs+state.ExpID(m.Classes[in.A].Expectation))
					} else {
						p.Expect(p.Pos, program.IDAny)
					}
					break
				}
				p.Pos += size
				count++
			}
			if count >= p.Tuning().ScanHotSpan() {
				p.FinishScan(site, start, count, hot, hit, hw0)
			}
			if count < int(in.B) {
				goto fail
			}
		case bytecode.OpAny:
			_, size, ok := p.Peek()
			if !ok {
				p.Expect(p.Pos, program.IDAny)
				goto fail
			}
			mc.single(size, in.A == 1)
		case bytecode.OpTop:
			if in.A == 1 {
				mc.push(p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Start: int32(p.Pos), End: int32(p.Pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpFail:
			goto fail
		case bytecode.OpAssert:
			var ok bool
			var desc state.ExpID
			switch in.A {
			case 0:
				ok, desc = p.AtInputStart(), program.IDBeginInput
			case 1:
				ok, desc = p.AtEOF(), program.IDEndInput
			case 2:
				ok, desc = p.AtLineStart(), program.IDBeginLine
			default:
				ch, _, more := p.Peek()
				ok, desc = !more || ch == '\n' || ch == '\r', program.IDEndLine
			}
			if !ok {
				p.Expect(p.Pos, desc)
				goto fail
			}
		case bytecode.OpJump:
			ip = int(in.A)
			continue
		case bytecode.OpChoice:
			mc.ents = append(mc.ents, vmEntry{kind: eChoice, ip: in.A, save: mc.save()})
		case bytecode.OpCommit:
			mc.ents = mc.ents[:len(mc.ents)-1]
			ip = int(in.A)
			continue
		case bytecode.OpCut:
			// A cut charges nothing: the entries it walks were pushed by the rule body's own
			// expressions, a constant of the grammar.
			marked := false
			for k := len(mc.ents) - 1; k >= ebase; k-- {
				e := &mc.ents[k]
				if e.kind == eChoice || e.kind == eIter || e.kind == eRunIter {
					e.cut = true
					marked = true
					break
				}
				if e.kind == eLook || e.kind == eNotLook {
					marked = true // a cut inside a lookahead does not reach outside it
					break
				}
			}
			if !marked {
				b.cut = true
			}
		case bytecode.OpPushPos:
			mc.push(mc.newPos(p.Pos))
		case bytecode.OpPushNil:
			mc.push(nil)
		case bytecode.OpSeq:
			n := int(in.A)
			kids := p.Nodes(n)
			top := len(mc.vals)
			for k := 0; k < n; k++ {
				kids[k] = asNodeValue(mc.vals[top-n+k])
			}
			start := *mc.vals[top-n-1].(*int)
			mc.vals = mc.vals[:top-n-1]
			mc.push(p.NewNode(nodes.Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(p.Pos), Children: kids, Fresh: true}))
		case bytecode.OpSpread:
			v := mc.pop()
			switch {
			case in.B == 1:
				if n := asNodeValue(v); n != nil {
					for _, k := range nodes.D(n).Children {
						mc.push(k)
					}
				}
			case in.A == 1:
				mc.push(v)
			}
		case bytecode.OpAtomic:
			start := *mc.pop().(*int)
			if in.A == 1 {
				mc.push(p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpCapture:
			p.SetCapture(int(in.A), asNodeValue(mc.vals[len(mc.vals)-1]))
			if in.B == 1 {
				mc.vals = mc.vals[:len(mc.vals)-1]
			}
		case bytecode.OpRepeat, bytecode.OpRepeatWide:
			if state.StreamPrefixIsolation && p.Emit != nil && b.r != nil && p.StreamsIn(b.r.ID, b.r.Rec) && vm.isStreamRepeat(ip) {
				p.StartStreamChunks()
			}
			min, max := int(in.A), int(in.B)
			if in.Op == bytecode.OpRepeatWide {
				min, max = int(m.Ints[in.A]), int(m.Ints[in.B])
			}
			mc.reps = append(mc.reps, repState{min: min, max: max, scope: int(in.C), base: len(mc.vals)})
			if p.Runs != nil && p.Silent == 0 {
				if site := vm.runSites[ip]; site != nil {
					// A Document parse: resume the run of the previous parse (state/resume.go).
					r := mc.newRunState()
					p.RunStart(r, ip, min, max, site.shiftable)
					mc.reps[len(mc.reps)-1].run = r
				}
			}
		case bytecode.OpSkipRepeat:
			min := int(in.A)
			if in.C&1 == 1 {
				min = int(m.Ints[in.A])
			}
			mc.reps = append(mc.reps, repState{min: min, max: -1, scope: -1, base: len(mc.vals), skip: true})
			p.SkipBegin(int(in.B), in.C&2 == 0)
		case bytecode.OpIter:
			rep := &mc.reps[len(mc.reps)-1]
			scanSite, runSite := 0, int(in.C)
			if in.B&4 != 0 { // (the scan sites of OpIter build lists: the others are OpScanSite)
				scanSite, runSite = runSite, 0
			}
			// A repetition that a Document resumes neither looks up nor makes records. This only keeps
			// its cost and reuse as they were: any record would be exact. No test tells the two apart.
			if scanSite > 0 && rep.run == nil && rep.count == p.Tuning().ScanHotSpan() && !rep.scan {
				// The walk of a scan site has reached its ScanHotSpan-th element: as closure's scanSiteList.
				rep.scan, rep.site, rep.coldStart = true, scanSite, *mc.vals[rep.base-1].(*int)
				w := scanWalk{hw0: p.Hw}
				p.Hw = 0
				if rep.hot = p.ScanHot(scanSite); rep.hot {
					var rec state.ScanRecord
					if rec, rep.hit = p.ScanLookup(scanSite, rep.coldStart); rep.hit {
						p.ScanJump(rec)
						rep.count = rec.Count()
						mc.vals = mc.vals[:rep.base]
						w.kids = rec.Kids()
					}
				}
				mc.scans = append(mc.scans, w)
			}
			if rep.max >= 0 && rep.count >= rep.max {
				ip = int(in.A)
				continue
			}
			if runSite > 0 && !rep.skip && !rep.cold && rep.count == 0 && rep.run == nil {
				// The first iteration of a run site that a Document does not resume begins its walk
				// if the site is hot, and is an ordinary repetition until it ends if not.
				switch {
				case !p.RunHot(runSite):
					rep.cold, rep.coldStart, rep.site = true, p.Pos, runSite
				case in.B&1 == 1:
					rep.skip, rep.build = true, true
					p.SkipBeginList(runSite, in.B&2 == 0)
				default:
					rep.skip = true
					p.SkipBegin(runSite, in.B&2 == 0)
				}
			}
			if rep.skip && rep.count >= rep.min && p.SkipHit() {
				ip = int(in.A)
				continue
			}
			if rep.run != nil {
				// The elements that the run reuses charge no unit (state.ResumeRepeat): the iteration
				// takes them until the run ends or the element is evaluated.
				r, done := rep.run, false
				for {
					if !p.RunMore(r) {
						done = true
						break
					}
					v, ok := p.RunNext(r)
					if !ok {
						break
					}
					if m.Code[in.A-1].B != 0 { // NEXT keeps values
						mc.push(v)
					}
				}
				rep.count = r.Count
				if done {
					ip = int(in.A)
					continue
				}
			}
			// One unit for the iteration, before its element (spec/runtime/parsing.md#cost): an
			// iteration that ends at the maximum or takes the rest of the walk from a record evaluates
			// no element.
			p.Spend()
			if rep.skip {
				p.SkipVisit()
			}
			kind := uint8(eIter)
			if rep.run != nil {
				kind = eRunIter
				p.RunBegin(rep.run)
			}
			mc.ents = append(mc.ents, vmEntry{kind: kind, ip: in.A, save: mc.save()})
			if sc := mc.reps[len(mc.reps)-1].scope; sc >= 0 {
				p.Frame = p.NewFrame(len(vm.scopes[sc]))
			}
		case bytecode.OpNext:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			rep := &mc.reps[len(mc.reps)-1]
			if rep.scope >= 0 {
				if in.B == 3 { // a projected repetition: the value is the field's capture
					var v *value.Node
					if int(in.C) < len(p.Frame.Vals) { // (a corrupted file may name another slot)
						v = p.Frame.Vals[in.C]
					}
					mc.push(v)
				} else if in.B != 0 { // in a value-free repetition, the element scope exists only for predicates
					top := len(mc.vals) - 1
					mc.vals[top] = p.AttachCaptures(asNodeValue(mc.vals[top]), &program.Scope{Names: vm.scopes[rep.scope]}, p.Frame, e.save.pos, p.Pos)
				}
				p.Frame = e.save.frame
			}
			if rep.build {
				// A run site walk keeps the element values, and lists them when it ends.
				p.SkipValue(asNodeValue(mc.pop()))
			}
			rep.count++
			if rep.run != nil {
				var v *value.Node
				if in.B != 0 {
					v = asNodeValue(mc.vals[len(mc.vals)-1])
				}
				p.RunEnd(rep.run, true, v)
			}
			if in.B == 2 && p.Emit != nil && b.r != nil && p.StreamsIn(b.r.ID, b.r.Rec) {
				// Elements of the start rule's #stream are passed on without being retained, and committed.
				p.EmitStream(asNodeValue(mc.pop()), p.Pos)
				if p.Pos == e.save.pos {
					ip = int(e.ip)
					continue
				}
				ip = int(in.A)
				continue
			}
			if p.Pos == e.save.pos || rep.max >= 0 && rep.count >= rep.max {
				ip = int(e.ip)
			} else {
				ip = int(in.A)
			}
			continue
		case bytecode.OpEndRepeat:
			rep := mc.reps[len(mc.reps)-1]
			mc.reps = mc.reps[:len(mc.reps)-1]
			if rep.build {
				// The list of a run site that builds values, from the walk's gathered values.
				ok := rep.count >= rep.min
				start := *mc.vals[rep.base-1].(*int)
				list := p.SkipEndList(start, ok)
				if !ok {
					goto fail
				}
				mc.vals = mc.vals[:rep.base-1]
				mc.push(list)
				break
			}
			if rep.skip {
				p.SkipEnd()
			}
			if rep.scan && rep.count < rep.min {
				p.Hw = max(mc.scans[len(mc.scans)-1].hw0, p.Hw)
				mc.scans = mc.scans[:len(mc.scans)-1]
			}
			if rep.count < rep.min {
				goto fail
			}
			if rep.cold && p.Pos-rep.coldStart >= p.Tuning().RunHotSpan() {
				p.RunWalked(rep.site, rep.coldStart)
			}
			if rep.run != nil {
				p.RunFinish(rep.run)
				mc.freeRunState(rep.run)
			}
			if in.A == 1 {
				var kids []*value.Node
				if rep.scan && rep.hit {
					kids = mc.scans[len(mc.scans)-1].kids // (shared with the record; no element came after it)
				} else {
					// Taking the gathered elements off charges nothing, as Parser.Kids.
					n := len(mc.vals) - rep.base
					kids = p.Nodes(n)
					for k := range kids {
						kids[k] = asNodeValue(mc.vals[rep.base+k])
					}
				}
				if rep.scan {
					p.EndScanWalk(rep.site, rep.coldStart, rep.count, kids, rep.hot, rep.hit, mc.scans[len(mc.scans)-1].hw0)
					mc.scans = mc.scans[:len(mc.scans)-1]
				}
				start := *mc.vals[rep.base-1].(*int)
				mc.vals = mc.vals[:rep.base-1]
				mc.push(p.NewNode(nodes.Data{Kind: valueimpl.ListKind, Start: int32(start), End: int32(p.Pos), Children: kids, Fresh: true}))
			}
		case bytecode.OpLook:
			kind := uint8(eLook)
			if in.B == 1 {
				kind = eNotLook
			}
			mc.ents = append(mc.ents, vmEntry{kind: kind, ip: in.A, save: mc.save()})
			p.Silent++
			if in.C == 1 {
				// A lookahead that leaves captures or bindings is no observation.
				p.LookBegin(-1)
			} else {
				p.LookBegin(ip)
			}
		case bytecode.OpEndLook:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			p.LookEnd(true)
			p.Silent--
			if in.A == 1 {
				mc.restore(&e.save)
				goto fail
			}
			p.Pos = e.save.pos
			mc.vals = mc.vals[:int(e.save.vals)]
		case bytecode.OpCall:
			b.ip = ip
			return evCall, nil, false
		case bytecode.OpOperatorsRule:
			b.ip = ip
			return evOperatorsRule, nil, false
		case bytecode.OpPred:
			if !vm.predicate(p, int(in.A)) {
				goto fail
			}
		case bytecode.OpBind:
			if !vm.bind(p, int(in.A), int(in.B)) {
				goto fail
			}
		case bytecode.OpLabel:
			mk := p.Isolate(p.Pos)
			mc.pushLab(eLabel, 0, vmSave{}, labState{msg: in.A, mode: in.B, far: mk.Far, base: mk.Base, bits: mk.Bits})
		case bytecode.OpEndLabel:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			l := mc.popLab(&e)
			far, inner := p.Unisolate(state.ExpMark{Far: l.far, Base: l.base, Bits: l.bits})
			p.MergeExpected(far, inner)
		case bytecode.OpRecover:
			mk := p.Isolate(p.Pos)
			mc.pushLab(eRecover, in.A, mc.save(), labState{far: mk.Far, base: mk.Base, bits: mk.Bits})
		case bytecode.OpEndRecover:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			l := mc.popLab(&e)
			far, inner := p.Unisolate(state.ExpMark{Far: l.far, Base: l.base, Bits: l.bits})
			p.MergeExpected(far, inner)
			ip = int(in.A)
			continue
		case bytecode.OpEndSkip:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			l := mc.popLab(&e)
			if p.Pos == e.save.pos {
				mc.restore(&e.save)
				p.MergeExpected(l.far, l.exp)
				goto fail
			}
			se := p.MakeError(l.far, l.exp)
			p.PushRecovered(se)
			if in.A == 1 {
				mc.push(errorNode(p, e.save.pos, se))
			}
		case bytecode.OpReturn, bytecode.OpEnd:
			var v *value.Node
			if len(mc.vals) > b.vbase { // skip code builds no values
				v = asNodeValue(mc.vals[len(mc.vals)-1])
			}
			mc.vals = mc.vals[:b.vbase]
			mc.reps = mc.reps[:b.rbase]
			return evDone, v, true
		default:
			panic(fmt.Sprintf("vm: invalid instruction %v at %d", in.Op, ip))
		}
		ip++
		continue

	fail:
		if ip, ok = mc.unwind(m, ebase); !ok {
			mc.vals = mc.vals[:b.vbase]
			mc.reps = mc.reps[:b.rbase]
			return evDone, nil, false
		}
	}
}

// unwind unwinds the entry stack to ebase on failure and returns the index of the instruction to
// resume at. ok is false if there is no entry to resume from. Popping an entry charges no step, as
// flow.reset returns to a mark (spec/flow-ir/operations.md): the work an entry undoes was charged when
// it was done, and restoring the state, the capture writes it undoes included, charges nothing.
func (mc *Machine) unwind(m *bytecode.Module, ebase int) (ip int, ok bool) {
	p := mc.p
	for {
		if len(mc.ents) == ebase {
			return 0, false
		}
		e := mc.ents[len(mc.ents)-1]
		mc.ents = mc.ents[:len(mc.ents)-1]
		switch e.kind {
		case eRunIter:
			p.RunEnd(mc.reps[e.save.reps-1].run, false, nil)
			mc.restore(&e.save)
			if e.cut {
				continue
			}
			return int(e.ip), true
		case eChoice, eIter:
			mc.restore(&e.save)
			if e.cut {
				continue
			}
			return int(e.ip), true
		case eLook:
			p.LookEnd(false)
			p.Silent--
			mc.restore(&e.save)
			continue
		case eNotLook:
			p.LookEnd(false)
			p.Silent--
			mc.restore(&e.save)
			return int(e.ip), true
		case eLabel:
			l := mc.popLab(&e)
			far, inner := p.Unisolate(state.ExpMark{Far: l.far, Base: l.base, Bits: l.bits})
			if l.mode == 0 {
				p.Expect(far, state.MsgBit|(program.NumFixedDescs+state.ExpID(l.msg)))
			} else {
				p.MergeExpected(far, state.RenamedExpected(p, inner, program.NumFixedDescs+state.ExpID(l.msg)))
			}
			continue
		case eRecover:
			l := mc.popLab(&e)
			far, inner := p.Unisolate(state.ExpMark{Far: l.far, Base: l.base, Bits: l.bits})
			exp := p.Keep(inner) // keep them while skip runs
			mc.restore(&e.save)
			mc.pushLab(eSkip, 0, e.save, labState{far: far, exp: exp})
			return int(e.ip), true
		case eSkip:
			l := mc.popLab(&e)
			mc.restore(&e.save)
			p.MergeExpected(l.far, l.exp)
		}
	}
}

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
	cl := &vm.m.Classes[a]
	if !state.BudgetControlsEnabled || p.Control == nil || len(cl.Ranges) <= 4 {
		return cl.Has(ch)
	}
	for k := 0; k < len(cl.Ranges); k += 2 {
		p.Spend()
		if cl.Ranges[k] <= ch && ch <= cl.Ranges[k+1] {
			return !cl.Negated
		}
	}
	return cl.Negated
}

// single advances one character (size units) and pushes a string node if b is set.
func (mc *Machine) single(size int, b bool) {
	p := mc.p
	start := p.Pos
	p.Pos += size
	if b {
		mc.push(p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}))
	}
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

func (vm *Program) isStreamRepeat(ip int) bool {
	code := vm.m.Code
	if ip+1 >= len(code) || code[ip+1].Op != bytecode.OpIter {
		return false
	}
	end := int(code[ip+1].A)
	return end > ip+2 && end < len(code) && code[end].Op == bytecode.OpEndRepeat && code[end-1].StreamsValue()
}

// newPos allocates a copy of the position pos from a chunk and returns it. Start positions are
// pushed onto the VM value stack as *int: converting an int to an interface would allocate each
// time.
func (mc *Machine) newPos(pos int) *int {
	if len(mc.posSlab) == 0 {
		mc.posSlab = make([]int, state.PtrChunk)
		mc.p.MemUsed += state.PtrChunk * uint64(unsafe.Sizeof(0))
	}
	n := &mc.posSlab[0]
	mc.posSlab = mc.posSlab[1:]
	*n = pos
	return n
}

// newRunState returns a RunState for a VM repetition, reusing a freed one.
func (mc *Machine) newRunState() *state.RunState {
	if n := len(mc.freeRuns); n > 0 {
		r := mc.freeRuns[n-1]
		mc.freeRuns = mc.freeRuns[:n-1]
		return r
	}
	return &state.RunState{}
}

// freeRunState makes the RunState of a finished repetition available to later ones.
func (mc *Machine) freeRunState(r *state.RunState) {
	r.Old = nil
	mc.p.ReleaseRunLists(r)
	mc.freeRuns = append(mc.freeRuns, r)
}
