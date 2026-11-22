package core

import (
	"fmt"
	"slices"
	"unicode/utf8"
)

// Analyze computes Nullable, NullableIns, First and Eff of every node of the module from the
// rules' bodies. Want and Shape are left as built. The rule references of the module must be valid.
//
// Nullable, NullableIns and the effects of calls are the least fixed point over the rules. First follows the
// first item of sequences, the bodies of captures, Text, Drop, Nest and Terminal, the first class
// of a class run and calls of rules that are neither left-recursion leaders nor Operators Rules,
// counting one level for each call of a recursive rule on the way (a Nest is of a rule that is not
// recursive).
func Analyze(m *Module) {
	newAnalyzer(m, nil).run()
}

// CheckAttributes reports whether the attributes that Analyze computes, Nullable, NullableIns,
// First and Eff, are those of every node of m. It computes them into a structure of its own and
// compares, so it never writes m: a verifier of a Core module checks the attributes that the
// builder and the passes left, as the rest of the module.
func CheckAttributes(m *Module) error {
	s := newSide(m)
	newAnalyzer(m, s).run()
	for _, e := range s.nodes {
		at := e.n.Attrs()
		if e.eff && (at.Nullable != e.null || at.NullableIns != e.ins || !sameEffects(at.Eff, e.e)) {
			return fmt.Errorf("rule %s: node %d: nullable=%v nullable-ins=%v effects [%s], computed nullable=%v nullable-ins=%v effects [%s]",
				ruleName(m, e.rule), at.ID, at.Nullable, at.NullableIns, effectString(at.Eff), e.null, e.ins, effectString(e.e))
		}
		if e.first && !sameFirst(at.First, e.f) {
			return fmt.Errorf("rule %s: node %d: first %s, computed %s", ruleName(m, e.rule), at.ID, firstOrNone(m, at.First), firstOrNone(m, e.f))
		}
	}
	return nil
}

// side holds the attributes that CheckAttributes computes, by node, in the order the nodes are first
// reached. The nodes of a module are numbered (Number) and a pass gives a node it adds the ID of the
// node it replaces, so the ID of a node finds its entry (byID, the index of the entry plus one) and
// no node is hashed; a node without an ID of its own, such as one of a module that was never
// numbered, has its entry in other.
type side struct {
	byID  []int32
	other map[Node]int
	nodes []sideAttrs
}

func newSide(m *Module) *side { return &side{byID: make([]int32, m.LastID+1)} }

// sideAttrs holds the computed attributes of one node: eff reports that null, ins and e are
// computed, first that f is.
type sideAttrs struct {
	n          Node
	rule       int
	eff, first bool
	null, ins  bool
	e          Effects
	f          *First
}

func (s *side) of(n Node, rule int) *sideAttrs {
	id := n.Attrs().ID
	if id > 0 && int(id) < len(s.byID) {
		if i := s.byID[id]; i == 0 {
			s.byID[id] = int32(len(s.nodes) + 1)
			s.nodes = append(s.nodes, sideAttrs{n: n, rule: rule})
			return &s.nodes[len(s.nodes)-1]
		} else if s.nodes[i-1].n == n {
			return &s.nodes[i-1]
		}
	}
	i, ok := s.other[n]
	if !ok {
		if s.other == nil {
			s.other = map[Node]int{}
		}
		i = len(s.nodes)
		s.other[n] = i
		s.nodes = append(s.nodes, sideAttrs{n: n, rule: rule})
	}
	return &s.nodes[i]
}

func ruleName(m *Module, id int) string {
	if id >= 0 && id < len(m.Rules) && m.Rules[id] != nil {
		return m.Rules[id].Name
	}
	return fmt.Sprint(id)
}

func firstOrNone(m *Module, f *First) string {
	if f == nil {
		return "none"
	}
	return FirstString(m, f)
}

func sameEffects(x, y Effects) bool { return x.Set == y.Set && slices.Equal(x.Slots, y.Slots) }

func sameFirst(x, y *First) bool { return x == nil && y == nil || x != nil && y != nil && *x == *y }

