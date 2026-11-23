package pipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"syntaxir.dev/sxir/internal/pass"
)

// Run is the manager of one compilation.
type Run struct {
	Settings   pass.Settings
	Observers  []Observer
	VerifyEach bool              // a manager policy: verify after every pass, check the pass contract
	NoShare    bool              // run every use of a Shared pipeline (for tests that compare)
	Recover    func(x any) error // turns a cancellation panic of a step into an error; nil re-panics
	Keep       bool              // the caller keeps the input of Execute: the run changes a copy
	Stop       bool              // the first failure breaks the run: no rollback, so no rollback copy
	// Numbering, if set, numbers the runs of this Execute (Event.Seq) after those of the other
	// Executes that share it; nil numbers them from 1.
	Numbering *Numbering
}

// Numbering is the numbering of the decided runs of optional passes (Event.Seq) of the Executes that
// share it, so that one compilation, which still runs one Execute per stage and table, numbers them
// once, in the order they run. The Executes that share it run one after another. It becomes internal
// once one Execute runs the whole compilation (record 074, migration step 6).
type Numbering struct{ last int }

// Needs says what an observer reads, so the manager computes only that.
type Needs struct {
	Before      bool // the dump of the input (Before's in)
	TextChanged bool // Result.TextChanged: the text form before and after a transform compared
	Units       bool // Result.Unit (typed test hooks); disables sharing
	Veto        bool // Before may return false (bisection); disables sharing
	Print       bool // Result.Print (dumps); without it no step prints its unit
}

// Event is one step on one unit.
type Event struct {
	Seq      int // the number of a decided run of an optional pass, from 1 (Run.Numbering); 0 for a required step
	Step     StepInfo
	Label    Label
	Replayed bool // the event of a Shared result that was reused
}

// StepInfo describes a step whatever its types.
type StepInfo struct {
	Name, Instance string // instance: the name, with #k for the k-th occurrence (k > 1)
	Required       bool
	// Pattern marks the decision of one application of a pattern (Ctx.Allow): Name is the pattern
	// and Instance the instance of the pass followed by "/" and the pattern.
	Pattern bool
	In, Out string
}

// Result is what an observer learns after a step.
type Result struct {
	Ran         bool // false: an observer vetoed the run
	Changed     bool // the pass's report (!PreserveAll), whatever observers watch
	TextChanged bool // only under Needs.TextChanged: the text form changed (a lowering: Changed)
	Counters    map[string]int
	// Patterns holds the decisions of the pass's patterns in this run, by pattern; nil if it asked
	// about none.
	Patterns map[string]Decisions
	Elapsed  time.Duration
	Unit     any             // only if an observer needs units
	Print    func(io.Writer) // only if an observer needs dumps; valid during After only
	Err      error
}

// Decisions counts the answers to the applications of one pattern: Applied ran, and Skipped were
// refused, by the pattern's flag or by an observer.
type Decisions struct{ Applied, Skipped int }

// An Observer watches every step, required ones included.
type Observer interface {
	Needs() Needs
	Before(e Event, in func(io.Writer)) bool
	After(e Event, r *Result)
}

type env struct {
	label    Label
	features map[FeatureID]any
}

func (e *env) with(f FeatureID, v any) *env {
	m := map[FeatureID]any{}
	for k, x := range e.features {
		m[k] = x
	}
	m[f] = v
	return &env{label: e.label, features: m}
}

type scope struct {
	err    error
	broken bool
	keep   bool     // the unit of this scope is kept by someone else: copy before changing it
	seg    *segment // the segment of transforms running on the unit of this scope
	// lazy, if set, gives the unit that the first pass to run takes in place of the one it was
	// given: a PerFunc segment copies its whole unit only when a pass runs (perFunc).
	lazy func() any
}

// segment is a run of consecutive transforms of one kind on one unit. The manager copies the unit
// once per segment, before the first pass that runs, when the start must survive: the unit is kept
// (keep), or a failure must roll back to it (the failure rule, unless Run.Stop).
type segment struct {
	k     *kind
	start any // the unit the segment started on
	rb    any // the unit before the first optional pass that ran: the rollback point
}

func (s *scope) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}

type recorded struct {
	e        Event
	before   string
	r        Result
	print    string
	hasPrint bool // the live event carried a dump (Result.Print set)
}

type sharedEntry struct {
	label  Label // the label of the use that ran the body
	in     any   // the input of that use, which Key.Agree compares a later one with
	out    any
	err    error
	broken bool
	events []recorded
}

type runner struct {
	ctx       context.Context
	r         *Run
	needs     Needs
	seq       *int // the last Event.Seq given (Run.Numbering)
	instances map[*passNode]string
	shared    map[string]*sharedEntry
	recs      []*[]recorded
	ran       int               // optional transforms that ran, for verification where a stage ends
	changed   int               // steps that ran, for discarding the analyses of a unit whose part a step changed
	copies    int               // the copies the manager took, for the tests of the copy policy
	analyses  map[any]*analyses // by unit
	parts     map[any][]any     // the parts of a unit (PerFunc functions, Within parts) that hold analyses
}

// copy takes a copy of a unit of the kind k.
func (rs *runner) copy(k *kind, u any) any {
	rs.copies++
	return k.copy(u)
}

