package libprint

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
)

// checkRoots are the methods of the parser that check the counter of logical compute units
// (spec/runtime/parsing.md#compute-units) or observe cancellation and the memory limit: each may end
// the parse. A function that takes from the counter (Parser.Fuel, the headroom below the check count) counts units, and checks the counter
// or leaves the check to its caller.
var checkRoots = []string{"PollUnits", "observeUnits", "unitsExceeded"}

// libCalls type-checks the Go library and returns its static call graph (calls through
// interfaces and function values are not edges), the functions that add to the counter themselves
// (counts), the functions that reach one of them (counting), and the functions that reach a count or
// a method of checkRoots (stopping).
func libCalls(t *testing.T) (pkg *types.Package, callees map[*types.Func][]*types.Func, counts, counting, stopping map[*types.Func]bool) {
	t.Helper()
	bp, err := build.Default.ImportDir(filepath.Join("..", "..", "..", "..", "..", "pkg", "runtime", "libgo"), 0)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range bp.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(bp.Dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err = conf.Check("syntaxir.dev/sxir/pkg/runtime/libgo", fset, files, info)
	if err != nil {
		t.Fatal(err)
	}
	callees = map[*types.Func][]*types.Func{}
	counts = map[*types.Func]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			caller := info.Defs[fd.Name].(*types.Func)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				var lhs []ast.Expr
				switch s := n.(type) {
				case *ast.AssignStmt:
					if s.Tok == token.SUB_ASSIGN {
						lhs = s.Lhs
					}
				case *ast.IncDecStmt:
					if s.Tok == token.DEC {
						lhs = []ast.Expr{s.X}
					}
				}
				for _, l := range lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "Fuel" {
						counts[caller] = true
					}
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var id *ast.Ident
				switch fn := ast.Unparen(call.Fun).(type) {
				case *ast.Ident:
					id = fn
				case *ast.SelectorExpr:
					id = fn.Sel
				case *ast.IndexExpr: // an instantiated generic function
					switch g := fn.X.(type) {
					case *ast.Ident:
						id = g
					case *ast.SelectorExpr:
						id = g.Sel
					}
				}
				if id == nil {
					return true
				}
				if callee, ok := info.Uses[id].(*types.Func); ok && callee.Pkg() == pkg {
					callees[caller] = append(callees[caller], callee.Origin())
				}
				return true
			})
		}
	}
	roots := maps.Clone(counts)
	parserType := pkg.Scope().Lookup("Parser").Type()
	for _, name := range checkRoots {
		obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(parserType), true, pkg, name)
		f, ok := obj.(*types.Func)
		if !ok {
			t.Fatalf("the Go library has no method Parser.%s", name)
		}
		roots[f] = true
	}
	return pkg, callees, counts, reaching(callees, counts), reaching(callees, roots)
}

// reaching returns the functions that reach one of roots through callees, roots among them.
func reaching(callees map[*types.Func][]*types.Func, roots map[*types.Func]bool) map[*types.Func]bool {
	reach := maps.Clone(roots)
	for changed := true; changed; {
		changed = false
		for caller, cs := range callees {
			if reach[caller] {
				continue
			}
			for _, c := range cs {
				if reach[c] {
					reach[caller] = true
					changed = true
					break
				}
			}
		}
	}
	return reach
}

// goCallee matches the calls in the Go form of an operation: a method of the parser (p.X), of one
// of its fields (p.A.X), or a function of the library.
var goCallee = regexp.MustCompile(`(?:\bp((?:\.[A-Za-z_]\w*)+)|(?:^|[^.\w])([A-Za-z_]\w*))\(`)