func newAnalyzer(m *Module, s *side) *analyzer {
	a := &analyzer{m: m, side: s, null: make([]bool, len(m.Rules)), ins: make([]bool, len(m.Rules)), eff: make([]Effect, len(m.Rules)),
		start: make([]bool, len(m.Rules))}
	a.sub = a.nullable
	a.rule = func(id int) (bool, bool) { return a.null[id], a.ins[id] }
	return a
}

// run computes the attributes of every node of the module.
func (a *analyzer) run() {
	m := a.m
	for _, s := range m.Starts {
		a.start[s] = true
	}
	for changed := true; changed; {
		changed = false
		for _, r := range m.Rules {
			m.Control.Poll()
			if r == nil {
				continue
			}
			a.cur = r.ID
			n, in, e := a.node(r.Body)
			if r.Finish.Kind == FinishAction && Raises(r.Finish.Action) {
				e.Set |= EffRaise
			}
			s := e.Set &^ (EffCapture | EffBind | EffCut)
			if n != a.null[r.ID] || in != a.ins[r.ID] || s != a.eff[r.ID] {
				a.null[r.ID], a.ins[r.ID], a.eff[r.ID] = n, in, s
				changed = true
			}
		}
	}
	a.first = make([]*First, len(m.Rules))
	a.state = make([]uint8, len(m.Rules))
	for _, r := range m.Rules {
		m.Control.Poll()
		if r != nil {
			a.cur = r.ID
			a.firsts(r.Body)
		}
	}
}

type analyzer struct {
	m *Module
	// side, if not nil, receives the attributes in place of the nodes (CheckAttributes).
	side  *side
	null  []bool
	ins   []bool
	eff   []Effect
	first []*First
	// state is 0 for a rule whose First is not computed, 1 while it is computed, 2 after.
	state []uint8
	// start holds the start rules, and cur is the rule whose body is analyzed.
	start []bool
	cur   int
	// sub reads the nullabilities that node computed for a subexpression, and rule those of a rule,
	// for Nullability.
	sub  func(Node) (bool, bool)
	rule func(int) (bool, bool)
}

