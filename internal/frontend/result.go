package frontend

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

var _ elaborate.Program = (*Result)(nil)

// NewResult returns the Result of the checked model m of the grammar input (Result.Input), whose
// sites are numbered sites, with its run-time program. A Result is not changed after it is made, except by the front end that adds
// its Core IR module before handing it out.
func NewResult(m *check.Model, input *ast.Grammar, sites check.Sites) *Result {
	r := &Result{Model: m, Input: input, sites: sites}
	r.runtime = r.newRuntime()
	return r
}

// CoreInput returns the rules and declarations of the program as the Core IR builder reads them
// (elaborate.Program).
func (r *Result) CoreInput() *elaborate.Input {
	p := r.Model
	in := &elaborate.Input{Types: p.Types, Vars: p.Vars, ByName: map[string]*elaborate.Rule{}}
	all := append(append([]*check.Rule(nil), p.RuleList...), p.Twins...)
	in.Rules = make([]*elaborate.Rule, len(all))
	byRule := make(map[*check.Rule]*elaborate.Rule, len(all))
	for i, r := range all {
		br := &elaborate.Rule{ID: r.ID, Name: r.Name, Where: r.Where, Def: r.Def, Body: r.BodyExpr(), Action: r.Action, NodeType: r.NodeType,
			Memo: r.Memo, Transient: r.Transient, Plain: r.Plain, Rec: r.Rec, Leader: r.Leader, Splice: r.Splice, Novalue: r.Novalue,
			Lean: r.Lean, Seen: r.Seen, Vars: r.Vars}
		if r.PredCaps != nil {
			br.PredCaps = r.PredCaps
		}
		br.Projections = func() map[string]string { return check.RuleProjections(p, r) }
		in.Rules[i], byRule[r] = br, br
	}
	for _, r := range all {
		if r.Twin != nil {
			byRule[r].Twin = byRule[r.Twin]
		}
	}
	for _, r := range p.RuleList {
		in.ByName[r.Name] = byRule[r]
	}
	for _, name := range p.StartRules {
		in.Starts = append(in.Starts, in.ByName[name])
	}
	return in
}

// ResolveCheck returns the type that a cast to t checks, or nil if every value fits it
// (elaborate.Program).
func (r *Result) ResolveCheck(t ast.TypeExpr) types.Ty { return r.Checker().ResolveCheck(t) }

// MissingTokens reports that the grammar uses #missing (elaborate.Program).
func (r *Result) MissingTokens() bool { return r.HasMissing }

// CastPos returns the grammar position of the cast t as line and column, or 0, 0 when the program
// records no source information or the position is unknown.
func (r *Result) CastPos(t *ast.Cast) (int, int) {
	if !r.RecordsSourceInfo {
		return 0, 0
	}
	if pos := t.Diagnostic(); pos.IsValid() {
		return pos.Line, pos.Col
	}
	return 0, 0
}

// Resumable reports whether a repetition with the element e can be resumed (elaborate.Program). An
// element whose captures are written to the enclosing scope (those inside &, when the element has
// no captures of its own) cannot be: reusing it would not write them.
func (r *Result) Resumable(e ast.Expr) bool { return r.resumable(e, nil) }

func (r *Result) resumable(e ast.Expr, control *cancel.Control) bool {
	ok := check.ElementScopedControlled(e, control) || !anyCapture(e, control)
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		switch x := x.(type) {
		case *ast.Predicate:
			ok = false
		case *ast.Ref:
			if rule := r.ByName[x.Name]; rule == nil || len(rule.Vars) > 0 {
				ok = false
			}
		}
	}, control)
	return ok
}

// anyCapture reports whether e contains a capture anywhere, lookaheads included.
func anyCapture(e ast.Expr, control *cancel.Control) bool {
	control.Poll()
	found := false
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		if _, ok := x.(*ast.Capture); ok {
			found = true
		}
	}, control)
	return found
}

// Positional reports whether the value of e may contain position values (elaborate.Program).
func (r *Result) Positional(e ast.Expr) bool { return r.positional(e, nil) }

func (r *Result) positional(e ast.Expr, control *cancel.Control) bool {
	found := false
	astinspect.WalkExprControlled(e, func(x ast.Expr) {
		if x, ok := x.(*ast.Ref); ok {
			if rule := r.ByName[x.Name]; rule == nil || rule.Positional {
				found = true
			}
		}
	}, control)
	return found
}

