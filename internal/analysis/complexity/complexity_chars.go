package complexity

import (
	"fmt"
	"math"
	"slices"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// charset is a set of characters as sorted, disjoint, non-adjacent ranges. The classifier uses it
// for the first characters of expressions, which it over-approximates: a character that an
// expression can examine first is always in its set.
type charset []ast.CharRange

// maxChar is the largest character value. Input that is not valid UTF-8 decodes to characters
// below it, so every character that a parse can examine is in [0, maxChar].
const maxChar = math.MaxInt32

var allChars = charset{{Lo: 0, Hi: maxChar}}

// lineBreakChars are the characters that $ examines.
var lineBreakChars = charset{{Lo: '\n', Hi: '\n'}, {Lo: '\r', Hi: '\r'}}

func (s charset) union(t charset) charset {
	if len(t) == 0 {
		return s
	}
	if len(s) == 0 {
		return t
	}
	all := make([]ast.CharRange, 0, len(s)+len(t))
	all = append(append(all, s...), t...)
	return normalizeRanges(all)
}

// normalizeRanges sorts rs and merges overlapping and adjacent ranges.
func normalizeRanges(rs []ast.CharRange) charset {
	rs = slices.Clone(rs)
	slices.SortFunc(rs, func(a, b ast.CharRange) int { return int(a.Lo) - int(b.Lo) })
	var out charset
	for _, r := range rs {
		if r.Lo > r.Hi {
			continue
		}
		if n := len(out); n > 0 && int64(r.Lo) <= int64(out[n-1].Hi)+1 {
			out[n-1].Hi = max(out[n-1].Hi, r.Hi)
			continue
		}
		out = append(out, r)
	}
	return out
}

func (s charset) intersects(t charset) bool {
	i, j := 0, 0
	for i < len(s) && j < len(t) {
		switch {
		case s[i].Hi < t[j].Lo:
			i++
		case t[j].Hi < s[i].Lo:
			j++
		default:
			return true
		}
	}
	return false
}

func (s charset) equal(t charset) bool { return slices.Equal(s, t) }

// complement returns the characters in [0, maxChar] that are not in s.
func (s charset) complement() charset {
	var out charset
	lo := rune(0)
	for _, r := range s {
		if r.Lo > lo {
			out = append(out, ast.CharRange{Lo: lo, Hi: r.Lo - 1})
		}
		if r.Hi == maxChar {
			return out
		}
		lo = r.Hi + 1
	}
	return append(out, ast.CharRange{Lo: lo, Hi: maxChar})
}

func charOf(r rune) charset { return charset{{Lo: r, Hi: r}} }

// asciiFold adds the other ASCII case of r, as case-insensitive literals match it.
func asciiFold(r rune) charset {
	switch {
	case 'a' <= r && r <= 'z':
		return normalizeRanges([]ast.CharRange{{Lo: r, Hi: r}, {Lo: r - 'a' + 'A', Hi: r - 'a' + 'A'}})
	case 'A' <= r && r <= 'Z':
		return normalizeRanges([]ast.CharRange{{Lo: r, Hi: r}, {Lo: r - 'A' + 'a', Hi: r - 'A' + 'a'}})
	}
	return charOf(r)
}

// litFirst is the first character of a literal of text, matched without case if fold, or the
// empty set for the empty literal. A literal that holds a replacement character also matches input
// that is not valid UTF-8, which decodes to that character.
func litFirst(text string, fold bool) charset {
	if text == "" {
		return nil
	}
	r, _ := utf8.DecodeRuneInString(text)
	if fold {
		return asciiFold(r)
	}
	return charOf(r)
}

// coreClassChars is the characters of a class of Core IR.
func coreClassChars(c *core.Class) charset {
	rs := make([]ast.CharRange, len(c.Ranges))
	for i, r := range c.Ranges {
		rs[i] = ast.CharRange{Lo: r.Lo, Hi: r.Hi}
	}
	s := normalizeRanges(rs)
	if c.Negated {
		return s.complement()
	}
	return s
}

// firstOf returns the first characters of n: the characters that an evaluation of n can examine
// beyond its start, as it consumes them or as a lookahead that it evaluates before it consumes
// input examines them. Characters that follow n when it matches without consuming input are not
// included; firstCont adds them.
func (k *classifier) firstOf(n core.Node) charset {
	k.control.Poll()
	switch n := n.(type) {
	case nil:
		return nil
	case *core.Call:
		return k.firstRule[k.rules[n.Rule].Name]
	case *core.Nest:
		if isRightOperand(n) {
			return k.operatorsStart(n.Body.(*core.OperatorsRule))
		}
		return k.firstOf(n.Body)
	case *core.Lit:
		return litFirst(n.Text, n.Fold)
	case *core.ClassNode:
		return coreClassChars(n.Class)
	case *core.Any:
		return allChars
	case *core.Anchor:
		if n.Kind == core.EndLine {
			return lineBreakChars
		}
		return nil
	case *core.Scan:
		if n.Max == 0 {
			return nil
		}
		if n.Class == nil {
			return allChars
		}
		return coreClassChars(n.Class)
	case *core.ClassRun:
		if len(n.Classes) == 0 {
			return nil
		}
		return coreClassChars(n.Classes[0])
	case *core.Seq:
		var s charset
		for _, it := range n.Items {
			s = s.union(k.firstOf(it))
			if !k.nullableOf(it) {
				break
			}
		}
		return s
	case *core.Choice:
		var s charset
		for _, alt := range n.Alts {
			s = s.union(k.firstOf(alt.Body))
		}
		return s
	case *core.Repeat:
		if n.Max == 0 {
			return nil
		}
		return k.firstOf(n.Body)
	case *core.Opt:
		return k.firstOf(n.Body)
	case *core.Look:
		return k.firstOf(n.Body)
	case *core.Capture:
		return k.firstOf(n.Body)
	case *core.Text:
		return k.firstOf(n.Body)
	case *core.Drop:
		return k.firstOf(n.Body)
	case *core.Label:
		return k.firstOf(n.Body)
	case *core.Missing:
		return k.firstOf(n.Body)
	case *core.Terminal:
		return k.firstOf(n.Body)
	case *core.Stream:
		return k.firstOf(n.Repeat)
	case *core.Recover:
		return k.firstOf(n.Body).union(k.firstOf(n.Skip))
	case *core.OperatorsRule:
		return k.operatorsStart(n)
	case *core.Pred, *core.BindNode, *core.Cut, *core.Succeed, *core.FailNode:
		return nil
	}
	panic(fmt.Sprintf("complexity: first characters of %T", n))
}

// operatorsStart is the first characters of an Operators Rule and of each right operand: its
// skip, its operands and its prefix operator parts. When an operand can match nothing, the infix
// and postfix operator parts after it examine characters from the start too, so they count as
// well.
func (k *classifier) operatorsStart(o *core.OperatorsRule) charset {
	var s charset
	if o.Skip != nil {
		s = k.firstOf(o.Skip.Body)
	}
	for _, l := range o.Operands {
		s = s.union(k.firstOf(l.Body))
	}
	emptyOperand := k.emptyOperand(o)
	for _, op := range o.Ops {
		if op.Kind == ast.Prefix || emptyOperand {
			s = s.union(k.firstOf(op.Line.Body))
		}
	}
	return s
}

// emptyOperand reports whether an operand of the Operators Rule o can match nothing, so that the
// infix and postfix parts after it can start an expression.
func (k *classifier) emptyOperand(o *core.OperatorsRule) bool {
	for _, l := range o.Operands {
		if k.nullableOf(l.Body) {
			return true
		}
	}
	return false
}

func (k *classifier) operandsNullable(rule string) bool {
	pr := k.operatorsOf(rule)
	return pr == nil || k.emptyOperand(pr)
}

// computeFirst computes the first characters of every rule as a fixed point.
func (k *classifier) computeFirst() {
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if s := k.firstOf(k.ruleBody(r.Name)).union(k.firstRule[r.Name]); !s.equal(k.firstRule[r.Name]) {
				k.firstRule[r.Name] = s
				changed = true
			}
		}
	}
}

