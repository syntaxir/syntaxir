package pipe

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/pass"
)

// Pipeline is an immutable composition of passes from units of I to units of O. It is built only
// with the combinators below (the typed door), and Execute is its one executor.
type Pipeline[I, O any] struct{ n node }

type node interface{ kinds() (in, out *kind) }

type (
	passNode struct {
		p *passDesc
		// params holds the overrides of the instance's parameters that a pipeline text gives; nil
		// for a pass of the typed door.
		params map[string]int
	}
	seqNode struct {
		k     *kind
		steps []node
	} // transforms of one kind, or a chain of steps
	chainNode   struct{ steps []node }
	perFuncNode struct {
		view *viewDesc
		body node
	}
	eachNode struct {
		split *splitDesc
		body  node
	}
	withinNode struct {
		lens *lensDesc
		body node
	}
	sharedNode struct {
		key  *keyDesc
		body node
	}
	withNode struct {
		prov *provDesc
		body node
	}
	slotNode struct {
		slot  *slotDesc
		skel  string // the skeleton of the declared default body, which every body must have
		def   node
		parts map[string]node
		order []string
	}
	forkNode struct {
		in, out  *kind
		names    []string
		branches []node
	}
)

func (n *passNode) kinds() (*kind, *kind) { return n.p.in, n.p.out }
func (n *seqNode) kinds() (*kind, *kind)  { return n.k, n.k }
func (n *chainNode) kinds() (*kind, *kind) {
	a, _ := n.steps[0].kinds()
	_, b := n.steps[len(n.steps)-1].kinds()
	return a, b
}
func (n *perFuncNode) kinds() (*kind, *kind) { return n.view.whole, n.view.whole }
func (n *eachNode) kinds() (*kind, *kind)    { return n.split.whole, n.split.result }
func (n *withinNode) kinds() (*kind, *kind)  { return n.lens.whole, n.lens.result }
func (n *sharedNode) kinds() (*kind, *kind)  { return n.body.kinds() }
func (n *withNode) kinds() (*kind, *kind)    { return n.body.kinds() }
func (n *slotNode) kinds() (*kind, *kind)    { return n.slot.k, n.slot.k }
func (n *forkNode) kinds() (*kind, *kind)    { return n.in, n.out }

// value returns the value of the parameter flag for this instance: its override, else the setting.
func (n *passNode) value(flag string, s pass.Settings) int {
	if v, ok := n.params[flag]; ok {
		return v
	}
	return s.Value(flag)
}

// Of makes a pipeline of one pass.
func Of[I, O any](p *Pass[I, O]) Pipeline[I, O] { return Pipeline[I, O]{&passNode{p: p.d}} }

// Seq composes transforms of the kind ir in order; it may be empty.
func Seq[M any](ir *IR[M], ps ...*Pass[M, M]) Pipeline[M, M] {
	n := &seqNode{k: kindOf(ir)}
	for _, p := range ps {
		n.steps = append(n.steps, &passNode{p: p.d})
	}
	return Pipeline[M, M]{n}
}

// Then composes two pipelines; b takes what a returns.
func Then[A, B, C any](a Pipeline[A, B], b Pipeline[B, C]) Pipeline[A, C] {
	return Pipeline[A, C]{chain(a.n, b.n)}
}

// Then3 composes three pipelines.
func Then3[A, B, C, D any](a Pipeline[A, B], b Pipeline[B, C], c Pipeline[C, D]) Pipeline[A, D] {
	return Pipeline[A, D]{chain(a.n, b.n, c.n)}
}

func chain(ns ...node) node {
	c := &chainNode{}
	for _, n := range ns {
		if x, ok := n.(*chainNode); ok {
			c.steps = append(c.steps, x.steps...)
		} else {
			c.steps = append(c.steps, n)
		}
	}
	return c
}

// Funcs views a unit W as functions, each a unit M that per-function passes take.
type Funcs[W, M any] struct {
	Name string
	Len  func(w W) int
	Func func(w W, i int) string
	Get  func(w W, i int) M
	Put  func(w W, i int, m M) (W, error)
}

type viewDesc struct {
	name        string
	whole, part *kind
	len         func(any) int
	fn          func(any, int) string
	get         func(any, int) any
	put         func(any, int, any) (any, error)
}

var views = map[string]*viewDesc{}

