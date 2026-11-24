// Package frontend turns a grammar into its checked form: the semantic model with its types and
// rule flags, the site numbers of its repetitions and its run-time program, and for the tools
// the Core IR module built from it with the manager of the analyses of the module, through which
// they read the facts that the middle end computes on it. The check reads the rule facts and the
// site numbers of the structural module from a FactSource that the caller hands it. It is the one
// place that answers what a grammar means; the backends and the tools that analyze a grammar
// (lint, the language server) read its Result.
//
// Run reports the errors that the canonicalization and the checker find. The semantic model and
// its checker are package check; this package canonicalizes the grammar, checks it into a model,
// and adds the site numbers and the run-time program.
package frontend

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A fact error of the untyped module that the compiler does not report is added to its errors
// by mergeErrors.

// Result is the checked form of a grammar: the semantic model with the site numbers of its
// repetitions and its run-time program. It is the program that the Core IR builder reads
// (elaborate.Program). The tools that analyze a grammar also read the Core IR module of a Result of
// Run or WithModule; the backends build their own from a Result of Check.
type Result struct {
	// Model is the semantic model of the checked grammar: its rules with their flags, and its
	// types.
	*check.Model
	// sites numbers the skip-run, run and scan sites of the program's repetitions.
	sites check.Sites
	// runtime is the run-time program (Runtime).
	runtime *program.Program
	// Module is the Core IR of the program, with its origin map (core.Module.Origin); nil in a
	// Result of Check.
	Module *core.Module
	// Analyses is the manager of the analyses of Module, through which the tools read its facts
	// (corepass.FactsOf); the front end computes none of them. It is nil when Module is.
	Analyses *pass.AnalysisManager[*core.Module]
	// Structural is the structural Core IR module of the program's rules, from which the compiler
	// took its rule facts, with the manager of its analyses, which holds them, and which carries the
	// checked types of the program. The complexity classifier reads it. It is nil when Module is.
	Structural *Structural
	// Input is the grammar that Check was given; a derived program (Derive) has the Input of its
	// program. Grammar and Source are the compiler's canonical forms, which are not input to Check.
	// It is nil in a Result of a model without a grammar.
	Input *ast.Grammar
}

// RunDefault is Run with the default options, for the tools that check a grammar without
// compiling it for a backend.
func RunDefault(g *ast.Grammar, facts FactSource, control *cancel.Control) (*Result, error) {
	return Run(g, check.Options{}, facts, control)
}

// Run checks g with the compiler's options opts, reading the facts that facts computes. A non-nil
// control polls for cancellation. The error is the compiler's error list, with the names of the
// hidden rules that inline actions generate shown as written.
func Run(g *ast.Grammar, opts check.Options, facts FactSource, control *cancel.Control) (*Result, error) {
	names := desugar.InlineNames{}
	r, st, err := CheckStructural(g, opts, nil, names, facts, control)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	if err := r.addModule(st, control); err != nil {
		return nil, err
	}
	return r, nil
}

// WithModule returns r with its Core IR module, for a caller that holds a Result of Check, which
// has none, and must not compile the grammar twice. The Result of Check does not keep its
// structural module, which is built again from the program's grammar. r is not changed.
func (r *Result) WithModule(control *cancel.Control) (*Result, error) {
	st, err := structuralOf(r.Model, control)
	if err != nil {
		return nil, err
	}
	out := *r
	if err := out.addModule(st, control); err != nil {
		return nil, err
	}
	return &out, nil
}

// addModule sets the Core IR module of r, whose structural module is st.
func (r *Result) addModule(st *Structural, control *cancel.Control) error {
	m, err := elaborate.CoreFrom(r, true)
	if err != nil {
		return err
	}
	r.Module, r.Analyses, r.Structural = m, newAnalyses(control), st
	return nil
}
