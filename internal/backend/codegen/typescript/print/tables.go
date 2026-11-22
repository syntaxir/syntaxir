package print

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
)

// A Parser is a TypeScript parser of a program as the TypeScript backend decided it: the program, the
// recognizer, the start rule and the options, which Validate checked.
type Parser struct {
	Prog  *common.Program
	Rec   *common.Program // the recognizer, or nil
	Start *common.Rule
	Opts  common.GenOptions
}

// Params are the values of the parameters of the step print-ts that the TypeScript printer reads: those
// of both printers and the TypeScript printer's own.
type Params struct {
	common.Params
	// SparseMemo keeps the first-call table of a large parser sparse (useSparseMemo).
	SparseMemo bool
}

// Print prints the parser p from its tables at the Exec stage with the parameters params and the
// hooks of a test, or returns the reach of the printed code (common.Replan) if it differs from
// that of the plan, unless the compilation gave the plan its reach (given) or a bisection limit is
// set.
func Print(p *Parser, params Params, tables *execdata.Tables, given bool, hooks *Hooks) (result.Output, error) {
	live := liveOf(tables.Plan)
	tsg := newGenerator(p.Prog, p.Opts, live)
	tsg.Params, tsg.sparseMemo, tsg.tables = params.Params, params.SparseMemo, tables
	if err := tsg.generate(p.Rec); err != nil {
		return nil, err
	}
	if printed := tsg.printedReach(p.Prog, p.Rec, p.Start); !given && p.Opts.Debug.OptBisectLimit == nil && !printed.equal(live) {
		return &common.Replan{Lang: result.TypeScript, Reach: printed.entries()}, nil
	}
	if hooks != nil && hooks.Generator != nil {
		defer hooks.Generator(tsg)
	}
	files, err := tsg.print(p.Opts, p.Rec, p.Start)
	if err != nil {
		return nil, err
	}
	return &result.Files{Lang: result.TypeScript, Files: files}, nil
}