// Execute checks p and runs it on in. An invalid pipeline fails before any step runs.
func Execute[I, O any](ctx context.Context, r *Run, p Pipeline[I, O], in I) (O, error) {
	var zero O
	root, ins := instantiate(p.n)
	rs := &runner{ctx: ctx, r: r, seq: new(int), instances: ins, shared: map[string]*sharedEntry{}, analyses: map[any]*analyses{}}
	if r.Numbering != nil {
		rs.seq = &r.Numbering.last
	}
	for _, o := range r.Observers {
		n := o.Needs()
		rs.needs.Before = rs.needs.Before || n.Before
		rs.needs.TextChanged = rs.needs.TextChanged || n.TextChanged
		rs.needs.Units = rs.needs.Units || n.Units
		rs.needs.Veto = rs.needs.Veto || n.Veto
		rs.needs.Print = rs.needs.Print || n.Print
	}
	if err := check(root, map[FeatureID]bool{}, map[string]bool{}); err != nil {
		return zero, err
	}
	if err := checkShape(root, nil); err != nil {
		return zero, err
	}
	sc := scope{keep: r.Keep}
	out := rs.exec(root, in, &env{features: map[FeatureID]any{}}, &sc)
	if testHook != nil {
		testHook(rs)
	}
	if sc.broken {
		return zero, sc.err
	}
	return out.(O), sc.err
}

// instantiate copies the tree so that every occurrence of a pass is a node of its own (a pipeline
// value may be used in several places), and names the occurrences: the k-th occurrence of a name on
// one path of execution is name#k (k > 1). The branches of a Fork and the alternative bodies of a
// slot are numbered apart, since no unit runs through two of them. The copy is in the normal form
// (normalize), so a tree runs and prints the same whichever door built it.
func instantiate(n node) (node, map[*passNode]string) {
	n = normalize(rewrite(n, func(x node) node {
		if p, ok := x.(*passNode); ok {
			return &passNode{p: p.p, params: p.params}
		}
		return x
	}))
	m := map[*passNode]string{}
	name(n, map[string]int{}, m)
	return n, m
}

func name(n node, count map[string]int, m map[*passNode]string) {
	copyOf := func() map[string]int {
		c := map[string]int{}
		for k, v := range count {
			c[k] = v
		}
		return c
	}
	switch x := n.(type) {
	case *passNode:
		count[x.p.Name]++
		m[x] = x.p.Name
		if c := count[x.p.Name]; c > 1 {
			m[x] = fmt.Sprintf("%s#%d", x.p.Name, c)
		}
	case *forkNode:
		for _, b := range x.branches {
			name(b, copyOf(), m)
		}
	case *slotNode:
		alts := []node{x.def}
		for _, k := range x.order {
			alts = append(alts, x.parts[k])
		}
		max := map[string]int{}
		for _, a := range alts {
			c := copyOf()
			name(a, c, m)
			for k, v := range c {
				if v > max[k] {
					max[k] = v
				}
			}
		}
		for k, v := range max {
			count[k] = v
		}
	case *withNode:
		name(x.body, count, m)
	default:
		children(n, func(c node) { name(c, count, m) })
	}
}

// walk visits the tree in execution order (a slot's default first, then its parts).
func walk(n node, f func(node)) {
	f(n)
	switch x := n.(type) {
	case *seqNode:
		for _, s := range x.steps {
			walk(s, f)
		}
	case *chainNode:
		for _, s := range x.steps {
			walk(s, f)
		}
	case *perFuncNode:
		walk(x.body, f)
	case *eachNode:
		walk(x.body, f)
	case *withinNode:
		walk(x.body, f)
	case *sharedNode:
		walk(x.body, f)
	case *withNode:
		walk(x.body, f)
	case *slotNode:
		walk(x.def, f)
		for _, k := range x.order {
			walk(x.parts[k], f)
		}
	case *forkNode:
		for _, b := range x.branches {
			walk(b, f)
		}
	}
}

// check validates what types cannot: provided features, After order and PerFunc bodies. seen holds
// the passes before n in its sequence scope.
func check(n node, provided map[FeatureID]bool, seen map[string]bool) error {
	switch x := n.(type) {
	case *passNode:
		if err := checkFlags(x.p); err != nil {
			return err
		}
		for _, f := range x.p.Features {
			if !provided[f] {
				return fmt.Errorf("%w: pass %s reads the feature %s, which no enclosing scope provides", errCheck, x.p.Name, f)
			}
		}
		for _, a := range x.p.After {
			if !seen[a] {
				return fmt.Errorf("%w: pass %s must follow %s", errCheck, x.p.Name, a)
			}
		}
		seen[x.p.Name] = true
	case *perFuncNode:
		if err := flatPasses(x.body); err != nil {
			return err
		}
	case *eachNode:
		for _, f := range x.split.features {
			if !provided[f] {
				return fmt.Errorf("%w: split %s reads the feature %s, which no enclosing scope provides", errCheck, x.split.name, f)
			}
		}
	case *withNode:
		p := map[FeatureID]bool{x.prov.feature: true}
		for k := range provided {
			p[k] = true
		}
		return check(x.body, p, seen)
	case *slotNode:
		if err := check(x.def, provided, cloneSet(seen)); err != nil {
			return err
		}
		for _, k := range x.order {
			if err := check(x.parts[k], provided, cloneSet(seen)); err != nil {
				return err
			}
		}
		return check(x.def, provided, seen)
	case *forkNode:
		for _, b := range x.branches {
			if err := check(b, provided, cloneSet(seen)); err != nil {
				return err
			}
		}
		return nil
	}
	var err error
	children(n, func(c node) {
		if err == nil {
			err = check(c, provided, seen)
		}
	})
	return err
}

