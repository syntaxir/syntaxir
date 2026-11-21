// Package frontend turns a grammar into its checked form: the compiled program with its types
// and rule flags, the Core IR module built from it, and the manager of the analyses of the module,
// through which the tools read the facts that the middle end computes on it. The check reads the
// rule facts of the structural module from a FactSource that the caller hands it. It is the one
// place that answers what a grammar means; the backends and the tools that analyze a grammar
// (lint, the language server) read its Result.
//
// Run reports the errors the compiler finds. The compiler's Check makes the program; the program
// and the type model still live in the compiler, so the front end imports it until they move to
// packages of their own.
package frontend

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A fact error of the untyped module that the compiler does not report is added to its errors
// by mergeErrors.

// Result is the checked form of a grammar.
type Result struct {
	// Program is the compiled program: rules, flags and types.
	Program *compile.Program
	// Module is the Core IR of the program, with its origin map (core.Module.Origin).
	Module *core.Module
	// Analyses is the manager of the analyses of Module, through which the tools read its facts
	// (corepass.FactsOf); the front end computes none of them.
	Analyses *pass.AnalysisManager[*core.Module]
}

// Grammar returns the grammar that the program runs: the grammar as written with its inline actions
// lowered to hidden rules and its attributes normalized. The origin map of Module gives the
// expressions of this grammar.
func (r *Result) Grammar() *ast.Grammar { return r.Program.Grammar }

// InlineNames returns the hidden rules of Grammar that stand for the inline actions of the grammar
// as written.
func (r *Result) InlineNames() desugar.InlineNames { return r.Program.InlineNames }

// RunDefault is Run with the default options, for the tools that check a grammar without
// compiling it for a backend.
func RunDefault(g *ast.Grammar, facts FactSource, control *cancel.Control) (*Result, error) {
	return Run(g, compile.Options{}, facts, control)
}

// Run checks g with the compiler's options opts, reading the facts that facts computes. A non-nil
// control polls for cancellation. The error is the compiler's error list, with the names of the
// hidden rules that inline actions generate shown as written.
func Run(g *ast.Grammar, opts compile.Options, facts FactSource, control *cancel.Control) (*Result, error) {
	names := desugar.InlineNames{}
	prog, _, err := CheckStructural(g, opts, nil, names, facts, control)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return FromProgram(prog, control)
}

// FromProgram builds the Result of a program that was compiled already, for a caller that keeps
// the program for its own use and must not compile the grammar twice.
func FromProgram(prog *compile.Program, control *cancel.Control) (*Result, error) {
	m, err := elaborate.CoreFrom(prog, true)
	if err != nil {
		return nil, err
	}
	return &Result{Program: prog, Module: m, Analyses: newAnalyses(control)}, nil
}
