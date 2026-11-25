package lint

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// analysis holds what the checks need to know about a grammar: the grammar that the front end
// checked and the facts of its Core IR module, which the front end computed. A fact of an expression
// is the fact of the outermost node built from it (Module.Origin). Every property is either an
// over-approximation or an under-approximation, chosen so that a check built on it reports only
// what is certain:
//
//   - nullable (may succeed without consuming input, at the end of the input or before it) and
//     mayMatch (may succeed at all) are over-approximations: "not nullable" and "cannot match"
//     are certain.
//   - succeeds (certainly succeeds, whatever the input), matchPrefix (certainly succeeds on every
//     input that begins with a given string) and fails (certainly fails on such inputs) are
//     under-approximations.
//   - prefix (a string that every match begins with) is an under-approximation.
//
// None of them account for the nesting limit (Limits.Depth), which can make any rule call fail.
type analysis struct {
	control *lintControl
	g       *ast.Grammar
	order   []*ast.RuleDef
	rules   map[string]*ast.RuleDef

	// m is the Core IR module of g and facts its facts, whose node queries poll the control of the
	// run. nodes maps each expression of g to the outermost node built from it, and ids each rule
	// name to the ID of the rule (not of its value-free twin).
	m     *core.Module
	facts *corepass.Facts
	nodes map[ast.Expr]core.Node
	ids   map[string]int

	// baseCase holds the rules that can match if _|_ and ! of an expression that always
	// succeeds are taken to match: the rules not in it can only match by matching themselves
	// (or another rule not in it) first.
	baseCase map[string]bool

	// Memos of the analyses that follow rule calls, and the rules being followed (a call of a
	// rule that is being followed is not followed again: the result is "not certain").
	matchMemo  map[matchKey]matchResult
	prefixMemo map[string]prefixResult
	busy       map[string]bool
	// cycles counts the calls not followed because of busy, so that results that depend on
	// them are not memoized.
	cycles int
}

// newAnalysisControlled returns the analysis of the grammar that the front end checked to res.
func newAnalysisControlled(res *frontend.Result, control *lintControl) *analysis {
	control.poll()

	var facts *cancel.Control
	if control != nil {
		facts = cancel.New(control.ctx)
	}
	a := &analysis{control: control, g: res.Grammar, rules: map[string]*ast.RuleDef{}, m: res.Module,
		facts: corepass.FactsOf(res.Analyses, res.Module).WithControl(facts), nodes: map[ast.Expr]core.Node{}, ids: map[string]int{},
		baseCase:  map[string]bool{},
		matchMemo: map[matchKey]matchResult{}, prefixMemo: map[string]prefixResult{}, busy: map[string]bool{}}
	for _, r := range control.rules(a.g) {
		control.poll()
		if a.rules[r.Name] == nil {
			a.rules[r.Name] = r
			a.order = append(a.order, r)
		}
	}
	// A value-free twin is built from the expressions of its rule: the rule's own nodes come first.
	for _, twins := range []bool{false, true} {
		for _, r := range a.m.Rules {
			control.poll()
			if r == nil || (r.Twin != nil) != twins {
				continue
			}
			if !twins {
				a.ids[r.Name] = r.ID
			}
			core.Walk(r.Body, func(n core.Node) {
				control.poll()
				if e := a.m.Origin.Of(n); e != nil && a.nodes[e] == nil {
					a.nodes[e] = n
				}
			})
		}
	}
	for changed := true; changed; {
		control.poll()
		changed = false
		for _, r := range a.order {
			control.poll()
			if !a.baseCase[r.Name] && a.mayMatchLeniently(r.Expr, a.baseCase) {
				a.baseCase[r.Name], changed = true, true
			}
		}
	}
	return a
}

