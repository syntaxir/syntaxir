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

// rowFacets are the facets of the rows of shapes whose conversions differ by the value: a row is
// witnessed when a case witnesses each of its facets. An option converts a present value and an
// absent one, whose typed value is nil (null) for every element type and so also for the scalars,
// whose typed values are otherwise never nil; a tuple converts its elements, absent ones among
// them, and an unset tuple, whose typed value is the tuple of the zero values of its elements.
// Every other shape has the one facet facetPresent.
var rowFacets = map[typedShape][]string{
	shapeOptional: {facetPresent, "absent", "absent int", "absent string", "absent bool"},
	shapeTuple:    {facetPresent, "absent element", "unset"},
}

const facetPresent = "present"

// facetsOf returns the facets of the shapes that map by row.
func (r TypedRows) facetsOf(row string) []string {
	var out []string
	for sh, rr := range r.row {
		if rr != row {
			continue
		}
		fs := rowFacets[sh]
		if fs == nil {
			fs = []string{facetPresent}
		}
		for _, f := range fs {
			if !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	slices.Sort(out)
	return out
}

// witness names a facet of a row.
func witness(row, facet string) string { return row + " (" + facet + ")" }

// Uncovered returns the facets of the rows of the table, as "row (facet)", that no case of cases
// witnesses (Witnesses), and for each witnessed facet the first case that does.
func (r TypedRows) Uncovered(t *testing.T, cases []Case) (missing []string, first map[string]int) {
	t.Helper()
	var all []string
	for _, row := range r.Rows {
		for _, f := range r.facetsOf(row) {
			all = append(all, witness(row, f))
		}
	}
	first = map[string]int{}
	for i, c := range cases {
		if len(first) == len(all) {
			break
		}
		if !slices.ContainsFunc(r.RowsOf(t, c), func(row string) bool {
			return slices.ContainsFunc(r.facetsOf(row), func(f string) bool { _, ok := first[witness(row, f)]; return !ok })
		}) {
			continue
		}
		for _, w := range r.Witnesses(t, c) {
			if _, ok := first[w]; !ok {
				first[w] = i
			}
		}
	}
	for _, w := range all {
		if _, ok := first[w]; !ok {
			missing = append(missing, w)
		}
	}
	return missing, first
}

// Witnesses returns the facets of the rows of RowsOf(c), as "row (facet)", that the values of the
// inputs of c witness: those the engine returns on an input of c, at the limits of missing tokens
// that the comparisons of typed values run (none, and one that inserts when the grammar uses
// #missing). Each value is walked with the type of main, as the typed parsers convert it, and a
// facet is witnessed only where a value of its shape occurs: a struct, node or scalar value of its
// type, a list with elements, a present or an absent option, a tuple and each of its present,
// absent and unset forms, the member of a join that the value is, a recovered error or missing
// value, and an alias where a value of a type written with it occurs. A grammar whose inputs all
// fail witnesses nothing, whatever its types.
func (r TypedRows) Witnesses(t *testing.T, c Case) []string {
	t.Helper()
	limits := []int{0}
	if InsertsMissing(t, c) {
		limits = append(limits, 8)
	}
	prog := refProgram(t, c.Src, check.Options{})
	w := newShapeWalker(t, c)
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
			w.main(v)
		}
	}
	var out []string
	for sh, facets := range w.facets {
		row, ok := r.row[sh]
		if !ok {
			continue
		}
		for f := range facets {
			if wf := witness(row, f); !slices.Contains(out, wf) {
				out = append(out, wf)
			}
		}
	}
	slices.Sort(out)
	return out
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
	w := newShapeWalker(t, c)
	w.visit(w.RuleRef("main"))
	if def := w.prog.ByName["main"]; def != nil && def.Def.Type != nil {
		w.written(def.Def.Type)
	}
	return w.shapes
}

// newShapeWalker returns a walker of the types of the grammar of c, checked with the start rule main.
func newShapeWalker(t testing.TB, c Case) *shapeWalker {
	t.Helper()
	prog := checkMain(t, c)
	if prog.Typed == nil {
		t.Fatalf("%s: the grammar has no types", c.Name)
	}
	return shapeWalkerOf(t, c, prog)
}

// shapeWalkerOf returns a walker of the types of prog, the checked grammar of c, which has types.
func shapeWalkerOf(t testing.TB, c Case, prog *common.Program) *shapeWalker {
	w := &shapeWalker{t: t, name: c.Name, prog: prog, seen: map[string]bool{}, joins: map[string]bool{}, aliasJoins: map[string]bool{}, facets: map[typedShape]map[string]bool{}}
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
	return w
}

