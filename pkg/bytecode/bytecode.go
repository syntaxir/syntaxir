// Package bytecode defines the module that a SyntaxIR grammar compiles to, which the bytecode VM
// runs, and its compiled-file format: the tables of a module (Module and the Info types), Flow
// bodies (Flow) and their instruction set (FlowOp, FlowInstr), action code (Action), the encoding of the
// compiled-file format (Encoder, Decoder, WriteModule, ReadModule, FileMagic) and the load-time
// validation of a module (ValidateModule). spec/bytecode/README.md specifies all of them; the
// instruction set changes with its version, ISAVersion.
//
// Package compiler makes modules (compiler.Marshal writes one as a compiled grammar file) and package
// runtime/vm loads and runs them; most programs use those packages and not this one. Like all of
// the module API at v0.x, this package may change in any release.
package bytecode

// Module is a grammar compiled to bytecode. It is the only thing the VM runs.
// The instruction set and its semantics are specified in spec/bytecode/README.md.
type Module struct {
	Strings []string   // string table
	Classes []Class    // character class table
	Types   []TypeInfo // type table (struct fields and named node types)
	Scopes  [][]int    // scopes of repetition elements (lists of capture names)
	Vars    []VarInfo  // variable table
	Rules   []RuleInfo // rule table
	// Starts holds the rule table indices of the start rules, the rules where parsing may begin,
	// in declaration order, and DefaultStart that of the default one.
	Starts         []int
	DefaultStart   int
	OperatorsRules []OperatorsRuleInfo
	// Actions holds the action code of the actions, predicates and bindings.
	Actions []Action
	// SourceInfo reports that the values record source information (compiler.WithSourceInfo).
	SourceInfo bool
	// DefaultValue reports that value actions were dropped for default values
	// (compiler.WithDefaultValue).
	DefaultValue bool
	// RunKeys holds the keys of the run sites that are not free of the environment, by increasing
	// site.
	RunKeys []RunKeyInfo
	// Flows holds the bodies of the rules, selected from Flow IR, by rule index. Every rule has one
	// but a rule whose body is the expression of its Operators Rule (RuleInfo.OperatorsRule).
	Flows map[int]*Flow
	// Streams holds the stream bodies of the rules whose body has a #stream repetition, by rule
	// index: the body that a stream parse runs at the top call of its start rule
	// (FlowStreamOn), whose repetition delivers its elements to the callback instead of collecting
	// them.
	Streams map[int]*Flow
}

// RunKeyInfo is the key of the records of a run site that is not free of the environment: the
// variables that its element reads, directly or in the rules it calls, and those it binds (Vars),
// and those it binds (Bind), as string table indices sorted by name. A walk that takes a record
// binds the variables of Bind to the values the walk of the record left in them.
type RunKeyInfo struct {
	Site       int
	Vars, Bind []int
}

// RuleStreams reports whether rule i can start a stream parse: whether it has a stream body
// (Module.Streams).
func (m *Module) RuleStreams(i int) bool { return m.Streams[i] != nil }

// InstructionSetVersion is the instruction set of every module: the value model's.
func (m *Module) InstructionSetVersion() int { return ISAVersion }

// Class is a character class. Ranges is a list of lower and upper bound pairs.
type Class struct {
	Ranges      []rune
	Negated     bool
	Expectation int // the expectation that a failure to match records (string table index)
}

// Has reports whether the character ch is in the class.
func (cl *Class) Has(ch rune) bool {
	for k := 0; k < len(cl.Ranges); k += 2 {
		if cl.Ranges[k] <= ch && ch <= cl.Ranges[k+1] {
			return !cl.Negated
		}
	}
	return cl.Negated
}

// TypeInfo describes a type: a struct type, or a named node type.
type TypeInfo struct {
	Name   int   // string table index
	Fields []int // struct field names (string table indices)
	// Zeros holds the zero descriptor of each field (string table indices, parallel to Fields): how
	// a field that a constructor left unset reads.
	Zeros []int
	// Node is, for a named node type, the underlying node type (string table index of its text,
	// written as the type texts of casts are), and -1 for a struct type.
	Node int
}

// Variable types
const (
	VarInt = iota
	VarString
	VarBool
)

