// Package engine is the option-driven entry layer over the run-time packages. It runs compiled
// programs: whole-input parses, recognition, prefix parses, streams and Documents, on the closure
// backend (the reference parser) or on the recursive or iterative bytecode VM, under the limits,
// tracing and profiling of a parse.
//
// # What the engine owns
//
// A Program is what the compiler or the loader of a compiled grammar produces. The engine owns the
// Program (its start rules, rule types and output schema source, and the loading and marshaling of a
// compiled grammar with the checks of its mode), the Parser and Document that run it with ParseOption
// functions, the choice of backend, and the admission of each run: the start rule, the mode and the
// limits are checked, and the program that runs under a budget is prepared, before any matching
// starts. The public parsers (the reference parser of package compiler and the parsers that
// package driver/entry builds) wrap them with the options of package runtime, which Use converts.
// It imports neither the compiler driver nor the generators.
//
// # What the engine does not own
//
// How a program matches belongs to the packages the engine calls: internal/runtime/state (memo,
// budget, trace, streams, runs and the shared mechanisms), internal/runtime/vm, internal/backend/closure
// and internal/runtime/document. Compilation, the IR passes, formatting and code generation belong
// to their own packages.
//
// # Tests
//
// A test of this package asserts what the engine owns: the option-driven API, the agreement of the
// backends on a grammar's results, and the admission of a run. A test of another package's
// mechanism lives in that package, in an external test package that compiles and runs grammars
// through internal/testing/enginetest, which package engine's own tests cannot import.
package engine
