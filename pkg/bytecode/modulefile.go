package bytecode

import (
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"
)

// Reading and writing modules and validating loaded modules.

func (w *Encoder) ints(xs []int) {
	w.Uint(len(xs))
	for _, x := range xs {
		w.Int(x)
	}
}

func (w *Encoder) line(l *LineInfo) {
	w.ints(l.Scope)
	w.Int(l.Action)
	w.flow(l.Flow)
	w.flowImm(FlowImmShape, l.Shape)
}

// optFlow writes whether there is a Flow body f, and f if there is.
func (w *Encoder) optFlow(f *Flow) {
	w.Bool(f != nil)
	if f != nil {
		w.flow(f)
	}
}

// WriteModule writes m.
func (w *Encoder) WriteModule(m *Module) {
	w.Uint(len(m.Strings))
	w.Bool(m.SourceInfo)
	w.Bool(m.DefaultValue)
	w.Uint(len(m.Classes))
	for _, c := range m.Classes {
		w.Bool(c.Negated)
		w.Int(c.Expectation)
		w.Uint(len(c.Ranges) / 2)
		for _, r := range c.Ranges {
			w.Int(int(r))
		}
	}
	w.Uint(len(m.Types))
	for _, t := range m.Types {
		w.Int(t.Name)
		w.ints(t.Fields)
		w.ints(t.Zeros)
		w.Int(t.Node)
	}
	w.Uint(len(m.Scopes))
	for _, l := range m.Scopes {
		w.ints(l)
	}
	w.Uint(len(m.Vars))
	for _, v := range m.Vars {
		w.Int(v.Name)
		w.Int(v.Type)
		w.Int(v.Int)
		w.Int(v.Str)
	}
	w.Uint(len(m.Rules))
	for _, r := range m.Rules {
		w.Int(r.Name)
		w.ints(r.Scope)
		w.Int(r.Action)
		w.Int(r.NodeType)
		w.Bool(r.Memo)
		w.Bool(r.Leader)
		w.Bool(r.Member)
		w.Int(r.Cycle)
		w.Bool(r.PositionDependent)
		w.Int(r.OperatorsRule)
		w.Bool(r.ValueFreeTwin)
		w.Bool(r.Transient)
		w.Bool(r.Rec)
		w.Int(r.Where)
		w.Bool(r.Splice)
		w.Bool(r.Value != nil)
		if r.Value != nil {
			w.flowImm(FlowImmShape, *r.Value)
		}
		w.ints(r.Vars)
	}
	w.ints(m.Starts)
	w.Int(m.DefaultStart)
	w.Uint(len(m.OperatorsRules))
	for _, p := range m.OperatorsRules {
		w.optFlow(p.SkipFlow)
		w.Uint(len(p.Operands))
		for i := range p.Operands {
			w.line(&p.Operands[i])
		}
		for _, ops := range [][]OperatorInfo{p.Prefix, p.InfixPostfix} {
			w.Uint(len(ops))
			for i := range ops {
				o := &ops[i]
				w.Int(o.ID)
				w.Int(o.Kind)
				w.Int(o.Assoc)
				w.Int(o.Level)
				w.line(&o.Line)
			}
		}
	}
	w.Uint(len(m.Actions))
	for i := range m.Actions {
		w.actionExpr(&m.Actions[i])
	}
	w.Uint(len(m.RunKeys))
	for _, k := range m.RunKeys {
		w.Int(k.Site)
		w.ints(k.Vars)
		w.ints(k.Bind)
	}
	w.flows(m.Flows)
	w.flows(m.Streams)
}

// flows writes the Flow bodies fs by increasing rule index: their number, then each rule index and
// its body.
func (w *Encoder) flows(fs map[int]*Flow) {
	rules := slices.Sorted(maps.Keys(fs))
	w.Uint(len(rules))
	for _, rule := range rules {
		w.Int(rule)
		w.flow(fs[rule])
	}
}

// flow writes the Flow body f: its number of registers and its instructions. An instruction is its
// operation (1 byte), then its destination and argument registers, its target and its immediate as
// far as the operation's shape (FlowShapes) has them.
func (w *Encoder) flow(f *Flow) {
	w.Uint(f.Regs)
	w.Uint(len(f.Code))
	for i := range f.Code {
		in := &f.Code[i]
		s, ok := in.Op.Shape()
		if !ok || len(in.Dst) != s.Dst || len(in.Args) != s.Args {
			panic(fmt.Sprintf("internal error: bytecode: flow instruction %d: %v with %d destinations and %d arguments", i, in.Op, len(in.Dst), len(in.Args)))
		}
		w.Byte(byte(in.Op))
		for _, d := range in.Dst {
			w.Int(d)
		}
		for _, a := range in.Args {
			w.Int(a)
		}
		if s.Target {
			w.Int(in.Target)
		}
		w.flowImm(s.Imm, in.Imm)
	}
}

