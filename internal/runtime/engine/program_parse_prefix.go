package engine

import (
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// PrefixResult records the successful cursor independently of the rule's value.
type PrefixResult struct {
	Node          *value.Node
	Consumed      int
	ConsumedBytes int
}

// ParsePrefixWith parses a named rule without an implicit whole-input check.
// Recovered success, including success after missing-token insertion, returns
// consumption and SyntaxErrors; unrecovered failures and fatal errors, among them
// an exhausted missing-token limit, return a zero result. Explicit EOF assertions
// still see the entire supplied input.
func (prog *Program) ParsePrefixWith(start, input string, o ParseOptions) (result PrefixResult, err error) {
	reportCompute(o, nil)
	if err := state.ValidateStreamDiagnostics(o, false); err != nil {
		return PrefixResult{}, err
	}
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(state.Fatal); ok {
				result, err = PrefixResult{}, f.Err
			} else if cp, ok := x.(state.ContextPanic); ok {
				panic(cp.Value)
			} else {
				panic(x)
			}
		}
	}()
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, false); err != nil {
		return PrefixResult{}, err
	}
	active, _, control, err := prog.admitParse(start, o, optionsFamily(o), false, input)
	if err != nil {
		return PrefixResult{}, err
	}
	defer reportCompute(o, control)
	control.Check()
	p := state.AcquirePooledParser(&active.sharedProgram)
	defer p.ReleaseBatchScratch()
	p.Input = state.NewControlledTextInput(input, o.Unit, !o.Recognize, p.Scratch, control)
	control.AttachMemory(p)
	p.MaxDepth, _ = state.DepthLimit(o, o.Backend) // admitParse rejected an invalid limit
	p.Expectations = o.Expectations
	p.DeferMemo = true
	p.SetTuning(o.Tune)
	if o.MissingTokens > 0 && active.HasMissing {
		p.Missing, p.MissingLen = &state.MissingTokenState{Limit: o.MissingTokens}, 0
	}
	if o.Traced() {
		p.SetTrace(o.Trace, o.Profiles)
		p.SelectTrace(o.TraceSelection)
		defer p.CloseTrace()
	}
	result, err = active.runPrefix(p, o.Backend, start)
	if !p.Aborted {
		control.Check()
	}
	if o.Recognize {
		result.Node = nil
	}
	result.ConsumedBytes = p.Input.PrefixByteOffset(input, result.Consumed)

	if !p.Aborted {
		control.Check()
	}
	return result, err
}

func (prog *Program) runPrefix(p *state.Parser, b Backend, start string) (result PrefixResult, err error) {
	p.Aborted = false
	r, err := prog.rule(b, start, state.FamilyOf(p), p.Control)
	if err != nil {
		return PrefixResult{}, err
	}
	p.Descs = prog.descs(b, state.FamilyOf(p))
	stride := prog.NSeen
	if be := prog.backend(b); be == Bytecode || be == BytecodeIterative {
		stride = prog.vmFor(be, state.FamilyOf(p)).NumSeen()
	}
	p.Memo.StartFirstCalls(stride, state.SparseMemo && stride > 64 && p.Loaded() > 1024)
	defer func() {
		if x := recover(); x != nil {
			p.Aborted = true
			result = PrefixResult{}
			recoverParse(x, prog.backend(b), &err)
		}
	}()
	p.SeedVars()
	v, ok := p.Call(r, 0)
	if !ok {
		return PrefixResult{}, p.SyntaxError()
	}
	result = PrefixResult{Node: v, Consumed: p.Pos}
	if len(p.Recovered) > 0 {
		err = value.SyntaxErrors(p.RecoveredErrors())
	}
	return result, err
}
