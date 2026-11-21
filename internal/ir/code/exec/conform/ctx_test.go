package conform

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/exec/ref"
)

var ctxModule = CtxModule()

func ctxSuite(name string, mu *mutant) Suite {
	return Suite{Name: name, Ops: CtxSuiteOps(), New: func(c ref.Config) Impl {
		if mu == nil {
			return modelImpl{ref.New(c)}
		}
		mu.modelImpl = modelImpl{ref.New(c)}
		return *mu
	}}
}

func TestCtxCasesVerify(t *testing.T) {
	if err := code.Verify(CtxCases()[0].Module); err != nil {
		t.Fatal(err)
	}
	if err := code.Verify(RandomCtx("random", rand.New(rand.NewPCG(1, 2)), 200).Module); err != nil {
		t.Fatal(err)
	}
}

const ctxSeeds, ctxCalls = 200, 250

func ctxFails(s Suite) (handwritten, random bool) {
	for _, c := range CtxCases() {
		if _, err := Run(s, c); err != nil {
			handwritten = true
		}
	}
	for seed := uint64(0); seed < ctxSeeds && !random; seed++ {
		_, err := Run(s, RandomCtx("random", rand.New(rand.NewPCG(seed, 9)), ctxCalls))
		random = err != nil
	}
	return handwritten, random
}

func TestModelPassesCtxCases(t *testing.T) {
	s := ctxSuite("model", nil)
	for _, c := range CtxCases() {
		if skipped, err := Run(s, c); err != nil || skipped {
			t.Errorf("%s: skipped %v, %v", c.Func.Name, skipped, err)
		}
	}
	for seed := uint64(0); seed < ctxSeeds; seed++ {
		if _, err := Run(s, RandomCtx("random", rand.New(rand.NewPCG(seed, 9)), ctxCalls)); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRandomCtxCasesUseTheOperations checks that randomized cases exercise every operation, and reach
// results kept in a context and the outcomes of lookaheads.
func TestRandomCtxCasesUseTheOperations(t *testing.T) {
	counts := map[string]int{}
	found := 0
	for seed := uint64(0); seed < ctxSeeds; seed++ {
		c := RandomCtx("random", rand.New(rand.NewPCG(seed, 9)), ctxCalls)
		walkCalls(c.Func.Body, func(x *code.Call) { counts[x.Callee]++ })
		walkCalls(c.Func.Body, func(x *code.Call) {
			if x.Callee == "ctx.expected" {
				found++
			}
		})
	}
	for _, name := range append(CtxOps, CtxTestOps...) {
		if counts[name] < 20 {
			t.Errorf("randomized cases call %s %d times", name, counts[name])
		}
	}
	if found < 30 {
		t.Errorf("randomized cases read %d results kept in a context", found)
	}
}

// TestCtxMutantsFail is the negative control of the cases of the dependency memo: each broken model
// fails a handwritten or a randomized case.
func TestCtxMutantsFail(t *testing.T) {
	for _, br := range CtxBrokenOps() {
		t.Run(br.Name, func(t *testing.T) {
			mu := mutant{op: br.Op, edit: func(m *ref.Model, a []any) []any {
				return br.Edit(func(op string, args ...any) []any { return m.Call(ctxModule.Op(op), args) }, a)
			}}
			handwritten, random := ctxFails(ctxSuite(br.Name, &mu))
			if !handwritten && !random {
				t.Error("no case detected the mutant")
			}
			if !random {
				t.Error("no randomized case detected the mutant")
			}
		})
	}
}

// ctxKeepOfLeader reports, from the answers of allows, whether ctx.keep of a call of the rule r that has
// read a seed is refused, as the contract requires when r leads a cycle and no growth ends, and allowed
// when r is a member of the cycle. It returns a description of the first answer that is wrong, or nil.
func ctxKeepOfLeader(allows func(m *ref.Model, o *code.Op, a []any) bool) error {
	call := func(m *ref.Model, name string, a ...any) []any { return m.Call(ctxModule.Op(name), a) }
	keepAllowed := func(r int64) bool {
		m := ref.New(CtxConfig())
		m.UseRegisters(map[string]any{"pos": int64(0), "silent": int64(0)})
		call(m, ref.CtxGrowOp, ref.RuleRef{Index: ctxLeaderA}, int64(0), true, int64(3), int64(7))
		e := call(m, "ctx.enter", ref.RuleRef{Index: r}, int64(0))
		c := e[0]
		call(m, "ctx.push", c)
		seed := call(m, "ctx.enter", ref.RuleRef{Index: ctxLeaderA}, int64(0))
		call(m, "ctx.found", seed[1])
		if pop := call(m, "ctx.pop", c)[0]; pop != call(m, "ctx.storeCtx")[0] {
			panic("the result that read a seed is not kept in the context")
		}
		return allows(m, ctxModule.Op("ctx.keep"), []any{c, ref.RuleRef{Index: r}, int64(0), int64(0), int64(0), true, int64(4), false,
			nil, nil, int64(4), call(m, "exp.empty")[0]})
	}
	if !keepAllowed(ctxMember) {
		return fmt.Errorf("ctx.keep of a member of the cycle is refused")
	}
	// The leader that does not grow is called as a member is, and its result is kept like one's: only the end
	// of its growth, which the model does not have, may keep it.
	if keepAllowed(ctxLeaderB) {
		return fmt.Errorf("ctx.keep of a leader is allowed outside a growth end")
	}
	return nil
}

// TestCtxKeepOfLeaderIsRefused checks that the model refuses what the contract does not allow: keeping the
// result of a leader, which only the end of its growth keeps, and that the check is held by this test: a
// model that drops it fails.
func TestCtxKeepOfLeaderIsRefused(t *testing.T) {
	if err := ctxKeepOfLeader(func(m *ref.Model, o *code.Op, a []any) bool { return m.Allows(o, a) }); err != nil {
		t.Error(err)
	}
	dropped := func(m *ref.Model, o *code.Op, a []any) bool {
		if o.Name == "ctx.keep" {
			// The model without the check of the leader: the other conditions of the keep are the model's.
			a = append([]any{}, a...)
			a[1] = ref.RuleRef{Index: ctxMember}
		}
		return m.Allows(o, a)
	}
	if ctxKeepOfLeader(dropped) == nil {
		t.Error("a model that does not refuse ctx.keep of a leader passes the test")
	}
}
