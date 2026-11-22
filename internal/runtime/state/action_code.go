package state

import (
	"fmt"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Action code: a ActionProgram as a bytecode module stores it (bytecode.Action), so that a saved program
// runs the action expressions a compiled one runs. The instructions are stored as they are; the pools that hold
// what the engine resolved from the program (node types, struct kinds, cast types) are stored as the
// names and type texts they were resolved from, and resolved again when the module is loaded.

// Export returns the action code of t, adding the strings it refers to with str, which returns the
// index of a string in the module's string table.
func (t *ActionProgram) Export(str func(string) int) (bytecode.Action, error) {
	x := bytecode.Action{Regs: [4]int{int(t.NA), int(t.NN), int(t.NS), int(t.NT)}}
	x.Code = make([]bytecode.ActionInstr, len(t.Code))
	for i, in := range t.Code {
		x.Code[i] = bytecode.ActionInstr{Op: uint8(in.Op), Dst: in.Dst, A: in.A, B: in.B, C: in.C}
	}
	for _, v := range t.Consts {
		var c bytecode.ActionConst
		switch v := v.(type) {
		case nil:
			c.Kind = bytecode.ConstNil
		case int:
			c.Kind, c.Int = bytecode.ConstInt, int64(v)
		case string:
			c.Kind, c.Int = bytecode.ConstString, int64(str(v))
		case bool:
			c.Kind, c.Int = bytecode.ConstBool, int64(b2i(v))
		default:
			return x, fmt.Errorf("core: action expression constant of type %T", v)
		}
		x.Consts = append(x.Consts, c)
	}
	for _, n := range t.Names {
		x.Names = append(x.Names, str(n))
	}
	for _, m := range t.Members {
		x.Members = append(x.Members, bytecode.ActionMember{Kind: m.Kind, Name: str(m.Name), Field: str(m.Field)})
	}
	for _, k := range t.NodeTypes {
		x.NodeTypes = append(x.NodeTypes, str(k[valueimpl.BaseList].Type))
	}
	for _, c := range t.Casts {
		typ := -1
		if c.Type != nil {
			typ = str(c.Type.Text)
		}
		x.Casts = append(x.Casts, bytecode.ActionCast{Type: typ, Line: c.Line, Col: c.Col})
	}
	for _, s := range t.Structs {
		x.Structs = append(x.Structs, bytecode.ActionStruct{Type: str(s.Kind.Type), Fields: s.Fields})
	}
	return x, nil
}

// ActionTypes resolves the names and type texts of action code when a module is loaded.
type ActionTypes struct {
	// NodeType returns the kinds of the named node type name, or nil if there is none.
	NodeType func(name string) *nodes.NodeTypeKinds
	// Struct returns the kind of the nodes of the struct type name, or nil if there is none.
	Struct func(name string) *valueimpl.Kind
	// Cast returns the type of the resolved type text of a cast, or an error.
	Cast func(text string) (*program.FieldType, error)
}

// Kinds of the operands of action instructions, for the checks of LoadAction.
const (
	oNone   = iota // not read
	oA             // a register of the value file
	oN             // a register of the integer file
	oS             // a register of the sequence file
	oT             // a register of the string file
	oJump          // a jump target
	oConst         // an index of Consts
	oName          // an index of Names
	oMember        // an index of Members
	oNode          // an index of NodeTypes
	oCast          // an index of Casts
	oStruct        // an index of Structs
	oSlot          // a capture slot: not negative
	oVar           // a variable of the module
	oUnary         // a unary operator (action.UnaryOps)
	oBinary        // a binary operator (action.BinaryOps)
	oLogic         // a logical operator (action.LogicalOps)
	oFlag          // 0 or 1
	oImm           // any integer
	oCount         // a number of units: not negative
)

// actionOperands holds, for each instruction, the kinds of its operands dst, a, b and c.
var actionOperands = map[ActionOp][4]uint8{
	TNop:        {},
	TConst:      {oA, oConst},
	TIntK:       {oN, oImm},
	TMoveA:      {oA, oA},
	TMoveN:      {oN, oN},
	TMoveS:      {oS, oS},
	TMoveT:      {oT, oT},
	TCap:        {oA, oSlot},
	TLocal:      {oA, oName},
	TVar:        {oA, oVar},
	TUnbox:      {oA, oA},
	TFromB:      {oA, oN},
	TFromT:      {oA, oT},
	TNot:        {oN, oN},
	TLt:         {oN, oN, oN},
	TAdd:        {oN, oN, oN},
	TEqN:        {oN, oN, oN},
	TIsNil:      {oN, oA},
	TMember:     {oA, oA, oMember},
	TUnary:      {oA, oA, oUnary},
	TBinary:     {oA, oA, oA, oBinary},
	TLogic:      {oN, oA, oLogic},
	TText:       {oT, oA},
	TTextEq:     {oN, oT, oT},
	TLength:     {oA, oA},
	TConvert:    {oA, oA, oNode},
	TConforms:   {oN, oA, oCast},
	TCastErr:    {oA, oA, oCast},
	TKids:       {oN},
	TPush:       {oNone, oA, oName},
	TPushAll:    {oNone, oA, oName},
	TAbove:      {oS, oN},
	THull:       {oA, oS, oFlag},
	TItems:      {oS, oA, oName},
	TItemAt:     {oA, oS, oN, oFlag},
	TSeqLen:     {oN, oS},
	TStep:       {},
	TKeep:       {oNone, oS, oN},
	TFilter:     {oN, oA},
	TCopy:       {oA, oS},
	TStruct:     {oA, oStruct},
	TSetField:   {oNone, oA, oA, oName},
	TCover:      {oN, oA, oA, oN},
	TMade:       {oNone, oA, oN},
	TJump:       {oJump},
	TJumpF:      {oJump, oN},
	TRet:        {oNone, oA},
	TRetErr:     {oNone, oA},
	TField:      {oN, oA, oA, oName},
	TUnits:      {oNone, oCount},
	TUnitsOf:    {oNone, oA, oCount},
	TUnitsSince: {oNone, oN},
	TLoop:       {oJump, oS, oN, oN},
	TEndLoop:    {oJump, oNone, oN, oN},
}

// LoadAction makes the ActionProgram of the action code x of a module with the string table strs and
// vars variables, resolving its types with types. It checks that every operand is in range for its
// kind (a register of its file, a pool index, a jump target in the code, a variable, an operator)
// and that the code ends with a return or a jump, so that running it cannot leave the code.
func LoadAction(x *bytecode.Action, strs []string, vars int, types ActionTypes) (*ActionProgram, error) {
	t := new(ActionProgram)
	if err := loadAction(t, x, strs, vars, types); err != nil {
		return nil, err
	}
	return t, nil
}

// LoadActions makes the ActionPrograms of the action codes xs as LoadAction does, allocating them together.
func LoadActions(xs []bytecode.Action, strs []string, vars int, types ActionTypes) ([]*ActionProgram, error) {
	progs := make([]ActionProgram, len(xs))
	actionExprs := make([]*ActionProgram, len(xs))
	for i := range xs {
		if err := loadAction(&progs[i], &xs[i], strs, vars, types); err != nil {
			return nil, fmt.Errorf("action %d: %w", i, err)
		}
		actionExprs[i] = &progs[i]
	}
	return actionExprs, nil
}

// loadAction fills t from x for LoadAction. Each pool gets its exact capacity up front.
func loadAction(t *ActionProgram, x *bytecode.Action, strs []string, vars int, types ActionTypes) error {
	*t = ActionProgram{
		NA: int32(x.Regs[0]), NN: int32(x.Regs[1]), NS: int32(x.Regs[2]), NT: int32(x.Regs[3]),
		Consts:    make([]any, 0, len(x.Consts)),
		Names:     make([]string, 0, len(x.Names)),
		Members:   make([]ActionMember, 0, len(x.Members)),
		NodeTypes: make([]*nodes.NodeTypeKinds, 0, len(x.NodeTypes)),
		Casts:     make([]ActionCast, 0, len(x.Casts)),
		Structs:   make([]ActionStruct, 0, len(x.Structs)),
	}
	for _, n := range x.Regs {
		if n < 0 || n > 1<<20 {
			return fmt.Errorf("invalid register count %d", n)
		}
	}
	str := func(i int) (string, error) {
		if i < 0 || i >= len(strs) {
			return "", fmt.Errorf("invalid string index %d", i)
		}
		return strs[i], nil
	}
	for _, c := range x.Consts {
		switch c.Kind {
		case bytecode.ConstNil:
			t.Consts = append(t.Consts, nil)
		case bytecode.ConstInt:
			if int64(int(c.Int)) != c.Int {
				return fmt.Errorf("action expression constant %d exceeds the implementation int range", c.Int)
			}
			t.Consts = append(t.Consts, int(c.Int))
		case bytecode.ConstString:
			s, err := str(int(c.Int))
			if err != nil {
				return err
			}
			t.Consts = append(t.Consts, s)
		case bytecode.ConstBool:
			if c.Int != 0 && c.Int != 1 {
				return fmt.Errorf("invalid boolean constant %d", c.Int)
			}
			t.Consts = append(t.Consts, c.Int == 1)
		default:
			return fmt.Errorf("invalid constant kind %d", c.Kind)
		}
	}
	for _, i := range x.Names {
		s, err := str(i)
		if err != nil {
			return err
		}
		t.Names = append(t.Names, s)
	}
	for _, m := range x.Members {
		name, err := str(m.Name)
		if err != nil {
			return err
		}
		field, err := str(m.Field)
		if err != nil {
			return err
		}
		if m.Kind < 0 || m.Kind > MemberCapture {
			return fmt.Errorf("invalid member kind %d", m.Kind)
		}
		t.Members = append(t.Members, ActionMember{Kind: m.Kind, Name: name, Field: field})
	}
	for _, i := range x.NodeTypes {
		s, err := str(i)
		if err != nil {
			return err
		}
		k := types.NodeType(s)
		if k == nil {
			return fmt.Errorf("action expression conversion to %q, which is not a named node type", s)
		}
		t.NodeTypes = append(t.NodeTypes, k)
	}
	for _, c := range x.Casts {
		if c.Line < 0 || c.Col < 0 {
			return fmt.Errorf("invalid cast position %d:%d", c.Line, c.Col)
		}
		var ft *program.FieldType
		if c.Type >= 0 {
			s, err := str(c.Type)
			if err != nil {
				return err
			}
			if ft, err = types.Cast(s); err != nil {
				return err
			}
		}
		t.Casts = append(t.Casts, ActionCast{Type: ft, Line: c.Line, Col: c.Col})
	}
	for _, s := range x.Structs {
		name, err := str(s.Type)
		if err != nil {
			return err
		}
		k := types.Struct(name)
		if k == nil {
			return fmt.Errorf("action expression struct literal of %q, which is not a struct type", name)
		}
		if s.Fields < 0 {
			return fmt.Errorf("invalid field count %d", s.Fields)
		}
		t.Structs = append(t.Structs, ActionStruct{Kind: k, Fields: s.Fields})
	}
	n := len(x.Code)
	if n == 0 {
		return fmt.Errorf("empty action code")
	}
	limit := func(kind uint8) (int, bool) {
		switch kind {
		case oA:
			return int(t.NA), true
		case oN:
			return int(t.NN), true
		case oS:
			return int(t.NS), true
		case oT:
			return int(t.NT), true
		case oJump:
			return n, true
		case oConst:
			return len(t.Consts), true
		case oName:
			return len(t.Names), true
		case oMember:
			return len(t.Members), true
		case oNode:
			return len(t.NodeTypes), true
		case oCast:
			return len(t.Casts), true
		case oStruct:
			return len(t.Structs), true
		case oVar:
			return vars, true
		case oUnary:
			return len(actionUnaryOps), true
		case oBinary:
			return len(actionBinaryOps), true
		case oLogic:
			return len(actionLogicalOps), true
		case oFlag:
			return 2, true
		}
		return 0, false
	}
	t.Code = make([]ActionInstr, n)
	for pc, in := range x.Code {
		kinds, ok := actionOperands[ActionOp(in.Op)]
		if !ok {
			return fmt.Errorf("invalid action instruction %d at %d", in.Op, pc)
		}
		for k, v := range [4]int32{in.Dst, in.A, in.B, in.C} {
			if kinds[k] == oSlot && v < 0 {
				return fmt.Errorf("invalid slot %d at %d", v, pc)
			}
			if kinds[k] == oCount && v < 0 {
				return fmt.Errorf("invalid count %d at %d", v, pc)
			}
			if max, ok := limit(kinds[k]); ok && (v < 0 || int(v) >= max) {
				return fmt.Errorf("invalid operand %d of action instruction %d at %d", v, in.Op, pc)
			}
		}
		t.Code[pc] = ActionInstr{Op: ActionOp(in.Op), Dst: in.Dst, A: in.A, B: in.B, C: in.C}
	}
	switch t.Code[n-1].Op {
	case TRet, TRetErr:
	default:
		return fmt.Errorf("action code does not end with a return")
	}
	return checkActionLoops(t.Code)
}

// checkActionLoops checks that the evaluation of code ends whatever its registers hold: every jump
// but the back edge of a counted loop goes forward, and a loop runs at most the number of elements
// that its TLoop counted. A TLoop at l whose exit is e+1 pairs with the TEndLoop at e, which jumps
// back to l+1 and names the same index and count registers; the loops nest; no jump from outside a
// loop's body goes into it; and no instruction of the body writes the loop's index or count, so the
// body is entered only through its TLoop, and each pass of its back edge increments an index that
// nothing else changes towards a count that nothing else changes.
func checkActionLoops(code []ActionInstr) error {
	type loop struct{ start, end int } // the body: from start to the TEndLoop at end
	var loops []loop
	for pc, in := range code {
		switch {
		case in.Op == TEndLoop:
			// Checked against its TLoop below.
		case actionOperands[in.Op][0] == oJump && int(in.Dst) <= pc:
			return fmt.Errorf("action jump from %d back to %d", pc, in.Dst)
		}
		if in.Op == TLoop {
			e := int(in.Dst) - 1
			if e <= pc || code[e].Op != TEndLoop || int(code[e].Dst) != pc+1 || code[e].B != in.B || code[e].C != in.C || in.B == in.C {
				return fmt.Errorf("action loop at %d has no matching end", pc)
			}
			loops = append(loops, loop{pc + 1, e})
		}
	}
	paired := make(map[int]bool, len(loops))
	for _, l := range loops {
		if paired[l.end] {
			return fmt.Errorf("action loop end at %d shared by two loops", l.end)
		}
		paired[l.end] = true
	}
	for pc, in := range code {
		if in.Op == TEndLoop && !paired[pc] {
			return fmt.Errorf("action loop end at %d without a loop", pc)
		}
	}
	// writes returns the integer registers that in writes.
	writes := func(in ActionInstr) []int32 {
		switch in.Op {
		case TLoop, TEndLoop:
			return []int32{in.B, in.C}
		}
		if actionOperands[in.Op][0] == oN {
			return []int32{in.Dst}
		}
		return nil
	}
	for _, l := range loops {
		inside := func(pc int) bool { return l.start <= pc && pc <= l.end }
		b, c := code[l.end].B, code[l.end].C
		for _, m := range loops {
			// Two loops, each from its TLoop to its TEndLoop, are apart or one is in the body of the
			// other.
			apart := m.end < l.start-1 || l.end < m.start-1
			if m != l && !apart && !(inside(m.start-1) && inside(m.end)) && !(m.start <= l.start-1 && l.end <= m.end) {
				return fmt.Errorf("action loops at %d and %d do not nest", l.start-1, m.start-1)
			}
		}
		for pc, in := range code {
			if actionOperands[in.Op][0] == oJump && inside(int(in.Dst)) && !inside(pc) {
				return fmt.Errorf("action jump from %d into the loop at %d", pc, l.start-1)
			}
			if !inside(pc) || pc == l.end {
				continue
			}
			for _, r := range writes(in) {
				if r == b || r == c {
					return fmt.Errorf("instruction %d writes the index or count of the loop at %d", pc, l.start-1)
				}
			}
		}
	}
	return nil
}
