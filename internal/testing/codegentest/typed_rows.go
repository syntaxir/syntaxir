package codegentest

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TypedRows is one of the tables of typed values of spec/runtime/generated-parsers.md (Typed Values):
// the Go table or the TypeScript table. Each row is named by a prefix of its first column, as the
// table writes it, which TestTypedRowsNameTheSpecRows holds to the rows of the table.
type TypedRows struct {
	Name string
	Rows []string
	// row returns the rows of the table that a type of the given shape maps by.
	row map[typedShape]string
}

// typedShape is what a type that a typed parser converts is, for the tables of typed values: the
// shapes are the rows of the Go table, and the TypeScript table maps them onto its own rows.
type typedShape int

const (
	shapeInt typedShape = iota
	shapeStringOrBool
	shapeStruct
	shapeNode
	shapeNamedNode
	shapeTuple
	shapeError
	shapeMissing
	shapeErrorValue
	shapeAliasJoin
	shapeInPlaceJoin
	shapeList
	shapeOptional
	shapeUndeclared
	shapeAny
	shapeAlias
)

// GoTypedRows is the Go table.
var GoTypedRows = typedRows("Go", map[typedShape]string{
	shapeInt:          "`int`, `string`, `bool`",
	shapeStringOrBool: "`int`, `string`, `bool`",
	shapeStruct:       "Struct type `T`",
	shapeNode:         "`node[T]`",
	shapeNamedNode:    "Named node type `N = node[T]`",
	shapeTuple:        "Tuple `(A, B, ...)`",
	shapeError:        "`error` (from `#recover`)",
	shapeMissing:      "`missing` (from `#missing`)",
	shapeErrorValue:   "`error` in a grammar with `#missing`",
	shapeAliasJoin:    "Join `U ~ A | B` of node types",
	shapeInPlaceJoin:  "Join of node types written in place",
	shapeList:         "`[]T`",
	shapeOptional:     "`*T`",
	shapeUndeclared:   "A rule whose type is not declared",
	shapeAny:          "`_`, joins with basic members",
	shapeAlias:        "Alias `Alias ~ T`",
})

// TSTypedRows is the TypeScript table. A rule whose type is not declared has no row of its own
// there: its value maps by the rows of its value's type.
var TSTypedRows = typedRows("TypeScript", map[typedShape]string{
	shapeInt:          "`int`",
	shapeStringOrBool: "`string`, `bool`",
	shapeStruct:       "Struct",
	shapeNode:         "`node[T]`",
	shapeNamedNode:    "Named node type `N = node[T]`",
	shapeTuple:        "Tuple `(A, B, ...)`",
	shapeError:        "`error` (from `#recover`)",
	shapeMissing:      "`missing` (from `#missing`)",
	shapeErrorValue:   "`error` (from `#recover`)",
	shapeAliasJoin:    "Join of node types, declared by an alias or written in place",
	shapeInPlaceJoin:  "Join of node types, declared by an alias or written in place",
	shapeList:         "`[]T`",
	shapeOptional:     "`*T`",
	shapeAny:          "Join with a member that is no node type, `_`",
	shapeAlias:        "Declared alias",
})

func typedRows(name string, row map[typedShape]string) TypedRows {
	var rows []string
	for _, r := range row {
		if !slices.Contains(rows, r) {
			rows = append(rows, r)
		}
	}
	slices.Sort(rows)
	return TypedRows{Name: name, Rows: rows, row: row}
}

// RowsOf returns the rows of the table that the typed values of the start rule main of c map by:
// those of the types its typed parser converts, from the type of main through the fields of its
// structs, the elements of its lists, tuples and options, the values of its nodes, the members
// of its joins and the value types of the rules whose types are not declared. The shapes follow
// the type mapping of the generators (goType of the Go generator, tsType of the TypeScript one);
// a join that has no interface of its own maps by the rows of its members when they share their
// Go type, and as `_` otherwise.
func (r TypedRows) RowsOf(t testing.TB, c Case) []string {
	t.Helper()
	shapes := typedShapesOf(t, c)
	var rows []string
	for _, s := range shapes {
		if row, ok := r.row[s]; ok && !slices.Contains(rows, row) {
			rows = append(rows, row)
		}
	}
	slices.Sort(rows)
	return rows
}