// node returns the Core node of e: the outermost node built from it, or, for a sequence of items
// that a check makes from a part of a sequence, a sequence of the nodes of the items.
func (a *analysis) node(e ast.Expr) core.Node {
	if n := a.nodes[e]; n != nil {
		return n
	}
	if s, ok := e.(*ast.Seq); ok {
		items := make([]core.Node, len(s.Items))
		for i, it := range s.Items {
			a.control.poll()
			items[i] = a.node(it)
		}
		return &core.Seq{Items: items}
	}
	pos := posOf(e)
	panic(fmt.Sprintf("lint: no Core node for the %T at %d:%d", e, pos.Line, pos.Col))
}

// rule returns the facts of the rule named name, which is a rule of g.
func (a *analysis) rule(name string) *corepass.Rule { return &a.facts.Rules[a.ids[name]] }

// leftRecursive reports that the rule named name is on a left-recursion cycle. While such a rule
// grows its match, a call of it inside the cycle returns the match found so far, which can be a
// failure: a call of it is never certain to succeed.
func (a *analysis) leftRecursive(name string) bool {
	_, ok := a.ids[name]
	return ok && a.rule(name).Cyclic()
}

// isEngineNullable reports whether e may succeed without consuming input, as the engine computes it
// to find left recursion (cruder than nullableAt: every lookahead counts as nullable).
func (a *analysis) isEngineNullable(e ast.Expr) bool {
	a.control.poll()
	return a.facts.Nullable(a.node(e))
}

// leftCalls appends to acc the rules that e may call before consuming input, as the engine finds
// them.
func (a *analysis) leftCalls(e ast.Expr, acc []string) []string {
	a.control.poll()
	for _, id := range a.facts.LeftCalls(a.node(e), false) {
		a.control.poll()
		acc = append(acc, a.m.Rules[id].Name)
	}
	return acc
}

// mayMatch reports whether e may succeed.
func (a *analysis) mayMatch(e ast.Expr) bool {
	a.control.poll()
	return a.facts.MayMatch(a.node(e))
}

// isNullable reports whether e may succeed without consuming input.
func (a *analysis) isNullable(e ast.Expr) bool {
	return a.nullableAt(e, false) || a.nullableAt(e, true)
}

// nullableAt reports whether e may succeed without consuming input at the end of the input (end
// set) or before it. Telling the two apart makes "!$$ x" not nullable when x can only match nothing
// at the end of the input, as in a line that ends with ("\n" / $$).
func (a *analysis) nullableAt(e ast.Expr, end bool) bool {
	a.control.poll()
	if end {
		return a.facts.NullableAtEnd(a.node(e))
	}
	return a.facts.NullableBeforeEnd(a.node(e))
}

// succeeds reports whether e certainly succeeds, whatever the input.
func (a *analysis) succeeds(e ast.Expr) bool {
	a.control.poll()
	return a.facts.Succeeds(a.node(e))
}

// hasLooseCut reports whether e contains a cut that commits a choice, repetition or optional
// expression around e: one that is not inside a choice, repetition, optional expression or
// lookahead of its own (which stop it), or in another rule (cuts do not cross rules).
func (a *analysis) hasLooseCut(e ast.Expr) bool {
	a.control.poll()
	return a.facts.LooseCut(a.node(e))
}

// mayMatchLeniently reports whether e may succeed, given the rules in set that may, with _|_ and !
// taken to match (baseCase is the least fixed point: a rule that can only match by matching itself
// first is not in it).
//
// Anchors that contradict their neighbors (anchorConflict) are not taken into account.
func (a *analysis) mayMatchLeniently(e ast.Expr, set map[string]bool) bool {
	control := a.control
	control.poll()

	switch e := analysisExpr(e, control).(type) {
	case *ast.Ref:
		return set[e.Name]
	case *ast.Bottom:
		return true
	case *ast.Seq:
		for _, it := range e.Items {
			control.poll()
			if !a.mayMatchLeniently(it, set) {
				return false
			}
		}
	case *ast.Choice:
		for _, alt := range e.Alts {
			control.poll()
			if a.mayMatchLeniently(alt, set) {
				return true
			}
		}
		return false
	case *ast.Repeat:
		return e.Min == 0 || a.mayMatchLeniently(e.Expr, set)
	case *ast.And:
		return a.mayMatchLeniently(e.Expr, set)
	case *ast.Not:
		return true
	case *ast.Atomic:
		return a.mayMatchLeniently(e.Expr, set)
	case *ast.Discard:
		return a.mayMatchLeniently(e.Expr, set)
	case *ast.Capture:
		return a.mayMatchLeniently(e.Expr, set)
	case *ast.Attributed:
		return hasAttr(e, "recover", control) || hasAttr(e, "missing", control) || a.mayMatchLeniently(e.Expr, set)
	case *ast.OperatorsRule:
		for _, o := range e.Operands {
			control.poll()
			if a.mayMatchLeniently(o.Expr, set) {
				return true
			}
		}
		return false
	}
	return true
}