// opCallees returns the functions of the library that the Go form of the operation op calls, in the
// node or the typed library, or ok false when the target has no form of op.
func opCallees(t *testing.T, pkg *types.Package, op *code.Op, typed bool) (form string, fns []*types.Func, ok bool) {
	t.Helper()
	tg := goTarget(goSpec{typed: typed})
	args := make([]string, len(op.Params))
	for i := range args {
		args[i] = fmt.Sprintf("a%d", i)
	}
	dst := make([]string, len(op.Results))
	for i := range dst {
		dst[i] = fmt.Sprintf("r%d", i)
	}
	c := &code.Call{Callee: op.Name}
	func() {
		defer func() {
			if recover() != nil {
				form, ok = "", false
			}
		}()
		// An empty statement or value form is a form that does nothing.
		if s, done := tg.Stmt(c, args, dst, true); done {
			form, ok = s, true
			return
		}
		if s, done := goValueOp(op.Name, args, typed); done {
			form, ok = s, true
			return
		}
		form = tg.Op(c, args)
		ok = form != ""
	}()
	if !ok {
		return "", nil, false
	}
	recv := "Parser"
	if typed {
		recv = "TParser"
	}
	for _, m := range goCallee.FindAllStringSubmatch(form, -1) {
		if m[1] != "" {
			// Follow the fields of the parser to the method.
			var typ types.Type = types.NewPointer(pkg.Scope().Lookup(recv).Type())
			names := strings.Split(m[1][1:], ".")
			for i, name := range names {
				obj, _, _ := types.LookupFieldOrMethod(typ, true, pkg, name)
				switch o := obj.(type) {
				case *types.Var:
					typ = o.Type()
					continue
				case *types.Func:
					if i == len(names)-1 {
						fns = append(fns, o.Origin())
					}
				}
				break
			}
			continue
		}
		if f, ok := pkg.Scope().Lookup(m[2]).(*types.Func); ok {
			fns = append(fns, f.Origin())
		}
	}
	return form, fns, true
}

// countWrappers are the functions of the library that count the units of an event that their caller
// makes, and check the counter.
var countWrappers = []string{"unitsSized", "SkipUnits", "valueUnits", "UnitsSized"}

// formSite stands for a count that the Go form of an operation makes itself, through a wrapper.
const formSite = "the form"

// countSites returns the count sites reachable from fns: the functions that call a wrapper or add to
// the counter themselves, with formSite when fns holds a wrapper.
func countSites(pkg *types.Package, callees map[*types.Func][]*types.Func, counts, counting map[*types.Func]bool, fns []*types.Func) map[string]bool {
	isLeaf := func(f *types.Func) bool { return slices.Contains(countWrappers, f.Name()) && f.Pkg() == pkg }
	sites := map[string]bool{}
	seen := map[*types.Func]bool{}
	var walk func(f *types.Func)
	walk = func(f *types.Func) {
		if seen[f] || !counting[f] {
			return
		}
		seen[f] = true
		if counts[f] {
			sites[siteName(f)] = true
		}
		for _, c := range callees[f] {
			if isLeaf(c) {
				sites[siteName(f)] = true
			} else {
				walk(c)
			}
		}
	}
	for _, f := range fns {
		if isLeaf(f) {
			sites[formSite] = true
		} else {
			walk(f)
		}
	}
	return sites
}

// siteName is the name of a function of the library in siteCost: Type.Method or Function.
func siteName(f *types.Func) string {
	if r := f.Signature().Recv(); r != nil {
		t := r.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		return t.(*types.Named).Obj().Name() + "." + f.Name()
	}
	return f.Name()
}

// siteCost attributes each count site that a host operation reaches to the quantities of its cost
// declaration that account for it; "" stands for the constant, and nil for any declared cost. An
// operation that reaches a site declares one of the site's quantities. A site that is not listed fails
// the test, so that a new count in the library is attributed when it is added, and so does a listed
// site that no operation reaches.
var siteCost = map[string][]string{
	// The sized events of values: the bytes that a string concatenation makes, a comparison covers or
	// len measures, and the text of a range of a stream, which node.rangeText reads.
	"Parser.strAdd":      {action.QValueBytes},
	"Parser.unitLen":     {action.QValueBytes},
	"Parser.compareText": {action.QValueBytes},
	"Parser.TextEqual":   {action.QValueBytes},
	"Parser.rangeText":   {action.QValueBytes},
}

// unitsOps are the operations that count the logical compute units their contracts name, of the
// values that the plain evaluation of an action expression builds and the lowering does not: the units
// are their effect, not the work of the operation, so they declare no cost.
var unitsOps = map[string]bool{"units.add": true, "units.items": true, "units.since": true}