// Uncovered returns the rows of the table that no case of cases witnesses (Witnesses), and for each
// row the first case that does.
func (r TypedRows) Uncovered(t *testing.T, cases []Case) (missing []string, first map[string]int) {
	t.Helper()
	first = map[string]int{}
	for i, c := range cases {
		if len(first) == len(r.Rows) {
			break
		}
		if !slices.ContainsFunc(r.RowsOf(t, c), func(row string) bool { _, ok := first[row]; return !ok }) {
			continue
		}
		for _, row := range r.Witnesses(t, c) {
			if _, ok := first[row]; !ok {
				first[row] = i
			}
		}
	}
	for _, row := range r.Rows {
		if _, ok := first[row]; !ok {
			missing = append(missing, row)
		}
	}
	return missing, first
}

// Witnesses returns the rows of RowsOf(c) that the inputs of c witness: a row is witnessed when the
// engine returns a value on an input of c, at the limits of missing tokens that the comparisons of
// typed values run (none, and one that inserts when the grammar uses #missing), and for the rows of
// recovered errors and missing values when such a value holds one. A grammar whose inputs all fail
// converts no value, whatever its types.
func (r TypedRows) Witnesses(t *testing.T, c Case) []string {
	t.Helper()
	limits := []int{0}
	if InsertsMissing(t, c) {
		limits = append(limits, 8)
	}
	prog := refProgram(t, c.Src, check.Options{})
	accepted, holds := false, map[string]bool{}
	for _, in := range c.Inputs {
		for _, limit := range limits {
			n, _ := prog(in, false, false, limit, nil)
			if n == nil {
				continue
			}
			v, err := canonicalEngineValue(n)
			if err != nil {
				t.Fatalf("%s: input %q: %v", c.Name, in, err)
			}
			accepted = true
			canonicalStructsOf(v, holds)
		}
	}
	if !accepted {
		return nil
	}
	var rows []string
	for _, sh := range typedShapesOf(t, c) {
		row, ok := r.row[sh]
		if !ok || slices.Contains(rows, row) {
			continue
		}
		switch sh {
		case shapeError:
			ok = holds["Error"]
		case shapeMissing:
			ok = holds["Missing"]
		case shapeErrorValue:
			ok = holds["Error"] || holds["Missing"]
		}
		if ok {
			rows = append(rows, row)
		}
	}
	slices.Sort(rows)
	return rows
}

// canonicalStructsOf adds to holds the type of each struct of the canonical value v.
func canonicalStructsOf(v any, holds map[string]bool) {
	switch v := v.(type) {
	case []any:
		for _, x := range v {
			canonicalStructsOf(x, holds)
		}
	case map[string]any:
		if typ, ok := v["type"].(string); ok {
			holds[typ] = true
		}
		for _, x := range v {
			canonicalStructsOf(x, holds)
		}
	}
}

// RequireCovered fails t for each row of the table that no case of cases witnesses.
func (r TypedRows) RequireCovered(t *testing.T, cases []Case) {
	t.Helper()
	r.covered(t, cases)
}

// covered is Uncovered that fails t for each row that no case witnesses.
func (r TypedRows) covered(t *testing.T, cases []Case) (first map[string]int) {
	t.Helper()
	missing, first := r.Uncovered(t, cases)
	for _, row := range missing {
		t.Errorf("no case witnesses the %s row %s of the table of typed values: none has an input whose value the row maps", r.Name, row)
	}
	return first
}

