package common

import (
	"fmt"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/limits"
	"syntaxir.dev/sxir/internal/runtime/program"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Generator is the state that the Go and TypeScript generators share: the program and rule being
// printed, the generation switches both read, the expectation table, the shared names of
// literals, methods and field type variables, and the Flow IR of the rules printed from it.
type Generator struct {
	Observability bool
	// NoBudget leaves the step charges out of Go output generated without budget controls, where
	// no control exists to charge: the statements, and the closures that only held one.
	NoBudget bool
	// Ordinary prints the bodies in their ordinary form (no budget controls and no Documents): the
	// helpers with an Ordinary form, no charges and inline class tests. The Flow passes choose the lean
	// forms of such functions (terminal programs, class runs and tables, guarded classes) that the
	// backend prints (FlowTarget).
	Ordinary bool
	// FlowTarget says which forms of the Flow passes the backend prints (flowpass.Target).
	FlowTarget flowpass.Target
	Document   bool
	// Traced reports the traced pass of a Go parser generated with observability: it prints the copies
	// of the rule tables that an observed parse runs (named with TracedSuffix), whose bodies report the
	// rule calls and record, in the primitives that read the input, how far each call examined it.
	Traced  bool
	FlowErr error // the first rule, line or stream body that Flow IR does not print, or the first internal error of the unread-variable check
	// TypeScript reports that the generator prints TypeScript, whose literal tables are shared only
	// with the optimization ts-literal-sharing.
	TypeScript bool
	// Only exact emitted bodies with the same signature share a helper. Rule
	// wrappers, memo IDs, frame layouts and expectation IDs remain in the code.
	methodNames  map[generatedMethod]string
	literalNames map[string]string
	methodKeys   []generatedMethod
	literalKeys  []string
	// Runner runs the pipelines of the IR with the optimizations that are on, the dumps of
	// GenOptions.PrintAfter and the verification of GenOptions.VerifyEachPass (Passes.Runner). It is
	// required: the optimizations are read only from it (Opt), and Passes has no other way to read
	// them, so that the printers and the passes cannot disagree on them.
	Runner Runner
	// Flow holds the Core IR of the typed rules while they are printed from Flow IR, or nil;
	// flowRules counts the rules printed from Flow IR as flat rules and flowFrames the bodies
	// and lines printed as matchers of runtime-owned frames.
	Flow  *FlowState
	Prog  *Program
	Table string // the rule table being generated: rules, or recRules for Recognize
	Cur   *Rule  // the rule being generated
	// Proj holds the projected repetition captures of the rule being generated (Projections).
	Proj    map[string]string
	Vars    strings.Builder
	Init    strings.Builder
	Methods strings.Builder
	n       int
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

// runner returns the Runner of the generator, and panics if there is none: a Generator is built
// with the Runner of its Passes, so a missing one is a programming error.
func (g *Generator) runner() Runner {
	if g.Runner == nil {
		panic("common.Generator has no Runner: set Generator.Runner to the Runner of its Passes before generating")
	}
	return g.Runner
}

// Opt returns the optimizations that are on: the settings of the Runner, the only source of them for a generator.
func (g *Generator) Opt() pass.Settings { return g.runner().Settings() }

// Core runs the elaboration build of the table's Core IR and the Core passes through the Runner.
func (g *Generator) Core(table string, build func() (*core.Module, error)) (*core.Module, error) {
	return g.runner().Core(table, build)
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

// UseSparseMemo removes the representation selector entirely when neither
// emitted rule table can cross the sparse-storage threshold.
func UseSparseMemo(o pass.Settings, prog, rec *Program) bool {
	return o.On("sparse-memo") && (prog.Run.NSeen > 64 || rec != nil && rec.Run.NSeen > 64)
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
	if !g.Opt().On("method-sharing") {
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
	if !g.Opt().On("literal-sharing") || g.TypeScript && !g.Opt().On("ts-literal-sharing") {
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

// Projections returns the projected repetition captures of the rule r (see ruleProjections), none
// in the recognizer.
func (g *Generator) Projections(r *Rule) map[string]string {
	if g.Table == "recRules" {
		return nil
	}
	return r.Projections
}

// FlowState holds the Core IR of the table being printed from Flow IR, with the options its functions
// are lowered with (plandata.TableIR), and the rules of its program.
type FlowState struct {
	*plandata.TableIR
	// Rules holds the rules of the program by ID, value-free twins included.
	Rules []*Rule
}

// CoreAction returns the action of Core IR of the rule r, the term that its value is made of, or nil
// if it has none or the table has no Core IR.
func (g *Generator) CoreAction(r *Rule) core.ActionExpr {
	if g.Flow == nil || r.ID >= len(g.Flow.M.Rules) {
		return nil
	}
	cr := g.Flow.M.Rules[r.ID]
	if cr == nil || cr.Finish.Kind != core.FinishAction {
		return nil
	}
	return cr.Finish.Action
}

// FailFlow records that Flow IR does not print a part of a table, which fails the generation: every
// table is printed from Flow IR and has no other route.
func (g *Generator) FailFlow(what string) {
	if g.FlowErr == nil {
		g.FlowErr = fmt.Errorf("Flow IR does not print %s", what)
	}
}

// FlowNodeFrameOK reports whether the rule r of the Node or recognizer table is printed from Flow IR as
// a framed body (flowplan.NodeFrameOK), with its Core rule. The typed table has no framed bodies.
func (g *Generator) FlowNodeFrameOK(r *Rule) (*core.Rule, bool) {
	if g.Flow == nil || g.Table == "trules" {
		return nil, false
	}
	cr := g.Flow.M.Rules[r.ID]
	if !flowplan.NodeFrameOK(r.PlanRule(), cr) {
		return nil, false
	}
	return cr, true
}

// FlowPasses runs the Flow part of the pipeline (Runner.Flow) on the functions of u, a unit of the
// table being generated, labeled "typed" for the typed rules as in the dumps of Core IR, and returns
// its module at the Flow stage. Every function that a generator prints goes through it. An error of
// the lowering is returned; a failure of the passes is kept as the first internal error of the
// generation (FlowErr), which fails it, and the module is returned for the printing to go on.
func (g *Generator) FlowPasses(u lowerdata.Unit) (*code.Module, error) {
	m, err := g.runner().Flow(g.IRTable(), u, g.Observability, g.FlowTarget)
	if m == nil {
		return nil, err
	}
	if err != nil && g.FlowErr == nil {
		g.FlowErr = fmt.Errorf("module %s of table %s: %w", m.Name, g.IRTable(), err)
	}
	return m, nil
}

// ExecPasses runs the Exec part of the pipeline (Runner.Exec) on the module m that FlowPasses
// returned, with target, the pipeline of the backend's own Exec passes, labeled with the table being
// generated as FlowPasses labels it, and returns its module at the Exec stage. An error of the
// conversion to Exec IR is returned; a failure of the passes is kept as the first internal error of
// the generation (FlowErr), which fails it. The printers print each function with its variables
// (execlower.View), so a pass keeps that function in the module and those locals in it: the verifier
// of the lowered function checks it at the end of the target's passes (execlower.Lowered.Keeps).
func (g *Generator) ExecPasses(m *code.Module, target TargetPipeline) (*code.Module, error) {
	x, err := g.runner().Exec(g.IRTable(), m, target, g.Observability)
	if x == nil {
		return nil, err
	}
	if err != nil && g.FlowErr == nil {
		g.FlowErr = fmt.Errorf("module %s of table %s: %w", x.Name, g.IRTable(), err)
	}
	return x, nil
}

// LowerTable lowers the functions of the plan p of the table being generated, with its decisions
// (flowplan.Unit; refuse is a test's hook, nil refuses none), through the Flow and the Exec part of
// the pipeline with target in one run (Runner.LowerTable), labeled as FlowPasses labels it, and
// returns the table at the Exec stage, whose functions the printers then print
// (execlower.Func). An error of the lowering or the conversion is returned; a failure of the
// passes is kept as the first internal error of the generation (FlowErr), which fails it, and the
// table is returned for the printing to go on.
func (g *Generator) LowerTable(p *plandata.Plan, refuse func(plandata.Key) bool, target TargetPipeline) (*plandata.Table[code.Exec], error) {
	r := g.runner()
	t, err := r.LowerTable(g.IRTable(), flowplan.Unit(p, g.Flow.TableIR, refuse), g.Observability, g.FlowTarget, target)
	if t == nil {
		return nil, err
	}
	if err != nil && g.FlowErr == nil {
		g.FlowErr = fmt.Errorf("module %s of table %s: %w", t.Module.Module().Name, g.IRTable(), err)
	}
	return t, nil
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

// StartRecFlow builds the Core IR of the program rec (the recognizer, or the program of the Node
// table) with the Core passes of the generator, and the options its functions are lowered with
// (flowplan.BuildTable), for printing its rules from Flow IR. The bytecode VM builds the Node table it
// runs from Flow IR through the same function.
func (g *Generator) StartRecFlow(rec *Program) error {
	c := plandata.TableConfig{Values: g.Table != "recRules", Budget: !g.NoBudget, Observe: g.Observability,
		Document: g.RecordsExamined(), Traced: g.Traced}
	t, err := flowplan.BuildTable(rec.Elab, c, func(build func() (*core.Module, error)) (*core.Module, error) {
		return g.Core(g.IRTable(), build)
	})
	if err != nil {
		return err
	}
	g.Flow = &FlowState{TableIR: t, Rules: append(append([]*Rule(nil), rec.RuleList...), rec.Twins...)}
	return nil
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