func hasAttr(e *ast.Attributed, name string, control *lintControl) bool {
	for _, at := range e.Attrs {
		control.poll()
		if at.Name == name {
			return true
		}
	}
	return false
}

// anchorConflict returns the anchor of the sequence that can never match where it is, if any:
//   - $$ followed by items that cannot match without consuming input at the end of the input;
//   - ^^ preceded by items that must consume input;
//   - $ followed by items whose matches all begin with a character other than a line break;
//   - ^ preceded by items that always consume the same text, which does not end with "\n".
func (a *analysis) anchorConflict(s *ast.Seq) ast.Expr {
	control := a.control
	control.poll()

	for i, it := range s.Items {
		control.poll()
		after := &ast.Seq{Items: s.Items[i+1:]}
		before := &ast.Seq{Items: s.Items[:i]}
		switch it.(type) {
		case *ast.EndInput:
			if !a.nullableAt(after, true) {
				return it
			}
		case *ast.BeginInput:
			if !a.isNullable(before) {
				return it
			}
		case *ast.EndLine:
			if p := a.prefix(after); p.s != "" && p.s[0] != '\n' && p.s[0] != '\r' {
				return it
			}
		case *ast.BeginLine:
			if p := a.prefix(before); p.exact && p.s != "" && !strings.HasSuffix(p.s, "\n") {
				return it
			}
		}
	}
	return nil
}

// matchResult is the result of matchPrefix.
type matchResult struct {
	ok bool
	// exact reports that e consumes exactly the part of t before rest.
	exact bool
	rest  string
}

type matchKey struct {
	rule string
	t    string
	fold bool
}

