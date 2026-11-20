package pipe

import "strings"

// skeleton is the required shape of a tree: its required steps, the blocks that hold them, in
// order, and its constant features. A slot body for a part must have the skeleton of the default.
func skeleton(n node) string {
	in := func(s string, body node) string {
		if b := skeleton(body); b != "" {
			return s + "{" + b + "}"
		}
		return ""
	}
	switch x := n.(type) {
	case *passNode:
		if x.p.Required {
			return x.p.Name + ";"
		}
		return ""
	case *slotNode:
		return in("slot "+x.slot.name, x.def)
	case *perFuncNode:
		return in("func "+x.view.name, x.body)
	case *eachNode:
		return in("each "+x.split.name, x.body)
	case *withinNode:
		return in("within "+x.lens.name, x.body)
	case *sharedNode:
		return in("shared "+x.key.name, x.body)
	case *withNode:
		if x.prov.constant {
			return "with " + string(x.prov.feature) + "=" + x.prov.text + "{" + skeleton(x.body) + "}"
		}
		return in("with "+x.prov.name, x.body)
	case *forkNode:
		var b strings.Builder
		for i, br := range x.branches {
			b.WriteString("branch " + x.names[i] + "{" + skeleton(br) + "}")
		}
		return "fork{" + b.String() + "}"
	}
	var b strings.Builder
	children(n, func(c node) { b.WriteString(skeleton(c)) })
	return b.String()
}