// flowImm writes the immediate v of kind k. A constant is a kind (1 byte: ConstNil, ConstInt or
// ConstBool) and a signed integer (0 for nil, 0 or 1 for a bool); a guard is (literal, rune,
// case-folded, class, depth); an expectation (ID, message); a call (rule, level).
func (w *Encoder) flowImm(k FlowImm, v any) {
	bad := func() { panic(fmt.Sprintf("internal error: bytecode: flow immediate %#v of kind %d", v, k)) }
	switch k {
	case FlowImmNone:
		if v != nil {
			bad()
		}
	case FlowImmConst:
		switch x := v.(type) {
		case nil:
			w.Byte(ConstNil)
			w.Int(0)
		case int:
			w.Byte(ConstInt)
			w.Int(x)
		case bool:
			w.Byte(ConstBool)
			if x {
				w.Int(1)
			} else {
				w.Int(0)
			}
		default:
			bad()
		}
	case FlowImmGuard:
		x, ok := v.(FlowGuardOf)
		if !ok {
			bad()
		}
		w.Int(x.Class)
		w.Int(x.Depth)
	case FlowImmExpect:
		x, ok := v.(FlowExpectOf)
		if !ok {
			bad()
		}
		w.Int(x.Exp)
		w.Bool(x.Message)
	case FlowImmCall:
		x, ok := v.(FlowCallOf)
		if !ok {
			bad()
		}
		w.Int(x.Rule)
		w.Int(x.Level)
	case FlowImmClass, FlowImmAnchor:
		x, ok := v.(int)
		if !ok {
			bad()
		}
		w.Int(x)
	case FlowImmLit:
		x, ok := v.(FlowLitOf)
		if !ok {
			bad()
		}
		w.Int(x.Str)
		w.Int(x.Exp)
		w.Bool(x.Fold)
	case FlowImmSilence:
		x, ok := v.(FlowSilenceOf)
		if !ok {
			bad()
		}
		w.Int(x.Delta)
		w.Int(x.Site)
		w.Bool(x.Ok)
	case FlowImmClassTest:
		x, ok := v.(FlowClassTestOf)
		if !ok {
			bad()
		}
		w.flowImm(FlowImmClass, x.Class)
		w.flowImm(FlowImmExpect, x.Expect)
	case FlowImmGuardTest:
		x, ok := v.(FlowGuardTestOf)
		if !ok {
			bad()
		}
		w.flowImm(FlowImmGuard, x.Guard)
		w.flowImm(FlowImmExpect, x.Expect)
	case FlowImmLitGuardTest:
		x, ok := v.(FlowLitGuardTestOf)
		if !ok {
			bad()
		}
		w.flowImm(FlowImmLitGuard, x.Guard)
		w.flowImm(FlowImmExpect, x.Expect)
	case FlowImmAnchorTest:
		x, ok := v.(FlowAnchorTestOf)
		if !ok {
			bad()
		}
		w.flowImm(FlowImmAnchor, x.Anchor)
		w.flowImm(FlowImmExpect, x.Expect)
	case FlowImmRunWalk:
		x, ok := v.(FlowRunWalkOf)
		if !ok {
			bad()
		}
		w.Int(x.Site)
		w.Bool(x.Build)
		w.Bool(x.KeyEnv)
	case FlowImmResume:
		x, ok := v.(FlowResumeOf)
		if !ok {
			bad()
		}
		w.Int(x.Site)
		w.Int(x.Min)
		w.Int(x.Max)
		w.Bool(x.Shiftable)
	case FlowImmBind:
		x, ok := v.(FlowBindOf)
		if !ok {
			bad()
		}
		w.Int(x.Var)
		w.Int(x.Action)
	case FlowImmLitGuard:
		x, ok := v.(FlowLitGuardOf)
		if !ok {
			bad()
		}
		w.Int(int(x.Rune))
		w.Bool(x.Fold)
		w.Int(x.Depth)
	case FlowImmCount, FlowImmStr, FlowImmSlot, FlowImmScope, FlowImmRunSite, FlowImmScanSite, FlowImmAction, FlowImmRule:
		x, ok := v.(int)
		if !ok {
			bad()
		}
		w.Int(x)
	case FlowImmShape:
		x, ok := v.(FlowShapeOf)
		if !ok {
			bad()
		}
		w.Bool(x.Elems)
		w.Bool(x.Shared)
		w.Bool(x.Tuple)
		w.Int(x.K)
		w.Int(x.Mode)
	default:
		bad()
	}
}

func (w *Encoder) actionExpr(t *Action) {
	for _, n := range t.Regs {
		w.Uint(n)
	}
	// Most action expressions use few of their pools: a byte whose bits 0 to 5 mark the pools that are not empty
	// (constants, names, member reads, node types, casts and struct literals), then those pools.
	lens := t.poolLens()
	var mask byte
	for i, n := range lens {
		if n != 0 {
			mask |= 1 << i
		}
	}
	w.Byte(mask)
	if lens[0] != 0 {
		w.Uint(len(t.Consts))
		for _, c := range t.Consts {
			w.Byte(byte(c.Kind))
			w.Body.Write(binary.AppendVarint(nil, c.Int))
		}
	}
	if lens[1] != 0 {
		w.ints(t.Names)
	}
	if lens[2] != 0 {
		w.Uint(len(t.Members))
		for _, x := range t.Members {
			w.Int(x.Kind)
			w.Int(x.Name)
			w.Int(x.Field)
		}
	}
	if lens[3] != 0 {
		w.ints(t.NodeTypes)
	}
	if lens[4] != 0 {
		w.Uint(len(t.Casts))
		for _, x := range t.Casts {
			w.Int(x.Type)
			w.Int(x.Line)
			w.Int(x.Col)
		}
	}
	if lens[5] != 0 {
		w.Uint(len(t.Structs))
		for _, x := range t.Structs {
			w.Int(x.Type)
			w.Int(x.Fields)
		}
	}
	w.Uint(len(t.Code))
	for _, in := range t.Code {
		// Most operands of action code are zero or unused: an instruction writes its opcode, a byte
		// whose bits 0 to 3 mark the operands D, A, B and C that are not zero, and those operands.
		ops := [4]int32{in.Dst, in.A, in.B, in.C}
		var mask byte
		for i, x := range ops {
			if x != 0 {
				mask |= 1 << i
			}
		}
		w.Byte(in.Op)
		w.Byte(mask)
		for _, x := range ops {
			if x != 0 {
				w.Int(int(x))
			}
		}
	}
}

// int32 reads an instruction operand.
func (r *Decoder) int32() int32 {
	if r.Err != nil {
		return 0
	}
	n, k := binary.Varint(r.Data[r.Pos:])
	if k <= 0 {
		r.Fail("invalid integer")
		return 0
	}
	r.Pos += k
	if n < -1<<31 || n >= 1<<31 {
		r.Fail("integer %d out of range", n)
		return 0
	}
	return int32(n)
}

func (r *Decoder) ints() []int {
	n := r.Count()
	xs := make([]int, 0, n)
	for i := 0; i < n && r.Err == nil; i++ {
		xs = append(xs, int(r.int32()))
	}
	return xs
}

func (r *Decoder) line() LineInfo {
	l := LineInfo{Scope: r.ints(), Action: int(r.int32())}
	if l.Flow = r.flow(); r.Err == nil {
		l.Shape, _ = r.flowImm(FlowImmShape).(FlowShapeOf)
	}
	return l
}

