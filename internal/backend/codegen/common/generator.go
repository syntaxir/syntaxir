package common

import (
	"fmt"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/runtime/limits"
	"syntaxir.dev/sxir/internal/runtime/program"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Generator is the state that the Go and TypeScript generators share: the program and rule being
// printed, the generation switches both read, the expectation table, the shared names of
// literals, methods and field type variables, and the Exec IR of the rules printed from it.
type Generator struct {
	Observability bool
	// Logical counts logical compute units in every form (the ordinary form, or a Document's), which
	// a parser with limit controls does: loops keep their step,
	// which the printer prints as a check of the counter.
	Logical bool
	// Ordinary prints the bodies in their ordinary form (no budget controls and no Documents): the
	// helpers with an Ordinary form, no charges and inline class tests. The Flow passes choose the lean
	// forms of such functions (terminal programs, class runs and tables, guarded classes) that the
	// backend prints (PrimTarget).
	Ordinary bool
	Document bool
	// Traced reports the traced pass of a Go parser generated with observability: it prints the copies
	// of the rule tables that an observed parse runs (named with TracedSuffix), whose bodies report the
	// rule calls and record, in the primitives that read the input, how far each call examined it.
	Traced   bool
	PrintErr error // the first rule, line or stream body that the printer does not print, or the first internal error of the unread-variable check
	// Only exact emitted bodies with the same signature share a helper. Rule
	// wrappers, memo IDs, frame layouts and expectation IDs remain in the code.
	methodNames  map[generatedMethod]string
	literalNames map[string]string
	methodKeys   []generatedMethod
	literalKeys  []string
	// Params are the values of the parameters of the printing step that runs the generator.
	Params Params
	// Exec is the table being printed, at the Exec stage, or nil: its plan, its module and the Core
	// IR it was lowered from, which holds the rules, the expectations and the origin map (Core).
	Exec *plandata.Table[code.Exec]
	// sites numbers the action sites of sitesOf, the Core IR of the table last printed (ActionSite).
	sites   *core.ActionSites
	sitesOf *core.Module
	// funcs and inPlace index the functions of the sites of funcsOf, the Action module of the table
	// last printed (SiteFunc, action.SiteFuncs).
	funcs, inPlace map[int]*code.Func
	funcsOf        *code.Module
	Prog           *Program
	Table          string // the rule table being generated: rules, or recRules for Recognize
	Cur            *Rule  // the rule being generated
	Vars           strings.Builder
	Init           strings.Builder
	Methods        strings.Builder
	n              int
	// Descs is the expectation table of the generated parser (descs in runtime); descIDs maps a
	// description to its ID.
	Descs   []string
	DescIDs map[string]program.ExpID
	// FieldTypes names the field type variable of each checked constructed field, result and cast (by
	// *ast.FieldInit or checkInfo.results key), numbered separately so that checks do not
	// rename other generated names (pkg/runtime/libgo/fieldcheck.go).
	FieldTypes map[any]string
	// fieldTypeKeys are the keys of fieldTypes in the order of their variables, for rollback.
	fieldTypeKeys []any
}

// FieldTypeVar returns the variable holding check type ft of key, declaring it with decl the
// first time.
func (g *Generator) FieldTypeVar(key any, ft types.Ty, decl func(v string, t *types.FieldType) string) string {
	v, ok := g.FieldTypes[key]
	if !ok {
		if g.FieldTypes == nil {
			g.FieldTypes = map[any]string{}
		}
		v = fmt.Sprintf("fieldType%d", len(g.FieldTypes)+1)
		g.FieldTypes[key] = v
		g.fieldTypeKeys = append(g.fieldTypeKeys, key)
		g.Vars.WriteString(decl(v, types.NewFieldType(ft)))
	}
	return v
}

// GeneratedMaxDepth returns the maximum depth that a generated parser enforces: o.MaxDepth, or the
// default depth when it is zero.
func GeneratedMaxDepth(o GenOptions) int {
	if o.MaxDepth == 0 {
		return limits.DefaultMaxDepth
	}
	return o.MaxDepth
}

type generatedMethod struct {
	signature string
	body      string
}

// MethodName returns an existing exact helper or reserves the next name.
// The signature keeps Node, typed Go and TypeScript functions distinct.
func (g *Generator) MethodName(signature, body string) (string, bool) {
	if !g.Params.MethodSharing {
		return g.Name("e"), false
	}
	key := generatedMethod{signature, body}
	if name, ok := g.methodNames[key]; ok {
		return name, true
	}
	if g.methodNames == nil {
		g.methodNames = make(map[generatedMethod]string)
	}
	name := g.Name("e")
	g.methodNames[key] = name
	g.methodKeys = append(g.methodKeys, key)
	return name, false
}

// LiteralName shares immutable literal tables by decoded text. Descriptions
// belong to each matcher, so spellings with different expectations stay distinct.
func (g *Generator) LiteralName(value string) (string, bool) {
	if !g.Params.LiteralSharing {
		return g.Name("lit"), false
	}
	if name, ok := g.literalNames[value]; ok {
		return name, true
	}
	if g.literalNames == nil {
		g.literalNames = make(map[string]string)
	}
	name := g.Name("lit")
	g.literalNames[value] = name
	g.literalKeys = append(g.literalKeys, value)
	return name, false
}

// SeedDescs starts the table of expectation descriptions with the fixed ones.
func (g *Generator) SeedDescs() {
	if g.DescIDs != nil {
		return
	}
	g.DescIDs = map[string]program.ExpID{}
	for i, d := range core.FixedDescs {
		g.Descs = append(g.Descs, d)
		g.DescIDs[d] = program.ExpID(i)
	}
}

// Desc returns the expectation ID of description s, adding it to the table.
func (g *Generator) Desc(s string) program.ExpID {
	g.SeedDescs()
	if id, ok := g.DescIDs[s]; ok {
		return id
	}
	id := program.ExpID(len(g.Descs))
	g.Descs = append(g.Descs, s)
	g.DescIDs[s] = id
	return id
}

// Name returns a fresh name: prefix followed by a number unique within the generator.
func (g *Generator) Name(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

// Core returns the Core IR that the table being printed (Exec) was lowered from.
func (g *Generator) Core() *core.Module { return g.Exec.IR.M }

// ActionSite returns the action site numbered i in the Core IR of the table being printed, which
// the operations of predicates, bindings and actions name (core.ActionSites).
func (g *Generator) ActionSite(i int) core.ActionSite {
	g.actionSites()
	return g.sites.Sites[i]
}

// actionSites numbers the action sites of the Core IR of the table being printed (sites).
func (g *Generator) actionSites() {
	if m := g.Core(); g.sitesOf != m {
		g.sites, g.sitesOf = core.ActionSitesOf(m), m
	}
}

// RuleOf returns the rule of the program with the ID id, value-free twins included.
func (g *Generator) RuleOf(id int) *Rule {
	if id < len(g.Prog.RuleList) {
		return g.Prog.RuleList[id]
	}
	return g.Prog.Twins[id-len(g.Prog.RuleList)]
}

// RuleSite returns the number of the action site of the action of the rule r, the action expression
// that its value is made of, or -1 if it has none or the table has no Core IR.
func (g *Generator) RuleSite(r *Rule) int {
	if g.Exec == nil || r.ID >= len(g.Core().Rules) {
		return -1
	}
	cr := g.Core().Rules[r.ID]
	if cr == nil || cr.Finish.Kind != core.FinishAction {
		return -1
	}
	g.actionSites()
	return g.sites.OfRule(r.ID)
}

// LineSite returns the number of the action site of the action of the line l of an Operators Rule, or
// -1 if l is nil.
func (g *Generator) LineSite(l *core.OpLine) int {
	if l == nil {
		return -1
	}
	g.actionSites()
	return g.sites.Of(l)
}

// SiteFunc returns the Action module of the table being printed (plandata.TableIR.Actions) and the
// function of its action site i: the function that returns the value and the failure, or, if
// inPlace, the function in place of a rule's action (action.Site.Result). A site without the function,
// a negative one included, is an internal error.
func (g *Generator) SiteFunc(i int, inPlace bool) (*code.Module, *code.Func) {
	m := g.Exec.IR.Actions.Module()
	if m == nil {
		panic(fmt.Errorf("internal error: the table %s has no Action module", g.Table))
	}
	if g.funcsOf != m {
		g.funcs, g.inPlace = action.SiteFuncs(m)
		g.funcsOf = m
	}
	f := g.funcs[i]
	if inPlace {
		f = g.inPlace[i]
	}
	if f == nil {
		panic(fmt.Errorf("internal error: rule %s: no function of the action site %d (in place: %t)", g.Cur.Name, i, inPlace))
	}
	return m, f
}

// Unprintable records that the printer does not print a part of a table, which fails the generation: every
// table is printed from Exec IR and has no other route.
func (g *Generator) Unprintable(what string) {
	if g.PrintErr == nil {
		g.PrintErr = fmt.Errorf("the printer does not print %s", what)
	}
}

// IRTable is the name of the table being generated in the dumps of the IR: "typed" for the typed
// rules, as Core IR names it, and the name of the table otherwise; with TracedSuffix in the traced pass.
func (g *Generator) IRTable() string {
	if g.Table == "trules" {
		return "typed" + g.TracedSuffix()
	}
	return g.Table + g.TracedSuffix()
}

// TracedSuffix is the suffix of the names that the traced pass allocates (Traced), and "" otherwise.
func (g *Generator) TracedSuffix() string {
	if g.Traced {
		return "Traced"
	}
	return ""
}

// RecordsExamined reports whether the bodies being lowered record how far each call examines the
// input in the primitives that read it: those of a build with Documents, and the traced bodies.
func (g *Generator) RecordsExamined() bool { return g.Document || g.Traced }

// FrameScope returns the scope of the frame whose slots are names.
func FrameScope(names []string) *check.Scope {
	s := check.NewScope()
	for _, name := range names {
		s.Slot(name)
	}
	return s
}

// NodeJoin reports whether the join u has a Go interface and a TypeScript union of its own: every
// member is a struct type, a named node type, an unnamed node type or error, and no two unnamed
// node members admit nodes of one kind (types.NodeKinds), which the checker reports for a join
// written so. A value of such a join converts to the member that its run-time type selects
// (JoinNodeMembers).
func NodeJoin(u types.JoinTy) bool {
	seen := map[string]bool{}
	for _, a := range u.Alts {
		switch a := a.(type) {
		case types.NamedTy:
			if a.Kind != 's' {
				return false
			}
		case types.NodeTy:
			if a.Name != "" {
				continue
			}
			for _, k := range types.NodeKinds(a) {
				if seen[k] {
					return false
				}
				seen[k] = true
			}
		case types.BasicTy:
			if a != types.TyErr {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// JoinNodeMembers returns the unnamed node members of the node join u (NodeJoin) by the kinds of
// node they take: valueimpl.TypeString, TypeTuple and TypeList, in that order, each with its
// member. Open is the member node[_] (or node[unknown]) if u has it: it takes every node that no
// other member takes by its name.
func JoinNodeMembers(u types.JoinTy) (kinds []KindMember, open bool) {
	for _, a := range u.Alts {
		n, ok := a.(types.NodeTy)
		if !ok || n.Name != "" {
			continue
		}
		if n.Value == types.TyTop || n.Value == types.TyAny {
			open = true
		}
		for _, k := range types.NodeKinds(n) {
			kinds = append(kinds, KindMember{k, n})
		}
	}
	slices.SortStableFunc(kinds, func(a, b KindMember) int { return kindOrder(a.Kind) - kindOrder(b.Kind) })
	return kinds, open
}

// KindMember is an unnamed node member of a join with a kind of node it takes.
type KindMember struct {
	Kind   string // valueimpl.TypeString, TypeTuple or TypeList
	Member types.NodeTy
}

func kindOrder(k string) int {
	switch k {
	case valueimpl.TypeString:
		return 0
	case valueimpl.TypeTuple:
		return 1
	}
	return 2
}

// NamedNodeKind returns the kind of the nodes of the named node type whose underlying node type is
// under, and whether it has exactly one: a node of a named node type that is no member of a join
// converts to the unnamed node member of its kind, the member it is assignable to.
func NamedNodeKind(under types.NodeTy) (string, bool) {
	ks := types.NodeKinds(under)
	if len(ks) != 1 {
		return "", false
	}
	return ks[0], true
}