// checkFlags reports a flag that the pass reads and that the registry does not know: the flag of an
// optional pass that is no test pass, of a pattern or of a parameter. Without the check the run would
// panic at the first step that reads it.
func checkFlags(d *passDesc) error {
	var want []string
	if !d.Required && !d.Test {
		want = append(want, d.Name)
	}
	want = append(append(want, d.Patterns...), d.Params...)
	for _, f := range want {
		if _, ok := pass.Lookup(f); !ok {
			return fmt.Errorf("%w: pass %s reads the flag %s, which is not registered", errCheck, d.Name, f)
		}
	}
	return nil
}

// checkShape validates what the text door could get wrong and the typed door cannot, so that both
// doors are held to it before any step runs: a Shared key and a derived provider read the kind of
// unit their block starts on; every body of a slot for a part has the required skeleton of the
// slot's default body (only optional passes differ per part); and a part label names a label of the
// split of the nearest enclosing Each (labels: nil outside any Each).
func checkShape(n node, labels []string) error {
	switch x := n.(type) {
	case *sharedNode:
		if a, _ := x.body.kinds(); a != x.key.k {
			return fmt.Errorf("%w: shared key %s reads %s, but its block starts on %s", errCheck, x.key.name, x.key.k.name, a.name)
		}
	case *withNode:
		if a, _ := x.body.kinds(); x.prov.in != nil && a != x.prov.in {
			return fmt.Errorf("%w: provider %s reads %s, but its block starts on %s", errCheck, x.prov.name, x.prov.in.name, a.name)
		}
	case *eachNode:
		return checkShape(x.body, x.split.labels)
	case *slotNode:
		want := x.skel
		if got := skeleton(x.def); got != want {
			return fmt.Errorf("%w: the body of slot %s changes the required steps (%q, want %q)", errCheck, x.slot.name, got, want)
		}
		for _, k := range x.order {
			if labels == nil {
				return fmt.Errorf("%w: slot %s has a body for the part %s outside any each", errCheck, x.slot.name, k)
			}
			if !contains(labels, k) {
				return fmt.Errorf("%w: slot %s has a body for the part %s, which the split does not give (%s)", errCheck, x.slot.name, k, strings.Join(labels, ", "))
			}
			if got := skeleton(x.parts[k]); got != want {
				return fmt.Errorf("%w: the body of slot %s for %s changes the required steps (%q, want %q)", errCheck, x.slot.name, k, got, want)
			}
			if err := checkShape(x.parts[k], labels); err != nil {
				return err
			}
		}
		return checkShape(x.def, labels)
	case *forkNode:
		for _, b := range x.branches {
			if err := checkShape(b, labels); err != nil {
				return err
			}
		}
		return nil
	}
	var err error
	children(n, func(c node) {
		if err == nil {
			err = checkShape(c, labels)
		}
	})
	if w, ok := n.(*withNode); ok && err == nil {
		err = checkShape(w.body, labels)
	}
	return err
}

func children(n node, f func(node)) {
	switch x := n.(type) {
	case *seqNode:
		for _, s := range x.steps {
			f(s)
		}
	case *chainNode:
		for _, s := range x.steps {
			f(s)
		}
	case *perFuncNode:
		f(x.body)
	case *eachNode:
		f(x.body)
	case *withinNode:
		f(x.body)
	case *sharedNode:
		f(x.body)
	}
}

func cloneSet(m map[string]bool) map[string]bool {
	c := map[string]bool{}
	for k, v := range m {
		c[k] = v
	}
	return c
}

