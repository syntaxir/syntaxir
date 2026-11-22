package complexity

import (
	"math/bits"
	"slices"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Region rules (see "Conditions for Linear Cost" in spec/runtime/parsing.md and Lemma 15 of
// spec/proofs/linear-cost.md).
//
// A trivia rule such as `ws = [ ]* (c [ ]*)*` with `c = "/*" [^*]* "*/"` has an alphabet of every
// character, so the first bound of a bounded scan finds no bound on the run before it. Its
// evaluations are still confined: when nothing outside the regions consumes the first character of
// a region except as a guarded token, no evaluation outside the regions starts strictly inside the
// match of a region, so the evaluations of the repetition that reach a position start in at most
// two positions, where the run bound gives d + 2. regionScans finds the region rules and returns
// the repetitions of the trivia rules among them, which take the region bound in place of the
// first bound.
//
// The region rules are chosen, then checked: a candidate that fails a condition is dropped and the
// check runs again, since dropping a region makes its terminals outer ones. The conditions are
// (D) the rule and the rules it reaches read and bind no variable and have no predicate, action,
// level call, left-recursion cycle, attributed expression, recovery, stream or Operators Rule, so
// that whether an expression of a region matches at a position, and where it ends, depend on the
// position alone; (N) the body of a region reaches no region, and no rule that it reaches is
// reached from the start rule otherwise than through a region; (O) the first characters of
// distinct regions are disjoint; and (G) every crossing, a terminal outside the regions that can
// consume a first character of a region, is guarded.

// regionScans returns the repetitions that are the bodies of trivia rules with the region bound.
// It runs after the run analysis, whose open runs pick the candidates.
func (k *classifier) regionScans() map[*core.Repeat]bool {
	regions := map[string]bool{} // the region rules; true for a trivia rule
	for _, r := range k.rules {
		k.control.Poll()
		body := k.ruleBody(r.Name)
		if !k.reach[r.Name] || r.Name == k.start || k.triviaParts(body) == nil || !k.regionFree(r.Name) {
			continue
		}
		sigma := triviaUnwrap(body).(*core.Repeat)
		if d, ok := k.scanRun[sigma]; ok && d >= runOpen && k.srcExprs[k.exprOf(sigma)] {
			regions[r.Name] = true
		}
	}
	tried := map[string]bool{} // the rules that were made auxiliary regions once
	for hasTrivia(regions) {
		k.control.Poll()
		drop, add := k.regionCheck(regions, tried)
		if len(drop) == 0 && len(add) == 0 {
			break
		}
		for _, n := range drop {
			delete(regions, n)
		}
		for _, n := range add {
			regions[n] = false
			tried[n] = true
		}
	}
	out := map[*core.Repeat]bool{}
	for n, trivia := range regions {
		if trivia {
			out[triviaUnwrap(k.ruleBody(n)).(*core.Repeat)] = true
		}
	}
	return out
}

func hasTrivia(regions map[string]bool) bool {
	for _, t := range regions {
		if t {
			return true
		}
	}
	return false
}

// regionFree reports whether a rule satisfies (D): it and the rules it reaches read and bind no
// variable and have no predicate, action, call of a level, rule of a left-recursion cycle,
// attributed expression, recovery, stream or Operators Rule. Then nothing but the input and the
// position decides whether an expression in them matches and where it ends: no variable, capture
// or level enters, no growth is open or starts inside, and a cut acts within the evaluation of its
// rule.
func (k *classifier) regionFree(name string) bool {
	for n := range k.reachable(name) {
		d := k.byName[n]
		if d.Action != nil || k.an.Cyclic[n] || len(k.vars[n]) > 0 {
			return false
		}
		free := true
		walkNodes(k.ruleBody(n), func(x core.Node) {
			// The lowering of inline actions has made them rules with actions, so no inline action
			// is left here.
			switch x := x.(type) {
			case *core.Pred, *core.BindNode, *core.Label, *core.Recover, *core.Missing, *core.Stream,
				*core.OperatorsRule, *core.Nest:
				free = false
			case *core.Call:
				if x.Level != 0 {
					free = false
				}
			}
		})
		if !free {
			return false
		}
	}
	return true
}

// regionCheck checks the conditions (N), (O) and (G) on the regions. It returns the regions that
// fail one, and the rules that hold an unguarded crossing of a trivia region and can be auxiliary
// regions instead, such as a string whose body holds blanks. A rule is made an auxiliary region
// once at most, so the check ends.
func (k *classifier) regionCheck(regions map[string]bool, tried map[string]bool) (drop, add []string) {
	names := sortedKeys(regions)
	// (N): the rules that the body of each region reaches, and the rules that the start rule
	// reaches without entering a region.
	inner := map[string]map[string]bool{}
	for _, n := range names {
		in := map[string]bool{}
		for _, c := range k.calls[n] {
			for m := range k.reachable(c) {
				in[m] = true
			}
		}
		inner[n] = in
	}
	outer := map[string]bool{}
	var visit func(n string)
	visit = func(n string) {
		if outer[n] || k.byName[n] == nil {
			return
		}
		outer[n] = true
		if _, ok := regions[n]; ok {
			return
		}
		for _, c := range k.calls[n] {
			visit(c)
		}
	}
	visit(k.start)
	for _, n := range names {
		for m := range inner[n] {
			if _, ok := regions[m]; ok || outer[m] {
				drop = append(drop, n)
				break
			}
		}
	}
	if len(drop) > 0 {
		return drop, nil
	}
	// (O): the first characters of distinct regions are disjoint. Of two trivia rules that overlap
	// both are dropped; an auxiliary region gives way to a trivia rule.
	nfa := k.regionNFA(names, inner)
	for i, a := range names {
		for _, b := range names[i+1:] {
			if !nfa.opener[a].intersects(nfa.opener[b]) {
				continue
			}
			for _, n := range []string{a, b} {
				if regions[n] == (regions[a] && regions[b]) {
					drop = append(drop, n)
				}
			}
		}
	}
	if len(drop) > 0 {
		return slices.Compact(sortedStrings(drop)), nil
	}
	// (G): every crossing is guarded.
	g := k.regionGuards(regions, names, outer, nfa)
	failed := map[string]bool{}
	adds := map[string]bool{}
	for _, v := range g.violations {
		if !regions[v.region] {
			failed[v.region] = true
			continue
		}
		if m := v.rule; !tried[m] && m != k.start && k.regionFree(m) {
			if _, isRegion := regions[m]; !isRegion {
				adds[m] = true
				continue
			}
		}
		failed[v.region] = true
	}
	if len(failed) > 0 {
		return sortedKeys(failed), nil
	}
	return nil, sortedKeys(adds)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

func sortedStrings(s []string) []string {
	slices.Sort(s)
	return s
}

// bitset is a set of small integers.
type bitset []uint64

func newBitset(n int) bitset { return make(bitset, (n+63)/64) }

func (b bitset) set(i int)      { b[i/64] |= 1 << (i % 64) }
func (b bitset) has(i int) bool { return b[i/64]&(1<<(i%64)) != 0 }

// or adds c to b and reports whether b changed.
func (b bitset) or(c bitset) bool {
	changed := false
	for i := range b {
		if v := b[i] | c[i]; v != b[i] {
			b[i] = v
			changed = true
		}
	}
	return changed
}

func (b bitset) and(c bitset) bitset {
	out := slices.Clone(b)
	for i := range out {
		out[i] &= c[i]
	}
	return out
}

func (b bitset) empty() bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func (b bitset) each(f func(int)) {
	for i, v := range b {
		for v != 0 {
			j := bits.TrailingZeros64(v)
			f(64*i + j)
			v &^= 1 << j
		}
	}
}

// prefixNFA over-approximates the matches of the region bodies as a Glushkov automaton: a position
// is one character of a terminal outside lookaheads, and an edge joins two positions that can
// consume consecutive characters of a match. Lookaheads and the other expressions that consume
// nothing are read as matching the empty string, alternatives as a union and repetitions as any
// number of iterations, so every match of a region body spells a path from a first position, and
// the characters that can follow a prefix of a match are found by following its characters. A
// rule that two places call joins what follows both, which only adds paths.
type prefixNFA struct {
	k      *classifier
	chars  []charset         // the characters of each position
	lits   map[core.Node]int // the first position of each terminal
	rules  map[string]nfaInfo
	next   []bitset
	start  map[string]bitset  // the first positions of the body of each region
	opener map[string]charset // the first characters of the body of each region
	size   int
}

type nfaInfo struct {
	first, last bitset
	null        bool
}

// regionNFA builds the automaton of the bodies of the regions names and the rules they reach.
func (k *classifier) regionNFA(names []string, inner map[string]map[string]bool) *prefixNFA {
	n := &prefixNFA{k: k, lits: map[core.Node]int{}, rules: map[string]nfaInfo{}, start: map[string]bitset{}, opener: map[string]charset{}}
	var rules []string
	seen := map[string]bool{}
	for _, r := range names {
		for _, m := range append([]string{r}, sortedKeys(inner[r])...) {
			if !seen[m] {
				seen[m] = true
				rules = append(rules, m)
			}
		}
	}
	// Position 0 stands for any expression that the conditions keep out of a region: it consumes
	// any characters in any order.
	n.chars = append(n.chars, allChars)
	for _, r := range rules {
		n.number(k.ruleBody(r))
	}
	n.size = len(n.chars)
	for _, r := range rules {
		n.rules[r] = nfaInfo{newBitset(n.size), newBitset(n.size), false}
	}
	for changed := true; changed; {
		changed = false
		for _, r := range rules {
			k.control.Poll()
			info := n.info(k.ruleBody(r), false)
			old := n.rules[r]
			c := old.first.or(info.first)
			c = old.last.or(info.last) || c
			if info.null && !old.null {
				old.null, c = true, true
			}
			n.rules[r] = old
			changed = changed || c
		}
	}
	n.next = make([]bitset, n.size)
	for i := range n.next {
		n.next[i] = newBitset(n.size)
	}
	n.next[0].set(0)
	for _, r := range rules {
		n.info(k.ruleBody(r), true)
	}
	for _, r := range names {
		s := n.rules[r].first
		n.start[r] = s
		var o charset
		s.each(func(p int) { o = o.union(n.chars[p]) })
		n.opener[r] = o
	}
	return n
}

// number gives a position to each character of each terminal of n outside lookaheads.
func (n *prefixNFA) number(x core.Node) {
	switch x := x.(type) {
	case *core.Lit:
		n.lits[x] = len(n.chars)
		for _, c := range x.Text {
			n.chars = append(n.chars, litChar(x, c))
		}
	case *core.ClassNode:
		n.lits[x] = len(n.chars)
		n.chars = append(n.chars, coreClassChars(x.Class))
	case *core.Any:
		n.lits[x] = len(n.chars)
		n.chars = append(n.chars, allChars)
	case *core.Look, *core.Pred, *core.BindNode:
	default:
		eachChild(x, n.number)
	}
}

// eachChild calls f on the nodes directly inside n that it evaluates as parts of its match.
func eachChild(n core.Node, f func(core.Node)) {
	switch n := n.(type) {
	case *core.Seq:
		for _, it := range n.Items {
			f(it)
		}
	case *core.Choice:
		for _, alt := range n.Alts {
			f(alt.Body)
		}
	case *core.Repeat:
		f(n.Body)
	case *core.Opt:
		f(n.Body)
	case *core.Capture:
		f(n.Body)
	case *core.Drop:
		f(n.Body)
	case *core.Text:
		f(n.Body)
	}
}

// litChar is the characters that the character c of the literal l matches.
func litChar(l *core.Lit, c rune) charset {
	if l.Fold {
		return asciiFold(c)
	}
	return charOf(c)
}

// info returns the first and last positions of x and whether it can match the empty string; with
// edges, it also adds the edges inside x.
func (n *prefixNFA) info(x core.Node, edges bool) nfaInfo {
	k := n.k
	k.control.Poll()
	empty := nfaInfo{newBitset(n.size), newBitset(n.size), true}
	one := func(p int) nfaInfo {
		i := nfaInfo{newBitset(n.size), newBitset(n.size), false}
		i.first.set(p)
		i.last.set(p)
		return i
	}
	if rule, ok := k.calledRule(x); ok {
		if r, ok := n.rules[rule]; ok {
			return r
		}
		return n.any()
	}
	switch x := x.(type) {
	case *core.Lit:
		if x.Text == "" {
			return empty
		}
		p := n.lits[x]
		m := utf8.RuneCountInString(x.Text)
		if edges {
			for i := p; i+1 < p+m; i++ {
				n.next[i].set(i + 1)
			}
		}
		i := one(p)
		i.last = newBitset(n.size)
		i.last.set(p + m - 1)
		return i
	case *core.ClassNode, *core.Any:
		return one(n.lits[x])
	case *core.Seq:
		out := empty
		out.first, out.last = newBitset(n.size), newBitset(n.size)
		last := newBitset(n.size)
		for _, it := range x.Items {
			i := n.info(it, edges)
			if edges {
				last.each(func(p int) { n.next[p].or(i.first) })
			}
			if out.null {
				out.first.or(i.first)
			}
			if i.null {
				last.or(i.last)
			} else {
				last = slices.Clone(i.last)
				out.null = false
			}
		}
		out.last = last
		return out
	case *core.Choice:
		out := nfaInfo{newBitset(n.size), newBitset(n.size), false}
		for _, alt := range x.Alts {
			i := n.info(alt.Body, edges)
			out.first.or(i.first)
			out.last.or(i.last)
			out.null = out.null || i.null
		}
		return out
	case *core.Repeat:
		if x.Max == 0 {
			return empty
		}
		i := n.info(x.Body, edges)
		if edges && x.Max != 1 {
			i.last.each(func(p int) { n.next[p].or(i.first) })
		}
		return nfaInfo{i.first, i.last, x.Min == 0 || i.null}
	case *core.Opt:
		i := n.info(x.Body, edges)
		return nfaInfo{i.first, i.last, true}
	case *core.Capture:
		return n.info(x.Body, edges)
	case *core.Drop:
		return n.info(x.Body, edges)
	case *core.Text:
		return n.info(x.Body, edges)
	case *core.Look, *core.Pred, *core.BindNode, *core.Cut, *core.Succeed, *core.FailNode, *core.Anchor:
		return empty
	}
	// What (D) keeps out of a region: any characters.
	return n.any()
}

func (n *prefixNFA) any() nfaInfo {
	i := nfaInfo{newBitset(n.size), newBitset(n.size), true}
	i.first.set(0)
	i.last.set(0)
	return i
}

// follow returns the characters that can follow the characters cs at the start of a match of the
// body of the region r, one set for each character: the next character inside the match, when the
// match goes on after them.
func (n *prefixNFA) follow(r string, cs []charset) charset {
	s := n.start[r]
	for i, c := range cs {
		if i > 0 {
			s = n.succ(s)
		}
		t := newBitset(n.size)
		s.each(func(p int) {
			if n.chars[p].intersects(c) {
				t.set(p)
			}
		})
		if t.empty() {
			return nil
		}
		s = t
	}
	var out charset
	n.succ(s).each(func(p int) { out = out.union(n.chars[p]) })
	return out
}

func (n *prefixNFA) succ(s bitset) bitset {
	out := newBitset(n.size)
	s.each(func(p int) { out.or(n.next[p]) })
	return out
}

// terminalChars returns the characters that a terminal consumes, one set for each character.
func terminalChars(n core.Node) []charset {
	switch n := n.(type) {
	case *core.Lit:
		var out []charset
		for _, c := range n.Text {
			out = append(out, litChar(n, c))
		}
		return out
	case *core.ClassNode:
		return []charset{coreClassChars(n.Class)}
	case *core.Any:
		return []charset{allChars}
	}
	return nil
}

// regionViolation is a crossing of a region that is not guarded, in the body of rule.
type regionViolation struct{ region, rule string }

// guardPass is the forward pass of the settled regions over the outer rules, and the check of the
// crossings. A state holds the trivia regions that end where the parse is: at every evaluation of
// an expression with a region in its state, an evaluation of that region matches up to its
// position. A rule starts with the regions that all its calls hold, and a call of it ends with
// those that its body ends with.
type guardPass struct {
	k          *classifier
	regions    map[string]bool
	names      []string
	index      map[string]int // the bit of each trivia region
	width      int
	entry      map[string]bitset
	exit       map[string]bitset
	outer      map[string]bool
	nfa        *prefixNFA
	changed    bool
	checking   bool
	rule       string
	violations []regionViolation
	seen       map[regionViolation]bool
}

func (k *classifier) regionGuards(regions map[string]bool, names []string, outer map[string]bool, nfa *prefixNFA) *guardPass {
	g := &guardPass{k: k, regions: regions, names: names, index: map[string]int{}, entry: map[string]bitset{},
		exit: map[string]bitset{}, outer: outer, nfa: nfa, seen: map[regionViolation]bool{}}
	for _, n := range names {
		if regions[n] {
			g.index[n] = g.width
			g.width++
		}
	}
	top := g.top()
	for n := range outer {
		if _, ok := regions[n]; !ok {
			g.entry[n], g.exit[n] = top, top
		}
	}
	g.entry[k.start] = g.none()
	for g.changed = true; g.changed; {
		g.changed = false
		g.each(func(r *ast.RuleDef) {
			if out := g.walk(g.k.ruleBody(r.Name), g.entry[r.Name], nil).and(g.exit[r.Name]); !slices.Equal(out, g.exit[r.Name]) {
				g.exit[r.Name] = out
				g.changed = true
			}
		})
	}
	g.checking = true
	g.each(func(r *ast.RuleDef) {
		g.rule = r.Name
		g.walk(g.k.ruleBody(r.Name), g.entry[r.Name], nil)
	})
	return g
}

// each calls f on the outer rules that are not regions, in the order of the grammar.
func (g *guardPass) each(f func(*ast.RuleDef)) {
	for _, r := range g.k.rules {
		if _, ok := g.regions[r.Name]; g.outer[r.Name] && !ok {
			g.k.control.Poll()
			f(r)
		}
	}
}

func (g *guardPass) none() bitset { return newBitset(g.width) }

func (g *guardPass) top() bitset {
	b := newBitset(g.width)
	for i := range g.width {
		b.set(i)
	}
	return b
}

// isCheck reports whether n is a check: a negative lookahead of an expression of terminals that
// calls no rule. A check is evaluated where it stands and examines a bounded amount of input.
func isCheck(n core.Node) bool {
	l, ok := n.(*core.Look)
	if !ok || !l.Neg {
		return false
	}
	pure := true
	walkNodes(l.Body, func(x core.Node) {
		switch x.(type) {
		case *core.Lit, *core.ClassNode, *core.Any, *core.Seq, *core.Choice, *core.Opt, *core.Repeat:
		default:
			pure = false
		}
	})
	return pure
}

// walk carries the state in through n and returns the state after it. next is the check that
// follows n when n ends where the check is evaluated, or nil.
func (g *guardPass) walk(n core.Node, in bitset, next *core.Look) bitset {
	k := g.k
	k.control.Poll()
	if rule, ok := k.calledRule(n); ok {
		if trivia, ok := g.regions[rule]; ok {
			out := g.none()
			if trivia {
				out.set(g.index[rule])
			}
			return out
		}
		if old := g.entry[rule]; !slices.Equal(old.and(in), old) {
			g.entry[rule] = old.and(in)
			g.changed = true
		}
		return g.exit[rule]
	}
	switch n := n.(type) {
	case *core.Lit, *core.ClassNode, *core.Any:
		if g.checking {
			g.cross(n, in, next)
		}
		if l, ok := n.(*core.Lit); ok && l.Text == "" {
			return in
		}
		return g.none()
	case *core.Seq:
		for i, it := range n.Items {
			var nx *core.Look
			if i+1 < len(n.Items) {
				if isCheck(n.Items[i+1]) {
					nx = n.Items[i+1].(*core.Look)
				}
			} else {
				nx = next
			}
			in = g.walk(it, in, nx)
		}
		return in
	case *core.Choice:
		out := g.top()
		for _, alt := range n.Alts {
			out = out.and(g.walk(alt.Body, in, next))
		}
		return out
	case *core.Opt:
		return in.and(g.walk(n.Body, in, next))
	case *core.Repeat:
		if n.Max == 0 {
			return in
		}
		// The state at the start of an iteration holds on every iteration, so the crossings are
		// checked with it only.
		checking := g.checking
		g.checking = false
		s := in
		for {
			t := s.and(g.walk(n.Body, s, nil))
			if slices.Equal(t, s) {
				break
			}
			s = t
		}
		g.checking = checking
		if checking {
			g.walk(n.Body, s, nil)
		}
		return s
	case *core.Capture:
		return g.walk(n.Body, in, next)
	case *core.Drop:
		return g.walk(n.Body, in, next)
	case *core.Text:
		return g.walk(n.Body, in, next)
	case *core.Nest:
		return g.walk(n.Body, in, next)
	case *core.Look:
		if n.Neg && isCheck(n) {
			return in
		}
		g.walk(n.Body, in, nil)
		return in
	case *core.Label:
		g.walk(n.Body, in, nil)
		return g.none()
	case *core.Missing:
		g.walk(n.Body, in, nil)
		return g.none()
	case *core.Recover:
		g.walk(n.Body, in, nil)
		g.walk(n.Skip, in, nil)
		return g.none()
	case *core.Stream:
		g.walk(n.Repeat, in, nil)
		return g.none()
	case *core.OperatorsRule:
		// Its parts follow one another in an order that the pass does not follow.
		for _, l := range n.Lines() {
			g.walk(l.Body, g.none(), nil)
		}
		return g.none()
	}
	// Predicates, anchors, cuts and the top expression consume nothing.
	return in
}

// cross checks the terminal t, whose state is in and whose next check is next, against each
// region: (G1) a character of the opening characters of a region only as its first character, and
// (G2) a first character of the region only where the region ended, or where the next character
// cannot go on with the match of the region, or before a check that fails wherever it can.
func (g *guardPass) cross(t core.Node, in bitset, next *core.Look) {
	cs := terminalChars(t)
	if len(cs) == 0 {
		return
	}
	for _, r := range g.names {
		o := g.nfa.opener[r]
		ok := true
		for _, c := range cs[1:] {
			if c.intersects(o) {
				ok = false
			}
		}
		if ok && cs[0].intersects(o) {
			ok = g.regions[r] && in.has(g.index[r])
			if !ok {
				f := g.nfa.follow(r, cs)
				ok = len(f) == 0 || next != nil && g.k.subset(f, g.k.sure(next.Body))
			}
		}
		if v := (regionViolation{r, g.rule}); !ok && !g.seen[v] {
			g.seen[v] = true
			g.violations = append(g.violations, v)
		}
	}
}
