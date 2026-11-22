package check

import (
	"sync"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Checker checks the types of the grammar of one semantic model, Model, and keeps what the checks
// compute for it. A model has one Checker (Model.Checker), made on first use, and every cache below
// lives as long as that Checker: for the model's lifetime.
type Checker struct {
	*Model
	// shapeControl is the compilation's cancellation control while the program is being built, so
	// that the shape analysis of rule bodies polls it; nil otherwise.
	shapeControl *cancel.Control
	// bottoms are the rules of type _|_ (bottomRules), computed once.
	bottomsOnce sync.Once
	bottoms     map[string]bool
	// nodeTypeCache holds the results of NodeTypeOf by type name: a NodeTy, or false.
	nodeTypeCache sync.Map
	// structHasZero caches whether the struct types have a zero value (HasZero) by name.
	structHasZero sync.Map
	// kinds are the run-time kinds of the types (Kinds), made once under kindsMu.
	kindsMu sync.Mutex
	kinds   *Kinds
	// castTypes caches the types of casts (CastType) by cast.
	castTypes sync.Map
	// structZeros caches the zero descriptors of the struct types (StructZeroOf) by name.
	structZeros sync.Map
	// unsupportedSeen holds the type syntax reported as not supported yet (MarkUnsupported), so
	// that an alias resolved from several places is reported once. The compilation that builds the
	// program is its only writer.
	unsupportedSeen map[ast.TypeExpr]bool
	// typeReads, if not nil, is called on each read of the type declarations (typeDecls); only the
	// tests set it (export_test.go), to count the work of a decision in reads.
	typeReads func()
}

// typeDecls returns the type declarations of the model by name.
func (p *Checker) typeDecls() map[string]ast.TypeDecl {
	if p.typeReads != nil {
		p.typeReads()
	}
	return p.Types
}

// NewChecker returns the checker of the model m.
func NewChecker(m *Model) *Checker { return &Checker{Model: m} }

// MarkUnsupported records that the unsupported type syntax t was reported and returns true the
// first time, so that a type reached from several places is reported once.
func (p *Checker) MarkUnsupported(t ast.TypeExpr) (first bool) {
	if p.unsupportedSeen[t] {
		return false
	}
	if p.unsupportedSeen == nil {
		p.unsupportedSeen = map[ast.TypeExpr]bool{}
	}
	p.unsupportedSeen[t] = true
	return true
}

// SetShapeControl sets the cancellation control that the shape analysis of rule bodies polls, nil
// outside a compilation.
func (p *Checker) SetShapeControl(control *cancel.Control) { p.shapeControl = control }

// ShapeControl returns the control set by SetShapeControl.
func (p *Checker) ShapeControl() *cancel.Control { return p.shapeControl }