// matchPrefix reports whether e certainly succeeds on every input that begins with t, or, with fold
// set, with t or any of the strings that differ from t only in the case of ASCII letters.
func (a *analysis) matchPrefix(e ast.Expr, t string, fold bool) matchResult {
	control := a.control
	control.poll()

	ok := func(rest string) matchResult { return matchResult{ok: true, exact: true, rest: rest} }
	inexact := matchResult{ok: true}
	switch e := analysisExpr(e, control).(type) {
	case *ast.Literal:
		// A case-insensitive literal matches the input that begins with any case of it, and the
		// folding keeps byte lengths, so the rest of t begins after len(e.Value) bytes.
		// A case-sensitive literal matches every case of t only where its text has no ASCII letter.
		if e.IgnoreCase && hasPrefixFold(t, e.Value) || !e.IgnoreCase && strings.HasPrefix(t, e.Value) && (!fold || !hasASCIILetter(e.Value)) {
			return ok(t[len(e.Value):])
		}
	case *ast.CharClass:
		if r, size := utf8.DecodeRuneInString(t); t != "" && classAcceptsControlled(e, r, control) &&
			(!fold || classAcceptsControlled(e, otherCase(r), control)) {
			return ok(t[size:])
		}
	case *ast.Any:
		if _, size := utf8.DecodeRuneInString(t); t != "" {
			return ok(t[size:])
		}
	case *ast.Top, *ast.Cut:
		return ok(t)
	case *ast.Seq:
		cur := ok(t)
		for _, it := range e.Items {
			control.poll()
			if cur.exact {
				if cur = a.matchPrefix(it, cur.rest, fold); !cur.ok {
					return cur
				}
			} else if !a.succeeds(it) {
				return matchResult{}
			}
		}
		return cur
	case *ast.Choice:
		for k, alt := range e.Alts {
			control.poll()
			if r := a.matchPrefix(alt, t, fold); r.ok {
				if k == 0 {
					return r
				}
				return inexact
			}
			// A cut that commits the choice to an earlier alternative can make it fail before
			// it gets here.
			if a.hasLooseCut(alt) {
				break
			}
		}
	case *ast.Repeat:
		cur := ok(t)
		for i := range e.Min {
			control.poll()
			if cur.exact {
				before := cur.rest
				if cur = a.matchPrefix(e.Expr, cur.rest, fold); !cur.ok {
					return cur
				}
				if cur.exact && cur.rest == before {
					if i+1 < e.Min {
						return matchResult{}
					}
					return cur // the first empty iteration ends the repetition
				}
				// An inexact match may consume nothing and stop before the
				// minimum. Do not prove another iteration from its success.
				if !cur.exact && i+1 < e.Min {
					return matchResult{}
				}
			} else if !a.succeeds(e.Expr) {
				return matchResult{}
			}
		}
		if e.Max == e.Min {
			return cur
		}
		// Further iterations are optional, unless one fails after a cut.
		if a.hasLooseCut(e.Expr) && !a.succeeds(e.Expr) {
			return matchResult{}
		}
		return inexact
	case *ast.Optional:
		if r := a.matchPrefix(e.Expr, t, fold); r.ok {
			return r
		}
		if !a.hasLooseCut(e.Expr) {
			return inexact
		}
	case *ast.Capture:
		return a.matchPrefix(e.Expr, t, fold)
	case *ast.Atomic:
		return a.matchPrefix(e.Expr, t, fold)
	case *ast.Discard:
		return a.matchPrefix(e.Expr, t, fold)
	case *ast.Attributed:
		// #recover only adds ways to succeed; @error and #stream do not change matching.
		return a.matchPrefix(e.Expr, t, fold)
	case *ast.And:
		if a.matchPrefix(e.Expr, t, fold).ok {
			return ok(t)
		}
	case *ast.Not:
		if a.fails(e.Expr, t, fold) {
			return ok(t)
		}
	case *ast.Ref:
		r := a.rules[e.Name]
		if r == nil || e.Level != "" || a.leftRecursive(e.Name) {
			break
		}
		if pr, _ := ast.RuleOperators(r.Expr); pr != nil {
			break
		}
		key := matchKey{e.Name, t, fold}
		if m, ok := a.matchMemo[key]; ok {
			return m
		}
		if a.busy[e.Name] {
			a.cycles++
			break
		}
		a.busy[e.Name] = true
		before := a.cycles
		m := a.matchPrefix(r.Expr, t, fold)
		delete(a.busy, e.Name)
		if a.cycles == before {
			a.matchMemo[key] = m
		}
		return m
	}
	// Predicates, anchors, bottom and Operators Rule expressions are not certain to succeed.
	return matchResult{}
}