// node sets the Nullable, NullableIns and Eff attributes of n and its subexpressions and returns
// them. The nullabilities follow Nullability, from those of the subexpressions that node computed
// first.
func (a *analyzer) node(n Node) (bool, bool, Effects) {
	if n == nil {
		return true, true, Effects{}
	}
	var e Effects
	const local = EffCapture | EffBind | EffCut
	switch n := n.(type) {
	case *Lit:
		if n.Text != "" {
			e.Set = EffConsume | EffFail | EffExpect
		}
	case *ClassNode, *Any:
		e.Set = EffConsume | EffFail | EffExpect
	case *Anchor:
		e.Set = EffFail | EffExpect
	case *Seq:
		for _, it := range n.Items {
			_, _, ee := a.node(it)
			e = union(e, ee)
		}
	case *Choice:
		for _, alt := range n.Alts {
			_, _, ee := a.node(alt.Body)
			e = union(e, ee)
		}
		e.Set &^= EffCut
	case *Opt:
		_, _, ee := a.node(n.Body)
		e = ee
		// An option fails only when its body fails after a cut that commits it.
		if ee.Set&EffCut == 0 {
			e.Set &^= EffFail
		}
		e.Set &^= EffCut
	case *Repeat:
		_, _, ee := a.node(n.Body)
		e = ee
		if n.Scope != nil {
			// The element's captures are its own; its bindings are the rule's, as everywhere
			// outside a rule call.
			e = Effects{Set: e.Set &^ EffCapture}
		}
		// A repetition without a minimum fails only when an iteration fails after a cut that
		// commits it.
		if n.Min == 0 && ee.Set&EffCut == 0 {
			e.Set &^= EffFail
		}
		e.Set &^= EffCut
	case *Look:
		_, _, ee := a.node(n.Body)
		e = ee
		// The end of a lookahead discards the errors recovered inside it.
		e.Set &^= EffConsume | EffExpect | EffCut | EffRecover
		e.Set |= EffFail
	case *Cut:
		e.Set = EffCut
	case *Succeed:
	case *FailNode:
		e.Set = EffFail
	case *Call:
		// A call of a recursive rule can exceed the nesting limit.
		e.Set = a.eff[n.Rule] | EffCall
		if a.m.Rules[n.Rule].Policy.Rec {
			e.Set |= EffRaise
		}
	case *Nest:
		_, _, ee := a.node(n.Body)
		e.Set = ee.Set&^local | EffCall
	case *OperatorsRule:
		for _, l := range n.Lines() {
			_, _, ee := a.node(l.Body)
			e.Set |= ee.Set &^ local
			if l.Action != nil && Raises(l.Action) {
				e.Set |= EffRaise
			}
		}
		e.Set |= EffConsume | EffFail | EffExpect | EffCall | EffRaise
	case *Text:
		_, _, e = a.node(n.Body)
	case *Drop:
		_, _, e = a.node(n.Body)
	case *Capture:
		_, _, e = a.node(n.Body)
		e = union(Effects{Set: EffCapture, Slots: []int{n.Slot}}, e)
	case *Pred:
		e.Set = EffFail
		if Raises(n.Action) {
			e.Set |= EffRaise
		}
	case *BindNode:
		e.Set = EffBind
		if Raises(n.Action) {
			e.Set |= EffRaise
		}
	case *Label:
		_, _, e = a.node(n.Body)
	case *Recover:
		_, _, eb := a.node(n.Body)
		nullSkip, _, es := a.node(n.Skip)
		e = union(eb, es)
		// A recovery fails when its body fails and its skip fails or consumes nothing.
		if eb.Set&EffFail == 0 || es.Set&EffFail == 0 && !nullSkip {
			e.Set &^= EffFail
		}
		e.Set |= EffRecover
	case *Missing:
		// An insertion consumes no input; without insertion the literal must match, and with it the
		// literal fails where it was already inserted at the same position. An insertion can exceed
		// the limit of missing tokens.
		a.node(n.Body)
		e.Set = EffConsume | EffFail | EffExpect | EffRecover | EffRaise
	case *Stream:
		_, _, e = a.node(n.Repeat)
		// Only the invocation of a start rule that starts a stream parse delivers elements.
		if a.start[a.cur] {
			e.Set |= EffCommit
		}
	case *Scan:
		// A scan records the expectation of the character where it stops.
		e.Set = EffConsume | EffExpect
		if n.Min > 0 {
			e.Set |= EffFail
		}
	case *ClassRun:
		e.Set = EffConsume | EffFail | EffExpect
	case *Terminal:
		_, _, e = a.node(n.Body)
	}
	null, ins := Nullability(n, a.sub, a.rule)
	if a.side != nil {
		at := a.side.of(n, a.cur)
		at.eff, at.null, at.ins, at.e = true, null, ins, e
	} else {
		at := n.Attrs()
		at.Nullable, at.NullableIns, at.Eff = null, ins, e
	}
	return null, ins, e
}

// Nullability returns the nullabilities of n, without token insertion and with it, by the rule of
// spec/core-ir/attributes.md (Nullability) for the kind of n. sub returns them for the nodes n is
// made of, and rule for the rule that a Call names. It is the one statement of that rule: Analyze
// applies it to the nodes of a module, and the facts of a module (internal/middleend/corepass) to
// nodes that Analyze did not see.
func Nullability(n Node, sub func(Node) (bool, bool), rule func(id int) (bool, bool)) (null, ins bool) {
	switch n := n.(type) {
	case nil:
		return true, true
	case *Lit:
		return n.Text == "", n.Text == ""
	case *Scan:
		return n.Min == 0, n.Min == 0
	case *ClassNode, *Any, *FailNode, *ClassRun:
		return false, false
	case *Anchor, *Opt, *Look, *Cut, *Succeed, *Pred, *BindNode:
		return true, true
	case *Seq:
		null, ins = true, true
		for _, it := range n.Items {
			nn, ni := sub(it)
			null, ins = null && nn, ins && ni
			if !null && !ins {
				break
			}
		}
		return null, ins
	case *Choice:
		for _, alt := range n.Alts {
			nn, ni := sub(alt.Body)
			null, ins = null || nn, ins || ni
			if null && ins {
				break
			}
		}
		return null, ins
	case *Repeat:
		if n.Min == 0 {
			return true, true
		}
		if n.Min > 1 {
			return false, false
		}
		return sub(n.Body)
	case *Call:
		return rule(n.Rule)
	case *OperatorsRule:
		for _, l := range n.Operands {
			nn, ni := sub(l.Body)
			null, ins = null || nn, ins || ni
			if null && ins {
				break
			}
		}
		return null, ins
	case *Missing:
		// An insertion consumes no input.
		null, _ = sub(n.Body)
		return null, true
	case *Stream:
		return sub(n.Repeat)
	case *Recover:
		// When the body fails, a skip that consumes nothing is no recovery, so the node succeeds
		// without consuming input only through its body.
		return sub(n.Body)
	case *Nest:
		return sub(n.Body)
	case *Text:
		return sub(n.Body)
	case *Drop:
		return sub(n.Body)
	case *Capture:
		return sub(n.Body)
	case *Label:
		return sub(n.Body)
	case *Terminal:
		return sub(n.Body)
	}
	panic(fmt.Sprintf("core: nullability of %T", n))
}