// optFlow reads whether there is a Flow body, and the body if there is.
func (r *Decoder) optFlow() *Flow {
	if !r.Bool() || r.Err != nil {
		return nil
	}
	return r.flow()
}

// ReadModule reads a module.
func (r *Decoder) ReadModule() *Module {
	m := &Module{}
	n := r.Uint()
	if r.Err == nil && n > len(r.Strs) {
		r.Fail("module has %d strings, file has %d", n, len(r.Strs))
	}
	if r.Err != nil {
		return m
	}
	m.Strings = r.Strs[:n]
	m.SourceInfo = r.Bool()
	m.DefaultValue = r.Bool()
	n = r.Count()
	m.Classes = sized[Class](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		c := Class{Negated: r.Bool(), Expectation: int(r.int32())}
		k := r.Count()
		for j := 0; j < 2*k && r.Err == nil; j++ {
			c.Ranges = append(c.Ranges, rune(r.int32()))
		}
		m.Classes = append(m.Classes, c)
	}
	n = r.Count()
	m.Types = sized[TypeInfo](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		m.Types = append(m.Types, TypeInfo{Name: int(r.int32()), Fields: r.ints(), Zeros: r.ints(), Node: int(r.int32())})
	}
	n = r.Count()
	m.Scopes = sized[[]int](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		m.Scopes = append(m.Scopes, r.ints())
	}
	n = r.Count()
	m.Vars = sized[VarInfo](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		m.Vars = append(m.Vars, VarInfo{Name: int(r.int32()), Type: int(r.int32()), Int: r.Int(), Str: int(r.int32())})
	}
	n = r.Count()
	m.Rules = sized[RuleInfo](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		ri := RuleInfo{
			Name: int(r.int32()), Scope: r.ints(), Action: int(r.int32()),
			NodeType: int(r.int32()),
			Memo:     r.Bool(), Leader: r.Bool(), Member: r.Bool(), Cycle: int(r.int32()), PositionDependent: r.Bool(), OperatorsRule: int(r.int32()), ValueFreeTwin: r.Bool(), Transient: r.Bool(), Rec: r.Bool(), Where: int(r.int32()), Splice: r.Bool(),
		}
		if r.Bool() && r.Err == nil {
			if x, ok := r.flowImm(FlowImmShape).(FlowShapeOf); ok {
				ri.Value = &x
			}
		}
		ri.Vars = r.ints()
		m.Rules = append(m.Rules, ri)
	}
	m.Starts = r.ints()
	m.DefaultStart = int(r.int32())
	n = r.Count()
	m.OperatorsRules = sized[OperatorsRuleInfo](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		p := OperatorsRuleInfo{SkipFlow: r.optFlow()}
		k := r.Count()
		for j := 0; j < k && r.Err == nil; j++ {
			p.Operands = append(p.Operands, r.line())
		}
		for _, ops := range []*[]OperatorInfo{&p.Prefix, &p.InfixPostfix} {
			k = r.Count()
			for j := 0; j < k && r.Err == nil; j++ {
				o := OperatorInfo{ID: int(r.int32()), Kind: int(r.int32()), Assoc: int(r.int32()), Level: int(r.int32())}
				o.Line = r.line()
				*ops = append(*ops, o)
			}
		}
		m.OperatorsRules = append(m.OperatorsRules, p)
	}
	n = r.Count()
	m.Actions = sized[Action](r, n, 6)
	for i := 0; i < n && r.Err == nil; i++ {
		m.Actions = append(m.Actions, r.actionExpr())
	}
	n = r.Count()
	m.RunKeys = sized[RunKeyInfo](r, n, 3)
	for i := 0; i < n && r.Err == nil; i++ {
		m.RunKeys = append(m.RunKeys, RunKeyInfo{Site: int(r.int32()), Vars: r.ints(), Bind: r.ints()})
	}
	m.Flows = r.flows("Flow bodies")
	m.Streams = r.flows("stream bodies")
	return m
}

// flows reads the Flow bodies that Encoder.flows wrote, nil if there are none; what names them in an
// error.
func (r *Decoder) flows(what string) map[int]*Flow {
	var fs map[int]*Flow
	n := r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		rule := int(r.int32())
		f := r.flow()
		if r.Err != nil {
			break
		}
		if _, dup := fs[rule]; dup {
			r.Fail("rule %d has two %s", rule, what)
			break
		}
		if fs == nil {
			fs = map[int]*Flow{}
		}
		fs[rule] = f
	}
	return fs
}

// flow reads a Flow body. The operation of each instruction says how many registers it names and
// whether a target and an immediate follow; an unknown operation is invalid. ValidateModule checks
// the registers, targets and immediates.
func (r *Decoder) flow() *Flow {
	f := &Flow{Regs: r.Uint()}
	n := r.Count()
	f.Code = sized[FlowInstr](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		in := FlowInstr{Op: FlowOp(r.Byte())}
		s, ok := in.Op.Shape()
		if r.Err == nil && !ok {
			r.Fail("invalid flow operation %d", in.Op)
			break
		}
		regs := func(k int) []int {
			if k == 0 {
				return nil
			}
			xs := make([]int, k)
			for j := range xs {
				xs[j] = int(r.int32())
			}
			return xs
		}
		in.Dst, in.Args = regs(s.Dst), regs(s.Args)
		if s.Target {
			in.Target = int(r.int32())
		}
		in.Imm = r.flowImm(s.Imm)
		f.Code = append(f.Code, in)
	}
	return f
}