// fails reports whether e certainly fails on every input that begins with t, or, with fold set,
// with t or any of the strings that differ from t only in the case of ASCII letters.
func (a *analysis) fails(e ast.Expr, t string, fold bool) bool {
	control := a.control
	control.poll()

	switch e := analysisExpr(e, control).(type) {
	case *ast.Literal:
		if e.IgnoreCase || fold {
			return !hasPrefixFold(t, e.Value) && !hasPrefixFold(e.Value, t)
		}
		return !strings.HasPrefix(t, e.Value) && !strings.HasPrefix(e.Value, t)
	case *ast.CharClass:
		r, _ := utf8.DecodeRuneInString(t)
		return t != "" && !classAcceptsControlled(e, r, control) && (!fold || !classAcceptsControlled(e, otherCase(r), control))
	case *ast.Bottom:
		return true
	case *ast.Seq:
		cur := t
		for _, it := range e.Items {
			control.poll()
			if a.fails(it, cur, fold) {
				return true
			}
			r := a.matchPrefix(it, cur, fold)
			if !r.ok || !r.exact {
				return false
			}
			cur = r.rest
		}
	case *ast.Choice:
		for _, alt := range e.Alts {
			control.poll()
			if !a.fails(alt, t, fold) {
				return false
			}
		}
		return true
	case *ast.Repeat:
		return e.Min > 0 && a.fails(e.Expr, t, fold)
	case *ast.Capture:
		return a.fails(e.Expr, t, fold)
	case *ast.Atomic:
		return a.fails(e.Expr, t, fold)
	case *ast.Discard:
		return a.fails(e.Expr, t, fold)
	case *ast.Attributed:
		return !hasAttr(e, "recover", control) && !hasAttr(e, "missing", control) && a.fails(e.Expr, t, fold)
	case *ast.Ref:
		r := a.rules[e.Name]
		if r == nil || e.Level != "" || a.busy[e.Name] {
			return false
		}
		if pr, _ := ast.RuleOperators(r.Expr); pr != nil {
			return false
		}
		a.busy[e.Name] = true
		defer delete(a.busy, e.Name)
		return a.fails(r.Expr, t, fold)
	}
	return false
}

// hasPrefixFold reports whether s begins with p when the ASCII letters of both are compared in
// lowercase, as for case-insensitive literals. It works on bytes, so invalid UTF-8 is kept as it is.
func hasPrefixFold(s, p string) bool {
	if len(p) > len(s) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if asciiLower(s[i]) != asciiLower(p[i]) {
			return false
		}
	}
	return true
}

// hasASCIILetter reports whether s has an ASCII letter, the only characters whose case folding
// changes.
func hasASCIILetter(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := asciiLower(s[i]); 'a' <= c && c <= 'z' {
			return true
		}
	}
	return false
}

// otherCase returns the other case of an ASCII letter, and any other character as it is.
func otherCase(r rune) rune {
	switch {
	case 'a' <= r && r <= 'z':
		return r - ('a' - 'A')
	case 'A' <= r && r <= 'Z':
		return r + ('a' - 'A')
	}
	return r
}

// asciiLower maps A-Z to a-z and leaves every other byte as it is.
func asciiLower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// prefixResult is the result of prefix.
type prefixResult struct {
	s string
	// exact reports that every match consumes exactly s.
	exact bool
	// fold reports that s is known only up to the case of its ASCII letters: every match begins with
	// s or with a string that differs from s only in the case of ASCII letters (and, if exact,
	// consumes exactly that string).
	fold bool
}

