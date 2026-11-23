package action

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
)

// The sites of an Action module.
//
// The Action module of a table (code.ActionStage) holds one function for each action site of the
// table's Core IR, in the order of the sites (core.ActionSites): the action of a rule, the action of
// a line of an Operators Rule, a predicate and the value of a binding. Each function has the
// attributes Site. A function evaluates its action expression and returns its value and its failure,
// with the signature of the callable type Action, or, in place of a rule's action (Site.Result), it
// assigns the value to a local of its own and raises a failure (action.raise). The module also
// declares the compositions that the functions call, which have no attributes.

// SiteKind is the kind of an action site.
type SiteKind uint8

const (
	SiteRule SiteKind = iota // the action of a rule
	SiteLine                 // the action of a line of an Operators Rule
	SitePred                 // a predicate
	SiteBind                 // the value of a binding
)

var siteKindNames = [...]string{SiteRule: "rule", SiteLine: "line", SitePred: "predicate", SiteBind: "binding"}

func (k SiteKind) String() string {
	if int(k) < len(siteKindNames) {
		return siteKindNames[k]
	}
	return fmt.Sprintf("SiteKind(%d)", k)
}

// Site is the attributes of a function of an Action module: the site whose action expression it
// evaluates, and the facts that the lowering records for the Action passes, which the step
// action-finish drops.
type Site struct {
	// Site is the number of the site among the action sites of the table's Core IR.
	Site int
	Kind SiteKind
	// Rule is the ID of the rule of the site.
	Rule int
	// Result, if not empty, names the local of the function that its body assigns the value to last,
	// in place of a rule's action: the function has no results and a failure ends the parse by
	// action.raise. Otherwise the function returns the value and the failure.
	Result string
	// Membership, if not nil, says that the action expression compares the text of one capture or
	// local with string literals, text($x) == "a" || ..., as the lowering wrote it.
	Membership *Membership
	// Structs are the struct literals that the lowering made of their fields (action.struct), in the
	// order of their statements.
	Structs []StructLit
}

// Membership is the fact of an action expression text($x) == "a" || ...: the read of the capture or
// local and the distinct literals, in the order written.
type Membership struct {
	Read Read
	Lits []string
}

// StructLit is the fact of a struct literal that the lowering made of its fields: the local that
// holds the struct (action.struct), the fields that its type declares, in order, and, for each field
// as written, the local that the lowering let its value to, or "" if the value is a local or a
// constant that it used as it is. The calls of action.setField and cover take the fields in the
// order written.
type StructLit struct {
	Node     string
	Declared []string
	Lets     []string
}

// SiteOf returns the attributes of the function f of an Action module, or nil for a composition.
func SiteOf(f *code.Func) *Site {
	s, _ := f.Attrs.(*Site)
	return s
}

func init() { code.RegisterStageCheck[code.Action](verifySites) }

// verifySites checks the functions of an Action module beyond Verify: every function with attributes
// has those of Site, with the signature that its form needs, and no site has two functions of one
// form.
func verifySites(m *code.Module) error {
	type form struct {
		site    int
		inPlace bool
	}
	seen := map[form]string{}
	act := m.HandleType(Action)
	for _, f := range m.Funcs {
		if f.Attrs == nil {
			continue
		}
		s := SiteOf(f)
		if s == nil {
			return fmt.Errorf("function %s: attributes of type %T, not of an action site", f.Name, f.Attrs)
		}
		if len(f.Params) != 1 || f.Params[0].Type != code.THandle(Act) {
			return fmt.Errorf("function %s: the function of a site takes the action context alone", f.Name)
		}
		if s.Result == "" {
			if act == nil || act.Callable == nil || !slices.Equal(f.Results, act.Callable.Results) {
				return fmt.Errorf("function %s: the function of a site returns the results of %s", f.Name, Action)
			}
		} else {
			if s.Kind != SiteRule {
				return fmt.Errorf("function %s: a %s site evaluated in place", f.Name, s.Kind)
			}
			if len(f.Results) != 0 || !slices.Contains(f.Locals, code.P(s.Result, code.THandle(Datum))) {
				return fmt.Errorf("function %s: a function in place has no results and declares its local %s", f.Name, s.Result)
			}
		}
		k := form{s.Site, s.Result != ""}
		if prev, ok := seen[k]; ok {
			return fmt.Errorf("functions %s and %s: two functions of site %d of one form", prev, f.Name, s.Site)
		}
		seen[k] = f.Name
	}
	return nil
}
