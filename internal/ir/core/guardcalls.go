package core

// GuardCalls returns the rule calls that the evaluation of n makes before it fails at its first
// terminal, outermost first, when the next character cannot begin n: the calls on the path that
// First follows (Analyze), which ends in a non-empty literal or class. Every such evaluation makes
// these calls, each at the position and in this order, and none other: the path passes no choice,
// repetition, predicate, action or binding, so no character decides it. ok is false when n has no
// First, or when its path enters an inlined call (a Nest), whose scope the calls do not describe.
//
// A target that skips an alternative on its guard (Alt.Guard) and must leave the state that the
// evaluation leaves, as logical compute units must (design record 076), makes the effects of these
// calls on the skip.
func GuardCalls(m *Module, n Node) (calls []int, ok bool) {
	for depth := 0; depth <= len(m.Rules); {
		switch x := n.(type) {
		case *Lit:
			return calls, x.Text != ""
		case *ClassNode:
			return calls, true
		case *ClassRun:
			return calls, len(x.Classes) > 0
		case *Seq:
			if len(x.Items) == 0 {
				return nil, false
			}
			n = x.Items[0]
		case *Capture:
			n = x.Body
		case *Text:
			n = x.Body
		case *Drop:
			n = x.Body
		case *Terminal:
			n = x.Body
		case *Call:
			r := m.Rules[x.Rule]
			if x.Level != 0 || r.Policy.Leader {
				return nil, false
			}
			if _, ops := r.Body.(*OperatorsRule); ops {
				return nil, false
			}
			calls = append(calls, x.Rule)
			n = r.Body
			depth++
		default:
			return nil, false
		}
	}
	// A path that calls more rules than the module has goes round a cycle, which has no First.
	return nil, false
}