// prefix returns a string that every match of e begins with.
func (a *analysis) prefix(e ast.Expr) prefixResult {
	control := a.control
	control.poll()

	switch e := analysisExpr(e, control).(type) {
	case *ast.Literal:
		// Every match of a case-insensitive literal begins with some case of it.
		return prefixResult{s: e.Value, exact: true, fold: e.IgnoreCase && hasASCIILetter(e.Value)}
	case *ast.CharClass:
		if len(e.Ranges) == 1 && e.Ranges[0].Lo == e.Ranges[0].Hi && !e.Negated {
			return prefixResult{s: string(e.Ranges[0].Lo), exact: true}
		}
	case *ast.And, *ast.Not, *ast.Predicate, *ast.Cut, *ast.Top,
		*ast.BeginInput, *ast.EndInput, *ast.BeginLine, *ast.EndLine:
		return prefixResult{s: "", exact: true}
	case *ast.Seq:
		var b strings.Builder
		fold := false
		for _, it := range e.Items {
			control.poll()
			p := a.prefix(it)
			b.WriteString(p.s)
			fold = fold || p.fold
			if !p.exact {
				return prefixResult{s: b.String(), fold: fold}
			}
		}
		return prefixResult{s: b.String(), exact: true, fold: fold}
	case *ast.Choice:
		var out prefixResult
		first := true
		for _, alt := range e.Alts {
			control.poll()
			if !a.mayMatch(alt) {
				continue // contributes no matches
			}
			p := a.prefix(alt)
			if first {
				out, first = p, false
				continue
			}
			// Where either prefix is known only up to case, the common part is too.
			fold := out.fold || p.fold
			same := func(x, y byte) bool { return x == y || fold && asciiLower(x) == asciiLower(y) }
			n := 0
			for n < len(out.s) && n < len(p.s) && same(out.s[n], p.s[n]) {
				control.poll()
				n++
			}
			for n > 0 && n < len(out.s) && !utf8.RuneStart(out.s[n]) {
				control.poll()
				n-- // keep whole characters
			}
			whole := len(out.s) == len(p.s) && n == len(out.s)
			out = prefixResult{s: out.s[:n], exact: out.exact && p.exact && whole, fold: fold && hasASCIILetter(out.s[:n])}
		}
		return out
	case *ast.Repeat:
		if e.Min == 0 {
			return prefixResult{s: "", exact: e.Max == 0}
		}
		p := a.prefix(e.Expr)
		if !p.exact {
			return p
		}
		return prefixResult{s: strings.Repeat(p.s, e.Min), exact: e.Max == e.Min, fold: p.fold && e.Min > 0}
	case *ast.Capture:
		return a.prefix(e.Expr)
	case *ast.Atomic:
		return a.prefix(e.Expr)
	case *ast.Discard:
		return a.prefix(e.Expr)
	case *ast.Attributed:
		if !hasAttr(e, "recover", control) && !hasAttr(e, "missing", control) { // recovery invalidates a guaranteed prefix
			return a.prefix(e.Expr)
		}
	case *ast.Ref:
		r := a.rules[e.Name]
		if r == nil || e.Level != "" {
			break
		}
		if p, ok := a.prefixMemo[e.Name]; ok {
			return p
		}
		if a.busy[e.Name] {
			a.cycles++
			break
		}
		a.busy[e.Name] = true
		before := a.cycles
		p := a.prefix(r.Expr)
		delete(a.busy, e.Name)
		if a.cycles == before {
			a.prefixMemo[e.Name] = p
		}
		return p
	}
	return prefixResult{}
}

// classAccepts reports whether the character class matches r.
func classAccepts(c *ast.CharClass, r rune) bool {
	for _, rg := range c.Ranges {
		if rg.Lo <= r && r <= rg.Hi {
			return !c.Negated
		}
	}
	return c.Negated
}

// --- Structure ---

// neutral strips the wrappers that do not change whether an expression matches or how much
// input it consumes: #atomic, -, @error and #stream, and captures if caps is set.
func neutral(e ast.Expr, caps bool, control *lintControl) ast.Expr {
	for {
		control.poll()
		e = analysisExpr(e, control)
		switch x := e.(type) {
		case *ast.Atomic:
			e = x.Expr
		case *ast.Discard:
			e = x.Expr
		case *ast.Capture:
			if !caps {
				return e
			}
			e = x.Expr
		case *ast.Attributed:
			if hasAttr(x, "recover", control) {
				return e
			}
			e = x.Expr
		default:
			return e
		}
	}
}

// items returns the items of e as a sequence.
func items(e ast.Expr, caps bool, control *lintControl) []ast.Expr {
	if s, ok := neutral(e, caps, control).(*ast.Seq); ok {
		return s.Items
	}
	return []ast.Expr{e}
}