// RegisterFuncs registers a view by name, for PerFunc and the text form.
func RegisterFuncs[W, M any](wk *IR[W], mk *IR[M], f *Funcs[W, M]) *Funcs[W, M] {
	register(views, f.Name, &viewDesc{name: f.Name, whole: kindOf(wk), part: kindOf(mk),
		len: func(w any) int { return f.Len(w.(W)) }, fn: func(w any, i int) string { return f.Func(w.(W), i) },
		get: func(w any, i int) any { return f.Get(w.(W), i) },
		put: func(w any, i int, m any) (any, error) { return f.Put(w.(W), i, m.(M)) }})
	return f
}

// PerFunc runs the passes of p on each function of a unit, pass-major: the first pass on every
// function in order, then the second. p must be a sequence of passes and slots.
func PerFunc[W, M any](f *Funcs[W, M], p Pipeline[M, M]) Pipeline[W, W] {
	return Pipeline[W, W]{&perFuncNode{view: lookup(views, f.Name), body: p.n}}
}

// Split divides a unit into labeled parts and joins their results. Parts and Join read the
// features that the split declares from the scope it runs in (SplitFeature), such as the target of
// the branch of a Fork that the split runs in.
type Split[W, U, V, R any] struct {
	Name     string
	Labels   []string    // every label Parts may give; a slot body for a part names one of them
	Features []FeatureID // the features Parts and Join read (SplitFeature panics on any other)
	Parts    func(c SplitCtx, w W) ([]U, []string, error)
	Join     func(c SplitCtx, w W, vs []V) (R, error)
}

// SplitCtx is what a split reads of the scope it runs in: the features it declares
// (Split.Features), as the enclosing blocks provide them.
type SplitCtx struct {
	split    *splitDesc
	features map[FeatureID]any
}

// SplitFeature returns the value of the feature k in the scope of the split of c. It panics if the
// split does not declare k.
func SplitFeature[T any](c SplitCtx, k *FeatureKey[T]) T {
	if !contains(c.split.features, k.id) {
		panic(fmt.Sprintf("pipe: split %s reads the undeclared feature %s", c.split.name, k.id))
	}
	return c.features[k.id].(T)
}

type splitDesc struct {
	name                      string
	labels                    []string
	features                  []FeatureID
	whole, part, done, result *kind
	parts                     func(SplitCtx, any) ([]any, []string, error)
	join                      func(SplitCtx, any, []any) (any, error)
}

var splits = map[string]*splitDesc{}

// RegisterSplit registers a split by name.
func RegisterSplit[W, U, V, R any](w *IR[W], u *IR[U], v *IR[V], r *IR[R], s *Split[W, U, V, R]) *Split[W, U, V, R] {
	register(splits, s.Name, &splitDesc{name: s.Name, labels: s.Labels, features: slices.Clone(s.Features), whole: kindOf(w), part: kindOf(u), done: kindOf(v), result: kindOf(r),
		parts: func(c SplitCtx, x any) ([]any, []string, error) {
			us, ls, err := s.Parts(c, x.(W))
			out := make([]any, len(us))
			for i, u := range us {
				out[i] = u
			}
			return out, ls, err
		},
		join: func(c SplitCtx, x any, vs []any) (any, error) {
			tv := make([]V, len(vs))
			for i, v := range vs {
				tv[i] = v.(V)
			}
			return s.Join(c, x.(W), tv)
		}})
	return s
}

// Each runs p on each part of a unit, in order, and joins the results.
func Each[W, U, V, R any](s *Split[W, U, V, R], p Pipeline[U, V]) Pipeline[W, R] {
	return Pipeline[W, R]{&eachNode{split: lookup(splits, s.Name), body: p.n}}
}

// Lens routes a part of a composite unit through a pipeline; the part may change kind (P to Q),
// which changes the whole (W to V). Put must not change w in place: the whole may be kept.
type Lens[W, V, P, Q any] struct {
	Name string
	Get  func(w W) P
	Put  func(w W, q Q) (V, error)
}

type lensDesc struct {
	name                         string
	whole, result, part, partOut *kind
	get                          func(any) any
	put                          func(any, any) (any, error)
}

var lenses = map[string]*lensDesc{}

