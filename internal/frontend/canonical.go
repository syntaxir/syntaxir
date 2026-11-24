package frontend

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Canonical is the grammar that a program is compiled from: the grammar after its inline actions
// are lowered, its default-value form (if the options ask for it) and its attributes are normalized.
type Canonical struct {
	// Grammar is the grammar the program runs.
	Grammar *ast.Grammar
	// Normalized is the grammar as written with its attributes normalized, without the hidden
	// rules that inline actions are lowered to; nil if Grammar is that grammar.
	Normalized *ast.Grammar
	// Starts are the start rules in order, and DefaultStart is the one used when none is named.
	Starts       []string
	DefaultStart string
	// StartErrors are the errors of the start declarations; compilation reports them.
	StartErrors diagnostic.ErrorList
}

// Canonicalize validates the grammar and returns the canonical form that Check checks.
// names receives the hidden rules that the lowering of inline actions generates.
func Canonicalize(g *ast.Grammar, opts check.Options, names desugar.InlineNames, control *cancel.Control) (*Canonical, error) {
	control.Check()
	if !opts.DefaultValue {
		if err := check.ValidateGrammar(g, control); err != nil {
			return nil, err
		}
	}
	// The inline actions are checked as written, then lowered to hidden rules. A program keeps
	// the grammar as written, whatever its options, normalized (Canonical.Normalized).
	source := g
	if desugar.HasInlineAction(g, control) {
		if errs := desugar.UndeclaredRules(g, control); errs != nil {
			return nil, errs
		}
		lowered, errs := desugar.LowerInlineActionsControlled(g, names, control)
		if errs != nil {
			return nil, errs
		}
		g = lowered
		control.Check()
	}
	if opts.DefaultValue {
		lowered := g
		g = desugar.DefaultValueGrammar(g, check.ValueNeededRules(control.Rules(g), control, false), control)
		control.Check()
		// A defect is reported where the grammar as written has it.
		valid := g
		if lowered != source {
			valid = desugar.SourceDefaultValueGrammar(source, lowered, g, names, control)
		}
		if err := check.ValidateGrammar(valid, control); err != nil {
			return nil, err
		}
	}
	keepSource := source != g
	var unsupported diagnostic.ErrorList
	for _, comment := range g.File.HeaderComments {
		control.Poll()
		unsupported = append(unsupported, &diagnostic.Error{Pos: comment.Pos, Msg: "comments are not allowed inside statement headers"})
	}
	checkBlocks := func(actionExpr ast.ActionExpr) {
		astinspect.WalkAction(actionExpr, func(t ast.ActionExpr) {
			control.Poll()
			if block, ok := t.(*ast.Block); ok {
				unsupported = append(unsupported, &diagnostic.Error{Pos: block.Diagnostic(), Msg: "action statement blocks are not implemented"})
			}
		})
	}
	for _, rd := range control.Rules(g) {
		control.Poll()
		checkBlocks(rd.Action)
		astinspect.WalkExprControlled(rd.Expr, func(e ast.Expr) {
			control.Poll()
			switch e := e.(type) {
			case *ast.Predicate:
				if block, ok := e.Action.(*ast.Block); ok && len(block.Statements) > 0 {
					for _, statement := range block.Statements {
						control.Poll()
						checkBlocks(statement)
					}
				} else {
					checkBlocks(e.Action)
				}
			case *ast.InlineAction:
				checkBlocks(e.Action)
			case *ast.OperatorsRule:
				for _, operand := range e.Operands {
					control.Poll()
					checkBlocks(operand.Action)
				}
				for _, level := range e.Levels {
					control.Poll()
					for _, operator := range level.Operators {
						control.Poll()
						checkBlocks(operator.Action)
					}
				}
			}
		}, control)
	}
	starts, defaultStart, startErrors := desugar.ResolveStarts(control.Rules(g), control)
	_, _, linearErrors := desugar.LinearDeclarations(g, control)
	startErrors = append(startErrors, linearErrors...)
	control.Check()
	if len(unsupported) != 0 {
		return nil, unsupported
	}
	var normalizationError error
	g, normalizationError = desugar.NormalizeAttributesControlled(g, opts.EagerAttributeCopies, control)
	control.Check()
	if normalizationError != nil {
		return nil, normalizationError
	}
	if !keepSource {
		source = nil // the grammar as written is the one the program runs
	} else {
		// The grammar as written is kept beside the one the program runs.
		if source, normalizationError = desugar.NormalizeAttributesControlled(source, opts.EagerAttributeCopies, control); normalizationError != nil {
			return nil, normalizationError
		}
	}
	return &Canonical{Grammar: g, Normalized: source, Starts: starts, DefaultStart: defaultStart, StartErrors: startErrors}, nil
}