// same reports whether x and y match the same inputs in the same way, by comparing their
// structure. If caps is set, the names of captures are ignored.
func same(x, y ast.Expr, caps bool, control *lintControl) bool {
	control.poll()
	x, y = neutral(x, caps, control), neutral(y, caps, control)
	switch x := x.(type) {
	case *ast.Ref:
		y, ok := y.(*ast.Ref)
		return ok && x.Name == y.Name && x.Level == y.Level
	case *ast.Literal:
		y, ok := y.(*ast.Literal)
		return ok && x.Value == y.Value && x.IgnoreCase == y.IgnoreCase
	case *ast.CharClass:
		y, ok := y.(*ast.CharClass)
		return ok && x.Negated == y.Negated && sameRangesControlled(normalizeControlled(x.Ranges, control), normalizeControlled(y.Ranges, control), control)
	case *ast.Seq:
		y, ok := y.(*ast.Seq)
		return ok && sameList(x.Items, y.Items, caps, control)
	case *ast.Choice:
		y, ok := y.(*ast.Choice)
		return ok && sameList(x.Alts, y.Alts, caps, control)
	case *ast.Repeat:
		y, ok := y.(*ast.Repeat)
		return ok && x.Min == y.Min && x.Max == y.Max && same(x.Expr, y.Expr, caps, control)
	case *ast.Optional:
		y, ok := y.(*ast.Optional)
		return ok && same(x.Expr, y.Expr, caps, control)
	case *ast.And:
		y, ok := y.(*ast.And)
		return ok && same(x.Expr, y.Expr, caps, control)
	case *ast.Not:
		y, ok := y.(*ast.Not)
		return ok && same(x.Expr, y.Expr, caps, control)
	case *ast.Capture:
		y, ok := y.(*ast.Capture)
		return ok && x.Name == y.Name && same(x.Expr, y.Expr, caps, control)
	case *ast.Predicate:
		y, ok := y.(*ast.Predicate)
		return ok && control.formatActionExpr(x.Action) == control.formatActionExpr(y.Action)
	case *ast.Attributed:
		y, ok := y.(*ast.Attributed)
		return ok && control.formatExpr(x) == control.formatExpr(y) && same(x.Expr, y.Expr, caps, control)
	case *ast.Any, *ast.Cut, *ast.Top, *ast.Bottom, *ast.BeginInput, *ast.EndInput,
		*ast.BeginLine, *ast.EndLine:
		return sameType(x, y)
	}
	return false
}

func sameType(x, y ast.Expr) bool {
	switch x.(type) {
	case *ast.Any:
		_, ok := y.(*ast.Any)
		return ok
	case *ast.Cut:
		_, ok := y.(*ast.Cut)
		return ok
	case *ast.Top:
		_, ok := y.(*ast.Top)
		return ok
	case *ast.Bottom:
		_, ok := y.(*ast.Bottom)
		return ok
	case *ast.BeginInput:
		_, ok := y.(*ast.BeginInput)
		return ok
	case *ast.EndInput:
		_, ok := y.(*ast.EndInput)
		return ok
	case *ast.BeginLine:
		_, ok := y.(*ast.BeginLine)
		return ok
	case *ast.EndLine:
		_, ok := y.(*ast.EndLine)
		return ok
	}
	return false
}

func sameList(xs, ys []ast.Expr, caps bool, control *lintControl) bool {
	if len(xs) != len(ys) {
		return false
	}
	for i := range xs {
		control.poll()
		if !same(xs[i], ys[i], caps, control) {
			return false
		}
	}
	return true
}

// hasPredicate reports whether e contains a predicate (in the rule, not in the rules it calls).
func hasPredicate(e ast.Expr, control *lintControl) bool {
	found := false
	walkExpr(e, func(x ast.Expr) {
		control.poll()
		if _, ok := x.(*ast.Predicate); ok {
			found = true
		}
	}, control)
	return found
}

// --- Walking ---

// walkExpr visits the expression and its subexpressions in preorder, including attribute
// arguments and the parts of Operators Rule expressions, polling control, which may be nil
// (astinspect.WalkExprWithPoll). The checks walk only the rules of the grammar that the front end
// checked (frontend.Result), which is canonical: its inline actions are lowered to hidden rules,
// so an expression of it holds no inline action and each inline action is a rule the checks read
// on its own.
func walkExpr(e ast.Expr, f func(ast.Expr), control *lintControl) {
	astinspect.WalkExprWithPoll(e, f, control.pollFunc())
}