// editForms are the operations that the engine and both libraries read and write while they apply an
// edit to a memo entry that a lookup finds (memoAdvance). They declare no cost: the edit is the work of
// memoAdvance, as TestMemoLookupChargesItsPendingEdits and the conformance suites of the Document memo
// measure.
var editForms = []string{"memo.seed", "memo.from", "memo.pinned", "memo.shifted", "memo.applied", "memo.advance",
	"edit.count", "edit.start", "edit.end", "edit.delta"}

// TestEditFormsCallNothing holds the forms of the operations that a lookup reads and writes while it
// applies an edit to an entry to fields and conversions, in the engine and in both libraries.
func TestEditFormsCallNothing(t *testing.T) {
	for _, tg := range []struct {
		name string
		op   func(c *code.Call, a []string) string
	}{{"engine", engineTarget().Op}, {"library", goTarget(goSpec{fn: "memoAdvance", name: "advanceEntry"}).Op},
		{"typed library", goTarget(goSpec{fn: "memoAdvance", typed: true, name: "advanceEntry"}).Op}} {
		for _, name := range editForms {
			op := compose.Build().Op(name)
			if op.Cost.Const != 0 || len(op.Cost.Terms) != 0 {
				t.Errorf("%s declares cost %s", name, op.Cost)
			}
			args := make([]string, len(op.Params))
			for i := range args {
				args[i] = fmt.Sprintf("a%d", i)
			}
			form := tg.op(&code.Call{Callee: name}, args)
			for _, m := range goCallee.FindAllStringSubmatch(form, -1) {
				if m[1] != "" || m[2] != "int" && m[2] != "int32" && m[2] != "uint32" && m[2] != "len" {
					t.Errorf("%s: %s calls %s: %s", tg.name, name, m[0], form)
				}
			}
		}
	}
}

// TestHostCostsMatchCharges checks the cost declarations of the host operations against the logical
// compute units (spec/runtime/parsing.md#compute-units) that the Go library counts for them, through
// its static calls (calls through interfaces and function values are not followed, so the units of a
// host callback such as the delivery of an element are not audited: they are the host's): an
// operation that reaches a count or a check of the counter may end the parse (NoReturn), and one that
// reaches a count declares a cost with a quantity for every count site it reaches (siteCost), but for
// unitsOps. A cost bounds the work of an operation, of which the units count only the logical events,
// so an operation may declare a cost and reach no count. It does not measure the bounds: a host
// operation has no reference model to compute its quantities.
func TestHostCostsMatchCharges(t *testing.T) {
	pkg, callees, counts, counting, stopping := libCalls(t)
	reached := map[string]bool{}
	for _, op := range compose.Build().Ops {
		if op.Kind != code.OpHost {
			continue
		}
		found := false
		var fns []*types.Func
		for _, typed := range []bool{false, true} {
			_, f, ok := opCallees(t, pkg, op, typed)
			found = found || ok
			fns = append(fns, f...)
		}
		if !found {
			t.Errorf("host operation %s has no Go form to audit", op.Name)
			continue
		}
		if slices.ContainsFunc(fns, func(f *types.Func) bool { return stopping[f] }) && !op.NoReturn {
			t.Errorf("%s reaches a count or a check of the counter but is not NoReturn", op.Name)
		}
		sites := countSites(pkg, callees, counts, counting, fns)
		if len(sites) == 0 || unitsOps[op.Name] {
			continue
		}
		declared := map[string]bool{}
		if op.Cost.Const != 0 {
			declared[""] = true
		}
		for _, term := range op.Cost.Terms {
			declared[term.Quantity] = true
		}
		if len(declared) == 0 {
			t.Errorf("%s reaches a count but declares no cost", op.Name)
			continue
		}
		var names []string
		for s := range sites {
			names = append(names, s)
		}
		sort.Strings(names)
		for _, s := range names {
			reached[s] = true
			qs, ok := siteCost[s]
			if !ok {
				t.Errorf("%s reaches the count site %s, which siteCost does not attribute", op.Name, s)
				continue
			}
			if qs == nil {
				continue
			}
			if !slices.ContainsFunc(qs, func(q string) bool { return declared[q] }) {
				t.Errorf("%s reaches the count site %s but its cost %s has none of %q", op.Name, s, op.Cost, qs)
			}
		}
	}
	for s := range siteCost {
		if !reached[s] {
			t.Errorf("siteCost attributes %s, which no host operation reaches", s)
		}
	}
}
