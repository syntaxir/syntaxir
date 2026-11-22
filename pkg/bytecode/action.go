package bytecode

// Action is the action code of an action, a predicate or the value of a binding: the instructions of the
// function that the lowering of action expressions makes of the action expression, over registers of four files, with the
// pools they index. The instruction set and its checks belong to the engine, which compiles and runs
// action code (internal/runtime/state); the module stores it and checks that the pools refer to strings
// of the string table. See "Action Code" in spec/bytecode/README.md.
type Action struct {
	// Regs holds the sizes of the register files: values, integers and booleans, element
	// sequences and strings.
	Regs [4]int
	Code []ActionInstr
	// Consts holds the constants of the action expression.
	Consts []ActionConst
	// Names holds string table indices: built-in names, the names of Operators Rule locals and of
	// struct fields.
	Names []int
	// Members holds the member reads.
	Members []ActionMember
	// NodeTypes holds the named node types that conversions make (string table indices).
	NodeTypes []int
	// Casts holds the types of casts.
	Casts []ActionCast
	// Structs holds the struct types that literals make.
	Structs []ActionStruct
}

// ActionInstr is an instruction of action code: an opcode, a destination register or jump target, and
// up to three operands.
type ActionInstr struct {
	Op      uint8
	Dst     int32
	A, B, C int32
}

// Kinds of an action expression constant.
const (
	ConstNil = iota
	ConstInt
	ConstString
	ConstBool
)

// ActionConst is a constant: nil, an implementation int, a string (string table index in Int) or a
// bool (0 or 1 in Int).
type ActionConst struct {
	Kind int
	Int  int64
}

// ActionMember is a member read: the kind of member (a field, Start, End, Value or a capture), the
// name the action expression reads and the field or capture it names (string table indices).
type ActionMember struct {
	Kind        int
	Name, Field int
}

// ActionCast is the type of a cast: its resolved type text (string table index, as the type table
// writes an underlying node type) and its grammar position, 0 and 0 without source information.
type ActionCast struct {
	Type      int
	Line, Col int
}

// ActionStruct is a struct literal: its struct type (string table index) and number of fields.
type ActionStruct struct {
	Type   int
	Fields int
}
