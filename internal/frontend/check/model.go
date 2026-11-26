package check

import (
	"sync"

	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Model is the semantic model of a checked grammar: the grammar the checker read, its type
// declarations, its variables and its start rules, and the rules with their static facts, its
// expectation table and the kinds of its types. A program loaded from a compiled grammar has a
// model without a grammar.
type Model struct {
	Grammar *ast.Grammar
	// Normalized is the grammar as written with its attributes normalized (frontend.Canonical) when
	// Grammar is its lowering (inline actions replaced by hidden rules, or its default-value form);
	// nil when Grammar is that grammar. Neither is the grammar as written: they hold forms that only
	// the compiler writes, which grammar validation rejects.
	Normalized *ast.Grammar
	// Types holds the type declarations by name.
	Types map[string]ast.TypeDecl
	// Vars holds the declared variables (nil if there are none).
	Vars *VarTable
	// StartRules holds the start rules in declaration order and DefaultStartRule the default one
	// (desugar.ResolveStarts).
	StartRules       []string
	DefaultStartRule string

	// The rest is the rule table with the static facts of the rules, which the compilation of the
	// grammar fills in.
	// HasMissing reports that a rule uses #missing, so that a parse with missing-token insertion
	// enabled can insert: the parses run the insertion program then (InsertionFlags).
	HasMissing bool
	// MissingType reports that the type missing is among the types of the grammar's values: a rule
	// uses #missing, or a type declaration or a rule's declared type names missing. The typed values
	// of such a grammar tell a Missing from an Error; only the generators of typed values read it.
	MissingType   bool
	RuleList      []*Rule
	DescTable     []string  // expectation table (starting with fixedDescs)
	Twins         []*Rule   // value-free twins (Rule.Twin); ids start at len(RuleList)
	NSeen         int       // number of rules with Rule.Seen set
	Typed         *TypeInfo // types found by the type checker (CheckTypes; nil if it did not run)
	NoProjections bool
	// RecordsSourceInfo reports that nodes record source information (Options.SourceInfo).
	RecordsSourceInfo bool
	// MakesDefaultValue reports that value actions and declared rule types were dropped
	// (Options.DefaultValue).
	MakesDefaultValue bool
	// InlineNames are the hidden rules that the inline actions of the grammar as written were
	// lowered to, with the inline actions they stand for; empty if it has none.
	InlineNames desugar.InlineNames
	ByName      map[string]*Rule
	// Opts are the options the program was compiled with. The engine builds further programs from the
	// same grammar later (the controlled closure graph and the recognizer) and derives their options
	// from these, so that none is lost (engine.Program.lazyOptions).
	Opts Options

	// TypeKinds holds the node kinds of the grammar's types (the TypeKind of the compiled program), made from
	// the checker's (Checker.Kinds).
	TypeKinds map[string]*valueimpl.Kind
	// NodeKinds holds the kinds of the grammar's named node types, also under the names of the
	// aliases of them.
	NodeKinds map[string]*nodes.NodeTypeKinds
	// InsertionFlags are the rule flags of the parses with missing-token insertion enabled, in
	// which left recursion through an insertion forms cycles of its own (RuleFact.CycleIns), when
	// they differ from the flags of the rules; nil otherwise. Such parses run a program built with
	// them.
	InsertionFlags []RuleFlags

	// chk checks the types of the model (Checker), made on first use under checkerOnce.
	chk         *Checker
	checkerOnce sync.Once
}

// NewModel returns a model of the grammar g, which is the lowering of normalized (nil if g is
// not a lowering, Model.Normalized), with no type declarations or rules yet.
func NewModel(g, normalized *ast.Grammar) *Model {
	return &Model{Grammar: g, Normalized: normalized, Types: map[string]ast.TypeDecl{}, ByName: map[string]*Rule{}}
}

// ResolveStart returns the rule name selects: name, or the default start rule when name is
// empty.
func (m *Model) ResolveStart(name string) string {
	if name == "" {
		return m.DefaultStartRule
	}
	return name
}