// VarInfo describes a variable. Every parse starts with the variable bound to its initial value.
type VarInfo struct {
	Name int // string table index
	Type int // VarInt, VarString or VarBool
	// Int is the initial value of an int variable, and 0 or 1 for a bool variable.
	Int int
	// Str is the string table index of the initial value of a string variable, and -1 for
	// the other types.
	Str int
}

// RuleInfo describes a rule.
type RuleInfo struct {
	Name   int // string table index
	Scope  []int
	Action int // action expression of the action (index of Actions, -1 if none)
	// NodeType is the named node type the rule's value is converted to (string table index, -1 if
	// none).
	NodeType int
	Memo     bool
	Leader   bool
	// Member reports that the rule belongs to a left-recursion cycle and is not its leader.
	Member bool
	// Cycle numbers the left-recursion cycle of a leader or member from 1, and is 0 for any other
	// rule.
	Cycle int
	// PositionDependent reports that the results of the rule may depend on positions: an action or
	// predicate of the rule, or of a rule it calls, reads positions.
	PositionDependent bool
	OperatorsRule     int // OperatorsRule table index (-1 if none)
	// ValueFreeTwin reports that this is a value-free twin, called from places where the value is
	// discarded (its value is always nil). It has the same name as the original rule and is placed
	// after all the original rules in the rule table.
	ValueFreeTwin bool
	// Transient reports that the rule is not memoized in ordinary parses (even if Memo is set). In
	// incremental parsing, Memo applies.
	Transient bool
	// Lexical reports that the rule only matches characters: it has no action, is on no cycle of
	// calls, binds, tests and recovers nothing and calls only lexical rules. A Document does not memoize a rule that is
	// ValueFreeTwin, Transient and Lexical either.
	Lexical bool
	// Rec reports that the rule is on a cycle of calls (or calls itself): its entries count
	// against the nesting limit, those of other rules do not.
	Rec bool
	// Where is the text that names the rule in the message of a failed action (string table index),
	// or -1 for the name of the rule. The hidden rule of an inline action names the rule that
	// holds it.
	Where int
	// Splice reports that the rule makes no node of its own: the elements of its value are the
	// elements of the sequence that calls it (a hidden rule without an action).
	Splice bool
	// Missing reports that the rule or a rule it calls contains #missing: while missing tokens
	// are inserted, its results depend on the insertions made before it, so they are not memoized
	// and its memo keys carry the insertion state.
	Missing bool
	// Value is the shape of the value that the rule makes from the value of its body, as
	// FlowScopeValue makes it with the start of the call, before the captures are attached; nil if
	// the value of the body is the rule's value: a rule with an action, a splice, an Operators Rule
	// or a rule without a value. The Flow bodies of a rule leave this step to the call.
	Value *FlowShapeOf
	// Vars are the variables the rule or its callees read (string table indices, sorted by name).
	// The rule's memo entries are keyed by their values at the call.
	Vars []int
}

// OperatorsRuleInfo is an entry of the Operators Rule table.
type OperatorsRuleInfo struct {
	// SkipFlow is the Flow body of the skip, or nil for an Operators Rule without one.
	SkipFlow     *Flow
	Operands     []LineInfo
	Prefix       []OperatorInfo
	InfixPostfix []OperatorInfo // infix and postfix operators
}

// LineInfo is an Operators Rule operand line or operator line.
type LineInfo struct {
	Scope  []int
	Action int // action expression of the action (index of Actions, -1 if none)
	// Flow is the Flow body of the line. It returns the value
	// of the line's expression, which Shape makes the value of the line as SCOPEVALUE and
	// SHAPESINGLE do: the scope value of an operand line, and the single of an operator line.
	Flow  *Flow
	Shape FlowShapeOf
}

// OperatorInfo is an Operators Rule operator.
type OperatorInfo struct {
	ID    int
	Kind  int // OperatorPrefix, OperatorPostfix or OperatorInfix
	Assoc int // AssocLeft, AssocRight or AssocNone
	Level int
	Line  LineInfo
}

// Operators Rule operator kinds and associativity
const (
	OperatorPrefix = iota
	OperatorPostfix
	OperatorInfix
)

const (
	AssocLeft = iota
	AssocRight
	AssocNone
)

// ISAVersion is the instruction set of the value model, the only one supported. Earlier
// versions built the previous syntax trees, so their files are not loaded and need to be
// compiled again.
const ISAVersion = 42
