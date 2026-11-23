package code

import (
	"fmt"
	"slices"
)

// Canonicalization (design: the pass pipeline, Canonicalization): local rewrite patterns that a pass
// applies to a function until none applies. A pattern rewrites one statement or expression, or a few
// adjacent ones, into a simpler form that does the same. Each pattern has a name, which is the name
// of the optimization that turns it off, and each application is a decision of its own, so that
// bisection can skip one application. The dialect of each stage declares the patterns of the stage
// (RegisterPatterns): the patterns of its own operations, and those of the statements and expressions
// of the base that keep the invariants of the stage. Package code makes every pattern (Patterns), so
// a pass that applies them can declare them all without importing a dialect.

// Pattern is a named local rewrite of the body of a function.
type Pattern struct {
	// Name is the name of the pattern and of the optimization that turns it off.
	Name string
	// Doc says in one line what the pattern rewrites.
	Doc string
	// apply rewrites in place each match in f that allow admits, asking allow once for each match,
	// and returns the number of rewrites.
	apply func(f *Func, allow func() bool) int
}

// newPattern returns the pattern name, which rewrites in place each match in a function that allow
// admits, asks allow once for each match before it rewrites it, and returns the number of rewrites.
// Only package code makes patterns, so Patterns lists them all.
func newPattern(name, doc string, apply func(f *Func, allow func() bool) int) Pattern {
	if name == "" || doc == "" || apply == nil {
		panic(fmt.Sprintf("code: pattern %q has no name, description or rewrite", name))
	}
	return Pattern{Name: name, Doc: doc, apply: apply}
}

// Patterns returns every pattern, at whatever stage, in the order of the base's patterns below: the
// patterns that a pass applying the patterns of a stage declares.
func Patterns() []Pattern {
	return []Pattern{FoldConstants, FoldBranches, DropAfterExit, DropUnusedLabels, SpliceBlocks}
}

// stagePatterns holds the patterns of each stage, in the order they are tried.
var stagePatterns = map[string][]Pattern{}

// RegisterPatterns appends ps to the patterns of the stage S, in the order given. The dialect package
// of the stage calls it from its init function. A name given twice at a stage panics.
func RegisterPatterns[S Stage](ps ...Pattern) {
	stage := stageName[S]()
	for _, p := range ps {
		if !slices.ContainsFunc(Patterns(), func(q Pattern) bool { return q.Name == p.Name }) {
			panic(fmt.Sprintf("code: pattern %q is not one of Patterns", p.Name))
		}
		if slices.ContainsFunc(stagePatterns[stage], func(q Pattern) bool { return q.Name == p.Name }) {
			panic(fmt.Sprintf("code: pattern %s is registered twice at the %s stage", p.Name, stage))
		}
		stagePatterns[stage] = append(stagePatterns[stage], p)
	}
}

// PatternsAt returns the patterns of the stage S, in the order they are tried. The dialect of the stage
// declares them when it is initialized, so PatternsAt panics when no pattern is declared at S: a
// program that has a unit of the stage links its dialect.
func PatternsAt[S Stage]() []Pattern {
	ps, ok := stagePatterns[stageName[S]()]
	if !ok {
		panic(fmt.Sprintf("code: no pattern is declared at the %s stage: its dialect is not linked", stageName[S]()))
	}
	return slices.Clone(ps)
}

// ApplyPatterns applies the patterns ps to f in place, in the order given, sweep after sweep, until a
// sweep rewrites nothing; the patterns try their matches in a fixed order, so the result is the same
// on every run. allow decides each application by the name of its pattern; a refused application
// leaves its match as it is. It returns the number of rewrites of each pattern, in the order of ps.
func ApplyPatterns(f *Func, ps []Pattern, allow func(pattern string) bool) []int {
	counts := make([]int, len(ps))
	if f.Body == nil {
		return counts
	}
	for {
		n := 0
		for i, p := range ps {
			k := p.apply(f, func() bool { return allow(p.Name) })
			counts[i] += k
			n += k
		}
		if n == 0 {
			return counts
		}
	}
}