// calls returns the rules that e calls, in order of appearance.
func calls(e ast.Expr) []string {
	return callsControlled(e, nil)
}

func callsControlled(e ast.Expr, control *lintControl) []string {
	control.poll()

	var out []string
	walkExpr(e, func(x ast.Expr) {
		control.poll()
		if r, ok := x.(*ast.Ref); ok {
			out = append(out, r.Name)
		}
	}, control)
	return out
}

// posOf returns the position where e begins, or the zero Pos if it is unknown.
func posOf(e ast.Expr) ast.Pos { return posOfControlled(e, nil) }

func posOfControlled(e ast.Expr, control *lintControl) ast.Pos {
	control.poll()
	switch e := analysisExpr(e, control).(type) {
	case *ast.Ref:
		return e.Diagnostic()
	case *ast.Literal:
		return e.Diagnostic()
	case *ast.CharClass:
		return e.Diagnostic()
	case *ast.Any:
		return e.Diagnostic()
	case *ast.Seq:
		if len(e.Items) > 0 {
			return posOfControlled(e.Items[0], control)
		}
	case *ast.Choice:
		if len(e.Alts) > 0 {
			return posOfControlled(e.Alts[0], control)
		}
	case *ast.Repeat:
		return e.Diagnostic()
	case *ast.Optional:
		return e.Diagnostic()
	case *ast.And:
		return e.Diagnostic()
	case *ast.Not:
		return e.Diagnostic()
	case *ast.Atomic:
		return e.Diagnostic()
	case *ast.Discard:
		return e.Diagnostic()
	case *ast.Capture:
		return e.Diagnostic()
	case *ast.Cut:
		return e.Diagnostic()
	case *ast.Top:
		return e.Diagnostic()
	case *ast.Bottom:
		return e.Diagnostic()
	case *ast.BeginInput:
		return e.Diagnostic()
	case *ast.EndInput:
		return e.Diagnostic()
	case *ast.BeginLine:
		return e.Diagnostic()
	case *ast.EndLine:
		return e.Diagnostic()
	case *ast.Predicate:
		return e.Diagnostic()
	case *ast.Attributed:
		return posOfControlled(e.Expr, control)
	case *ast.OperatorsRule:
		return e.Diagnostic()
	}
	return ast.Pos{}
}

// show formats e for a message, shortened if it is long.
func show(e ast.Expr, control *lintControl) string {
	s := control.formatExpr(e)
	// Keep only the displayed prefix and a bounded lookahead. The rune
	// conversion preserves the old normalization of invalid UTF-8 even in
	// caller-built ASTs that have not passed validation.
	var prefix [37]rune
	count := 0
	for _, r := range s {
		if count == 40 {
			return "`" + string(prefix[:]) + "...`"
		}
		if count < len(prefix) {
			prefix[count] = r
		}
		count++
	}
	return "`" + s + "`"
}

// analysisExpr presents behavior pragmas to the existing matching proofs without
// mutating the source tree or losing the wrapped expression's positions.
func analysisExpr(e ast.Expr, control *lintControl) ast.Expr {
	switch x := e.(type) {
	case *ast.Group:
		control.poll()
		return analysisExpr(x.Expr, control)
	case *ast.Stream:
		return &ast.Attributed{Expr: x.Expr, Attrs: []*ast.Attribute{{Pos: x.Diagnostic(), Name: "stream", Pragma: true}}}
	case *ast.Recovery:
		return &ast.Attributed{Expr: x.Expr, Attrs: []*ast.Attribute{{Pos: x.Diagnostic(), Name: "recover", Pragma: true, KwArgs: map[string]any{"skip": x.Skip}}}}
	}
	return e
}