// Cover returns the first case of cases that witnesses each row of the table, in the order of
// cases, after failing t for the rows that no case witnesses: a run of few cases that converts
// values of every row.
func (r TypedRows) Cover(t *testing.T, cases []Case) []Case {
	t.Helper()
	first := r.covered(t, cases)
	var idx []int
	for _, i := range first {
		if !slices.Contains(idx, i) {
			idx = append(idx, i)
		}
	}
	slices.Sort(idx)
	out := make([]Case, len(idx))
	for k, i := range idx {
		out[k] = cases[i]
	}
	return out
}

// Cases returns the cases of cases that a test of the table runs: in the default tier the cases
// of Cover, which witness every row, in the long tier (go test -tags sxir_long) all of them. It fails t
// for the rows that no case witnesses.
func (r TypedRows) Cases(t *testing.T, cases []Case) []Case {
	t.Helper()
	if allCases {
		r.RequireCovered(t, cases)
		return cases
	}
	return r.Cover(t, cases)
}

// InsertsMissing reports whether a rule of the grammar of c uses #missing, so that its parsers take a
// positive limit of missing tokens and insert with one.
func InsertsMissing(t testing.TB, c Case) bool {
	t.Helper()
	return checkMain(t, c).HasMissing
}

// checkMain checks the grammar of c for generation with the start rule main.
func checkMain(t testing.TB, c Case) *common.Program {
	t.Helper()
	if Compile == nil {
		t.Fatal("codegentest.Compile is not set: the test package must set it before checking grammars")
	}
	g, err := compiletest.Parse(c.Src)
	if err != nil {
		t.Fatalf("%s: %v", c.Name, err)
	}
	p, err := driver.CheckWith(context.Background(), Compile, g, common.CompileOptions{}, "main")
	if err != nil {
		t.Fatalf("%s: %v", c.Name, err)
	}
	return p.View()
}

// typedShapesOf returns the shapes of the types that the typed parser of c converts (RowsOf).
func typedShapesOf(t testing.TB, c Case) []typedShape {
	t.Helper()
	prog := checkMain(t, c)
	if prog.Typed == nil {
		t.Fatalf("%s: the grammar has no types", c.Name)
	}
	w := &shapeWalker{prog: prog, seen: map[string]bool{}, joins: map[string]bool{}, aliasJoins: map[string]bool{}}
	for name, a := range prog.Typed.Aliases {
		if u, ok := a.(types.JoinTy); ok && common.NodeJoin(u) {
			w.joins[u.String()], w.aliasJoins[u.String()] = true, true
		} else if _, ok := prog.Types[name].(*ast.TypeAlias); ok {
			w.aliases = append(w.aliases, name)
		}
	}
	w.ValueResolver = common.NewValueResolver(prog, func(x types.Ty) bool { return w.joins[x.String()] })
	for _, j := range w.AnonJoins() {
		for _, s := range append([]string{j.Members.String()}, j.Spellings...) {
			w.joins[s] = true
			if j.Alias != "" {
				w.aliasJoins[s] = true
			}
		}
	}
	w.missing = prog.MissingType
	w.visit(w.RuleRef("main"))
	if def := prog.ByName["main"]; def != nil && def.Def.Type != nil {
		w.written(def.Def.Type)
	}
	return w.shapes
}

// shapeWalker gathers the shapes of the types that a typed parser converts.
type shapeWalker struct {
	common.ValueResolver
	prog *common.Program
	seen map[string]bool
	// joins holds the joins with an interface of their own, by their written forms, and aliasJoins
	// those of them that an alias declares; aliases are the aliases of other types.
	joins, aliasJoins map[string]bool
	aliases           []string
	missing           bool
	shapes            []typedShape
}

func (w *shapeWalker) add(s typedShape) {
	if !slices.Contains(w.shapes, s) {
		w.shapes = append(w.shapes, s)
	}
}

