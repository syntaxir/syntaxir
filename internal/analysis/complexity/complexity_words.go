package complexity

import (
	"slices"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// --- Exclusion by the leading text of a unit ---

// word is the text with which a path of a unit begins: a case-sensitive literal, optionally
// followed by a negative lookahead of one character class, as a keyword "let" !idchar is. After
// the unit matches its word, the input at its start is the text followed, when after is set, by a
// character that is not in after or by the end of the input.
type word struct {
	text  []rune
	after charset
	// bounded reports whether after is known.
	bounded bool
}

// wordBudget bounds the steps of one search for words or through a continuation.
const wordBudget = leadBudget

// excludedByWord reports whether every path of the continuation c is excluded at the start of the
// unit u by the words with which the paths of u begin. A unit that fails within a word examines
// no more than the word and the character after it. After the unit matches a word, the input at
// its start begins with that word, and each path of the continuation fails at a cost that the
// word bounds: it fails on the text of the word, or it evaluates a negative lookahead of a rule
// that is proven to match there. Either way the continuation repeats no scan of u.
func (k *classifier) excludedByWord(u, c []*cont) bool {
	words, ok := k.unitWords(u)
	if !ok {
		return false
	}
	for _, w := range words {
		if !k.wordExcludes(c, w) {
			return false
		}
	}
	return true
}

// unitWords returns the word with which each path of u begins. It looks only through
// expressions that evaluate their content first, and through calls of rules that read no variable
// and have no level restriction.
func (k *classifier) unitWords(u []*cont) ([]word, bool) {
	budget := wordBudget
	var words []word
	ok := true
	var visit func(p *cont)
	visit = func(p *cont) {
		for ok {
			k.control.Poll()
			budget--
			if budget < 0 || p == nil || len(words) > leadMax {
				ok = false
				return
			}
			if len(p.items) == 0 {
				if p.next == nil {
					ok = false
					return
				}
				p = p.next
				continue
			}
			rest := p.rest()
			switch n := k.nodeOf(p.items[0]).(type) {
			case *core.Capture:
				p = rest.then(k.exprOf(n.Body))
			case *core.Drop:
				p = rest.then(k.exprOf(n.Body))
			case *core.Text:
				p = rest.then(k.exprOf(n.Body))
			case *core.Seq:
				p = rest.then(k.exprsOf(n.Items)...)
			case *core.Choice:
				for _, alt := range n.Alts {
					visit(rest.then(k.exprOf(alt.Body)))
				}
				return
			case *core.Call:
				if !k.plainRuleCall(n) {
					ok = false
					return
				}
				p = rest.then(k.rules[n.Rule].Expr)
			case *core.Lit:
				if n.Text == "" || n.Fold {
					ok = false
					return
				}
				w := word{text: []rune(n.Text)}
				w.after, w.bounded = k.notAfter(rest)
				words = append(words, w)
				return
			default:
				ok = false
				return
			}
		}
	}
	for _, p := range u {
		visit(p)
	}
	return words, ok && len(words) > 0
}

// exprsOf returns the expressions that the nodes ns stand for (exprOf).
func (k *classifier) exprsOf(ns []core.Node) []ast.Expr {
	es := make([]ast.Expr, len(ns))
	for i, n := range ns {
		es[i] = k.exprOf(n)
	}
	return es
}

// notAfter returns the characters of a negative lookahead of one character class that the path p
// evaluates before anything else, if it does.
func (k *classifier) notAfter(p *cont) (charset, bool) {
	for range wordBudget {
		if p == nil {
			return nil, false
		}
		if len(p.items) == 0 {
			p = p.next
			continue
		}
		switch n := k.nodeOf(p.items[0]).(type) {
		case *core.Seq:
			p = p.rest().then(k.exprsOf(n.Items)...)
		case *core.Look:
			if !n.Neg {
				return nil, false
			}
			return k.oneChar(n.Body)
		default:
			return nil, false
		}
	}
	return nil, false
}

// oneChar returns the characters of n when n is a character class or a call of a rule whose body
// is one.
func (k *classifier) oneChar(n core.Node) (charset, bool) {
	if c, ok := n.(*core.Call); ok {
		if !k.plainRuleCall(c) {
			return nil, false
		}
		n = k.ruleBody(k.rules[c.Rule].Name)
	}
	if _, ok := n.(*core.ClassNode); !ok {
		return nil, false
	}
	return classChars(k.exprOf(n).(*ast.CharClass)), true
}

// plainRuleCall reports whether c calls a rule of the grammar without a level restriction, and the
// rule reads no variable, so that the call matches as the rule body does. Both checks are
// defensive: the body under a level restriction is an Operators Rule, whose start paths do not
// depend on the level, and variables change matching only through predicates, which no word or
// proven lookahead accepts. They keep the reasoning valid if either of those changes.
func (k *classifier) plainRuleCall(c *core.Call) bool {
	return plainCall(c) && len(k.vars[k.rules[c.Rule].Name]) == 0
}

// wordKey is a rule end that a path of a continuation reaches at a position in a word.
type wordKey struct {
	endKey
	at int
}

// wordExcludes reports whether every path of the continuation fails at bounded cost at a position
// where the input begins with the word w. A path reads the text of w as it consumes it; past the
// text, nothing is known and the path is not excluded.
func (k *classifier) wordExcludes(paths []*cont, w word) bool {
	budget := wordBudget
	live := false
	ended := map[wordKey]bool{}
	var visit func(p *cont, at int)
	visit = func(p *cont, at int) {
		for !live {
			k.control.Poll()
			budget--
			if budget < 0 {
				live = true
				return
			}
			if p == nil {
				// The parse ends here and examines nothing more.
				return
			}
			if len(p.items) == 0 {
				if p.next != nil {
					p = p.next
					continue
				}
				if p.end != endNone {
					key := wordKey{endKey{p.end, p.rule}, at}
					if ended[key] {
						return
					}
					ended[key] = true
				}
				next := func(c *cont) { visit(c, at) }
				switch p.end {
				case endOps:
					k.opsLeads(p.rule, p.inner, next)
					k.endLeads(p.rule, func(c *cont, _ *site) { next(c) })
				case endRule:
					k.endLeads(p.rule, func(c *cont, _ *site) { next(c) })
				}
				return
			}
			rest := p.rest()
			it := p.items[0]
			if it == nil {
				p = rest
				continue
			}
			if at == len(w.text) {
				// Past the text nothing is known.
				live = true
				return
			}
			// An item that cannot examine the next character of the text neither consumes input
			// here nor examines more than that character: it fails, or it continues at the same
			// position.
			n := k.nodeOf(it)
			if !k.firstOf(n).intersects(charOf(w.text[at])) {
				if !k.nullableOf(n) {
					return
				}
				p = rest
				continue
			}
			switch n := n.(type) {
			case *core.Capture:
				p = rest.then(k.exprOf(n.Body))
			case *core.Drop:
				p = rest.then(k.exprOf(n.Body))
			case *core.Text:
				p = rest.then(k.exprOf(n.Body))
			case *core.Seq:
				p = rest.then(k.exprsOf(n.Items)...)
			case *core.Label, *core.Recover, *core.Missing, *core.Stream:
				// A #recover or a #missing can match other input than its operand does.
				operand, recovers := attributeOperand(n)
				if recovers {
					live = true
					return
				}
				p = rest.then(k.exprOf(operand))
			case *core.Choice:
				for _, alt := range n.Alts {
					visit(rest.then(k.exprOf(alt.Body)), at)
				}
				return
			case *core.Opt:
				visit(rest.then(k.exprOf(n.Body)), at)
				visit(rest, at)
				return
			case *core.Repeat:
				if n.Max == 0 {
					p = rest
					continue
				}
				again := k.againOf(k.exprOf(n).(*ast.Repeat))
				visit(rest.then(k.exprOf(n.Body), again), at)
				if n.Min == 0 {
					visit(rest, at)
				}
				return
			case *core.ClassNode, *core.Any:
				// The item matches the character of the text, which first has shown.
				p, at = rest, at+1
			case *core.Lit:
				m, ok := matchText(n, w.text[at:])
				switch {
				case !ok:
					return
				case m < 0:
					live = true
					return
				}
				p, at = rest, at+m
			case *core.Look:
				if !n.Neg {
					live = true
					return
				}
				switch k.matchesWord(n.Body, w, at) {
				case wordMatches:
					return
				case wordFails:
					p = rest
				default:
					live = true
					return
				}
			case *core.Call:
				if !k.plainRuleCall(n) {
					live = true
					return
				}
				name := k.rules[n.Rule].Name
				if pr := k.operatorsBody(name); pr != nil {
					k.operatorsLeads(pr, name, func(c *cont) { visit(c, at) })
					return
				}
				p = rest.then(k.byName[name].Expr)
			default:
				live = true
				return
			}
		}
	}
	for _, p := range paths {
		visit(p, 0)
	}
	return !live
}

// matchText matches the literal l at the start of text. It returns the length that l matches and
// true, false when l fails within text, or -1 and true when the text ends before l does.
func matchText(l *core.Lit, text []rune) (int, bool) {
	if l.Fold {
		return -1, true
	}
	lit := []rune(l.Text)
	n := min(len(lit), len(text))
	if !slices.Equal(lit[:n], text[:n]) {
		return 0, false
	}
	if len(lit) > len(text) {
		return -1, true
	}
	return len(lit), true
}

// wordOutcome is what is known of an expression at a position in a word.
type wordOutcome uint8

const (
	wordUnknown wordOutcome = iota
	wordMatches
	wordFails
)

// matchesWord evaluates the node K at position at in the word w. K must be a choice of
// case-sensitive literals that are not empty, or one such literal, optionally followed by a
// negative lookahead of a character class, through captures, #atomic and calls of rules that read
// no variable and have no level restriction, with nothing else that can depend on how the parser
// reached it: no predicate, other lookahead, cut, recovery, action or level restriction. The
// literals are tried in order against the text; a lookahead after the text is known only when it
// has the characters that cannot follow the word.
func (k *classifier) matchesWord(n core.Node, w word, at int) wordOutcome {
	n, ok := k.wordBody(n)
	if !ok {
		return wordUnknown
	}
	var not charset
	hasNot := false
	if s, ok := n.(*core.Seq); ok {
		if len(s.Items) != 2 {
			return wordUnknown
		}
		l, ok := s.Items[1].(*core.Look)
		if !ok || !l.Neg {
			return wordUnknown
		}
		if not, ok = k.oneChar(l.Body); !ok {
			return wordUnknown
		}
		hasNot = true
		if n, ok = k.wordBody(s.Items[0]); !ok {
			return wordUnknown
		}
	}
	alts := []core.Node{n}
	if c, ok := n.(*core.Choice); ok {
		alts = core.Children(c)
	}
	text := w.text[at:]
	for _, alt := range alts {
		alt, ok := k.wordBody(alt)
		if !ok {
			return wordUnknown
		}
		l, ok := alt.(*core.Lit)
		if !ok || l.Text == "" || l.Fold {
			return wordUnknown
		}
		m, ok := matchText(l, text)
		switch {
		case !ok:
			continue
		case m < 0:
			return wordUnknown
		case !hasNot:
			return wordMatches
		case m < len(text):
			if not.intersects(charOf(text[m])) {
				return wordFails
			}
			return wordMatches
		case w.bounded && not.equal(w.after):
			return wordMatches
		}
		return wordUnknown
	}
	return wordFails
}

// wordBody returns n through captures, #atomic and calls of rules that read no variable and have
// no level restriction.
func (k *classifier) wordBody(n core.Node) (core.Node, bool) {
	for range len(k.rules) + 1 {
		switch x := n.(type) {
		case *core.Capture:
			n = x.Body
		case *core.Text:
			n = x.Body
		case *core.Call:
			if !k.plainRuleCall(x) {
				return nil, false
			}
			n = k.ruleBody(k.rules[x.Rule].Name)
		default:
			return n, true
		}
	}
	return nil, false
}
