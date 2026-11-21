// Package frontend turns a grammar into its checked form: the compiled program with its types
// and rule flags, the Core IR module built from it, and the facts computed on the module. It is
// the one place that answers what a grammar means; the backends and the tools that analyze a
// grammar (lint, the language server) read its Result.
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
	"syntaxir.dev/sxir/internal/middleend/corepass"
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
	// Facts are the grammar facts of Module, which Analyses holds.
	Facts *corepass.Facts
	// Analyses is the manager of the analyses of Module, which the tools read them through.
	Analyses *pass.AnalysisManager[*core.Module]
}

// RunDefault is Run with the default options, for the tools that check a grammar without
// compiling it for a backend.
func RunDefault(g *ast.Grammar, control *cancel.Control) (*Result, error) {
	return Run(g, compile.Options{}, control)
}

// Run checks g with the compiler's options opts. A non-nil control polls for cancellation. The
// error is the compiler's error list, with the names of the hidden rules that inline actions
// generate shown as written.
func Run(g *ast.Grammar, opts compile.Options, control *cancel.Control) (*Result, error) {
	names := desugar.InlineNames{}
	prog, st, err := CheckStructural(g, opts, nil, names, control)
	if err != nil {
		// The facts of a grammar that checks have no error (the compiler reports the same ones),
		// so they are looked for only when checking fails, in the module the check built.
		return nil, mergeErrors(names.Rewrite(err), st.errors(opts, names))
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
	am := corepass.NewAnalyses(control)
	return &Result{Program: prog, Module: m, Facts: corepass.FactsOf(am, m), Analyses: am}, nil
}