// flowImm reads an immediate of kind k.
func (r *Decoder) flowImm(k FlowImm) any {
	switch k {
	case FlowImmConst:
		kind, x := r.Byte(), r.int64()
		if r.Err != nil {
			return nil
		}
		switch {
		case kind == ConstNil && x == 0:
			return nil
		case kind == ConstInt && int64(int(x)) == x:
			return int(x)
		case kind == ConstBool && (x == 0 || x == 1):
			return x == 1
		}
		r.Fail("invalid flow constant %d of kind %d", x, kind)
	case FlowImmGuard:
		return FlowGuardOf{Class: int(r.int32()), Depth: int(r.int32())}
	case FlowImmLitGuard:
		return FlowLitGuardOf{Rune: r.int32(), Fold: r.Bool(), Depth: int(r.int32())}
	case FlowImmExpect:
		return FlowExpectOf{Exp: int(r.int32()), Message: r.Bool()}
	case FlowImmCall:
		return FlowCallOf{Rule: int(r.int32()), Level: int(r.int32())}
	case FlowImmClass, FlowImmAnchor:
		return int(r.int32())
	case FlowImmLit:
		return FlowLitOf{Str: int(r.int32()), Exp: int(r.int32()), Fold: r.Bool()}
	case FlowImmSilence:
		return FlowSilenceOf{Delta: int(r.int32()), Site: int(r.int32()), Ok: r.Bool()}
	case FlowImmClassTest:
		x := FlowClassTestOf{Class: r.flowImm(FlowImmClass).(int)}
		x.Expect = r.flowImm(FlowImmExpect).(FlowExpectOf)
		return x
	case FlowImmGuardTest:
		x := FlowGuardTestOf{Guard: r.flowImm(FlowImmGuard).(FlowGuardOf)}
		x.Expect = r.flowImm(FlowImmExpect).(FlowExpectOf)
		return x
	case FlowImmLitGuardTest:
		x := FlowLitGuardTestOf{Guard: r.flowImm(FlowImmLitGuard).(FlowLitGuardOf)}
		x.Expect = r.flowImm(FlowImmExpect).(FlowExpectOf)
		return x
	case FlowImmAnchorTest:
		x := FlowAnchorTestOf{Anchor: r.flowImm(FlowImmAnchor).(int)}
		x.Expect = r.flowImm(FlowImmExpect).(FlowExpectOf)
		return x
	case FlowImmRunWalk:
		return FlowRunWalkOf{Site: int(r.int32()), Build: r.Bool(), KeyEnv: r.Bool()}
	case FlowImmResume:
		return FlowResumeOf{Site: int(r.int32()), Min: r.Int(), Max: r.Int(), Shiftable: r.Bool()}
	case FlowImmBind:
		return FlowBindOf{Var: int(r.int32()), Action: int(r.int32())}
	case FlowImmCount, FlowImmStr, FlowImmSlot, FlowImmScope, FlowImmRunSite, FlowImmScanSite, FlowImmAction, FlowImmRule:
		return int(r.int32())
	case FlowImmShape:
		x := FlowShapeOf{Elems: r.Bool(), Shared: r.Bool(), Tuple: r.Bool()}
		x.K = int(r.int32())
		x.Mode = int(r.int32())
		return x
	}
	return nil
}

// int64 reads a signed integer of 64 bits.
func (r *Decoder) int64() int64 {
	if r.Err != nil {
		return 0
	}
	n, k := binary.Varint(r.Data[r.Pos:])
	if k <= 0 {
		r.Fail("invalid integer")
		return 0
	}
	r.Pos += k
	return n
}

// sized returns an empty slice with room for the n elements that follow, or nil for none. Each
// element takes at least least bytes of the data, so a valid count fits the bytes left; the room is
// bounded by them, so that a forged count reserves no more elements than the rest of the data could
// hold.
func sized[T any](r *Decoder, n, least int) []T {
	if left := (len(r.Data) - r.Pos) / least; n > left {
		n = left
	}
	if n <= 0 {
		return nil
	}
	return make([]T, 0, n)
}

// poolLens returns the lengths of the pools of t, in the order a module writes them.
func (t *Action) poolLens() [6]int {
	return [6]int{len(t.Consts), len(t.Names), len(t.Members), len(t.NodeTypes), len(t.Casts), len(t.Structs)}
}

func (r *Decoder) actionExpr() Action {
	var t Action
	for i := range t.Regs {
		t.Regs[i] = r.Uint()
	}
	mask := r.Byte()
	if r.Err == nil && mask > 0x3f {
		r.Fail("invalid action pool mask %#x", mask)
	}
	// count reads the length of pool i: zero if the mask leaves it out, else not zero, so that each
	// action expression has one encoding.
	count := func(i int) int {
		if mask&(1<<i) == 0 || r.Err != nil {
			return 0
		}
		n := r.Count()
		if r.Err == nil && n == 0 {
			r.Fail("action pool marked but empty")
		}
		return n
	}
	n := count(0)
	t.Consts = sized[ActionConst](r, n, 2)
	for i := 0; i < n && r.Err == nil; i++ {
		t.Consts = append(t.Consts, ActionConst{Kind: int(r.Byte()), Int: r.int64()})
	}
	n = count(1)
	t.Names = sized[int](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		t.Names = append(t.Names, int(r.int32()))
	}
	n = count(2)
	t.Members = sized[ActionMember](r, n, 3)
	for i := 0; i < n && r.Err == nil; i++ {
		t.Members = append(t.Members, ActionMember{Kind: int(r.int32()), Name: int(r.int32()), Field: int(r.int32())})
	}
	n = count(3)
	t.NodeTypes = sized[int](r, n, 1)
	for i := 0; i < n && r.Err == nil; i++ {
		t.NodeTypes = append(t.NodeTypes, int(r.int32()))
	}
	n = count(4)
	t.Casts = sized[ActionCast](r, n, 3)
	for i := 0; i < n && r.Err == nil; i++ {
		t.Casts = append(t.Casts, ActionCast{Type: int(r.int32()), Line: int(r.int32()), Col: int(r.int32())})
	}
	n = count(5)
	t.Structs = sized[ActionStruct](r, n, 2)
	for i := 0; i < n && r.Err == nil; i++ {
		t.Structs = append(t.Structs, ActionStruct{Type: int(r.int32()), Fields: int(r.int32())})
	}
	n = r.Count()
	t.Code = sized[ActionInstr](r, n, 2)
	for i := 0; i < n && r.Err == nil; i++ {
		t.Code = append(t.Code, r.actionInstr())
	}
	return t
}