// boundedOf reports whether every match of n is shorter than a constant of the grammar.
func (k *classifier) boundedOf(n core.Node) bool {
	k.control.Poll()
	switch n := n.(type) {
	case nil, *core.Lit, *core.ClassNode, *core.Any, *core.ClassRun, *core.Anchor, *core.Look,
		*core.Pred, *core.BindNode, *core.Cut, *core.Succeed, *core.FailNode:
		return true
	case *core.Call:
		return k.boundedRule[k.rules[n.Rule].Name]
	case *core.Nest:
		return !isRightOperand(n) && k.boundedOf(n.Body)
	case *core.Scan:
		return n.Max >= 0
	case *core.Seq:
		for _, it := range n.Items {
			if !k.boundedOf(it) {
				return false
			}
		}
		return true
	case *core.Choice:
		for _, alt := range n.Alts {
			if !k.boundedOf(alt.Body) {
				return false
			}
		}
		return true
	case *core.Repeat:
		return n.Max >= 0 && k.boundedOf(n.Body)
	case *core.Opt:
		return k.boundedOf(n.Body)
	case *core.Capture:
		return k.boundedOf(n.Body)
	case *core.Text:
		return k.boundedOf(n.Body)
	case *core.Drop:
		return k.boundedOf(n.Body)
	case *core.Label:
		return k.boundedOf(n.Body)
	case *core.Missing:
		return k.boundedOf(n.Body)
	case *core.Terminal:
		return k.boundedOf(n.Body)
	case *core.Stream:
		return k.boundedOf(n.Repeat)
	case *core.Recover:
		return k.boundedOf(n.Body) && k.boundedOf(n.Skip)
	case *core.OperatorsRule:
		return false
	}
	panic(fmt.Sprintf("complexity: boundedness of %T", n))
}

// computeBounded marks the rules whose matches are bounded in length: the greatest fixed point
// would admit recursive rules, so a rule is bounded only if it is not recursive.
func (k *classifier) computeBounded() {
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if !k.boundedRule[r.Name] && !k.recursive[r.Name] && k.boundedOf(k.ruleBody(r.Name)) {
				k.boundedRule[r.Name] = true
				changed = true
			}
		}
	}
}
