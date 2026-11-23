package core

import "fmt"

// ActionSiteKind is the place of an action expression in a module.
type ActionSiteKind uint8

const (
	// SiteRule is the action of a rule (FinishAction).
	SiteRule ActionSiteKind = iota
	// SiteLine is the action of a line of an Operators Rule.
	SiteLine
	// SitePred is the action expression of a predicate.
	SitePred
	// SiteBind is the action expression of a binding.
	SiteBind
)

// ActionSite is an action expression of a module at its place.
type ActionSite struct {
	Kind ActionSiteKind
	// Rule is the ID of the rule that holds the site.
	Rule int
	// Node is the *Pred of SitePred or the *BindNode of SiteBind, and Line the line of SiteLine.
	Node Node
	Line *OpLine
	// Action is the action expression.
	Action ActionExpr
}

// ActionSites are the action sites of a module, numbered in order: those of each rule by ID, which
// are the predicates and the bindings of its body in walk order (Walk), the actions of the lines of an
// Operators Rule in line order (OperatorsRule.Lines), then the rule's action. A node or a line that
// appears in more than one place of the module is one site, numbered at its first place. The number
// of a site depends on the module alone, so every stage below Core that names the action expressions
// of a module by number agrees on it.
type ActionSites struct {
	Sites []ActionSite
	// at holds the number of each site by its *Pred, *BindNode or *OpLine; rule holds the number of
	// the action of each rule by ID, -1 for a rule without one.
	at   map[any]int
	rule []int
}

// ActionSitesOf returns the action sites of the module m.
func ActionSitesOf(m *Module) *ActionSites {
	s := &ActionSites{at: map[any]int{}, rule: make([]int, len(m.Rules))}
	add := func(key any, site ActionSite) {
		if _, ok := s.at[key]; ok {
			return
		}
		s.at[key] = len(s.Sites)
		s.Sites = append(s.Sites, site)
	}
	for i, r := range m.Rules {
		Walk(r.Body, func(n Node) {
			switch x := n.(type) {
			case *Pred:
				add(x, ActionSite{Kind: SitePred, Rule: r.ID, Node: x, Action: x.Action})
			case *BindNode:
				add(x, ActionSite{Kind: SiteBind, Rule: r.ID, Node: x, Action: x.Action})
			}
		})
		if o, ok := r.Body.(*OperatorsRule); ok {
			for _, l := range o.Lines() {
				if l.Action != nil {
					add(l, ActionSite{Kind: SiteLine, Rule: r.ID, Line: l, Action: l.Action})
				}
			}
		}
		s.rule[i] = -1
		if r.Finish.Kind == FinishAction {
			s.rule[i] = len(s.Sites)
			s.Sites = append(s.Sites, ActionSite{Kind: SiteRule, Rule: r.ID, Action: r.Finish.Action})
		}
	}
	return s
}

// Of returns the number of the site of the predicate or the binding n, or of the action of the line
// l. A node or a line that is not a site of the module is an internal error.
func (s *ActionSites) Of(n any) int {
	switch n.(type) {
	case *Pred, *BindNode, *OpLine:
		if i, ok := s.at[n]; ok {
			return i
		}
	}
	panic(fmt.Sprintf("internal error: %T is not an action site of the module", n))
}

// OfRule returns the number of the site of the action of the rule whose ID is rule. A rule without
// an action is an internal error.
func (s *ActionSites) OfRule(rule int) int {
	if i := s.rule[rule]; i >= 0 {
		return i
	}
	panic(fmt.Sprintf("internal error: rule %d has no action", rule))
}