// aliasName matches an identifier of a written type.
var aliasName = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// written adds the alias shape if the written type x names an alias of a type other than a node
// join (the types the checker resolves no longer name their aliases), and visits the fields of
// the structs it names.
func (w *shapeWalker) written(x ast.TypeExpr) {
	for _, name := range aliasName.FindAllString(grammar.FormatType(x), -1) {
		if slices.Contains(w.aliases, name) {
			w.add(shapeAlias)
			if a, ok := w.prog.Types[name].(*ast.TypeAlias); ok && !w.seen["alias:"+name] {
				w.seen["alias:"+name] = true
				w.written(a.Type)
			}
		}
	}
}

func (w *shapeWalker) visit(x types.Ty) {
	key := fmt.Sprintf("%T:%s", x, x)
	if w.seen[key] {
		return
	}
	w.seen[key] = true
	switch x := x.(type) {
	case types.BasicTy:
		switch x {
		case types.TyInt:
			w.add(shapeInt)
		case types.TyString, types.TyBool:
			w.add(shapeStringOrBool)
		case types.TyErr:
			if w.missing {
				w.add(shapeErrorValue)
			} else {
				w.add(shapeError)
			}
		default:
			w.add(shapeAny)
		}
	case common.Vrule:
		w.add(shapeUndeclared)
		w.visit(w.Vres[x.Name])
	case types.TupleTy:
		w.add(shapeTuple)
		for _, e := range x.Elems {
			w.visit(e)
		}
	case types.NodeTy:
		if x.Name != "" {
			w.add(shapeNamedNode)
			if n, ok := w.prog.Check.NodeTypeOf(x.Name); ok {
				w.visitNodeValue(n.Underlying().Value)
			}
			return
		}
		w.add(shapeNode)
		w.visitNodeValue(x.Value)
	case types.RecordTy:
		w.add(shapeNode)
	case types.NamedTy:
		if x.Kind == 'c' {
			w.add(shapeMissing)
			return
		}
		w.add(shapeStruct)
		if td, ok := w.prog.Types[x.Name].(*ast.TypeDef); ok {
			for _, f := range td.Fields() {
				w.written(f.Type)
			}
		}
		for _, f := range w.prog.Typed.Fields[x.Name] {
			w.visit(f.T)
		}
	case types.ListTy:
		w.add(shapeList)
		w.visit(x.Elem)
	case types.OptTy:
		w.add(shapeOptional)
		w.visit(x.Elem)
	case types.JoinTy:
		switch {
		case w.aliasJoins[x.String()]:
			w.add(shapeAliasJoin)
		case w.joins[x.String()]:
			w.add(shapeInPlaceJoin)
		case !sameGoShape(x.Alts):
			w.add(shapeAny)
			return
		}
		for _, a := range x.Alts {
			w.visit(a)
		}
	default:
		w.add(shapeAny)
	}
}

// visitNodeValue visits the value of a node, which is open (any) for node[_].
func (w *shapeWalker) visitNodeValue(v types.Ty) {
	if v == types.TyTop || v == types.TyAny {
		return
	}
	w.visit(v)
}

// sameGoShape reports whether the types alts share their Go type, by the parts of it that the
// mapping decides: then a join of them without an interface has that type, as joinGoType of the Go
// generator gives it.
func sameGoShape(alts []types.Ty) bool {
	for _, a := range alts[1:] {
		if goShape(a) != goShape(alts[0]) {
			return false
		}
	}
	return len(alts) > 0
}

// goShape returns a key of the Go type of x: the written type with the names of rules, whose Go
// types are their resolved types, kept apart.
func goShape(x types.Ty) string {
	switch x := x.(type) {
	case types.OptTy:
		if b, ok := x.Elem.(types.BasicTy); ok && (b == types.TyInt || b == types.TyString || b == types.TyBool) {
			return "*" + string(b)
		}
		if _, ok := x.Elem.(types.TupleTy); ok {
			return "*" + goShape(x.Elem)
		}
		return goShape(x.Elem)
	case common.Vrule:
		return "rule " + x.Name
	}
	return fmt.Sprintf("%T:%s", x, strings.TrimSpace(x.String()))
}