// The patterns of the statements and expressions of the base, in the order a stage that has them
// tries them. A pattern keeps the behavior of what it rewrites: the same results, the same calls of
// operations with the same arguments in the same order, and the same way of ending.
var (
	// FoldConstants replaces an operator or a conversion on constants by its value, computed as
	// running it computes it, except a division or a remainder by zero; "!" of a constant by its
	// value; and "&&" or "||" with a constant operand that decides it by that constant, or by its
	// other operand when the constant does not decide it. An operand that is dropped holds no call.
	FoldConstants = newPattern("fold-constants", "an operator or a conversion on constants becomes its value", foldConstants)
	// FoldBranches replaces an If whose condition is a constant by its Init and the statements of
	// the branch the condition selects (the branch as a block when it declares a local), a Loop whose
	// condition is the constant false by its Init, and the constant true condition of a Loop by
	// none.
	FoldBranches = newPattern("fold-branches", "an If or a loop with a constant condition becomes the code that runs", foldBranches)
	// DropAfterExit removes the statements after an Exit, Break, Continue or Return in the same block.
	DropAfterExit = newPattern("drop-after-exit", "the statements after an exit, break, continue or return in a block are removed", dropAfterExit)
	// DropUnusedLabels removes the label of a block that no Exit names.
	DropUnusedLabels = newPattern("drop-unused-labels", "the label of a block that no exit names is removed", dropUnusedLabels)
	// SpliceBlocks splices a block without a label that declares no local into the statements that
	// hold it, and removes an empty Else.
	SpliceBlocks = newPattern("splice-blocks", "a block without a label that declares no local is spliced into its parent", spliceBlocks)
)

// EachList calls fn with each statement list of b, the lists nested in a statement before the list
// that holds it, and replaces the list by what fn returns: the rewriting of statements of a pass.
func EachList(b *Block, fn func([]Stmt) []Stmt) { eachList(b, fn) }

// RewriteExprs replaces each expression of b in place by what fn returns for it, after its
// subexpressions are replaced; fn returns a *Call for a call: the rewriting of expressions of a pass.
func RewriteExprs(b *Block, fn func(Expr) Expr) { rewriteExprs(b, fn) }

// eachList calls fn with each statement list of b, the lists nested in a statement before the list
// that holds it, and replaces the list by what fn returns.
func eachList(b *Block, fn func([]Stmt) []Stmt) {
	if b == nil {
		return
	}
	for _, st := range b.Stmts {
		switch s := st.(type) {
		case *Block:
			eachList(s, fn)
		case *Loop:
			eachList(s.Body, fn)
		case *If:
			eachList(s.Then, fn)
			eachList(s.Else, fn)
		case *Protect:
			eachList(s.Body, fn)
			eachList(s.Finally, fn)
		}
	}
	b.Stmts = fn(b.Stmts)
}

// rewriteExprs replaces each expression of b in place by what fn returns for it, after its
// subexpressions are replaced. fn returns a *Call for a call.
func rewriteExprs(b *Block, fn func(Expr) Expr) {
	var x func(Expr) Expr
	x = func(e Expr) Expr {
		switch e := e.(type) {
		case nil:
			return nil
		case *Unary:
			e.X = x(e.X)
		case *Binary:
			e.X, e.Y = x(e.X), x(e.Y)
		case *Convert:
			e.X = x(e.X)
		case *Call:
			e.Fn = x(e.Fn)
			for i, a := range e.Args {
				e.Args[i] = x(a)
			}
		}
		return fn(e)
	}
	exprs := func(es []Expr) {
		for i, e := range es {
			es[i] = x(e)
		}
	}
	set := func(s *Set) {
		if s != nil {
			exprs(s.Values)
		}
	}
	var blk func(*Block)
	blk = func(b *Block) {
		if b == nil {
			return
		}
		for _, st := range b.Stmts {
			switch s := st.(type) {
			case *Block:
				blk(s)
			case *Loop:
				set(s.Init)
				s.Cond = x(s.Cond)
				set(s.Post)
				blk(s.Body)
			case *If:
				set(s.Init)
				s.Cond = x(s.Cond)
				blk(s.Then)
				blk(s.Else)
			case *Protect:
				blk(s.Body)
				blk(s.Finally)
			case *Let:
				s.Value = x(s.Value)
			case *Set:
				set(s)
			case *Do:
				s.Call = x(s.Call).(*Call)
			case *Return:
				exprs(s.Values)
			case *Assert:
				s.Cond = x(s.Cond)
			}
		}
	}
	blk(b)
}

// constant returns the value of x if it is a constant that is not a handle.
func constant(x Expr) (any, bool) {
	c, ok := x.(*Const)
	if !ok || c.Type.Kind == KHandle {
		return nil, false
	}
	return c.Value, true
}

// callFree reports whether x holds no call.
func callFree(x Expr) bool {
	free := true
	Walk(&Block{Stmts: []Stmt{&Assert{Cond: x}}}, nil, func(e Expr) {
		if _, ok := e.(*Call); ok {
			free = false
		}
	})
	return free
}

