package conform

import (
	"fmt"
	"math/rand/v2"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

// The cases of the traced call path (call.tracked and call.track). A call of a case runs the body of a
// rule that no parse memoizes, so that it takes the direct path of the call protocol, which the model
// covers; the bodies are the scripts of ref.RuleSpec, as in the cases of the rules. The harness
// compares the events that the trace of the parse delivered (component call) and the bodies that ran
// (component rules) after each call.

// CallOps are the operations of the traced call path, with those of the recovered errors and of the
// missing-token mode that the calls change.
var CallOps = []string{"call.tracked", "call.track", "rec.mark", "rec.truncate", "missing.active", "missing.mark"}

// CallRules is the number of rules that the cases of the traced call path name.
const CallRules = 3

// CallModule returns the declarations of RuleModule.
func CallModule() *code.Module { return RuleModule() }

// CallConfig returns the configuration of a traced parse of the cases of the traced call path: rules
// that no parse memoizes, with the bodies of specs.
func CallConfig(traced bool, specs ...ref.RuleSpec) ref.Config {
	cfg := RuleConfig(specs...)
	cfg.Traced = traced
	return cfg
}

var callFeatures = []string{"budget", "stream", "document", "observe", "missing"}

// CallCases returns the handwritten conformance cases of the traced call path, in one module. As with
// Cases, the model checks each case first.
func CallCases() []Case {
	m := CallModule()
	var cs []Case
	add := func(name string, cfg ref.Config, body func(b *code.Builder)) {
		cs = append(cs, Case{Module: m, Func: m.AddFunc(name, nil, nil, callFeatures, body), Config: cfg})
	}
	rule := func(i int64) code.Expr { return code.C(RuleOp, code.Int(i)) }
	i, pos := code.Int, code.R(exec.Pos)
	ok := func(len int64) ref.Eval { return ref.Eval{OK: true, Len: len} }

	add("call_untraced", CallConfig(false), func(b *code.Builder) {
		b.Assert(code.Not(code.C("call.tracked")), "a call of an untraced parse does not take the traced path")
	})
	add("call_traced", CallConfig(true), func(b *code.Builder) {
		b.Assert(code.C("call.tracked"), "a call of a traced parse takes the traced path")
		b.Assert(code.C("call.tracked"), "and asking does not change that")
	})

	add("call_track_match", CallConfig(true, ref.RuleSpec{Body: []ref.Eval{ok(3), {Len: 2}}}), func(b *code.Builder) {
		b.SetReg(exec.Pos, i(2))
		b.LetN([]string{"v1", "ok1"}, code.C("call.track", rule(0), i(0)))
		b.Assert(code.And(code.L("ok1"), code.Eq(pos, i(5))), "a traced call that matches moves the position to its end")
		b.Assert(code.C("call.tracked"), "the next call is traced again")
		b.LetN([]string{"v2", "ok2"}, code.C("call.track", rule(0), i(1)))
		b.Assert(code.And(code.Not(code.L("ok2")), code.Eq(pos, i(5))), "one that fails leaves the position")
	})

	add("call_track_levels", CallConfig(true, ref.RuleSpec{Body: []ref.Eval{ok(1)}}, ref.RuleSpec{Body: []ref.Eval{ok(0)}}), func(b *code.Builder) {
		for k, level := range []int64{0, 2, 1} {
			b.LetN([]string{fmt.Sprint("v", k), fmt.Sprint("ok", k)}, code.C("call.track", rule(int64(k%2)), i(level)))
		}
		b.SetReg(exec.Silent, i(1))
		b.LetN([]string{"v3", "ok3"}, code.C("call.track", rule(1), i(0)))
		b.SetReg(exec.Silent, i(0))
	})

	errs := ref.RuleSpec{Captures: 1, Body: []ref.Eval{{OK: true, Len: 1, Errs: []int64{4}}, {Len: 2, Errs: []int64{5, 6}}}}
	add("call_track_errors", CallConfig(true, errs), func(b *code.Builder) {
		b.LetN([]string{"v1", "ok1"}, code.C("call.track", rule(0), i(0)))
		b.Assert(code.Eq(code.C("rec.mark"), i(1)), "the errors that a matching call recovered stay")
		b.LetN([]string{"v2", "ok2"}, code.C("call.track", rule(0), i(0)))
		b.Assert(code.And(code.And(code.Not(code.L("ok2")), code.Eq(code.C("rec.mark"), i(1))), code.Eq(pos, i(1))),
			"a call that fails drops the errors it recovered and returns to its start")
	})

	plain := ref.RuleSpec{Body: []ref.Eval{{Len: 3, Errs: []int64{7}}}}
	add("call_track_plain", CallConfig(true, plain), func(b *code.Builder) {
		b.SetReg(exec.Pos, i(4))
		b.LetN([]string{"v1", "ok1"}, code.C("call.track", rule(0), i(0)))
		b.Assert(code.And(code.And(code.Not(code.L("ok1")), code.Eq(pos, i(4))), code.Eq(code.C("rec.mark"), i(0))),
			"a plain body that fails leaves neither its length nor its errors")
	})

	missing := CallConfig(true, ref.RuleSpec{Body: []ref.Eval{
		{Missing: []ref.MissingInsert{{Site: 1, Pos: 0, Err: 8}}}, {OK: true, Len: 1, Missing: []ref.MissingInsert{{Site: 2, Pos: 1, Err: 9}}},
	}})
	missing.Missing, missing.MissingRules = true, []bool{true}
	add("call_track_missing", missing, func(b *code.Builder) {
		b.LetN([]string{"v1", "ok1"}, code.C("call.track", rule(0), i(0)))
		b.Assert(code.And(code.Not(code.L("ok1")), code.Eq(code.C("missing.mark"), i(0))), "a call that fails leaves no token it inserted")
		b.LetN([]string{"v2", "ok2"}, code.C("call.track", rule(0), i(0)))
		b.Assert(code.And(code.L("ok2"), code.Eq(code.C("missing.mark"), i(1))), "a call that matches keeps its token")
	})
	return cs
}

// RandomCallConfig returns the configuration of a parse of the randomized cases of the traced call
// path: CallRules rules that no parse memoizes, with bodies of a few evaluations.
func RandomCallConfig(rng *rand.Rand, missing bool) ref.Config {
	specs := make([]ref.RuleSpec, CallRules)
	for k := range specs {
		s := ref.RuleSpec{Captures: max(rng.Int64N(4)-2, 0)}
		for range 1 + rng.IntN(3) {
			e := ref.Eval{OK: rng.IntN(3) > 0, Len: rng.Int64N(4)}
			for range rng.IntN(3) {
				e.Errs = append(e.Errs, rng.Int64N(50))
			}
			for k := range s.Captures {
				if rng.IntN(2) == 0 {
					e.Captures = append(e.Captures, k)
				}
			}
			if missing {
				for range rng.IntN(3) {
					e.Missing = append(e.Missing, ref.MissingInsert{Site: rng.Int64N(3), Pos: rng.Int64N(4), Err: 100 + rng.Int64N(50)})
				}
			}
			s.Body = append(s.Body, e)
		}
		specs[k] = s
	}
	cfg := CallConfig(rng.IntN(5) > 0, specs...)
	cfg.Missing = missing
	if missing {
		for range specs {
			cfg.MissingRules = append(cfg.MissingRules, rng.IntN(2) == 0)
		}
	}
	return cfg
}

// RandomCall returns a randomized case of n calls of the operations of the traced call path, drawn
// with rng, that meets every precondition. In a traced parse most calls are traced; the position and
// the lookahead change between them.
func RandomCall(name string, rng *rand.Rand, provided []string, n int) Case {
	m := CallModule()
	missing := containsString(provided, "missing.mark") && rng.IntN(3) == 0
	cfg := RandomCallConfig(rng, missing)
	mirror := ref.New(cfg)
	regs := map[string]any{exec.Pos: int64(0), exec.Silent: int64(0)}
	mirror.UseRegisters(regs)
	f := m.AddFunc(name, nil, nil, callFeatures, func(b *code.Builder) {
		if missing {
			b.Assert(code.C("missing.active"), "the parse is in the missing-token mode")
		}
		for calls := 0; calls < n; {
			switch rng.IntN(8) {
			case 0:
				p := rng.Int64N(10)
				regs[exec.Pos] = p
				b.SetReg(exec.Pos, code.Int(p))
				continue
			case 1:
				silent := rng.Int64N(2)
				regs[exec.Silent] = silent
				b.SetReg(exec.Silent, code.Int(silent))
				continue
			case 2:
				calls++
				mirror.Call(m.Op("call.tracked"), nil)
				b.Let(fmt.Sprint("t", calls), code.C("call.tracked"))
				continue
			case 3:
				calls++
				if containsString(provided, "rec.mark") {
					b.Let(fmt.Sprint("m", calls), code.C("rec.mark"))
				}
				continue
			}
			k, level := rng.Int64N(CallRules), rng.Int64N(3)
			o := m.Op("call.track")
			args := []any{ref.RuleRef{Index: k}, level}
			if !mirror.Allows(o, args) {
				continue
			}
			calls++
			mirror.Call(o, args)
			b.LetN(resultNames(calls, 2), code.C("call.track", code.C(RuleOp, code.Int(k)), code.Int(level)))
		}
	})
	return Case{Module: m, Func: f, Config: cfg}
}

func containsString(s []string, x string) bool { return slices.Contains(s, x) }