// RegisterLens registers a lens by name.
func RegisterLens[W, V, P, Q any](w *IR[W], v *IR[V], p *IR[P], q *IR[Q], l *Lens[W, V, P, Q]) *Lens[W, V, P, Q] {
	register(lenses, l.Name, &lensDesc{name: l.Name, whole: kindOf(w), result: kindOf(v), part: kindOf(p), partOut: kindOf(q),
		get: func(x any) any { return l.Get(x.(W)) }, put: func(x, y any) (any, error) { return l.Put(x.(W), y.(Q)) }})
	return l
}

// Within runs p on the part of a unit that l selects.
func Within[W, V, P, Q any](l *Lens[W, V, P, Q], p Pipeline[P, Q]) Pipeline[W, V] {
	return Pipeline[W, V]{&withinNode{lens: lookup(lenses, l.Name), body: p.n}}
}

// Key is the registered key of a Shared pipeline.
type Key[I any] struct {
	Name string
	Of   func(in I) string
	// Agree, if set, checks that a later input with the key of an earlier one agrees with it on
	// what the key does not spell but the shared result depends on: a later use gets the result of
	// the earlier input, so an input that disagrees is an error of whoever made it, which fails the
	// run (StepError "key <name>"). first is the input of the use that ran the body; Agree reads only
	// what the body does not change.
	Agree func(first, later I) error
}

type keyDesc struct {
	name  string
	k     *kind
	of    func(any) string
	agree func(first, later any) error
}

var keys = map[string]*keyDesc{}

// RegisterKey registers a Shared key by name.
func RegisterKey[I any](ir *IR[I], k *Key[I]) *Key[I] {
	d := &keyDesc{name: k.Name, k: kindOf(ir), of: func(x any) string { return k.Of(x.(I)) }}
	if k.Agree != nil {
		d.agree = func(first, later any) error { return k.Agree(first.(I), later.(I)) }
	}
	register(keys, k.Name, d)
	return k
}

// Shared runs p at most once per effective key within one Execute: the key's value for the input,
// joined with the values of every feature and parameter that the passes of p declare. A later use
// gets the same result, and the observers see the recorded events again (Event.Replayed).
func Shared[I, O any](k *Key[I], p Pipeline[I, O]) Pipeline[I, O] {
	return Pipeline[I, O]{&sharedNode{key: lookup(keys, k.Name), body: p.n}}
}

type provDesc struct {
	name     string // a derived provider's name, or "" for a constant
	in       *kind  // a derived provider: the kind of unit it computes the feature from
	feature  FeatureID
	text     string // a constant's value as text
	value    func(any) any
	format   func(any) string
	constant bool
}

var providers = map[string]*provDesc{}

// WithConst provides the feature k with the value v to every pass of p.
func WithConst[I, O, T any](k *FeatureKey[T], v T, p Pipeline[I, O]) Pipeline[I, O] {
	return Pipeline[I, O]{&withNode{prov: constProv(k, v), body: p.n}}
}

func constProv[T any](k *FeatureKey[T], v T) *provDesc {
	return &provDesc{feature: k.id, text: k.format(v), constant: true, value: func(any) any { return v },
		format: func(x any) string { return k.format(x.(T)) }}
}

// Derive is a registered provider that computes a feature from the unit.
type Derive[I, T any] struct {
	Name string
	Key  *FeatureKey[T]
	Of   func(in I) T
}

// RegisterDerive registers a derived provider by name, with the kind of unit it reads.
func RegisterDerive[I, T any](ir *IR[I], d *Derive[I, T]) *Derive[I, T] {
	register(providers, d.Name, &provDesc{name: d.Name, in: kindOf(ir), feature: d.Key.id, value: func(x any) any { return d.Of(x.(I)) },
		format: func(x any) string { return d.Key.format(x.(T)) }})
	return d
}

// WithDerived provides the feature of d, computed from the unit p starts on, to every pass of p.
func WithDerived[I, O, T any](d *Derive[I, T], p Pipeline[I, O]) Pipeline[I, O] {
	return Pipeline[I, O]{&withNode{prov: lookup(providers, d.Name), body: p.n}}
}

// SlotKey names a typed extension point of an assembled pipeline.
type SlotKey[M any] struct{ name string }

type slotDesc struct {
	name string
	k    *kind
}

var slots = map[string]*slotDesc{}

// NewSlot declares and registers a slot of the kind ir.
func NewSlot[M any](name string, ir *IR[M]) *SlotKey[M] {
	register(slots, name, &slotDesc{name: name, k: kindOf(ir)})
	return &SlotKey[M]{name: name}
}

