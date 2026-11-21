package conform

import (
	"math/rand/v2"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

var callModule = CallModule()

func callCall(m *ref.Model, name string, args ...any) []any {
	return m.Call(callModule.Op(name), args)
}

// callMutants are broken models of the traced call path, the negative control of its cases.
func callMutants() map[string]mutant {
	return map[string]mutant{
		"tracked is always false":    {op: "call.tracked", edit: func(m *ref.Model, a []any) []any { return []any{false} }},
		"tracked ignores the tracer": {op: "call.tracked", edit: func(m *ref.Model, a []any) []any { return []any{true} }},
		"track fails every call": {op: "call.track", edit: func(m *ref.Model, a []any) []any {
			callCall(m, "call.track", a...)
			return []any{nil, false}
		}},
		"track matches every call": {op: "call.track", edit: func(m *ref.Model, a []any) []any {
			r := callCall(m, "call.track", a...)
			return []any{r[0], true}
		}},
		"track runs the call twice": {op: "call.track", edit: func(m *ref.Model, a []any) []any {
			callCall(m, "call.track", a...)
			return callCall(m, "call.track", a...)
		}},
		"track drops the errors of a call that matched": {op: "call.track", edit: func(m *ref.Model, a []any) []any {
			n := m.Entries()
			r := callCall(m, "call.track", a...)
			callCall(m, "rec.truncate", n)
			return r
		}},
	}
}

func callRandom(seed uint64) Case {
	return RandomCall("random", rand.New(rand.NewPCG(seed, 11)), CallOps, 80)
}

// callFails reports whether some handwritten or randomized case of the traced call path fails on s.
func callFails(s Suite) (handwritten, random bool) {
	for _, c := range CallCases() {
		if _, err := Run(s, c); err != nil {
			handwritten = true
		}
	}
	for seed := uint64(0); seed < 200 && !random; seed++ {
		_, err := Run(s, callRandom(seed))
		random = err != nil
	}
	return handwritten, random
}

func TestCallCasesVerify(t *testing.T) {
	if err := code.Verify(CallCases()[0].Module); err != nil {
		t.Fatal(err)
	}
	if err := code.Verify(callRandom(1).Module); err != nil {
		t.Fatal(err)
	}
}

// TestModelPassesCallCases checks the cases of the traced call path against the contract as the model
// states it.
func TestModelPassesCallCases(t *testing.T) {
	s := Suite{Name: "model", Ops: append(append([]string{}, CallOps...), RuleCaseOps...), New: func(c ref.Config) Impl { return modelImpl{ref.New(c)} }}
	for _, c := range CallCases() {
		if skipped, err := Run(s, c); err != nil || skipped {
			t.Errorf("%s: skipped %v, %v", c.Func.Name, skipped, err)
		}
	}
	for seed := uint64(0); seed < 200; seed++ {
		if _, err := Run(s, callRandom(seed)); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRandomCallCasesUseTheOperations checks that randomized cases exercise what they are for.
func TestRandomCallCasesUseTheOperations(t *testing.T) {
	counts := map[string]int{}
	for seed := uint64(0); seed < 200; seed++ {
		walkCalls(callRandom(seed).Func.Body, func(x *code.Call) { counts[x.Callee]++ })
	}
	for _, name := range []string{"call.tracked", "call.track"} {
		if counts[name] < 200 {
			t.Errorf("randomized cases call %s %d times", name, counts[name])
		}
	}
}

// TestCallMutantsFail is the negative control of the cases of the traced call path: each broken model
// fails a handwritten case and a randomized one.
func TestCallMutantsFail(t *testing.T) {
	for name, mu := range callMutants() {
		t.Run(name, func(t *testing.T) {
			s := Suite{Name: name, Ops: append(append([]string{}, CallOps...), RuleCaseOps...), New: func(c ref.Config) Impl {
				mu.modelImpl = modelImpl{ref.New(c)}
				return mu
			}}
			handwritten, random := callFails(s)
			if !handwritten {
				t.Error("no handwritten case detected the mutant")
			}
			if !random {
				t.Error("no randomized case detected the mutant")
			}
		})
	}
}

// TestCallPrecondition checks that the model refuses what the contract does not allow: a traced call in
// a parse that is not traced, and of a rule that a parse memoizes.
func TestCallPrecondition(t *testing.T) {
	track := callModule.Op("call.track")
	args := []any{ref.RuleRef{Index: 0}, int64(0)}
	if !ref.New(CallConfig(true, ref.RuleSpec{})).Allows(track, args) {
		t.Error("the model refuses a traced call of a rule that runs its body")
	}
	if ref.New(CallConfig(false, ref.RuleSpec{})).Allows(track, args) {
		t.Error("the model allows a traced call in a parse that is not traced")
	}
	for _, s := range []ref.RuleSpec{{Memo: true}, {DocMemo: true}, {Leader: true}} {
		if ref.New(CallConfig(true, s)).Allows(track, args) {
			t.Errorf("the model allows a traced call of the rule %+v", s)
		}
	}
}