// shapeWalker gathers the shapes of the types that a typed parser converts: visit those of the types
// it maps, valueOf the facets of the shapes of the values it converts.
type shapeWalker struct {
	common.ValueResolver
	t    testing.TB
	name string
	prog *common.Program
	seen map[string]bool
	// joins holds the joins with an interface of their own, by their written forms, and aliasJoins
	// those of them that an alias declares; aliases are the aliases of other types.
	joins, aliasJoins map[string]bool
	aliases           []string
	missing           bool
	shapes            []typedShape
	facets            map[typedShape]map[string]bool
}

// add adds the shape s, with the facet f of its values.
func (w *shapeWalker) add(s typedShape, f ...string) {
	if !slices.Contains(w.shapes, s) {
		w.shapes = append(w.shapes, s)
	}
	if w.facets[s] == nil {
		w.facets[s] = map[string]bool{}
	}
	if len(f) == 0 {
		f = []string{facetPresent}
	}
	for _, f := range f {
		w.facets[s][f] = true
	}
}

// aliasName matches an identifier of a written type.
var aliasName = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// written adds the alias shape if the written type x names an alias of a type other than a node
// join (the types the checker resolves no longer name their aliases), and of the aliases that the
// alias it names is written with.
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

// basicShape returns the shape of the basic type x, as goType of the Go generator maps it: int,
// string and bool by their rows, error by that of recovered errors, and the open types by `_`. An
// unknown basic type fails the walk, so a basic type that the generators come to map is mapped here.
func (w *shapeWalker) basicShape(x types.BasicTy) typedShape {
	switch x {
	case types.TyInt:
		return shapeInt
	case types.TyString, types.TyBool:
		return shapeStringOrBool
	case types.TyErr:
		if w.missing {
			return shapeErrorValue
		}
		return shapeError
	case types.TyAny, types.TyTop, types.TyNever, types.TyNil, types.TyNode:
		return shapeAny
	}
	w.t.Fatalf("%s: the tables of typed values have no row for the basic type %s", w.name, x)
	return 0
}

// unknownType fails the walk on a kind of type that the tables of typed values do not map, so a
// kind of type that the generators come to map is mapped here.
func (w *shapeWalker) unknownType(x types.Ty) {
	w.t.Fatalf("%s: the tables of typed values have no row for the type %s (%T)", w.name, x, x)
}

func (w *shapeWalker) visit(x types.Ty) {
	key := fmt.Sprintf("%T:%s", x, x)
	if w.seen[key] {
		return
	}
	w.seen[key] = true
	switch x := x.(type) {
	case types.BasicTy:
		w.add(w.basicShape(x))
	case common.Vrule:
		w.add(shapeUndeclared)
		w.visit(w.Vres[x.Name])
	case types.RuleTy:
		// The generators map a type that names a rule, which only the checker's inference holds,
		// as `_`.
		w.add(shapeAny)
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
		case !w.sameGoShape(x.Alts):
			w.add(shapeAny)
			return
		}
		for _, a := range x.Alts {
			w.visit(a)
		}
	default:
		w.unknownType(x)
	}
}

// visitNodeValue visits the value of a node, which is open (any) for node[_].
func (w *shapeWalker) visitNodeValue(v types.Ty) {
	if v == types.TyTop || v == types.TyAny {
		return
	}
	w.visit(v)
}

// main adds the facets of the canonical engine value v of the start rule main, and the alias shape
// when the written type of main names an alias.
func (w *shapeWalker) main(v any) {
	w.valueOf(w.RuleRef("main"), v)
	if def := w.prog.ByName["main"]; def != nil && def.Def.Type != nil {
		w.written(def.Def.Type)
	}
}