// nullable returns the nullabilities of n, which the analysis has computed.
func (a *analyzer) nullable(n Node) (bool, bool) {
	if n == nil {
		return true, true
	}
	if a.side != nil {
		at := a.side.of(n, a.cur)
		return at.null, at.ins
	}
	at := n.Attrs()
	return at.Nullable, at.NullableIns
}

// union returns the union of two effect summaries, with the slots of x first.
func union(x, y Effects) Effects {
	s := Effects{Set: x.Set | y.Set, Slots: x.Slots}
	for _, v := range y.Slots {
		if !hasSlot(s.Slots, v) {
			s.Slots = append(s.Slots[:len(s.Slots):len(s.Slots)], v)
		}
	}
	return s
}

func hasSlot(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// firsts sets the First attribute of n and its subexpressions.
func (a *analyzer) firsts(n Node) {
	if n == nil {
		return
	}
	if o, ok := n.(*OperatorsRule); ok {
		for _, l := range o.Lines() {
			a.firsts(l.Body)
		}
	}
	for _, c := range Children(n) {
		a.firsts(c)
	}
	if a.side != nil {
		at := a.side.of(n, a.cur)
		at.first, at.f = true, a.head(n)
	} else {
		n.Attrs().First = a.head(n)
	}
}

// head returns the First of n, following only the expressions n must begin with, so that a rule
// is entered only where its call can be the first thing a body matches.
func (a *analyzer) head(n Node) *First {
	switch n := n.(type) {
	case *Lit:
		if n.Text != "" {
			r, _ := utf8.DecodeRuneInString(n.Text)
			if n.Fold && ('A' <= r && r <= 'Z' || 'a' <= r && r <= 'z') {
				// Only ASCII letters fold: the first character is either case of r.
				return &First{Lit: true, Fold: true, Rune: r | 0x20, Exp: n.Exp}
			}
			return &First{Lit: true, Rune: r, Exp: n.Exp}
		}
	case *ClassNode:
		return &First{Class: n.Class, Exp: n.Class.Exp}
	case *Seq:
		if len(n.Items) > 0 {
			return a.head(n.Items[0])
		}
	case *Capture:
		return a.head(n.Body)
	case *Text:
		return a.head(n.Body)
	case *Drop:
		return a.head(n.Body)
	case *Call:
		f := a.ruleFirst(n.Rule)
		if a.m.Rules[n.Rule].Policy.Rec {
			f = deeper(f)
		}
		return f
	case *Nest:
		return a.head(n.Body)
	case *Terminal:
		return a.head(n.Body)
	case *ClassRun:
		return &First{Class: n.Classes[0], Exp: n.Classes[0].Exp}
	}
	return nil
}

// ruleFirst returns the First of a rule's body, nil for a leader, an Operators Rule or a rule
// whose First is being computed.
func (a *analyzer) ruleFirst(id int) *First {
	r := a.m.Rules[id]
	if r.Policy.Leader {
		return nil
	}
	if _, ok := r.Body.(*OperatorsRule); ok {
		return nil
	}
	switch a.state[id] {
	case 1:
		return nil
	case 2:
		return a.first[id]
	}
	a.state[id] = 1
	a.first[id] = a.head(r.Body)
	a.state[id] = 2
	return a.first[id]
}

func deeper(f *First) *First {
	if f == nil {
		return nil
	}
	g := *f
	g.Depth++
	return &g
}
