package vmlower

import (
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Input is what the bytecode lowering reads of a checked grammar: the Core IR source of the
// grammar, its checked types and variables, its rule table and the site facts of its run sites.
// The lowering takes nothing else. The front end's checking result supplies every field, and the
// caller that holds a checked program fills it from there (internal/driver/checked.VMInput); the
// lowering does not import the compiler that makes the checked program.
type Input struct {
	// Elab is the program the Core IR of the grammar is elaborated from (CoreOf). It also
	// answers the environment-free sites (EnvFree), the checker of the types (Checker) and whether
	// the grammar uses #missing (MissingTokens).
	Elab elaborate.Program
	// Vars are the variables the grammar declares (check.Model.Vars), by name and index.
	Vars *check.VarTable
	// Types are the type declarations of the grammar that are type definitions, in source order.
	Types []*ast.TypeDef
	// Zeros are the zero descriptors of the struct types, by type name, for the types that have
	// them.
	Zeros map[string]Zero
	// Rules is the rule table of the grammar: the rules, then their value-free twins at the
	// positions of their IDs (Twins). The bytecode rule IDs are the indexes of this table.
	Rules []Rule
	Twins []Rule
	// StartRules names the start rules, and DefaultStartRule the rule that is started by default.
	StartRules       []string
	DefaultStartRule string
	// RunKeys are the keys of the run sites that are not free of the environment, by site.
	RunKeys map[int]RunKey
	// RecordsSourceInfo reports that nodes record source information; MakesDefaultValue that the
	// value actions and declared rule types were dropped. Both are written to the module.
	RecordsSourceInfo bool
	MakesDefaultValue bool
}

// Zero is the zero descriptor of a struct type: its field names in order, and the zero of each
// field by name (the text of its zero value).
type Zero struct {
	Names []string
	Zeros map[string]string
}

// Rule is an entry of the rule table of Input: the properties of a rule that its bytecode entry
// records. Its ID is its index in Input.Rules or Input.Twins.
type Rule struct {
	Name, Where string
	// NodeType and OpNodeType name the node type the rule's value is converted to, and the node
	// type of the operator nodes of an Operators Rule.
	NodeType, OpNodeType string
	// Vars are the variables the rule reads, sorted.
	Vars                                                []string
	Memo, Leader, Member, Positional, Splice, Transient bool
	Rec, NoValue                                        bool
	Cycle                                               int
}

// RunKey is the key of a run site: the variables that key its records and the variables that
// the site binds.
type RunKey struct {
	Vars, Bind []string
}