// valueOf adds the facets of the shapes that the typed parsers convert the canonical engine value v
// of type x by, as visit walks the types: only where a value of a shape occurs. A nil v is an
// absent value: an absent option, an unset tuple, or a value of another type that converts to the
// zero value of its type, which shows no shape.
func (w *shapeWalker) valueOf(x types.Ty, v any) {
	switch x := x.(type) {
	case types.BasicTy:
		if v == nil {
			return
		}
		s := w.basicShape(x)
		if s == shapeErrorValue || s == shapeError {
			m, _ := v.(map[string]any)
			if typ := m["type"]; typ != "Error" && typ != "Missing" {
				w.t.Fatalf("%s: value %v of type error is no recovered error or missing value", w.name, v)
			}
		}
		w.add(s)
	case common.Vrule:
		if v != nil {
			w.add(shapeUndeclared)
		}
		w.valueOf(w.Vres[x.Name], v)
	case types.RuleTy:
		if v != nil {
			w.add(shapeAny)
		}
	case types.TupleTy:
		if v == nil {
			w.add(shapeTuple, "unset")
			return
		}
		elems, ok := v.([]any)
		if !ok || len(elems) != len(x.Elems) {
			w.t.Fatalf("%s: value %v of the tuple type %s is no tuple of its elements", w.name, v, x)
		}
		w.add(shapeTuple)
		for i, e := range x.Elems {
			if elems[i] == nil {
				w.add(shapeTuple, "absent element")
			}
			w.valueOf(e, elems[i])
		}
	case types.NodeTy:
		if v == nil {
			return
		}
		m := w.object(x, v, "value")
		if x.Name != "" {
			w.add(shapeNamedNode)
			if n, ok := w.prog.Check.NodeTypeOf(x.Name); ok {
				w.valueOfNode(n.Underlying().Value, m["value"])
			}
			return
		}
		w.add(shapeNode)
		w.valueOfNode(x.Value, m["value"])
	case types.RecordTy:
		if v != nil {
			w.object(x, v, "value")
			w.add(shapeNode)
		}
	case types.NamedTy:
		if v == nil {
			return
		}
		m := w.object(x, v, "fields")
		if x.Kind == 'c' {
			w.add(shapeMissing)
			return
		}
		w.add(shapeStruct)
		fields := m["fields"].(map[string]any)
		if td, ok := w.prog.Types[x.Name].(*ast.TypeDef); ok {
			for _, f := range td.Fields() {
				if fields[f.Name] != nil {
					w.written(f.Type)
				}
			}
		}
		for _, f := range w.prog.Typed.Fields[x.Name] {
			w.valueOf(f.T, fields[f.Name])
		}
	case types.ListTy:
		if v == nil {
			return
		}
		elems, ok := v.([]any)
		if !ok {
			w.t.Fatalf("%s: value %v of the list type %s is no list", w.name, v, x)
		}
		if len(elems) != 0 {
			w.add(shapeList)
		}
		for _, e := range elems {
			w.valueOf(x.Elem, e)
		}
	case types.OptTy:
		if v != nil {
			w.add(shapeOptional)
			w.valueOf(x.Elem, v)
			return
		}
		w.add(shapeOptional, "absent")
		if g := w.goShape(x.Elem); scalarShape(g) {
			w.add(shapeOptional, "absent "+g)
		}
	case types.JoinTy:
		if v == nil {
			return
		}
		switch {
		case w.aliasJoins[x.String()]:
			w.add(shapeAliasJoin)
		case w.joins[x.String()]:
			w.add(shapeInPlaceJoin)
		case !w.sameGoShape(x.Alts):
			w.add(shapeAny)
			return
		}
		for _, a := range x.Alts {
			if w.fits(a, v) {
				w.valueOf(a, v)
				return
			}
		}
		w.t.Fatalf("%s: value %v is no member of the join %s", w.name, v, x)
	default:
		w.unknownType(x)
	}
}

// valueOfNode adds the facets of the value v of a node, which is open (any) for node[_].
func (w *shapeWalker) valueOfNode(x types.Ty, v any) {
	if x == types.TyTop || x == types.TyAny {
		return
	}
	w.valueOf(x, v)
}

// object returns the canonical value v of type x, which must be an object with the key key.
func (w *shapeWalker) object(x types.Ty, v any, key string) map[string]any {
	m, ok := v.(map[string]any)
	if _, has := m[key]; !ok || !has {
		w.t.Fatalf("%s: value %v of type %s has no %s", w.name, v, x, key)
	}
	return m
}

// fits reports whether the canonical engine value v, which is not nil, is a value of the member x of a
// join: the members of a join are told apart by their kinds and the names of their types.
func (w *shapeWalker) fits(x types.Ty, v any) bool {
	m, isObject := v.(map[string]any)
	switch x := x.(type) {
	case common.Vrule:
		return w.fits(w.Vres[x.Name], v)
	case types.BasicTy:
		switch x {
		case types.TyInt:
			_, ok := v.(float64)
			return ok
		case types.TyString:
			_, ok := v.(string)
			return ok
		case types.TyBool:
			_, ok := v.(bool)
			return ok
		case types.TyErr:
			return isObject && (m["type"] == "Error" || m["type"] == "Missing")
		}
		return true
	case types.TupleTy:
		elems, ok := v.([]any)
		return ok && len(elems) == len(x.Elems)
	case types.ListTy:
		_, ok := v.([]any)
		return ok
	case types.OptTy:
		return w.fits(x.Elem, v)
	case types.NodeTy:
		_, hasValue := m["value"]
		if x.Name != "" {
			return hasValue && m["type"] == x.Name
		}
		return hasValue
	case types.RecordTy:
		_, hasValue := m["value"]
		return hasValue
	case types.NamedTy:
		if x.Kind == 'c' {
			return isObject && m["type"] == "Missing"
		}
		_, hasFields := m["fields"]
		return hasFields && m["type"] == x.Name
	case types.JoinTy:
		return slices.ContainsFunc(x.Alts, func(a types.Ty) bool { return w.fits(a, v) })
	}
	return true
}