// actionInstr reads an instruction of action code: its opcode, the mask of its operands that are not
// zero, and those operands. A mask with other bits, or a marked operand that is zero, is invalid,
// so that each instruction has one encoding.
func (r *Decoder) actionInstr() ActionInstr {
	op, mask := r.Byte(), r.Byte()
	if r.Err == nil && mask > 0xf {
		r.Fail("invalid action expression operand mask %#x", mask)
	}
	var ops [4]int32
	for i := range ops {
		if mask&(1<<i) == 0 || r.Err != nil {
			continue
		}
		if ops[i] = r.int32(); r.Err == nil && ops[i] == 0 {
			r.Fail("action expression operand marked but zero")
		}
	}
	return ActionInstr{Op: op, Dst: ops[0], A: ops[1], B: ops[2], C: ops[3]}
}

// ValidateModule checks that every table reference of the module and every reference of its Flow
// bodies is in range, that every rule and Operators Rule line has a body, that the Flow bodies are
// well formed (validateFlows) and that every rule on a cycle of calls is marked Rec. fieldType
// checks the text of a field type, which the type system above the module defines.
func ValidateModule(m *Module, fieldType func(text string) error) error {
	var err error
	fail := func(format string, args ...any) {
		if err == nil {
			err = fmt.Errorf(format, args...)
		}
	}
	in := func(what string, i, n int) {
		if i < 0 || i >= n {
			fail("%s %d out of range", what, i)
		}
	}
	opt := func(what string, i, n int) {
		if i != -1 {
			in(what, i, n)
		}
	}
	str := func(i int) { in("string", i, len(m.Strings)) }
	strs := func(xs []int) {
		for _, x := range xs {
			str(x)
		}
	}
	for _, c := range m.Classes {
		if len(c.Ranges)%2 != 0 {
			fail("invalid class")
		}
		str(c.Expectation)
	}
	for _, t := range m.Types {
		str(t.Name)
		strs(t.Fields)
		strs(t.Zeros)
		if err == nil && len(t.Zeros) != len(t.Fields) {
			fail("type %q has %d zero descriptors for %d fields", m.Strings[t.Name], len(t.Zeros), len(t.Fields))
		}
		opt("string", t.Node, len(m.Strings))
		if err == nil && t.Node >= 0 && len(t.Fields) != 0 {
			fail("node type %q has fields", m.Strings[t.Name])
		}
	}
	if err == nil {
		err = validateZeros(m)
	}
	for _, l := range m.Scopes {
		strs(l)
	}
	// typeErr parses the field type at string index i once no error is recorded. Callers format
	// the location only on failure, so a valid module allocates no messages.
	typeErr := func(i int) error {
		if err != nil {
			return nil
		}
		return fieldType(m.Strings[i])
	}
	for _, t := range m.Types {
		if t.Node >= 0 && err == nil {
			if perr := typeErr(t.Node); perr != nil {
				fail("%v in node type %s", perr, m.Strings[t.Name])
			}
			if err == nil && !strings.HasPrefix(m.Strings[t.Node], "node[") {
				fail("node type %q is not over a node type", m.Strings[t.Name])
			}
		}
	}
	// nodeType reports whether name is a declared named node type.
	nodeType := func(name string) bool {
		for _, t := range m.Types {
			if m.Strings[t.Name] == name {
				return t.Node >= 0
			}
		}
		return false
	}
	// structType reports whether name is a declared struct type.
	structType := func(name string) bool {
		for _, t := range m.Types {
			if m.Strings[t.Name] == name {
				return t.Node < 0
			}
		}
		return false
	}
	// Every line has a Flow body (validateFlows checks it).
	line := func(l *LineInfo) {
		if err == nil && l.Flow == nil {
			fail("Operators Rule line without a Flow body")
		}
		strs(l.Scope)
		opt("action", l.Action, len(m.Actions))
	}
	seenVars := map[string]bool{}
	for _, v := range m.Vars {
		str(v.Name)
		in("variable type", v.Type, 3)
		opt("string", v.Str, len(m.Strings))
		if err == nil {
			name := m.Strings[v.Name]
			switch {
			case seenVars[name]:
				fail("variable %q is defined twice", name)
			case (v.Type == VarString) != (v.Str >= 0):
				fail("invalid initial value of variable %q", name)
			case v.Type == VarBool && v.Int != 0 && v.Int != 1:
				fail("invalid initial value of variable %q", name)
			case v.Type == VarString && v.Int != 0:
				fail("invalid initial value of variable %q", name)
			}
			seenVars[name] = true
		}
	}
	// declared checks that a string index names a declared variable.
	declared := func(i int) {
		str(i)
		if err == nil && !seenVars[m.Strings[i]] {
			fail("variable %q is not declared", m.Strings[i])
		}
	}
	for i, r := range m.Rules {
		str(r.Name)
		// A rule runs its Flow body, or else the expression of its Operators Rule.
		if err == nil && m.Flows[i] == nil && r.OperatorsRule < 0 {
			fail("rule %s has no body", m.Strings[r.Name])
		}
		strs(r.Scope)
		opt("action", r.Action, len(m.Actions))
		opt("string", r.NodeType, len(m.Strings))
		if err == nil && r.NodeType >= 0 && !nodeType(m.Strings[r.NodeType]) {
			fail("type %q is not a named node type", m.Strings[r.NodeType])
		}
		opt("string", r.Where, len(m.Strings))
		opt("operatorsRule", r.OperatorsRule, len(m.OperatorsRules))
		if err == nil && r.Value != nil {
			if r.Action >= 0 || r.Splice || r.OperatorsRule >= 0 || r.ValueFreeTwin {
				fail("rule %s shapes the value of its body, which is its value", m.Strings[r.Name])
			} else if e := validateFlowImm(m, 0, FlowImmShape, *r.Value); e != nil {
				fail("rule %s: %v", m.Strings[r.Name], e)
			}
		}
		for _, v := range r.Vars {
			declared(v)
		}
	}
	for i, k := range m.RunKeys {
		if err == nil && (k.Site <= 0 || i > 0 && k.Site <= m.RunKeys[i-1].Site) {
			fail("invalid run key site %d", k.Site)
		}
		for _, v := range k.Vars {
			declared(v)
		}
		for _, v := range k.Bind {
			declared(v)
			if err == nil && !slices.Contains(k.Vars, v) {
				fail("run key of site %d binds %q, which it does not hold", k.Site, m.Strings[v])
			}
		}
	}
	if len(m.Starts) == 0 {
		fail("no start rule")
	}
	startNames := map[int]bool{}
	for i, x := range m.Starts {
		in("start rule", x, len(m.Rules))
		if err == nil && (i > 0 && x <= m.Starts[i-1] || startNames[m.Rules[x].Name]) {
			fail("invalid start rule %d", x)
		}
		if err == nil {
			startNames[m.Rules[x].Name] = true
		}
	}
	if err == nil && !slices.Contains(m.Starts, m.DefaultStart) {
		fail("default start rule %d is not a start rule", m.DefaultStart)
	}
	for _, p := range m.OperatorsRules {
		for i := range p.Operands {
			line(&p.Operands[i])
		}
		for _, ops := range [][]OperatorInfo{p.Prefix, p.InfixPostfix} {
			for i := range ops {
				o := &ops[i]
				in("operator kind", o.Kind, 3)
				in("associativity", o.Assoc, 3)
				line(&o.Line)
			}
		}
	}
	for i := range m.Actions {
		t := &m.Actions[i]
		for _, c := range t.Consts {
			switch c.Kind {
			case ConstNil, ConstInt:
			case ConstString:
				str(int(c.Int))
			case ConstBool:
				if c.Int != 0 && c.Int != 1 {
					fail("invalid boolean constant %d in action %d", c.Int, i)
				}
			default:
				fail("invalid constant kind %d in action %d", c.Kind, i)
			}
		}
		strs(t.Names)
		for _, x := range t.Members {
			str(x.Name)
			str(x.Field)
		}
		for _, x := range t.NodeTypes {
			str(x)
			if err == nil && !nodeType(m.Strings[x]) {
				fail("type %q of a conversion is not a named node type in action %d", m.Strings[x], i)
			}
		}
		for _, x := range t.Casts {
			opt("string", x.Type, len(m.Strings))
			if x.Type >= 0 {
				if perr := typeErr(x.Type); perr != nil {
					fail("%v in action %d", perr, i)
				}
			}
			if x.Line < 0 || x.Col < 0 {
				fail("invalid cast position %d:%d in action %d", x.Line, x.Col, i)
			}
		}
		for _, x := range t.Structs {
			str(x.Type)
			if err == nil && !structType(m.Strings[x.Type]) {
				fail("type %q of a struct literal is not a struct type in action %d", m.Strings[x.Type], i)
			}
			if x.Fields < 0 {
				fail("invalid field count %d in action %d", x.Fields, i)
			}
		}
	}
	if err != nil {
		return err
	}
	if err := validateFlows(m); err != nil {
		return err
	}
	return validateRec(m)
}

