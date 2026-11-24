package frontend

import (
	"slices"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// structuralErrors returns the errors that depend on the facts of g: they read the structure of the
// grammar only, so they are found on an untyped module, before any type is checked. It returns nil
// if g cannot be built into such a module (the compiler then reports why); the errors are in the
// order of Facts.Errors, with the names of the hidden rules that inline actions generate shown as
// written.
func structuralErrors(g *ast.Grammar, opts check.Options, names desugar.InlineNames, facts FactSource, control *cancel.Control) diagnostic.ErrorList {
	canon, err := Canonicalize(g, opts, names, control)
	if err != nil {
		return nil
	}
	return structuralFrom(canon, control).errors(opts, names, facts)
}

// errors returns the structural errors of the module (structuralErrors), which facts computes from
// the facts that its analyses hold.
func (s *Structural) errors(opts check.Options, names desugar.InlineNames, facts FactSource) diagnostic.ErrorList {
	if s == nil || s.Module == nil {
		return nil
	}
	out := facts.Errors(s.Analyses, s.Module, s.Rules, !opts.LeftRecursionBindings)
	if len(out) == 0 {
		return nil
	}
	return names.Rewrite(out).(diagnostic.ErrorList)
}

// mergeErrors returns the compiler's error with the fact errors it does not report. The compiler's
// errors keep their order; each added error goes before the first error already in the list that
// comes after it in the source, or last. If the compiler reports all of them, it returns the
// compiler's error unchanged.
func mergeErrors(err error, factErrs diagnostic.ErrorList) error {
	list, ok := err.(diagnostic.ErrorList)
	if !ok || len(factErrs) == 0 {
		return err
	}
	merged := slices.Clone(list)
	for _, fe := range factErrs {
		if slices.ContainsFunc(list, func(e *diagnostic.Error) bool { return *e == *fe }) {
			continue
		}
		i := slices.IndexFunc(merged, func(e *diagnostic.Error) bool { return before(fe.Pos, e.Pos) })
		if i < 0 {
			i = len(merged)
		}
		merged = slices.Insert(merged, i, fe)
	}
	if len(merged) == len(list) {
		return err
	}
	return merged
}

// before reports whether a comes before b in the source.
func before(a, b ast.Pos) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col
}