// SkipSite returns the number of the skip-run site e (from 1), or 0 if e is not one.
func (r *Result) SkipSite(e *ast.Repeat) int { return r.sites.Skip[e] }

// RunSite returns the number of the run site e, or 0 if e is not one. Numbers follow those of the
// skip-run sites, so a site's records share the parser's tables with them.
func (r *Result) RunSite(e *ast.Repeat) int { return r.sites.Run[e] }

// ScanSite returns the number of the scan site e, or 0 if e is not one; the walks that build a
// value use the next number.
func (r *Result) ScanSite(e *ast.Repeat) int { return r.sites.Scan[e] }

// EnvFree reports whether the skip-run or run site is free of the environment: its records are
// kept by position alone, and a walk that reuses one keeps its own environment. Otherwise the
// records are keyed by the environment too, and a walk that reuses one takes its end environment.
func (r *Result) EnvFree(site int) bool {
	return site < len(r.sites.EnvFree) && r.sites.EnvFree[site]
}

// Runtime returns the run-time program of the Result: the tables that a parser runs, without the
// grammar or its types. It is made with the Result.
func (r *Result) Runtime() *program.Program { return r.runtime }

func (r *Result) newRuntime() *program.Program {
	p := r.Model
	rt := &program.Program{
		ByName:            make(map[string]*program.Rule, len(p.RuleList)),
		DescTable:         p.DescTable,
		NSeen:             p.NSeen,
		Vars:              runtimeVars(p.Vars),
		StartRules:        p.StartRules,
		DefaultStartRule:  p.DefaultStartRule,
		HasMissing:        p.HasMissing,
		RecordsSourceInfo: p.RecordsSourceInfo,
		MakesDefaultValue: p.MakesDefaultValue,
		Types:             map[string]*program.Type{},
		TypeKinds:         p.TypeKinds,
		NodeKinds:         p.NodeKinds,
		NumSkipSites:      r.sites.NumSkip,
		NumRunSites:       r.sites.NumRun,
		NumScanSites:      r.sites.NumScan,
		EnvFreeSites:      r.sites.EnvFree,
		RunKeys:           r.sites.RunKeys,
		StructZero: func(name string) *program.StructZero {
			if z := p.Checker().StructZeroOf(name); z != nil {
				return &program.StructZero{Names: z.Names, Zeros: z.Zeros}
			}
			return nil
		},
	}
	for name, decl := range p.Types {
		if td, ok := decl.(*ast.TypeDef); ok {
			t := &program.Type{Name: name}
			for _, f := range check.DefFields(td) {
				t.Fields = append(t.Fields, f.Name)
			}
			rt.Types[name] = t
		}
	}
	for _, rule := range p.RuleList {
		x := runtimeRule(rule)
		rt.RuleList = append(rt.RuleList, x)
		rt.ByName[x.Name] = x
	}
	for _, rule := range p.Twins {
		rt.Twins = append(rt.Twins, runtimeRule(rule))
	}
	return rt
}

// runtimeRule returns the run-time facts of the rule r.
func runtimeRule(r *check.Rule) *program.Rule {
	return &program.Rule{
		Where: r.Where, Splice: r.Splice, ID: r.ID, Name: r.Name, Scope: runtimeScope(r.Scope),
		NodeType: r.NodeType, OpNodeType: r.OpNodeType, Memo: r.Memo, Missing: r.Missing,
		Leader: r.Leader, Member: r.Member, Cycle: r.Cycle, LeaderIndex: r.LeaderIndex,
		Stream: r.Stream, Positional: r.Positional, Vars: r.Vars,
		Transient: r.Transient, Seen: r.Seen, Plain: r.Plain, Rec: r.Rec, Novalue: r.Novalue,
	}
}

// runtimeScope returns the run-time scope of the capture scope s, which shares its slots.
func runtimeScope(s *check.Scope) *program.Scope {
	if s == nil {
		return nil
	}
	return &program.Scope{Names: s.Names, Slots: s.Slots}
}

// runtimeVars returns the run-time table of the variables of t.
func runtimeVars(t *check.VarTable) *program.VarTable {
	var decls []*program.VarDecl
	for _, d := range t.All() {
		decls = append(decls, &program.VarDecl{Name: d.Name, Init: d.Init})
	}
	return program.NewVarTable(decls)
}
