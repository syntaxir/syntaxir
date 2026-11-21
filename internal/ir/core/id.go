package core

import "zeroparse.dev/zp/pkg/grammar/ast"

// Number gives every node of the rules of m that has no ID one, in the order of the rules and of
// Walk, after LastID, and sets LastID. The builder of a module numbers it once it is built; a node
// reached twice keeps the ID it got first.
func Number(m *Module) {
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		Walk(r.Body, func(n Node) {
			if a := n.Attrs(); a.ID == 0 {
				a.ID = m.NewID()
			}
		})
	}
}

// NewID returns an ID that no node of m has, for a node that a pass adds and that takes the place
// of no other.
func (m *Module) NewID() NodeID {
	m.LastID++
	return m.LastID
}

// Origin maps the IDs of the nodes of a module (Attrs.ID) to the grammar expressions they were built
// from, as the build of the module recorded them (Module.Origin). It holds across the passes of the
// module, which keep the IDs of the nodes they keep and give a node that takes the place of another
// the ID of the node it replaces.
type Origin map[NodeID]ast.Expr

// Of returns the expression the node n was built from, or nil: for a node without an ID (one that a
// pass or a view built and that takes the place of none), or a module built without its origin.
func (o Origin) Of(n Node) ast.Expr {
	if n == nil || n.Attrs().ID == 0 {
		return nil
	}
	return o[n.Attrs().ID]
}