// validateFlows checks the Flow bodies of m, its stream bodies among them: each belongs to a rule of
// the rule table; each
// instruction has a known operation with the registers, target and immediate of its shape; every
// register is within the frame (a destination may be -1, which discards); every target is an
// instruction of the body; every immediate refers to a class, rule, expectation or scope in range,
// and every capture slot to a slot of the largest frame the body may run with (its rule's scope or a
// scope of its NEWFRAME); the body ends with JUMP or RETURN, so that running it cannot pass its
// end; and every cycle of the body is bounded (Flow.UnboundedCycle), so that a run of it ends under a
// finite compute limit. A runtime does not check that registers hold what an instruction reads;
// it checks that a slot is in the frame it indexes.
func validateFlows(m *Module) error {
	for _, fs := range []struct {
		what string
		m    map[int]*Flow
	}{{"flow body", m.Flows}, {"stream body", m.Streams}} {
		for _, rule := range slices.Sorted(maps.Keys(fs.m)) {
			if rule < 0 || rule >= len(m.Rules) {
				return fmt.Errorf("%s of rule %d out of range", fs.what, rule)
			}
			if err := validateFlow(m, len(m.Rules[rule].Scope), fs.m[rule]); err != nil {
				return fmt.Errorf("%s of rule %s: %w", fs.what, m.Strings[m.Rules[rule].Name], err)
			}
		}
	}
	// A line runs in the frame of its scope, and a skip in a frame without slots.
	for i, p := range m.OperatorsRules {
		if p.SkipFlow != nil {
			if err := validateFlow(m, 0, p.SkipFlow); err != nil {
				return fmt.Errorf("flow body of the skip of Operators Rule %d: %w", i, err)
			}
		}
		lines := slices.Clone(p.Operands)
		for _, o := range slices.Concat(p.Prefix, p.InfixPostfix) {
			lines = append(lines, o.Line)
		}
		for j, l := range lines {
			if l.Flow == nil {
				continue
			}
			if err := validateFlow(m, len(l.Scope), l.Flow); err != nil {
				return fmt.Errorf("flow body of line %d of Operators Rule %d: %w", j, i, err)
			}
			if err := validateFlowImm(m, 0, FlowImmShape, l.Shape); err != nil {
				return fmt.Errorf("line %d of Operators Rule %d: %w", j, i, err)
			}
		}
	}
	return nil
}

// validateFlow checks the Flow body f, which runs in a frame of the number of slots slots.
func validateFlow(m *Module, slots int, f *Flow) error {
	if f == nil || f.Regs < 0 {
		return fmt.Errorf("invalid frame")
	}
	if n := len(f.Code); n == 0 || !f.Code[n-1].Op.Ends() {
		return fmt.Errorf("the body does not end with JUMP or a return")
	}
	// slots becomes the number of slots of the largest frame the body may run with.
	for _, in := range f.Code {
		if sc, ok := in.Imm.(int); ok && in.Op == FlowNewFrame && sc >= 0 && sc < len(m.Scopes) {
			slots = max(slots, len(m.Scopes[sc]))
		}
	}
	for pc := range f.Code {
		in := &f.Code[pc]
		s, ok := in.Op.Shape()
		if !ok {
			return fmt.Errorf("invalid flow operation %d at %d", in.Op, pc)
		}
		if len(in.Dst) != s.Dst || len(in.Args) != s.Args {
			return fmt.Errorf("%v at %d with %d destinations and %d arguments", in.Op, pc, len(in.Dst), len(in.Args))
		}
		for _, d := range in.Dst {
			if d < -1 || d >= f.Regs {
				return fmt.Errorf("register %d out of range at %d", d, pc)
			}
		}
		for _, a := range in.Args {
			if a < 0 || a >= f.Regs {
				return fmt.Errorf("register %d out of range at %d", a, pc)
			}
		}
		if s.Target && (in.Target < 0 || in.Target >= len(f.Code)) || !s.Target && in.Target != 0 {
			return fmt.Errorf("invalid jump from %d to %d", pc, in.Target)
		}
		if err := validateFlowImm(m, slots, s.Imm, in.Imm); err != nil {
			return fmt.Errorf("%v at %d: %w", in.Op, pc, err)
		}
	}
	if regs := f.Unassigned(); len(regs) > 0 {
		return fmt.Errorf("register %d may be read before it is written", regs[0])
	}
	if pc, ok := f.RestoreAcrossCommit(); ok {
		return fmt.Errorf("%v at %d may restore a state saved before an EMITSTREAM", f.Code[pc].Op, pc)
	}
	if pc, ok := f.UnboundedCycle(); ok {
		return fmt.Errorf("%v at %d is on a cycle that no step bounds", f.Code[pc].Op, pc)
	}
	return nil
}

