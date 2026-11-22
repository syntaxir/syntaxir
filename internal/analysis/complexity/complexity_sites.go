package complexity

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/core"
)

// The classifier's model of the sites that the compiler numbers on Core IR
// (internal/middleend/corepass, ComputeSites), on the nodes of the rules of a grammar or a view of
// it.

// restartableRepeats returns the repetitions that an iteration of a repetition can reach: those in
// the element of another repetition that iterates (its maximum is not 0 or 1), and those in a rule
// that such an element calls, or that calls itself, directly or indirectly. Every other repetition is reached by a
// derivation once, from the start rule through sequences, choices and calls, so one walk of it
// starts at each position at most once per environment, and a record could not be reused.
func (k *classifier) restartableRepeats() map[*core.Repeat]bool {
	out := map[*core.Repeat]bool{}
	iterated := map[string]bool{}
	var queue []string
	mark := func(n string) {
		if !iterated[n] {
			iterated[n] = true
			queue = append(queue, n)
		}
	}
	for _, r := range k.rules {
		walkNodes(k.ruleBody(r.Name), func(x core.Node) {
			rep, ok := x.(*core.Repeat)
			if !ok || rep.Max == 0 || rep.Max == 1 {
				return
			}
			walkNodes(rep.Body, func(y core.Node) {
				if inner, ok := y.(*core.Repeat); ok {
					out[inner] = true
				} else if c, ok := k.calledRule(y); ok {
					mark(c)
				}
			})
		})
	}
	// A rule that calls itself, directly or through others, repeats as an iteration does: a right
	// recursion walks the repetitions in its rules from each position its alternatives advance to.
	for _, r := range k.rules {
		n := r.Name
		seen := map[string]bool{}
		stack := append([]string(nil), k.calls[n]...)
		for len(stack) > 0 && !iterated[n] {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if c == n {
				mark(n)
			} else if !seen[c] {
				seen[c] = true
				stack = append(stack, k.calls[c]...)
			}
		}
	}
	for len(queue) > 0 {
		n := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, c := range k.calls[n] {
			mark(c)
		}
	}
	for _, r := range k.rules {
		if !iterated[r.Name] {
			continue
		}
		walkNodes(k.ruleBody(r.Name), func(x core.Node) {
			if rep, ok := x.(*core.Repeat); ok {
				out[rep] = true
			}
		})
	}
	return out
}

// isScan reports whether e repeats a single character, which a scan walks without a record.
func isScan(e *core.Repeat) bool {
	switch e.Body.(type) {
	case *core.ClassNode, *core.Any:
		return true
	}
	return false
}

// blocksRun reports whether the node n of an attribute keeps a repetition that holds it from
// being a run site: #stream, #missing, #error and #diagnostic.
func blocksRun(n core.Node) bool {
	switch n.(type) {
	case *core.Stream, *core.Missing, *core.Label:
		return true
	}
	return false
}

// scanSiteRepeats calls f for each repetition without an upper bound in n, the body of a rule that
// calls no rule, in order, with whether it is a scan site: it repeats a character class or any
// character and does not deliver the elements of a #stream. A repetition in a #recover skip is
// reported as a site; it is one unless it is a skip-run site, which records its walks as one.
func scanSiteRepeats(n core.Node, control *cancel.Control, f func(rep *core.Repeat, site bool)) {
	streamed := map[*core.Repeat]bool{}
	walkNodes(n, func(x core.Node) {
		control.Poll()
		switch x := x.(type) {
		case *core.Stream:
			streamed[x.Repeat] = true
		case *core.Repeat:
			if x.Max < 0 {
				f(x, isScan(x) && !streamed[x])
			}
		}
	})
}
