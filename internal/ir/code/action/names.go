package action

import (
	"strconv"

	"syntaxir.dev/sxir/internal/ir/code"
)

// The names of the locals of an Action function.
//
// The lowering names each local and each loop of a site's function with a prefix that says what it
// holds (tv a value, tf a failure, tj an index, each a loop, ...) and a number, one number for each
// statement that defines names, in the order the statements appear: the locals that one call lets
// share its number. A pass that adds a definition names its locals with fresh numbers (Namer), and
// Renumber restores the order, so that the names do not depend on the passes that ran.

// splitName returns the prefix and the number of the name of a local or loop, and false for a name
// without a number, which Renumber keeps.
func splitName(name string) (string, int, bool) {
	i := len(name)
	for i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
		i--
	}
	if i == len(name) {
		return "", 0, false
	}
	n, err := strconv.Atoi(name[i:])
	return name[:i], n, err == nil
}

// Namer makes fresh names in a function: a prefix and a number above every number that the
// function's names hold.
type Namer struct{ n int }

// NewNamer returns a namer for the function f.
func NewNamer(f *code.Func) *Namer {
	nm := &Namer{}
	see := func(name string) {
		if _, n, ok := splitName(name); ok && n > nm.n {
			nm.n = n
		}
	}
	for _, p := range f.Locals {
		see(p.Name)
	}
	code.Walk(f.Body, func(s code.Stmt) {
		switch s := s.(type) {
		case *code.Let:
			for _, n := range s.Names {
				see(n)
			}
		case *code.Loop:
			see(s.Label)
		}
	}, nil)
	return nm
}

// Next returns the next fresh number, which the names of one definition share (LocalName).
func (nm *Namer) Next() int {
	nm.n++
	return nm.n
}

// LocalName returns the name with the prefix and the number n.
func LocalName(prefix string, n int) string { return prefix + strconv.Itoa(n) }

// Renumber renames the numbered locals and loops of f so that each statement that defines names
// has the next number, from 1, in the order in which the statements appear, and the names that it
// defines share that number. A name without a number keeps it.
func Renumber(f *code.Func) {
	rename := map[string]string{}
	next := 0
	define := func(names ...string) {
		numbered := false
		for _, name := range names {
			if _, _, ok := splitName(name); ok {
				numbered = true
			}
		}
		if !numbered {
			return
		}
		next++
		for _, name := range names {
			if p, _, ok := splitName(name); ok {
				rename[name] = LocalName(p, next)
			}
		}
	}
	code.Walk(f.Body, func(s code.Stmt) {
		switch s := s.(type) {
		case *code.Let:
			define(s.Names...)
		case *code.Loop:
			define(s.Label)
		}
	}, nil)
	seen := map[*code.Local]bool{}
	to := func(name string) string {
		if r, ok := rename[name]; ok {
			return r
		}
		return name
	}
	code.Walk(f.Body, func(s code.Stmt) {
		switch s := s.(type) {
		case *code.Let:
			for i, n := range s.Names {
				s.Names[i] = to(n)
			}
		case *code.Loop:
			s.Label = to(s.Label)
		case *code.Set:
			for i, d := range s.Dst {
				if !d.Reg {
					s.Dst[i].Name = to(d.Name)
				}
			}
		case *code.Break:
			s.Label = to(s.Label)
		case *code.Continue:
			s.Label = to(s.Label)
		case *code.Exit:
			s.Label = to(s.Label)
		case *code.Block:
			s.Label = to(s.Label)
		}
	}, func(e code.Expr) {
		// The lowering uses one Local node wherever it reads the local, so each node is renamed once.
		if l, ok := e.(*code.Local); ok && !seen[l] {
			seen[l] = true
			l.Name = to(l.Name)
		}
	})
	for i, p := range f.Locals {
		f.Locals[i].Name = to(p.Name)
	}
}

// HasPrefix reports whether the name of a local has the prefix p followed by its number.
func HasPrefix(name, p string) bool {
	q, _, ok := splitName(name)
	return ok && q == p
}
