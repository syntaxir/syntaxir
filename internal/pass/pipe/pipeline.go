package pipe

import "fmt"

// Pipeline is an immutable composition of passes from units of I to units of O. It is built only
// with the combinators below (the typed door), and Execute is its one executor.
type Pipeline[I, O any] struct{ n node }

type node interface{ kinds() (in, out *kind) }

type (
	passNode struct{ p *passDesc }
	seqNode  struct {
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

// Split divides a unit into labeled parts and joins their results.
type Split[W, U, V, R any] struct {
	Name   string
	Labels []string // every label Parts may give; a slot body for a part names one of them
	Parts  func(w W) ([]U, []string, error)
	Join   func(w W, vs []V) (R, error)
}

type splitDesc struct {
	name                      string
	labels                    []string
	whole, part, done, result *kind
	parts                     func(any) ([]any, []string, error)
	join                      func(any, []any) (any, error)
}

var splits = map[string]*splitDesc{}

// RegisterSplit registers a split by name.
func RegisterSplit[W, U, V, R any](w *IR[W], u *IR[U], v *IR[V], r *IR[R], s *Split[W, U, V, R]) *Split[W, U, V, R] {
	register(splits, s.Name, &splitDesc{name: s.Name, labels: s.Labels, whole: kindOf(w), part: kindOf(u), done: kindOf(v), result: kindOf(r),
		parts: func(x any) ([]any, []string, error) {
			us, ls, err := s.Parts(x.(W))
			out := make([]any, len(us))
			for i, u := range us {
				out[i] = u
			}
			return out, ls, err
		},
		join: func(x any, vs []any) (any, error) {
			tv := make([]V, len(vs))
			for i, v := range vs {
				tv[i] = v.(V)
			}
			return s.Join(x.(W), tv)
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
}

type keyDesc struct {
	name string
	k    *kind
	of   func(any) string
}

var keys = map[string]*keyDesc{}

// RegisterKey registers a Shared key by name.
func RegisterKey[I any](ir *IR[I], k *Key[I]) *Key[I] {
	register(keys, k.Name, &keyDesc{name: k.Name, k: kindOf(ir), of: func(x any) string { return k.Of(x.(I)) }})
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
}

// Fill replaces the body of the slot k, for the Each part labeled part ("" for every part).
func Fill[M any](k *SlotKey[M], part string, p Pipeline[M, M]) Edit {
	return Edit{slot: k.name, part: part, body: p.n}
}

// Apply returns p with the edits applied; an edit of a slot that p does not have is an error.
func Apply[I, O any](p Pipeline[I, O], edits ...Edit) (Pipeline[I, O], error) {
	n := p.n
	for _, e := range edits {
		found := false
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
			return p, fmt.Errorf("%w: no slot %s", errCheck, e.slot)
		}
	}
	return Pipeline[I, O]{n}, nil
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
