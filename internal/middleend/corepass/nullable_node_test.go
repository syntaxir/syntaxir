package corepass_test

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
)

// checkUnnumberedNullable reports a node of m whose nullabilities, asked of f for a copy of the node
// that Analyze did not see (one whose ID is 0, as a rewrite builds), differ from the attributes
// that Analyze gave the node: the queries compute such a node by core.NullableBy from its parts,
// which must agree with Analyze applying the same rule to the module.
func checkUnnumberedNullable(m *core.Module, f *corepass.Facts) error {
	c := m.Clone()
	for i, r := range m.Rules {
		if r == nil {
			continue
		}
		var orig, copies []core.Node
		core.Walk(r.Body, func(n core.Node) { orig = append(orig, n) })
		core.Walk(c.Rules[i].Body, func(n core.Node) { copies = append(copies, n) })
		for _, n := range copies {
			n.Attrs().ID = 0
		}
		for j, n := range orig {
			at := n.Attrs()
			if null, ins := f.Nullable(copies[j]), f.NullableIns(copies[j]); null != at.Nullable || ins != (at.Nullable || at.NullableIns) {
				return fmt.Errorf("rule %s: node %d (%T): unnumbered nullable=%v nullable-ins=%v, attributes %v %v",
					r.Name, at.ID, n, null, ins, at.Nullable, at.NullableIns)
			}
		}
	}
	return nil
}