func boolConst(v bool) Expr { return &Const{Type: TBool, Value: v} }

func foldConstants(f *Func, allow func() bool) int {
	n := 0
	rewriteExprs(f.Body, func(x Expr) Expr {
		y := foldOne(x)
		if y == x || !allow() {
			return x
		}
		n++
		return y
	})
	return n
}

// foldOne returns the folded form of x, or x if FoldConstants does not apply to it.
func foldOne(x Expr) Expr {
	switch x := x.(type) {
	case *Unary:
		v, ok := constant(x.X)
		if !ok {
			return x
		}
		if x.Op == "!" {
			return boolConst(!v.(bool))
		}
		c := x.X.(*Const)
		return &Const{Type: c.Type, Value: Apply("-", ConvertValue(int64(0), c.Type), v)}
	case *Binary:
		a, aok := constant(x.X)
		b, bok := constant(x.Y)
		switch x.Op {
		case "&&", "||":
			decide := x.Op == "||" // the value of an operand that decides the result
			switch {
			case aok && a.(bool) == decide:
				return boolConst(decide)
			case aok:
				return x.Y
			case bok && b.(bool) != decide:
				return x.X
			case bok && callFree(x.X):
				return boolConst(decide)
			}
			return x
		}
		if !aok || !bok {
			return x
		}
		if (x.Op == "/" || x.Op == "%") && Apply("==", b, ConvertValue(int64(0), x.Y.(*Const).Type)).(bool) {
			return x
		}
		v := Apply(x.Op, a, b)
		t := x.X.(*Const).Type
		if _, isBool := v.(bool); isBool {
			t = TBool
		}
		return &Const{Type: t, Value: v}
	case *Convert:
		if v, ok := constant(x.X); ok {
			return &Const{Type: x.To, Value: ConvertValue(v, x.To)}
		}
	}
	return x
}

func foldBranches(f *Func, allow func() bool) int {
	n := 0
	eachList(f.Body, func(ss []Stmt) []Stmt {
		var out []Stmt
		for _, st := range ss {
			switch s := st.(type) {
			case *Loop:
				v, ok := constant(s.Cond)
				if !ok || !allow() {
					break
				}
				n++
				if v.(bool) {
					s.Cond = nil
					break
				}
				if s.Init != nil {
					out = append(out, s.Init)
				}
				continue
			case *If:
				v, ok := constant(s.Cond)
				if !ok || !allow() {
					break
				}
				n++
				if s.Init != nil {
					out = append(out, s.Init)
				}
				taken := s.Else
				if v.(bool) {
					taken = s.Then
				}
				switch {
				case taken == nil:
				case declares(taken):
					out = append(out, taken)
				default:
					out = append(out, taken.Stmts...)
				}
				continue
			}
			out = append(out, st)
		}
		return out
	})
	return n
}

// ends reports whether st ends its block: an Exit, Break, Continue or Return.
func ends(st Stmt) bool {
	switch st.(type) {
	case *Exit, *Break, *Continue, *Return:
		return true
	}
	return false
}

func dropAfterExit(f *Func, allow func() bool) int {
	n := 0
	eachList(f.Body, func(ss []Stmt) []Stmt {
		for i, st := range ss {
			if ends(st) && i+1 < len(ss) && allow() {
				n++
				return ss[:i+1]
			}
		}
		return ss
	})
	return n
}

func dropUnusedLabels(f *Func, allow func() bool) int {
	used := map[string]bool{}
	Walk(f.Body, func(s Stmt) {
		if e, ok := s.(*Exit); ok {
			used[e.Label] = true
		}
	}, nil)
	n := 0
	Walk(f.Body, func(s Stmt) {
		if b, ok := s.(*Block); ok && b.Label != "" && !used[b.Label] && allow() {
			b.Label = ""
			n++
		}
	}, nil)
	return n
}

func spliceBlocks(f *Func, allow func() bool) int {
	n := 0
	eachList(f.Body, func(ss []Stmt) []Stmt {
		var out []Stmt
		for _, st := range ss {
			switch s := st.(type) {
			case *Block:
				if s.Label == "" && !declares(s) && allow() {
					n++
					out = append(out, s.Stmts...)
					continue
				}
			case *If:
				if s.Else != nil && len(s.Else.Stmts) == 0 && allow() {
					n++
					s.Else = nil
				}
			}
			out = append(out, st)
		}
		return out
	})
	return n
}

// declares reports whether the block declares a local directly.
func declares(b *Block) bool {
	return slices.ContainsFunc(b.Stmts, func(st Stmt) bool {
		_, ok := st.(*Let)
		return ok
	})
}
