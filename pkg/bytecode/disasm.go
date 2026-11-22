package bytecode

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Disassemble renders the module as human-readable text (for debugging).
func (m *Module) Disassemble() string {
	var b strings.Builder
	str := func(i int) string {
		if i < 0 || i >= len(m.Strings) {
			return fmt.Sprintf("<%d>", i)
		}
		return strconv.Quote(m.Strings[i])
	}
	names := func(ids []int) string {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = m.Strings[id]
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	// Label the action expressions of the actions of rules and Operators Rule lines.
	tlabels := map[int][]string{}
	for _, r := range m.Rules {
		name := m.Strings[r.Name]
		if r.Action >= 0 {
			tlabels[r.Action] = append(tlabels[r.Action], name+".action:")
		}
	}
	line := func(l LineInfo, name string) {
		if l.Action >= 0 {
			tlabels[l.Action] = append(tlabels[l.Action], name+".action:")
		}
	}
	for i, p := range m.OperatorsRules {
		for k, o := range p.Operands {
			line(o, fmt.Sprintf("operatorsRule%d.operand%d", i, k))
		}
		for _, o := range append(append([]OperatorInfo{}, p.Prefix...), p.InfixPostfix...) {
			line(o.Line, fmt.Sprintf("operatorsRule%d.op%d", i, o.ID))
		}
	}

	b.WriteString("rules:\n")
	for i, r := range m.Rules {
		fmt.Fprintf(&b, "  %d %s scope=%s", i, m.Strings[r.Name], names(r.Scope))
		if r.Action >= 0 {
			fmt.Fprintf(&b, " action=%d", r.Action)
		}
		if r.NodeType >= 0 {
			fmt.Fprintf(&b, " type=%s", m.Strings[r.NodeType])
		}
		if r.OperatorsRule >= 0 {
			fmt.Fprintf(&b, " operatorsRule=%d", r.OperatorsRule)
		}
		if r.Where >= 0 {
			fmt.Fprintf(&b, " where=%q", m.Strings[r.Where])
		}
		if r.Value != nil {
			fmt.Fprintf(&b, " value=%s", m.flowImm(FlowImmShape, *r.Value, str))
		}
		var flags []string
		for _, f := range []struct {
			on   bool
			name string
		}{{r.Memo, "memo"}, {r.Leader, "leader"}, {r.Member, "member"}, {r.PositionDependent, "positional"}, {r.ValueFreeTwin, "novalue"}, {r.Transient, "transient"}, {r.Rec, "rec"}, {r.Splice, "splice"}} {
			if f.on {
				flags = append(flags, f.name)
			}
		}
		if len(flags) > 0 {
			fmt.Fprintf(&b, " %s", strings.Join(flags, ","))
		}
		b.WriteString("\n")
	}
	for i, p := range m.OperatorsRules {
		fmt.Fprintf(&b, "operatorsRule %d: skip=%t operands=%d\n", i, p.SkipFlow != nil, len(p.Operands))
		for _, o := range append(append([]OperatorInfo{}, p.Prefix...), p.InfixPostfix...) {
			kind := []string{"prefix", "postfix", "infix"}[o.Kind]
			assoc := []string{"left", "right", "none"}[o.Assoc]
			fmt.Fprintf(&b, "  op%d %s %s level=%d action=%d scope=%s", o.ID, kind, assoc, o.Level, o.Line.Action, names(o.Line.Scope))
			b.WriteString("\n")
		}
	}
	b.WriteString("actions:\n")
	for i, t := range m.Actions {
		for _, l := range tlabels[i] {
			fmt.Fprintf(&b, "%s\n", l)
		}
		fmt.Fprintf(&b, "  action %d regs=%v\n", i, t.Regs)
		m.disassembleAction(&b, &t, str)
	}
	for _, k := range m.RunKeys {
		fmt.Fprintf(&b, "runKey site=%d vars=%s bind=%s\n", k.Site, names(k.Vars), names(k.Bind))
	}
	for _, rule := range slices.Sorted(maps.Keys(m.Flows)) {
		f := m.Flows[rule]
		name := fmt.Sprintf("rule%d", rule)
		if rule >= 0 && rule < len(m.Rules) {
			name = m.Strings[m.Rules[rule].Name]
		}
		m.disassembleFlow(&b, name, f, str)
	}
	for _, rule := range slices.Sorted(maps.Keys(m.Streams)) {
		name := fmt.Sprintf("rule%d", rule)
		if rule >= 0 && rule < len(m.Rules) {
			name = m.Strings[m.Rules[rule].Name]
		}
		m.disassembleFlow(&b, name+".stream", m.Streams[rule], str)
	}
	for i := range m.OperatorsRules {
		p := &m.OperatorsRules[i]
		if p.SkipFlow != nil {
			m.disassembleFlow(&b, fmt.Sprintf("operatorsRule%d.skip", i), p.SkipFlow, str)
		}
		for j := range p.Operands {
			if l := &p.Operands[j]; l.Flow != nil {
				m.disassembleFlow(&b, fmt.Sprintf("operatorsRule%d.operand%d %s", i, j, m.flowImm(FlowImmShape, l.Shape, str)), l.Flow, str)
			}
		}
		for _, o := range slices.Concat(p.Prefix, p.InfixPostfix) {
			if o.Line.Flow != nil {
				m.disassembleFlow(&b, fmt.Sprintf("operatorsRule%d.op%d %s", i, o.ID, m.flowImm(FlowImmShape, o.Line.Shape, str)), o.Line.Flow, str)
			}
		}
	}
	return b.String()
}

// disassembleFlow writes the Flow body f under the name name.
func (m *Module) disassembleFlow(b *strings.Builder, name string, f *Flow, str func(int) string) {
	fmt.Fprintf(b, "flow %s regs=%d:\n", name, f.Regs)
	for pc, in := range f.Code {
		fmt.Fprintf(b, "  %4d  %-10s %s\n", pc, in.Op, m.flowOperands(&in, str))
	}
}

// flowOperands renders the registers, target and immediate of a Flow instruction: destinations
// before an arrow (_ for a discarded one), then the arguments, the target and the immediate.
func (m *Module) flowOperands(in *FlowInstr, str func(int) string) string {
	var parts []string
	reg := func(r int) string {
		if r < 0 {
			return "_"
		}
		return fmt.Sprintf("r%d", r)
	}
	if len(in.Dst) > 0 {
		ds := make([]string, len(in.Dst))
		for i, d := range in.Dst {
			ds[i] = reg(d)
		}
		parts = append(parts, strings.Join(ds, ",")+" =")
	}
	for _, a := range in.Args {
		parts = append(parts, reg(a))
	}
	sh, _ := in.Op.Shape()
	if sh.Target {
		parts = append(parts, fmt.Sprintf("-> %d", in.Target))
	}
	if imm := m.flowImm(sh.Imm, in.Imm, str); imm != "" {
		parts = append(parts, imm)
	} else if in.Op == FlowConst {
		parts = append(parts, "nil")
	}
	return strings.Join(parts, " ")
}

// flowImm renders the immediate v of the kind k, or "" for none.
func (m *Module) flowImm(k FlowImm, v any, str func(int) string) string {
	var parts []string
	switch x := v.(type) {
	case nil:
	case int:
		switch {
		case k == FlowImmClass && x >= 0 && x < len(m.Classes):
			parts = append(parts, str(m.Classes[x].Expectation))
		case k == FlowImmStr && x >= 0 && x < len(m.Strings):
			parts = append(parts, strconv.Quote(m.Strings[x]))
		case k == FlowImmSlot:
			parts = append(parts, fmt.Sprintf("slot=%d", x))
		case k == FlowImmRunSite, k == FlowImmScanSite:
			parts = append(parts, fmt.Sprintf("site=%d", x))
		case k == FlowImmAction:
			parts = append(parts, fmt.Sprintf("action=%d", x))
		case k == FlowImmRule && x >= 0 && x < len(m.Rules):
			parts = append(parts, m.Strings[m.Rules[x].Name])
		case k == FlowImmScope && x >= 0 && x < len(m.Scopes):
			ns := make([]string, len(m.Scopes[x]))
			for i, id := range m.Scopes[x] {
				ns[i] = m.Strings[id]
			}
			parts = append(parts, "scope=["+strings.Join(ns, " ")+"]")
		default:
			parts = append(parts, fmt.Sprint(x))
		}
	case bool:
		parts = append(parts, fmt.Sprint(x))
	case FlowRunWalkOf:
		parts = append(parts, fmt.Sprintf("site=%d build=%t keyenv=%t", x.Site, x.Build, x.KeyEnv))
	case FlowResumeOf:
		parts = append(parts, fmt.Sprintf("resume=%d min=%d max=%d shiftable=%t", x.Site, x.Min, x.Max, x.Shiftable))
	case FlowBindOf:
		parts = append(parts, fmt.Sprintf("var=%d action=%d", x.Var, x.Action))
	case FlowGuardOf:
		g := fmt.Sprintf("class=%d", x.Class)
		if x.Class >= 0 && x.Class < len(m.Classes) {
			g = str(m.Classes[x.Class].Expectation)
		}
		parts = append(parts, fmt.Sprintf("%s depth=%d", g, x.Depth))
	case FlowLitGuardOf:
		g := strconv.QuoteRune(x.Rune)
		if x.Fold {
			g += " fold"
		}
		parts = append(parts, fmt.Sprintf("%s depth=%d", g, x.Depth))
	case FlowExpectOf:
		e := fmt.Sprintf("exp=%d", x.Exp)
		if x.Exp >= FixedExps {
			e = str(x.Exp - FixedExps)
		}
		if x.Message {
			e += " message"
		}
		parts = append(parts, e)
	case FlowCallOf:
		r := fmt.Sprintf("rule%d", x.Rule)
		if x.Rule >= 0 && x.Rule < len(m.Rules) {
			r = m.Strings[m.Rules[x.Rule].Name]
		}
		parts = append(parts, fmt.Sprintf("%s level=%d", r, x.Level))
	case FlowLitOf:
		l := fmt.Sprintf("str%d", x.Str)
		if x.Str >= 0 && x.Str < len(m.Strings) {
			l = strconv.Quote(m.Strings[x.Str])
		}
		if x.Fold {
			l += " fold"
		}
		e := fmt.Sprintf("exp=%d", x.Exp)
		if x.Exp >= FixedExps {
			e = str(x.Exp - FixedExps)
		}
		parts = append(parts, l+" "+e)
	case FlowShapeOf:
		parts = append(parts, fmt.Sprintf("elems=%t shared=%t tuple=%t k=%d mode=%d", x.Elems, x.Shared, x.Tuple, x.K, x.Mode))
	case FlowSilenceOf:
		parts = append(parts, fmt.Sprintf("delta=%d site=%d ok=%t", x.Delta, x.Site, x.Ok))
	case FlowClassTestOf:
		parts = append(parts, m.flowImm(FlowImmClass, x.Class, str), m.flowImm(FlowImmExpect, x.Expect, str))
	case FlowScanClassOf:
		parts = append(parts, m.flowImm(FlowImmClassTest, x.Test, str), m.flowImm(FlowImmScanSite, x.Site, str))
	case FlowGuardTestOf:
		parts = append(parts, m.flowImm(FlowImmGuard, x.Guard, str), m.flowImm(FlowImmExpect, x.Expect, str))
	case FlowLitGuardTestOf:
		parts = append(parts, m.flowImm(FlowImmLitGuard, x.Guard, str), m.flowImm(FlowImmExpect, x.Expect, str))
	case FlowAnchorTestOf:
		parts = append(parts, m.flowImm(FlowImmAnchor, x.Anchor, str), m.flowImm(FlowImmExpect, x.Expect, str))
	default:
		parts = append(parts, fmt.Sprintf("%#v", x))
	}
	return strings.Join(parts, " ")
}

// disassembleAction renders the pools and the instructions of the action expression t. The opcodes of action code
// belong to the engine, so instructions show their numbers.
func (m *Module) disassembleAction(b *strings.Builder, t *Action, str func(int) string) {
	for i, c := range t.Consts {
		v := "nil"
		switch c.Kind {
		case ConstInt:
			v = strconv.FormatInt(c.Int, 10)
		case ConstString:
			v = str(int(c.Int))
		case ConstBool:
			v = strconv.FormatBool(c.Int == 1)
		}
		fmt.Fprintf(b, "    const %d %s\n", i, v)
	}
	for i, n := range t.Names {
		fmt.Fprintf(b, "    name %d %s\n", i, str(n))
	}
	for i, x := range t.Members {
		fmt.Fprintf(b, "    member %d kind=%d %s %s\n", i, x.Kind, str(x.Name), str(x.Field))
	}
	for i, n := range t.NodeTypes {
		fmt.Fprintf(b, "    node %d %s\n", i, str(n))
	}
	for i, x := range t.Casts {
		fmt.Fprintf(b, "    cast %d %s at=%d:%d\n", i, str(x.Type), x.Line, x.Col)
	}
	for i, x := range t.Structs {
		fmt.Fprintf(b, "    struct %d %s fields=%d\n", i, str(x.Type), x.Fields)
	}
	for pc, in := range t.Code {
		fmt.Fprintf(b, "  %4d  T%-3d %d %d %d %d\n", pc, in.Op, in.Dst, in.A, in.B, in.C)
	}
}
