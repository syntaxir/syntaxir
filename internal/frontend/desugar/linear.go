package desugar

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// LinearAttribute is the name of the declaration attribute that declares linear cost: on the
// package clause for every start rule of the grammar, on a rule for that rule.
const LinearAttribute = "linear"

// IsLinear reports whether a, a declaration attribute, is @linear. A declaration holds @
// attributes only: grammar validation rejects a pragma there. The attribute only adds a check of
// the grammar, so the analyses that treat a rule with declaration attributes apart do not count
// it.
func IsLinear(a *ast.Attribute) bool { return a.Name == LinearAttribute }

// DeclaredBeyondScan reports whether the declaration of rd keeps it from being a pure scan rule
// (corepass.SiteRule.Declared): it has a type, an action, or an attribute other than
// @start and @linear, which only add checks.
func DeclaredBeyondScan(rd *ast.RuleDef) bool {
	if rd.Type != nil || rd.Action != nil {
		return true
	}
	for _, a := range rd.Attrs {
		if a.Name != "start" && !IsLinear(a) {
			return true
		}
	}
	return false
}

// LinearDeclarations reads the @linear declaration attributes of g. It returns whether the
// package clause declares the whole grammar linear and the rules that declare themselves linear,
// in declaration order. @linear takes no arguments and appears at most once on a declaration;
// the package clause takes no other declaration attribute. Each violation is an error. The other
// declaration attributes of rules are checked by ResolveStarts.
func LinearDeclarations(g *ast.Grammar, control *cancel.Control) (whole bool, rules []string, errs diagnostic.ErrorList) {
	errorf := func(pos ast.Pos, format string, args ...any) {
		errs = append(errs, &diagnostic.Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
	}
	declared := func(attrs []*ast.Attribute, what string, others bool) bool {
		found := false
		for _, a := range attrs {
			control.Poll()
			if !IsLinear(a) {
				if !others {
					errorf(a.Diagnostic(), "unknown declaration attribute @%s of the package clause", a.Name)
				}
				continue
			}
			if found {
				errorf(a.Diagnostic(), "duplicate @linear on %s", what)
				continue
			}
			found = true
			if len(a.Args) != 0 || len(a.KwArgs) != 0 {
				errorf(a.Diagnostic(), "@linear takes no arguments")
			}
		}
		return found
	}
	whole = declared(g.File.Pkg.Attrs, "the package clause", false)
	for _, rd := range control.Rules(g) {
		if declared(rd.Attrs, "rule "+rd.Name, true) && !slices.Contains(rules, rd.Name) {
			rules = append(rules, rd.Name)
		}
	}
	return whole, rules, errs
}