// Slot marks p as the default body of the slot k.
func Slot[M any](k *SlotKey[M], p Pipeline[M, M]) Pipeline[M, M] {
	return Pipeline[M, M]{&slotNode{slot: lookup(slots, k.name), skel: skeleton(p.n), def: p.n, parts: map[string]node{}}}
}

// Edit changes an assembled pipeline before it runs.
type Edit struct {
	slot, part string
	body       node
	// prov, if not nil, is the constant provider that replaces every constant provider of its
	// feature (SetConst).
	prov *provDesc
	// branch, if not empty, is the branch of a Fork that the edit inner applies to (InBranch).
	branch string
	inner  *Edit
}

// InBranch applies the edit e only to the branches of a Fork named branch, such as a value that
// the driver learns from a first run for one target only. A pipeline without such a branch is an
// error, as is an e that does not apply to one.
func InBranch(branch string, e Edit) Edit {
	return Edit{branch: branch, inner: &e}
}

// SetConst gives every constant provider of the feature k in the pipeline the value v, such as a
// value that the driver learns from a first run of the pipeline. A pipeline without one is an error.
func SetConst[T any](k *FeatureKey[T], v T) Edit {
	return Edit{prov: constProv(k, v)}
}

// Fill replaces the body of the slot k, for the Each part labeled part ("" for every part).
func Fill[M any](k *SlotKey[M], part string, p Pipeline[M, M]) Edit {
	return Edit{slot: k.name, part: part, body: p.n}
}

// Apply returns p with the edits applied, in order; an edit of a slot or a feature that p does not
// have is an error.
func Apply[I, O any](p Pipeline[I, O], edits ...Edit) (Pipeline[I, O], error) {
	n := p.n
	for _, e := range edits {
		var err error
		if n, err = apply(n, e); err != nil {
			return p, err
		}
	}
	return Pipeline[I, O]{n}, nil
}

// apply returns n with the edit e applied.
func apply(n node, e Edit) (node, error) {
	found := false
	switch {
	case e.inner != nil:
		var err error
		n = rewrite(n, func(x node) node {
			f, ok := x.(*forkNode)
			if !ok || err != nil {
				return x
			}
			c := &forkNode{in: f.in, out: f.out, names: f.names, branches: slices.Clone(f.branches)}
			for i, name := range f.names {
				if name == e.branch {
					found = true
					if c.branches[i], err = apply(c.branches[i], *e.inner); err != nil {
						return x
					}
				}
			}
			return c
		})
		if err != nil {
			return n, err
		}
		if !found {
			return n, fmt.Errorf("%w: no branch %s", errCheck, e.branch)
		}
		return n, nil
	case e.prov != nil:
		n = rewrite(n, func(x node) node {
			w, ok := x.(*withNode)
			if !ok || !w.prov.constant || w.prov.feature != e.prov.feature {
				return x
			}
			found = true
			return &withNode{prov: e.prov, body: w.body}
		})
		if !found {
			return n, fmt.Errorf("%w: no constant provider of the feature %s", errCheck, e.prov.feature)
		}
		return n, nil
	}
	n = rewrite(n, func(x node) node {
		s, ok := x.(*slotNode)
		if !ok || s.slot.name != e.slot {
			return x
		}
		found = true
		c := &slotNode{slot: s.slot, skel: s.skel, def: s.def, parts: map[string]node{}, order: append([]string(nil), s.order...)}
		for k, v := range s.parts {
			c.parts[k] = v
		}
		if e.part == "" {
			c.def, c.parts, c.order = e.body, map[string]node{}, nil
		} else {
			if c.parts[e.part] == nil {
				c.order = append(c.order, e.part)
			}
			c.parts[e.part] = e.body
		}
		return c
	})
	if !found {
		return n, fmt.Errorf("%w: no slot %s", errCheck, e.slot)
	}
	return n, nil
}

// Branch is one backend of a Fork.
type Branch[I, O any] struct {
	name string
	p    Pipeline[I, O]
}

// B makes the branch of a Fork named name, which labels the units of the branch with that backend.
func B[I, O any](name string, p Pipeline[I, O]) Branch[I, O] { return Branch[I, O]{name, p} }

