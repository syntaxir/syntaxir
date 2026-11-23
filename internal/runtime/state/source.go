package state

import (
	"context"

	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime"
)

// ParserSource returns the Source of the program that p runs and the start rule of p: the Source is
// nil for a program loaded from a compiled grammar, and ok is false for a parser that the engine
// does not run, such as a generated one. The engine sets it when it is initialized, so that the
// tools that read the grammar of a parser (the sampler) reach it without linking the engine:
// a parser that the engine runs exists only in a program that links the engine. It is nil in a
// program that does not, where every parser is one that the engine does not run.
var ParserSource func(p runtime.Parser) (src Source, start string, ok bool)

// A Source is the compiler side of a program compiled from a grammar: what the parses of the
// program build from the grammar on first use, and what the compiler knows about its rules. The
// driver supplies it; a program loaded from a compiled grammar has none.
type Source interface {
	// Runtime returns the run-time program that the parses run.
	Runtime() *program.Program
	// Module compiles the program to a bytecode module, once.
	Module() (*bytecode.Module, error)
	// Core builds the representation that the closure families are built from, polling ctx (nil
	// for none): a context that ends returns a BuildCancelled with its error, and a panic of a
	// method of ctx unwinds as ContextPanic.
	Core(ctx context.Context) (any, error)
	// Family builds the rules of the closure family f from core, by ID.
	Family(core any, f Family) ([]*Rule, error)
	// Recognizer compiles the grammar again as a program that builds no tree, and Insertion as the
	// program of the parses with missing-token insertion enabled, with the rule flags of insertion.
	Recognizer() (Source, error)
	Insertion() (Source, error)
	// RuleType, RuleTypeWithPoll, ShapeSignature and ValueType describe the types of the rules
	// (see the methods of engine.Program of the same names).
	RuleType(name string) (string, bool)
	RuleTypeWithPoll(name string, poll func()) (string, bool)
	ShapeSignature(name string, poll func()) (string, bool)
	ValueType(name string) (string, bool)
}