// validateFlowImm checks the immediate v of kind k of an instruction of a body whose largest frame
// has the number of slots slots.
func validateFlowImm(m *Module, slots int, k FlowImm, v any) error {
	ok := false
	switch k {
	case FlowImmNone:
		ok = v == nil
	case FlowImmConst:
		switch v.(type) {
		case nil, bool, int:
			ok = true
		}
	case FlowImmGuard:
		var x FlowGuardOf
		if x, ok = v.(FlowGuardOf); !ok {
			break
		}
		switch {
		case x.Class < 0 || x.Class >= len(m.Classes):
			return fmt.Errorf("invalid guard class %d", x.Class)
		case x.Depth < 0:
			return fmt.Errorf("invalid guard depth %d", x.Depth)
		}
	case FlowImmLitGuard:
		var x FlowLitGuardOf
		if x, ok = v.(FlowLitGuardOf); !ok {
			break
		}
		switch {
		case !utf8.ValidRune(x.Rune) || x.Fold && x.Rune >= 'A' && x.Rune <= 'Z':
			return fmt.Errorf("invalid guard character %U", x.Rune)
		case x.Depth < 0:
			return fmt.Errorf("invalid guard depth %d", x.Depth)
		}
	case FlowImmExpect:
		var x FlowExpectOf
		if x, ok = v.(FlowExpectOf); ok && (x.Exp < 0 || x.Exp >= FixedExps+len(m.Strings)) {
			return fmt.Errorf("expectation %d out of range", x.Exp)
		}
	case FlowImmCall:
		var x FlowCallOf
		if x, ok = v.(FlowCallOf); !ok {
			break
		}
		if x.Rule < 0 || x.Rule >= len(m.Rules) {
			return fmt.Errorf("rule %d out of range", x.Rule)
		}
		if x.Level < 0 {
			return fmt.Errorf("invalid binding level %d", x.Level)
		}
	case FlowImmClass:
		var x int
		if x, ok = v.(int); ok && (x < 0 || x >= len(m.Classes)) {
			return fmt.Errorf("class %d out of range", x)
		}
	case FlowImmAnchor:
		var x int
		if x, ok = v.(int); ok && (x < 0 || x > 3) {
			return fmt.Errorf("invalid anchor %d", x)
		}
	case FlowImmLit:
		var x FlowLitOf
		if x, ok = v.(FlowLitOf); !ok {
			break
		}
		if x.Str < 0 || x.Str >= len(m.Strings) {
			return fmt.Errorf("literal %d out of range", x.Str)
		}
		if x.Exp < 0 || x.Exp >= FixedExps+len(m.Strings) {
			return fmt.Errorf("expectation %d out of range", x.Exp)
		}
	case FlowImmCount:
		var x int
		if x, ok = v.(int); ok && x < 0 {
			return fmt.Errorf("invalid count %d", x)
		}
	case FlowImmStr:
		var x int
		if x, ok = v.(int); ok && (x < 0 || x >= len(m.Strings)) {
			return fmt.Errorf("string %d out of range", x)
		}
	case FlowImmSlot:
		var x int
		if x, ok = v.(int); ok && (x < 0 || x >= slots) {
			return fmt.Errorf("capture slot %d out of range", x)
		}
	case FlowImmScope:
		var x int
		if x, ok = v.(int); ok && (x < 0 || x >= len(m.Scopes)) {
			return fmt.Errorf("scope %d out of range", x)
		}
	case FlowImmAction:
		var x int
		if x, ok = v.(int); ok && (x < 0 || x >= len(m.Actions)) {
			return fmt.Errorf("action %d out of range", x)
		}
	case FlowImmRule:
		var x int
		if x, ok = v.(int); ok && (x < 0 || x >= len(m.Rules)) {
			return fmt.Errorf("rule %d out of range", x)
		}
	case FlowImmBind:
		var x FlowBindOf
		if x, ok = v.(FlowBindOf); !ok {
			break
		}
		if x.Var < 0 || x.Var >= len(m.Vars) {
			return fmt.Errorf("variable %d out of range", x.Var)
		}
		if x.Action < 0 || x.Action >= len(m.Actions) {
			return fmt.Errorf("action %d out of range", x.Action)
		}
	case FlowImmRunSite:
		var x int
		if x, ok = v.(int); ok && (x < 1 || x > m.siteLimit()) {
			return fmt.Errorf("invalid run site %d", x)
		}
	case FlowImmClassTest:
		var x FlowClassTestOf
		if x, ok = v.(FlowClassTestOf); !ok {
			break
		}
		if err := validateFlowImm(m, slots, FlowImmClass, x.Class); err != nil {
			return err
		}
		return validateFlowImm(m, slots, FlowImmExpect, x.Expect)
	case FlowImmGuardTest:
		var x FlowGuardTestOf
		if x, ok = v.(FlowGuardTestOf); !ok {
			break
		}
		if err := validateFlowImm(m, slots, FlowImmGuard, x.Guard); err != nil {
			return err
		}
		return validateFlowImm(m, slots, FlowImmExpect, x.Expect)
	case FlowImmLitGuardTest:
		var x FlowLitGuardTestOf
		if x, ok = v.(FlowLitGuardTestOf); !ok {
			break
		}
		if err := validateFlowImm(m, slots, FlowImmLitGuard, x.Guard); err != nil {
			return err
		}
		return validateFlowImm(m, slots, FlowImmExpect, x.Expect)
	case FlowImmAnchorTest:
		var x FlowAnchorTestOf
		if x, ok = v.(FlowAnchorTestOf); !ok {
			break
		}
		if err := validateFlowImm(m, slots, FlowImmAnchor, x.Anchor); err != nil {
			return err
		}
		return validateFlowImm(m, slots, FlowImmExpect, x.Expect)
	case FlowImmRunWalk:
		var x FlowRunWalkOf
		if x, ok = v.(FlowRunWalkOf); !ok {
			break
		}
		if x.Site < 1 || x.Site > m.siteLimit() {
			return fmt.Errorf("invalid run site %d", x.Site)
		}
		if x.KeyEnv && !slices.ContainsFunc(m.RunKeys, func(k RunKeyInfo) bool { return k.Site == x.Site }) {
			return fmt.Errorf("run site %d is keyed by the variables but has no run key", x.Site)
		}
	case FlowImmResume:
		var x FlowResumeOf
		if x, ok = v.(FlowResumeOf); !ok {
			break
		}
		if x.Site < 1 || x.Min < 0 || x.Max < -1 || x.Max >= 0 && x.Max < x.Min {
			return fmt.Errorf("invalid resumed repetition %d with %d to %d elements", x.Site, x.Min, x.Max)
		}
	case FlowImmScanSite:
		var x int
		if x, ok = v.(int); ok && (x < 1 || int64(x) > int64(scanSiteLimit(m.siteLimit()))) {
			return fmt.Errorf("invalid scan site %d", x)
		}
	case FlowImmShape:
		var x FlowShapeOf
		if x, ok = v.(FlowShapeOf); ok && (x.K < 0 || x.Mode < 0 || x.Mode > 2) {
			return fmt.Errorf("invalid shape %+v", x)
		}
	case FlowImmSilence:
		var x FlowSilenceOf
		if x, ok = v.(FlowSilenceOf); ok && (x.Delta != 1 && x.Delta != -1 || x.Site < -1 || x.Delta == 1 && x.Ok) {
			return fmt.Errorf("invalid lookahead %+v", x)
		}
	}
	if !ok {
		return fmt.Errorf("invalid immediate %#v", v)
	}
	return nil
}

