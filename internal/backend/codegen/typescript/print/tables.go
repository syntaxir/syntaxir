package print

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
)

// A Parser is a TypeScript parser of a program as the TypeScript backend decided it: the program, the
// recognizer, the insertion program and its recognizer, the start rule and the options, which
// Validate checked.
type Parser struct {
	Prog *common.Program
	Rec  *common.Program // the recognizer, or nil
	// Ins and InsRec are the insertion program, which the parses with missing-token insertion enabled
	// run, and its recognizer, which the recognitions with it enabled run, each nil if the parser has
	// none: only a parser of a grammar with #missing has them, a recognize-only one only InsRec.
	Ins, InsRec *common.Program
	Start       *common.Rule
	Opts        common.GenOptions
}

// Tables returns the rule tables of the parser as the feature analysis reads them: the Node table,
// the recognizer, and the tables of the insertion program and of its recognizer, which insert missing
// tokens.
func (p *Parser) Tables() []common.RuleTable {
	t := common.Tables(p.Prog, p.Rec)
	if p.Ins != nil {
		t = append(t, common.RuleTable{Prog: p.Ins, Name: "insRules", Inserts: true})
	}
	if p.InsRec != nil {
		t = append(t, common.RuleTable{Prog: p.InsRec, Name: "insRecRules", Inserts: true})
	}
	return t
}

// Params are the values of the parameters of the step print-ts that the TypeScript printer reads: those
// of both printers and the TypeScript printer's own.
type Params struct {
	common.Params
	// SparseMemo keeps the first-call table of a large parser sparse (useSparseMemo).
	SparseMemo bool
	// Control, if not nil, is the control of the printing step (cancel.New of the context of the step), which the
	// printers of the grammar's code poll (BodyTarget.Control, Target.Control).
	Control *cancel.Control
}

// Print prints the parser p from its tables at the Exec stage with the parameters params and the
// hooks of a test, or returns the reach of the printed code (common.Replan) if it differs from
// that of the plan, unless the compilation gave the plan its reach (given) or a bisection limit is
// set.
func Print(p *Parser, params Params, tables *execdata.Tables, given bool, hooks *Hooks) (result.Output, error) {
	live := liveOf(tables.Plan)
	tsg := newGenerator(p.Prog, p.Opts, live)
	tsg.Params, tsg.sparseMemo, tsg.tables, tsg.control = params.Params, params.SparseMemo, tables, params.Control
	if err := tsg.generate(p); err != nil {
		return nil, err
	}
	if printed := tsg.printedReach(p); !given && p.Opts.Debug.OptBisectLimit == nil && !printed.equal(live) {
		return &common.Replan{Lang: result.TypeScript, Reach: printed.entries()}, nil
	}
	if hooks != nil && hooks.Generator != nil {
		defer hooks.Generator(tsg)
	}
	files, err := tsg.print(p)
	if err != nil {
		return nil, err
	}
	return &result.Files{Lang: result.TypeScript, Files: files}, nil
}