func flatPasses(n node) error {
	switch x := n.(type) {
	case *passNode:
		if x.p.in != x.p.out {
			return fmt.Errorf("%w: PerFunc runs transforms only, and %s changes the kind", errCheck, x.p.Name)
		}
		return nil
	case *seqNode, *chainNode:
		var err error
		children(n, func(c node) {
			if err == nil {
				err = flatPasses(c)
			}
		})
		return err
	case *slotNode:
		if err := flatPasses(x.def); err != nil {
			return err
		}
		for _, k := range x.order {
			if err := flatPasses(x.parts[k]); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("%w: PerFunc runs a sequence of passes and slots only", errCheck)
}

// flatten returns the passes of a PerFunc body for the part labeled table.
func flatten(n node, table string) []*passNode {
	switch x := n.(type) {
	case *passNode:
		return []*passNode{x}
	case *slotNode:
		if b, ok := x.parts[table]; ok {
			return flatten(b, table)
		}
		return flatten(x.def, table)
	}
	var out []*passNode
	children(n, func(c node) { out = append(out, flatten(c, table)...) })
	return out
}

func (rs *runner) exec(n node, in any, e *env, sc *scope) any {
	if sc.broken {
		return nil
	}
	switch x := n.(type) {
	case *passNode:
		return rs.step(x, in, e, sc)
	case *seqNode:
		return rs.chain(x.steps, in, e, sc)
	case *chainNode:
		return rs.chain(x.steps, in, e, sc)
	case *slotNode:
		if b, ok := x.parts[e.label.Table]; ok {
			return rs.exec(b, in, e, sc)
		}
		return rs.exec(x.def, in, e, sc)
	case *withNode:
		return rs.exec(x.body, in, e.with(x.prov.feature, x.prov.value(in)), sc)
	case *withinNode:
		// The part runs in a scope of its own: it is kept if the whole is (the part of a kept unit),
		// and its copy does not end the keeping of the whole.
		sc.seg = nil
		ps := &scope{err: sc.err, keep: sc.keep}
		pre := rs.changed
		q := rs.exec(x.body, x.lens.get(in), e, ps)
		if ps.err != nil && sc.err == nil {
			sc.fail(ps.err)
		}
		if ps.broken {
			sc.broken = true
			return nil
		}
		if rs.changed != pre {
			rs.drop(in)
		}
		out, err := x.lens.put(in, q)
		if err != nil {
			sc.fail(&StepError{Step: "within " + x.lens.name, Label: e.label, Err: err})
			sc.broken = true
			return nil
		}
		rs.own(out, q)
		return out
	case *perFuncNode:
		sc.seg = nil
		return rs.perFunc(x, in, e, sc)
	case *eachNode:
		sc.seg = nil
		return rs.each(x, in, e, sc)
	case *sharedNode:
		sc.seg = nil
		return rs.sharedRun(x, in, e, sc)
	case *forkNode:
		sc.seg = nil
		return rs.fork(x, in, e, sc)
	}
	panic("unknown node")
}

// chain runs steps in order. Where a run of transforms of one kind ends (the next step changes the
// kind, or the chain ends), the unit is verified if an optional pass of the chain ran on it: the end
// of a stage. A run of required transforms only is not verified (outside Run.VerifyEach): required
// steps are trusted, and verification exists to catch the optional passes, whose rollback depends on
// it. A kind-changing pass verifies its result itself (its postcondition).
func (rs *runner) chain(steps []node, in any, e *env, sc *scope) any {
	u, dirty := in, false
	for i, s := range steps {
		pre := rs.ran
		u = rs.exec(s, u, e, sc)
		if sc.broken {
			return nil
		}
		a, k := s.kinds()
		if _, ok := s.(*passNode); ok && a == k && rs.ran != pre {
			dirty = true
		}
		if a != k {
			dirty = false
		}
		end := i == len(steps)-1
		if !end {
			na, nb := steps[i+1].kinds()
			end = na != nb
		}
		if end && dirty {
			u = rs.verify(k, u, e, sc)
			if sc.broken {
				return nil
			}
			dirty = false
		}
	}
	return u
}

// verify verifies the unit u of the kind k where a run of transforms ends, unless every pass was
// verified (Run.VerifyEach). A failure is a failure of the segment: the unit rolls back to the start
// of the segment, which the manager kept, or the scope breaks where there is none (Run.Stop, or a
// required transform ended the segment), so an invalid unit never reaches a later step.
func (rs *runner) verify(k *kind, u any, e *env, sc *scope) any {
	if rs.r.VerifyEach || k.verify == nil {
		return u
	}
	err := k.verify(u)
	if err == nil {
		return u
	}
	sc.fail(&StepError{Step: "verify " + k.name, Label: e.label, Err: err})
	rs.drop(u)
	seg := sc.seg
	rb := any(nil)
	if seg != nil && seg.k == k && !rs.r.Stop {
		rb = seg.rb
		if rb == nil && k.copy == nil {
			rb = seg.start // a value kind: no pass changed the start
		}
	}
	if rb == nil {
		sc.broken = true
		return nil
	}
	sc.seg, sc.keep = &segment{k: k, start: rb}, true
	return rb
}

// text is the text form of a unit of a kind that prints; a kind without a printer has none.
func text(k *kind, u any) string {
	var b bytes.Buffer
	k.print(&b, u)
	return b.String()
}

// step runs one pass instance on one unit: the only place a pass function is called.
//
// Copies follow the segment policy. A transform may change its input in place: the input belongs
// to the segment. The manager copies the unit once per segment, before the first pass that runs,
// if the start must survive: the unit is kept by someone else (the caller under Run.Keep, a Shared
// result, the input of a Fork), or the segment holds an optional pass whose failure must roll the
// unit back (unless Run.Stop). Observers that need the state before every pass make the manager
// copy before every transform; the rollback point stays the segment's.
func (rs *runner) step(x *passNode, in any, e *env, sc *scope) (out any) {
	d := x.p
	ev := Event{Step: StepInfo{Name: d.Name, Instance: rs.instances[x], Required: d.Required, In: d.in.name, Out: d.out.name}, Label: e.label}
	if err := rs.ctx.Err(); err != nil {
		sc.fail(&StepError{Step: ev.Step.Instance, Label: e.label, Err: err})
		sc.broken = true
		return nil
	}
	transform := d.in == d.out
	if !transform || sc.seg == nil || sc.seg.k != d.in {
		sc.seg = &segment{k: d.in, start: in}
	}
	cs := &ctxState{rs: rs, env: e, pass: d, params: x.params, instance: ev.Step.Instance, unit: in}
	if !d.Required {
		if sc.err != nil || !d.Test && rs.r.Settings.Value(d.Name) == 0 || d.applies != nil && !d.applies(cs) {
			return in
		}
		*rs.seq++
		ev.Seq = *rs.seq
	}
	observed := len(rs.r.Observers) > 0
	// A kind without a printer has no text form: no input dump, no text comparison.
	var before string
	inText := d.in.print != nil && (rs.needs.Before || rs.needs.TextChanged || rs.r.VerifyEach)
	if inText {
		before = text(d.in, in)
	}
	if observed && !rs.before(ev, d, before) {
		rs.after(ev, before, &Result{Ran: false}, "")
		return in
	}
	if sc.lazy != nil {
		in = sc.lazy() // the unit of a PerFunc copy, taken before the first pass that runs
		cs.unit = in
	}
	arg := in
	if transform && d.in.copy != nil {
		seg, copied := sc.seg, false
		if sc.keep {
			arg, copied, sc.keep = rs.copy(d.in, in), true, false
			if !d.Required && !rs.r.Stop {
				seg.rb = in // the kept original is never changed
			}
		} else if !d.Required && !rs.r.Stop && seg.rb == nil {
			seg.rb, arg, copied = in, rs.copy(d.in, in), true
		}
		if !copied && (rs.r.VerifyEach || rs.needs.Units || rs.needs.Veto || rs.needs.TextChanged) {
			arg = rs.copy(d.in, in) // the unit an observer got after the pass before stays as it was
		}
	}
	cs.unit = arg
	var start time.Time
	if observed {
		start = time.Now()
	}
	res, pres, err := rs.call(d, cs, arg)
	var el time.Duration
	if observed {
		el = time.Since(start)
	}
	if err == nil && (rs.r.VerifyEach || !transform) && d.check != nil {
		err = d.check(res)
	}
	var after string
	outText := err == nil && transform && inText && (rs.needs.TextChanged || rs.r.VerifyEach)
	if outText {
		after = text(d.out, res)
		if rs.r.VerifyEach && pres.All() && after != before {
			err = errors.New("the pass preserves all analyses but changed the unit")
		}
	}
	var r *Result
	if observed {
		r = &Result{Ran: true, Counters: cs.counts, Patterns: cs.decisions, Elapsed: el, Err: err}
	}
	if err != nil {
		se := &StepError{Step: ev.Step.Instance, Label: e.label, Err: err}
		sc.fail(se)
		if observed {
			rs.after(ev, before, r, "")
		}
		rs.drop(arg)
		rs.dropParts(arg)
		if d.Required || !transform || rs.r.Stop || sc.seg.rb == nil && in != nil && d.in.copy != nil {
			sc.broken = true // nothing to roll back to: the unit may be half changed
			return nil
		}
		rb := sc.seg.rb
		if rb == nil {
			rb = in // a value kind: the pass could not change it
		}
		sc.seg, sc.keep = &segment{k: d.in, start: rb}, true // the required steps after it copy before changing it
		return rb
	}
	rs.changed++
	if transform && sameUnit(arg, res) {
		if !pres.All() {
			if a := rs.analyses[res]; a != nil {
				a.invalidate(pres)
			}
			rs.dropParts(res) // the transform may have changed the parts in place
		}
	} else {
		rs.drop(arg) // a lowering consumes its input; a transform that returned another unit drops it
		rs.dropParts(arg)
	}
	if transform {
		if d.Required {
			// A required transform ends its segment: a later failure rolls back to the unit after
			// it, so the work of a required step is never undone.
			sc.seg = nil
		} else {
			rs.ran++
		}
	} else {
		sc.keep = false // a lowering returns a new unit
	}
	if !observed {
		return res
	}
	r.Changed = !pres.All()
	if rs.needs.TextChanged {
		r.TextChanged = r.Changed
		if outText {
			r.TextChanged = after != before
		}
	}
	if rs.needs.Units {
		r.Unit = res
	}
	var printed string
	if rs.needs.Print && d.out.print != nil {
		if len(rs.recs) > 0 {
			printed = text(d.out, res) // a Shared body records its dumps for the replay
		}
		r.Print = func(w io.Writer) { d.out.print(w, res) }
	}
	rs.after(ev, before, r, printed)
	return res
}

// before asks the observers whether the step may run: a required step always runs. Only an observed
// run makes the closure of the input dump, so that a run without observers allocates none.
func (rs *runner) before(ev Event, d *passDesc, before string) bool {
	in := func(w io.Writer) { io.WriteString(w, before) }
	ran := true
	for _, o := range rs.r.Observers {
		if !o.Before(ev, in) && !d.Required {
			ran = false
		}
	}
	return ran
}

// sameUnit reports whether a and b are the same unit: equal, comparable values.
func sameUnit(a, b any) bool {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	return va.IsValid() && vb.IsValid() && va.Comparable() && vb.Comparable() && a == b
}

// drop discards the analyses of the unit u, which a step changed or consumed.
func (rs *runner) drop(u any) {
	if v := reflect.ValueOf(u); v.IsValid() && v.Comparable() {
		delete(rs.analyses, u)
	}
}

// own records the parts of the unit whole that hold analyses, so that a step that changes the whole
// discards them: a part is cached by its identity, which a change of the whole in place keeps.
func (rs *runner) own(whole any, parts ...any) {
	if v := reflect.ValueOf(whole); !v.IsValid() || !v.Comparable() {
		return
	}
	for _, p := range parts {
		if v := reflect.ValueOf(p); v.IsValid() && v.Comparable() && rs.analyses[p] != nil && !contains(rs.parts[whole], p) {
			if rs.parts == nil {
				rs.parts = map[any][]any{}
			}
			rs.parts[whole] = append(rs.parts[whole], p)
		}
	}
}

// dropParts discards the analyses of the parts of u and of their parts, which a step that changed u
// may have changed.
func (rs *runner) dropParts(u any) {
	if len(rs.parts) == 0 {
		return
	}
	if v := reflect.ValueOf(u); !v.IsValid() || !v.Comparable() {
		return
	}
	ps := rs.parts[u]
	delete(rs.parts, u)
	for _, p := range ps {
		rs.drop(p)
		rs.dropParts(p)
	}
}

func (rs *runner) call(d *passDesc, cs *ctxState, in any) (out any, p pass.PreservedSet, err error) {
	defer func() {
		if x := recover(); x != nil {
			if rs.r.Recover == nil {
				panic(x)
			}
			err = rs.r.Recover(x)
		}
	}()
	return d.run(cs, in)
}

func (rs *runner) after(ev Event, before string, r *Result, printed string) {
	for _, rec := range rs.recs {
		*rec = append(*rec, recorded{e: ev, before: before, r: *r, print: printed, hasPrint: r.Print != nil})
	}
	for _, o := range rs.r.Observers {
		o.After(ev, r)
	}
}

func (rs *runner) perFunc(x *perFuncNode, in any, e *env, sc *scope) any {
	if sc.err != nil && !hasRequired(x.body) {
		return in
	}
	v := x.view
	n := v.len(in)
	// One copy of the whole unit per segment, taken before the first pass that runs, when the start
	// must survive (kept, or a failure rolls a function back to it); a function's rollback point is
	// its state in the original. A segment in which no pass runs copies nothing.
	rollback := !rs.r.Stop && sc.err == nil && rs.hasEnabledOptional(x.body, e.label.Table)
	orig, whole := in, in
	units, envs, scs := make([]any, n), make([]*env, n), make([]*scope, n)
	copied := false
	materialize := func() {
		if copied {
			return
		}
		copied = true
		whole = rs.copy(v.whole, orig)
		for j := range n {
			units[j] = v.get(whole, j) // no pass has run yet: every function is as in orig
			scs[j].lazy = nil
		}
	}
	for i := range n {
		units[i] = v.get(in, i)
		envs[i] = &env{label: Label{Backend: e.label.Backend, Table: e.label.Table, Func: v.fn(in, i)}, features: e.features}
		scs[i] = &scope{err: sc.err}
		if rollback && v.whole.copy != nil {
			scs[i].seg = &segment{k: v.part, start: units[i], rb: units[i]} // under Run.Stop: none, so a failure breaks
		}
		if (sc.keep || rollback) && v.whole.copy != nil {
			i := i
			scs[i].lazy = func() any { materialize(); return units[i] }
		}
	}
	dirty := make([]bool, n)
	pre := rs.changed
	for _, p := range flatten(x.body, e.label.Table) {
		for i := range n {
			if !scs[i].broken {
				ran := rs.ran
				units[i] = rs.step(p, units[i], envs[i], scs[i])
				dirty[i] = dirty[i] || rs.ran != ran
			}
		}
	}
	for i := range n {
		if dirty[i] && !scs[i].broken {
			units[i] = rs.verify(v.part, units[i], envs[i], scs[i])
		}
	}
	if rs.changed != pre {
		rs.drop(orig) // a function changed, in place under Run.Stop: the analyses of the whole are stale
	}
	w := whole
	rolledBack := false
	for i := range n {
		if scs[i].err != nil && sc.err == nil {
			sc.fail(scs[i].err)
		}
		if scs[i].broken {
			sc.broken = true
			continue
		}
		rolledBack = rolledBack || copied && scs[i].keep // the function is the original's again
		var err error
		if w, err = v.put(w, i, units[i]); err != nil {
			sc.fail(&StepError{Step: "func " + v.name, Label: envs[i].label, Err: err})
		}
	}
	rs.own(w, units...)
	if copied {
		rs.drop(w)
		// The copy belongs to the run; it is kept only if a function rolled back to the kept
		// original, which the whole now shares.
		sc.keep = sc.keep && rolledBack
	}
	return w
}

func (rs *runner) hasEnabledOptional(n node, table string) bool {
	for _, p := range flatten(n, table) {
		if !p.p.Required && (p.p.Test || rs.r.Settings.Value(p.p.Name) != 0) {
			return true
		}
	}
	return false
}

func hasRequired(n node) bool {
	found := false
	walk(n, func(x node) {
		if p, ok := x.(*passNode); ok && p.p.Required {
			found = true
		}
	})
	return found
}

func (rs *runner) each(x *eachNode, in any, e *env, sc *scope) any {
	sx := SplitCtx{split: x.split, features: e.features}
	parts, labels, err := x.split.parts(sx, in)
	if err != nil {
		sc.fail(&StepError{Step: "each " + x.split.name, Label: e.label, Err: err})
		sc.broken = true
		return nil
	}
	for _, l := range labels {
		if x.split.labels != nil && !contains(x.split.labels, l) {
			sc.fail(&StepError{Step: "each " + x.split.name, Label: e.label, Err: fmt.Errorf("undeclared part label %s", l)})
			sc.broken = true
			return nil
		}
	}
	outs := make([]any, len(parts))
	broken, entry := false, sc.err // every part starts as the Each did: a failure of one skips nothing in another
	for i, p := range parts {
		ps := &scope{err: entry, keep: sc.keep}
		pe := &env{label: Label{Backend: e.label.Backend, Table: labels[i]}, features: e.features}
		outs[i] = rs.exec(x.body, p, pe, ps)
		if ps.err != nil && sc.err == nil {
			sc.fail(ps.err)
		}
		broken = broken || ps.broken
	}
	if broken {
		sc.broken = true
		return nil
	}
	out, err := x.split.join(sx, in, outs)
	if err != nil {
		sc.fail(&StepError{Step: "each " + x.split.name, Label: e.label, Err: err})
		sc.broken = true
	}
	return out
}

// sharedKey joins the key's value with the values of the features and parameters that the passes
// of the body read from outside it, whether the use was entered after a failure (its optional passes
// are then skipped), and a fingerprint of the body as it runs on this unit: its instances, its
// constant features, and every slot resolved for the current part. Two uses whose bodies differ (a
// slot filled for one table, a text that edits one branch) never share; two uses whose instances are
// numbered differently do not either, so a replay always reports the instances that would have run.
func (rs *runner) sharedKey(x *sharedNode, in any, e *env, failed bool) string {
	var parts []string
	var visit func(n node, inner map[FeatureID]bool)
	visit = func(n node, inner map[FeatureID]bool) {
		switch y := n.(type) {
		case *passNode:
			for _, f := range y.p.Features {
				if !inner[f] { // a feature provided inside the body is in the fingerprint
					parts = append(parts, string(f)+"="+features[f].format(e.features[f]))
				}
			}
			for _, f := range y.p.Params {
				parts = append(parts, fmt.Sprintf("%s.%s=%d", y.p.Name, f, y.value(f, rs.r.Settings)))
			}
			return
		case *eachNode:
			for _, f := range y.split.features {
				if !inner[f] {
					parts = append(parts, string(f)+"="+features[f].format(e.features[f]))
				}
			}
			visit(y.body, inner)
			return
		case *withNode:
			in := map[FeatureID]bool{y.prov.feature: true}
			for k := range inner {
				in[k] = true
			}
			visit(y.body, in)
			return
		case *slotNode:
			visit(y.def, inner)
			for _, k := range y.order {
				visit(y.parts[k], inner)
			}
			return
		case *forkNode:
			for _, b := range y.branches {
				visit(b, inner)
			}
			return
		}
		children(n, func(c node) { visit(c, inner) })
	}
	visit(x.body, nil)
	sort.Strings(parts)
	parts = slices.Compact(parts)
	var fp strings.Builder
	rs.fingerprint(x.body, e.label.Table, false, &fp)
	return fmt.Sprintf("%s:%s|%s|failed=%v|%s", x.key.name, x.key.of(in), strings.Join(parts, ","), failed, fp.String())
}

// fingerprint writes the canonical form of the body that runs on a unit of the part table. Inside
// a nested Each the part is not known, so every alternative of a slot is written.
func (rs *runner) fingerprint(n node, table string, nested bool, b *strings.Builder) {
	open := func(s string, body node) {
		b.WriteString(s + "{")
		rs.fingerprint(body, table, nested, b)
		b.WriteString("}")
	}
	switch x := n.(type) {
	case *passNode:
		b.WriteString(rs.instances[x])
		b.WriteString(";")
	case *slotNode:
		if !nested {
			if p, ok := x.parts[table]; ok {
				rs.fingerprint(p, table, nested, b)
			} else {
				rs.fingerprint(x.def, table, nested, b)
			}
			return
		}
		open("slot "+x.slot.name, x.def)
		for _, k := range x.order {
			open("for "+k, x.parts[k])
		}
	case *withNode:
		open("with "+string(x.prov.feature)+"="+x.prov.text+x.prov.name, x.body)
	case *perFuncNode:
		open("func "+x.view.name, x.body)
	case *withinNode:
		open("within "+x.lens.name, x.body)
	case *sharedNode:
		open("shared "+x.key.name, x.body)
	case *eachNode:
		b.WriteString("each " + x.split.name + "{")
		rs.fingerprint(x.body, table, true, b)
		b.WriteString("}")
	case *forkNode:
		for i, br := range x.branches {
			open("branch "+x.names[i], br)
		}
	default:
		children(n, func(c node) { rs.fingerprint(c, table, nested, b) })
	}
}

func (rs *runner) sharedRun(x *sharedNode, in any, e *env, sc *scope) any {
	if rs.r.NoShare || rs.needs.Units || rs.needs.Veto {
		return rs.exec(x.body, in, e, sc)
	}
	k := rs.sharedKey(x, in, e, sc.err != nil)
	if h := rs.shared[k]; h != nil {
		if x.key.agree != nil {
			if err := x.key.agree(h.in, in); err != nil {
				sc.fail(&StepError{Step: "key " + x.key.name, Label: e.label, Err: err})
				sc.broken = true
				return nil
			}
		}
		for _, rec := range h.events {
			ev := rec.e
			ev.Replayed = true
			ev.Label = relabelOf(ev.Label, h.label, e.label)
			if ev.Seq != 0 {
				*rs.seq++
				ev.Seq = *rs.seq
			}
			for _, o := range rs.r.Observers {
				o.Before(ev, func(w io.Writer) { io.WriteString(w, rec.before) })
			}
			r := rec.r
			r.Elapsed = 0
			r.Print = nil // never the first run's closure: its unit may have changed since
			if rec.hasPrint {
				p := rec.print
				r.Print = func(w io.Writer) { io.WriteString(w, p) }
			}
			rs.after(ev, rec.before, &r, rec.print)
		}
		if h.err != nil {
			sc.fail(relabel(h.err, h.label, e.label))
		}
		sc.broken = sc.broken || h.broken
		sc.keep = true // the entry keeps the result for the next use
		return h.out
	}
	var evs []recorded
	rs.recs = append(rs.recs, &evs)
	inner := &scope{err: sc.err, keep: sc.keep}
	out := rs.exec(x.body, in, e, inner)
	rs.recs = rs.recs[:len(rs.recs)-1]
	h := &sharedEntry{in: in, out: out, broken: inner.broken, events: evs, label: e.label}
	if inner.err != sc.err {
		h.err = inner.err
	}
	rs.shared[k] = h
	if inner.err != nil {
		sc.fail(inner.err)
	}
	sc.broken = sc.broken || inner.broken
	sc.keep = true // the entry keeps the result for the next use
	return out
}

// relabelOf gives a label of a replayed event the fields of the current use, cur, where the label
// has the field of the entry, at: the fields that the body set (a nested Each or PerFunc) stay.
func relabelOf(l, at, cur Label) Label {
	if l.Backend == at.Backend {
		l.Backend = cur.Backend
	}
	if l.Table == at.Table {
		l.Table = cur.Table
	}
	if l.Func == at.Func {
		l.Func = cur.Func
	}
	return l
}

// relabel gives the step errors of a replayed Shared result the label of the current use, as a run
// without sharing would report them (relabelOf).
func relabel(err error, at, cur Label) error {
	var se *StepError
	if errors.As(err, &se) && se == err {
		c := *se
		c.Label = relabelOf(c.Label, at, cur)
		return &c
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		var es []error
		for _, x := range j.Unwrap() {
			es = append(es, relabel(x, at, cur))
		}
		return errors.Join(es...)
	}
	return err
}

func (rs *runner) fork(x *forkNode, in any, e *env, sc *scope) any {
	list := newList(x.out, len(x.branches))
	var errs []error
	entry := sc.err
	for i, b := range x.branches {
		bs := &scope{err: entry, keep: true} // every branch reads the same input
		// A branch sets the backend and keeps the table and function: it runs on the same unit, so a
		// slot body for the enclosing Each's part runs in it (checkShape assumes the same labels).
		be := &env{label: Label{Backend: x.names[i], Table: e.label.Table, Func: e.label.Func}, features: e.features}
		out := rs.exec(b, in, be, bs)
		if bs.err != nil && bs.err != entry {
			errs = append(errs, bs.err)
		}
		if bs.broken {
			sc.broken = true
			continue
		}
		setList(list, i, out)
	}
	if len(errs) > 0 {
		sc.fail(errors.Join(errs...))
	}
	if sc.broken {
		return nil
	}
	return list.Interface()
}

func newList(k *kind, n int) reflect.Value { return reflect.MakeSlice(k.typ, n, n) }

func setList(l reflect.Value, i int, v any) {
	if v != nil {
		l.Index(i).Set(reflect.ValueOf(v))
	}
}

// allow decides one application of the pattern of a running pass: a run of its own, numbered like
// an optional pass, which the pattern's flag turns off and an observer may veto. A decision changes
// nothing by itself (Result.Changed is false); its pass reports the change.
func (rs *runner) allow(cs *ctxState, pattern string) bool {
	observed := len(rs.r.Observers) > 0
	if rs.r.Settings.Value(pattern) == 0 {
		if observed {
			cs.decide(pattern, false)
		}
		return false
	}
	*rs.seq++
	if !observed {
		return true
	}
	k := cs.pass.in.name
	ev := Event{Seq: *rs.seq, Step: StepInfo{Name: pattern, Instance: cs.instance + "/" + pattern, In: k, Out: k, Pattern: true}, Label: cs.env.label}
	ran := true
	for _, o := range rs.r.Observers {
		if !o.Before(ev, func(io.Writer) {}) {
			ran = false
		}
	}
	rs.after(ev, "", &Result{Ran: ran}, "")
	cs.decide(pattern, ran)
	return ran
}

// decide counts one answer of Allow for the result of the step.
func (cs *ctxState) decide(pattern string, applied bool) {
	if cs.decisions == nil {
		cs.decisions = map[string]Decisions{}
	}
	d := cs.decisions[pattern]
	if applied {
		d.Applied++
	} else {
		d.Skipped++
	}
	cs.decisions[pattern] = d
}

// testHook, when set by a test, sees the state of each run after it ends.
var testHook func(*runner)