// Fork runs every branch on one input, in order, and returns their results in order.
func Fork[I, O any](bs ...Branch[I, O]) Pipeline[I, []O] {
	if len(bs) == 0 {
		panic("pipe: a fork has no branch")
	}
	in, out := bs[0].p.n.kinds()
	f := &forkNode{in: in, out: listKind(out)}
	for _, b := range bs {
		f.names = append(f.names, b.name)
		f.branches = append(f.branches, b.p.n)
	}
	return Pipeline[I, []O]{f}
}

// normalize returns the normal form of a tree, in which the grouping of steps means nothing: a
// sequence or chain inside a chain is spliced into it (an empty one is dropped), and every block
// body (of within, with, shared, each, a slot and a fork branch) is a sequence or chain even if it
// holds one step. The manager verifies where a run of transforms ends inside a block, so without
// the normal form Then(Seq(a, b), Of(c)) and the text "a b c" would verify at different points, and
// so would a body Of(a) and a body Seq(k, a). Execute and Describe both normalize, so the typed door
// and the text door give the same run for the same text.
func normalize(n node) node {
	block := func(b node) node {
		switch b.(type) {
		case *seqNode, *chainNode:
			return b
		}
		return &chainNode{steps: []node{b}}
	}
	return block(rewrite(n, func(x node) node {
		switch y := x.(type) {
		case *chainNode:
			c := &chainNode{}
			for _, s := range y.steps {
				switch z := s.(type) {
				case *seqNode:
					c.steps = append(c.steps, z.steps...)
				case *chainNode:
					c.steps = append(c.steps, z.steps...)
				default:
					c.steps = append(c.steps, s)
				}
			}
			if len(c.steps) == 0 {
				return y.steps[0] // only empty sequences: the first keeps the kind
			}
			return c
		case *withinNode:
			y.body = block(y.body)
		case *withNode:
			y.body = block(y.body)
		case *sharedNode:
			y.body = block(y.body)
		case *eachNode:
			y.body = block(y.body)
		case *slotNode:
			y.def = block(y.def)
			for k, v := range y.parts {
				y.parts[k] = block(v)
			}
		case *forkNode:
			for i, b := range y.branches {
				y.branches[i] = block(b)
			}
		}
		return x
	}))
}

// rewrite rebuilds the tree bottom-up through f.
func rewrite(n node, f func(node) node) node {
	switch x := n.(type) {
	case *seqNode:
		c := &seqNode{k: x.k}
		for _, s := range x.steps {
			c.steps = append(c.steps, rewrite(s, f))
		}
		return f(c)
	case *chainNode:
		c := &chainNode{}
		for _, s := range x.steps {
			c.steps = append(c.steps, rewrite(s, f))
		}
		return f(c)
	case *perFuncNode:
		return f(&perFuncNode{view: x.view, body: rewrite(x.body, f)})
	case *eachNode:
		return f(&eachNode{split: x.split, body: rewrite(x.body, f)})
	case *withinNode:
		return f(&withinNode{lens: x.lens, body: rewrite(x.body, f)})
	case *sharedNode:
		return f(&sharedNode{key: x.key, body: rewrite(x.body, f)})
	case *withNode:
		return f(&withNode{prov: x.prov, body: rewrite(x.body, f)})
	case *slotNode:
		c := &slotNode{slot: x.slot, skel: x.skel, def: rewrite(x.def, f), parts: map[string]node{}, order: x.order}
		for k, v := range x.parts {
			c.parts[k] = rewrite(v, f)
		}
		return f(c)
	case *forkNode:
		c := &forkNode{in: x.in, out: x.out, names: x.names}
		for _, b := range x.branches {
			c.branches = append(c.branches, rewrite(b, f))
		}
		return f(c)
	}
	return f(n)
}

// names holds every registered name of a kind, split, lens, view, key, provider and slot: a name
// is unique across all of them, so the text form names one thing with it.
var names = map[string]bool{}

func claim(name string) {
	if name == "" || names[name] {
		panic(fmt.Sprintf("pipe: %q has no name or is registered twice", name))
	}
	names[name] = true
}

func register[T any](m map[string]*T, name string, v *T) {
	claim(name)
	m[name] = v
}

func lookup[T any](m map[string]*T, name string) *T {
	if v := m[name]; v != nil {
		return v
	}
	panic(fmt.Sprintf("pipe: %q is not registered", name))
}