// sameGoShape reports whether the types alts have one Go type: then a join of them without an
// interface has that type, as joinGoType of the Go generator gives it, and converts as its first
// member.
func (w *shapeWalker) sameGoShape(alts []types.Ty) bool {
	if len(alts) == 0 {
		return false
	}
	first := w.goShape(alts[0])
	for _, a := range alts[1:] {
		if w.goShape(a) != first {
			return false
		}
	}
	return true
}

// scalarShape reports whether the Go type key g of goShape is a scalar, which an option points to.
func scalarShape(g string) bool { return g == "int" || g == "string" || g == "bool" }

// goShape returns a key of the Go type of x, by the cases of goType of the Go generator: rules by the
// Go types of their values, options of types whose Go types are no scalars and no tuples by the Go
// types of their elements, and the open types, the joins without an interface or one Go type and the types
// that goType does not map by `any`.
func (w *shapeWalker) goShape(x types.Ty) string {
	switch x := x.(type) {
	case types.BasicTy:
		switch x {
		case types.TyInt, types.TyString, types.TyBool, types.TyErr:
			return string(x)
		}
	case common.Vrule:
		return w.goShape(w.Vres[x.Name])
	case types.TupleTy:
		keys := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			keys[i] = w.goShape(e)
		}
		return "(" + strings.Join(keys, ", ") + ")"
	case types.NodeTy:
		switch {
		case x.Name != "":
			return "*" + x.Name
		case x.Value == types.TyTop || x.Value == types.TyAny:
			return "*TNode[any]"
		}
		return "*TNode[" + w.goShape(x.Value) + "]"
	case types.RecordTy:
		return "*TNode[any]"
	case types.NamedTy:
		if x.Kind == 'c' {
			return "*Missing"
		}
		return "*" + x.Name
	case types.ListTy:
		return "[]" + w.goShape(x.Elem)
	case types.OptTy:
		// An option points to a scalar or a tuple, by the Go type of its element (optKind of the
		// Go generator).
		g := w.goShape(x.Elem)
		if scalarShape(g) || strings.HasPrefix(g, "(") {
			return "*" + g
		}
		return g
	case types.JoinTy:
		if w.joins[x.String()] {
			return "join " + x.String()
		}
		if w.sameGoShape(x.Alts) {
			return w.goShape(x.Alts[0])
		}
	}
	return "any"
}

// FieldZerosOf returns the zero values of the fields of the struct types of the grammar of c, in
// their canonical typed forms: 0, "" and false for the scalars, the tuple of the zero values of its
// elements for a tuple, that of its common type for a join without an interface whose members share
// their Go type, and nil (null) for every other type, options of scalars among them. A grammar
// without types has no structs.
func FieldZerosOf(t testing.TB, c Case) FieldZeros {
	t.Helper()
	zeros := FieldZeros{}
	prog := checkMain(t, c)
	if prog.Typed == nil {
		return zeros
	}
	w := shapeWalkerOf(t, c, prog)
	for name, fields := range w.prog.Typed.Fields {
		zeros[name] = map[string]any{}
		for _, f := range fields {
			zeros[name][f.Name] = w.zero(f.T)
		}
	}
	return zeros
}

// zero returns the canonical typed zero value of type x (FieldZerosOf).
func (w *shapeWalker) zero(x types.Ty) any {
	switch x := x.(type) {
	case types.BasicTy:
		switch x {
		case types.TyInt:
			return 0.0
		case types.TyString:
			return ""
		case types.TyBool:
			return false
		}
	case common.Vrule:
		return w.zero(w.Vres[x.Name])
	case types.TupleTy:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = w.zero(e)
		}
		return out
	case types.JoinTy:
		if !w.joins[x.String()] && w.sameGoShape(x.Alts) {
			return w.zero(x.Alts[0])
		}
	}
	return nil
}
