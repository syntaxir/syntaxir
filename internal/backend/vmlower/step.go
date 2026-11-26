package vmlower

import (
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// Name is the name of the bytecode lowering as a step of the pipeline.
const Name = "vmlower"

// Lowering is the bytecode lowering (compile) as the required step Name, the last step of the
// pipeline of a VM target: it takes the program with its Node table at the end of the Flow stage
// (vm-input) and returns its bytecode module. Its counters are the patterns of statements of the
// selection table, the superinstructions, each counting its matches in the module.
var Lowering = pipe.New(pipe.Def[Input, result.Output]{
	Decl: pipe.Decl{Name: Name, Required: true, Obligation: "The bytecode is the module of the program, with the Flow body of each function of its table.",
		Counters: superinstructions()},
	In: InputIR, Out: result.IR,
	Run: func(c *pipe.Ctx[Input], in Input) (result.Output, pass.PreservedSet, error) {
		m, counts, err := compile(in)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		for _, p := range statementPatterns {
			c.Count(p.Name, counts[p.Name])
		}
		return &Bytecode{Module: m}, pass.PreserveNone(), nil
	},
})

// Bytecode is the output of a VM target: the bytecode module of the program.
type Bytecode struct {
	Module *bytecode.Module
}

// Backend returns result.VM.
func (*Bytecode) Backend() result.Backend { return result.VM }
