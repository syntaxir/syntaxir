package common

import (
	"fmt"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A Feature is a mechanism of the library of generated parsers that a parser runs only if its grammar
// or its generation options can need it. The feature analysis (Analyze) decides once per generation
// which features a parser can use, from the options and from the rules that its entry points reach;
// every target reads the same FeatureSet, so that no printer decides on its own which library code a
// parser carries.
//
// The analysis over-approximates: a feature that a parse could use is always in the set. The bits are
// part of the contract between the generator and the Go library (pkg/runtime/libgo/features.go declares
// the same values), and a test checks that the two agree.
type Feature uint32

const (
	// FeatureDocument is the incremental Document (-document).
	FeatureDocument Feature = 1 << iota
	// FeatureStream is the streaming parse (-stream).
	FeatureStream
	// FeatureObserve is tracing and profiling (-observability).
	FeatureObserve
	// FeatureBudget is the limit controls: cancellation and the compute and memory limits (on by default, off with -no-limits).
	FeatureBudget
	// FeatureMemo is the memo table: a reached rule is memoized or leads a left-recursion cycle.
	FeatureMemo
	// FeatureLeftRecursion is the growth of left-recursion seeds and the dependency memo of their
	// contexts: a reached rule is on a left-recursion cycle.
	FeatureLeftRecursion
	// FeatureRecovery is error recovery: a reached rule contains a recovery (#recover, an Attributed pragma once desugared).
	FeatureRecovery
	// FeatureOperators is the Operators Rule: a reached rule is one.
	FeatureOperators
	// FeatureVars is the variable environment: the grammar declares variables.
	FeatureVars
	// FeatureSkipRuns is the run-site records: the program has skip-run sites or run sites.
	FeatureSkipRuns
	// FeatureScanSites is the scan records: the program has scan sites.
	FeatureScanSites
	// FeatureMissing is the missing-token mode: a reached rule of a table of the insertion program
	// contains #missing.
	FeatureMissing

	// AllFeatures is every feature: the set of a parser that the analysis does not narrow.
	AllFeatures = 1<<iota - 1
)

// featureNames are the names of the features, by bit, as FeatureSet.String writes them.
var featureNames = []string{"document", "stream", "observe", "budget", "memo", "leftRecursion", "recovery", "operators", "vars", "skipRuns", "scanSites", "missing"}

// FeatureNames returns the names of the features, by bit.
func FeatureNames() []string { return append([]string(nil), featureNames...) }

// FeatureSet is the features a generated parser can use.
type FeatureSet Feature

// Has reports whether s has every feature of f.
func (s FeatureSet) Has(f Feature) bool { return Feature(s)&f == f }

// String lists the names of the features of s, separated by commas.
func (s FeatureSet) String() string {
	var names []string
	for i, n := range featureNames {
		if Feature(s)&(1<<i) != 0 {
			names = append(names, n)
		}
	}
	return strings.Join(names, ",")
}

// RuleTable is a rule table of a generated parser as the feature analysis reads it: its program, its
// name in the plan, and whether it inserts missing tokens, as the tables of the insertion program do.
type RuleTable struct {
	Prog    *Program
	Name    string
	Inserts bool
}

// Tables returns the rule tables of a parser of prog without insertion tables: the Node table and,
// with Recognize, the recognizer table of rec (nil otherwise).
func Tables(prog, rec *Program) []RuleTable {
	t := []RuleTable{{Prog: prog, Name: "rules"}}
	if rec != nil {
		t = append(t, RuleTable{Prog: rec, Name: "recRules"})
	}
	return t
}

// Analyze returns the features of the parser generated with opts from its rule tables tables, which
// the program was planned as plan (plandata.Program). The rules considered are those that the start
// rules (ParseRule, the start rule of opts and the typed entry points name only start rules) can
// enter in each table, value-free twins included, followed through the calls of the Core IR that the
// plan was made from (plandata.Program.Reached); no Core IR is built for the analysis.
func Analyze(tables []RuleTable, opts GenOptions, plan *plandata.Program) (FeatureSet, error) {
	var f Feature
	if opts.Document {
		f |= FeatureDocument
	}
	if opts.Stream {
		f |= FeatureStream
	}
	if opts.Observability {
		f |= FeatureObserve
	}
	if !opts.NoLimitControls {
		f |= FeatureBudget
	}
	for _, t := range tables {
		reached, err := reachedRules(t.Prog, t.Name, plan)
		if err != nil {
			return 0, err
		}
		for _, r := range reached {
			f |= ruleFeatures(r)
			if t.Inserts && r.Missing {
				// The rule or a rule it calls contains #missing, which the table lowers with its
				// insertion: each insertion is a recovered error.
				f |= FeatureMissing | FeatureRecovery
			}
		}
		if t.Prog.Vars != nil {
			f |= FeatureVars
		}
		if t.Prog.Run.NumSkipSites+t.Prog.Run.NumRunSites > 0 {
			f |= FeatureSkipRuns
		}
		if t.Prog.Run.NumScanSites > 0 {
			f |= FeatureScanSites
		}
	}
	return FeatureSet(f), nil
}

// ruleFeatures returns the features that a call of r can use.
func ruleFeatures(r *Rule) Feature {
	var f Feature
	if r.Memo || r.Leader {
		f |= FeatureMemo
	}
	if r.Leader || r.Member || r.Cycle != 0 {
		f |= FeatureLeftRecursion
	}
	if r.Def == nil {
		return f
	}
	if _, ok := r.Def.Expr.(*ast.OperatorsRule); ok {
		f |= FeatureOperators
	}
	astinspect.WalkExpr(r.Def.Expr, func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Recovery:
			f |= FeatureRecovery
		case *ast.Attributed:
			for _, a := range e.Attrs {
				if a.Name == "recover" {
					f |= FeatureRecovery
				}
			}
		}
	})
	return f
}

// reachedRules returns the rules of p, value-free twins included, that a parse can enter from the
// start rules of p in the rule table table of plan, whose Core IR plan was planned from.
func reachedRules(p *Program, table string, plan *plandata.Program) ([]*Rule, error) {
	if !slices.ContainsFunc(plan.Cores.Tables, func(t plandata.CoreTable) bool { return t.Name == table }) {
		return nil, fmt.Errorf("internal error: feature analysis: the plan has no table %s", table)
	}
	var roots []plandata.Entry
	for _, name := range p.StartRules {
		roots = append(roots, plandata.Entry{Table: table, ID: p.ByName[name].ID})
	}
	rules := append(append([]*Rule(nil), p.RuleList...), p.Twins...)
	reach := plan.Reached(roots)
	var out []*Rule
	for id, r := range rules {
		if reach[plandata.Entry{Table: table, ID: id}] || reach[plandata.Entry{Table: table, ID: id, Wrapper: true}] {
			out = append(out, r)
		}
	}
	return out, nil
}

// ParserFeatures returns the features of the parser generated with opts and the parameters p from
// the tables planned as plan: those Analyze finds, or AllFeatures with the parameter UsedFeatures off.
func ParserFeatures(tables []RuleTable, opts GenOptions, p Params, plan *plandata.Program) (FeatureSet, error) {
	if !p.UsedFeatures {
		return FeatureSet(AllFeatures), nil
	}
	return Analyze(tables, opts, plan)
}
