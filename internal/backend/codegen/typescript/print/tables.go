package print

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/pass"
)

// A Parser is a TypeScript parser of a program as the TypeScript backend decided it: the program, the
// recognizer, the start rule and the options, which Validate checked.
type Parser struct {
	Prog  *common.Program
	Rec   *common.Program // the recognizer, or nil
	Start *common.Rule
	Opts  common.GenOptions
}

// Print prints the parser p from its tables at the Exec stage with the settings of the compilation
// and the hooks of a test, or returns the reach of the printed code (common.Replan) if it differs from
// that of the plan, unless the compilation gave the plan its reach (given) or a bisection limit is
// set.
func Print(p *Parser, settings pass.Settings, tables *execdata.Tables, given bool, hooks *Hooks) (result.Output, error) {
	live := liveOf(tables.Plan)
	tsg := newGenerator(p.Prog, p.Opts, live)
	tsg.Settings, tsg.tables = settings, tables
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