// scanSiteLimit is the largest scan site number that Flow bodies of n instructions can name: the
// sites of the skip runs and run sites come first, at most one for each instruction, and then two
// for each scan site.
func scanSiteLimit(n int) int32 { return int32(min(3*n+2, 1<<30)) }

// siteLimit is the number of instructions of the Flow bodies of m: a skip-run or run site is at
// most that number, since each is a repetition that takes instructions of its own.
func (m *Module) siteLimit() int {
	n := 0
	add := func(f *Flow) {
		if f != nil {
			n += len(f.Code)
		}
	}
	for _, f := range m.Flows {
		add(f)
	}
	for _, f := range m.Streams {
		add(f)
	}
	for i := range m.OperatorsRules {
		flowLines(&m.OperatorsRules[i], add)
	}
	return n
}

// validateZeros checks the zero descriptors of the struct types of m (TypeInfo.Zeros): each is one
// descriptor of the grammar in ZeroDesc, every {Name} names a struct type of the module, and no struct
// type reaches itself through the {Name} of its descriptors, which a compiled module never has since
// such a type has no zero value. The string table indices are already checked.
func validateZeros(m *Module) error {
	structs := map[string][]string{} // struct type -> struct types its descriptors refer to
	for _, t := range m.Types {
		if t.Node < 0 {
			structs[m.Strings[t.Name]] = nil
		}
	}
	for _, t := range m.Types {
		if t.Node >= 0 {
			continue
		}
		name := m.Strings[t.Name]
		for i, z := range t.Zeros {
			refs, ok := zeroRefs(m.Strings[z])
			if !ok {
				return fmt.Errorf("type %q has the malformed zero descriptor %q for field %q", name, m.Strings[z], m.Strings[t.Fields[i]])
			}
			for _, r := range refs {
				if _, ok := structs[r]; !ok {
					return fmt.Errorf("type %q has the zero descriptor %q for field %q, which names %q, not a struct type", name, m.Strings[z], m.Strings[t.Fields[i]], r)
				}
			}
			structs[name] = append(structs[name], refs...)
		}
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var cyclic func(name string) bool
	cyclic = func(name string) bool {
		switch state[name] {
		case visiting:
			return true
		case done:
			return false
		}
		state[name] = visiting
		for _, r := range structs[name] {
			if cyclic(r) {
				return true
			}
		}
		state[name] = done
		return false
	}
	for _, t := range m.Types {
		if t.Node < 0 && cyclic(m.Strings[t.Name]) {
			return fmt.Errorf("type %q reaches itself through its zero descriptors", m.Strings[t.Name])
		}
	}
	return nil
}

// zeroRefs parses d as one zero descriptor and returns the struct type names it refers to, or false
// if d is not exactly one descriptor: i, s, b, n, l or - alone, a tuple of descriptors in parentheses,
// or a struct type name in braces. A - stands for a whole descriptor, never an element of a tuple.
func zeroRefs(d string) ([]string, bool) {
	var refs []string
	var one func(i int, inTuple bool) (int, bool)
	one = func(i int, inTuple bool) (int, bool) {
		if i >= len(d) {
			return i, false
		}
		switch d[i] {
		case 'i', 's', 'b', 'n', 'l':
			return i + 1, true
		case '-':
			return i + 1, !inTuple
		case '(':
			i++
			for i < len(d) && d[i] != ')' {
				var ok bool
				if i, ok = one(i, true); !ok {
					return i, false
				}
			}
			return i + 1, i < len(d)
		case '{':
			end := strings.IndexByte(d[i:], '}')
			if end <= 1 {
				return i, false
			}
			refs = append(refs, d[i+1:i+end])
			return i + end + 1, true
		}
		return i, false
	}
	end, ok := one(0, false)
	return refs, ok && end == len(d)
}
