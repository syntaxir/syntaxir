package conform

import (
	"maps"
	"math/rand/v2"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

func (m modelImpl) Handle(name string) any { return m.Model.OprHandle(name) }

var oprModule = OprModule()

func oprCall(m *ref.Model, name string, args ...any) []any { return m.Call(oprModule.Op(name), args) }

// oprModelOps are the operations that the model provides to the cases of the family: those of every
// suite, of a parser of typed values and of the missing-token mode.
func oprModelOps() []string {
	return slices.Concat(OprSuiteOps(), OprTypedOps, OprMissingOps, OprMissingSupportOps)
}

func oprSuite(name string, mu *mutant) Suite {
	return Suite{Name: name, Ops: oprModelOps(), New: func(c ref.Config) Impl {
		if mu == nil {
			return modelImpl{ref.New(c)}
		}
		mu.modelImpl = modelImpl{ref.New(c)}
		return *mu
	}}
}

func TestOprCasesVerify(t *testing.T) {
	for _, c := range OprCases() {
		if err := code.Verify(c.Module); err != nil {
			t.Fatal(err)
		}
		break
	}
	r := RandomOpr("random", rand.New(rand.NewPCG(1, 2)), OprOps, 200, OprConfig())
	if err := code.Verify(r.Module); err != nil {
		t.Fatal(err)
	}
}

const oprSeeds, oprCalls = 200, 250

// oprRandom returns the randomized case of the seed with every operation of the family, in a parse in
// the missing-token mode for three seeds of four.
func oprRandom(seed uint64) Case {
	cfg := OprMissingConfig()
	if seed%4 == 3 {
		cfg = OprConfig()
	}
	return RandomOpr("random", rand.New(rand.NewPCG(seed, 7)), slices.Concat(OprOps, OprTypedOps, OprMissingOps), oprCalls, cfg)
}

// oprFails reports whether some handwritten or randomized case of the Operators Rule family fails on s.
func oprFails(s Suite) (handwritten, random bool) {
	for _, c := range OprCases() {
		if _, err := Run(s, c); err != nil {
			handwritten = true
		}
	}
	for seed := uint64(0); seed < oprSeeds && !random; seed++ {
		c := oprRandom(seed)
		_, err := Run(s, c)
		random = err != nil
	}
	return handwritten, random
}

func TestModelPassesOprCases(t *testing.T) {
	s := oprSuite("model", nil)
	for _, c := range OprCases() {
		if skipped, err := Run(s, c); err != nil || skipped {
			t.Errorf("%s: skipped %v, %v", c.Func.Name, skipped, err)
		}
	}
	for seed := uint64(0); seed < oprSeeds; seed++ {
		c := oprRandom(seed)
		if _, err := Run(s, c); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRandomOprCasesUseTheOperations checks that randomized cases exercise every operation.
func TestRandomOprCasesUseTheOperations(t *testing.T) {
	counts := map[string]int{}
	for seed := uint64(0); seed < oprSeeds; seed++ {
		c := oprRandom(seed)
		walkCalls(c.Func.Body, func(x *code.Call) { counts[x.Callee]++ })
	}
	for _, name := range slices.Concat(OprOps, OprTypedOps, OprMissingOps, ref.OprValueOps) {
		if counts[name] < 20 {
			t.Errorf("randomized cases call %s %d times", name, counts[name])
		}
	}
}

// TestOprMutantsFail is the negative control of the cases of the Operators Rule family: each broken
// model fails a handwritten or a randomized case.
func TestOprMutantsFail(t *testing.T) {
	for _, br := range slices.Concat(OprBrokenOps(), OprMatchBrokenOps(), OprValueBrokenOps(), OprTypedBrokenOps(), OprMissingBrokenOps()) {
		t.Run(br.Name, func(t *testing.T) {
			mu := mutant{op: br.Op, edit: func(m *ref.Model, a []any) []any {
				return br.Edit(func(op string, args ...any) []any { return oprCall(m, op, args...) }, a)
			}}
			handwritten, random := oprFails(oprSuite(br.Name, &mu))
			if !handwritten && !random {
				t.Error("no case detected the mutant")
			}
			if !random {
				t.Error("no randomized case detected the mutant")
			}
		})
	}
}

// regMutant is the model with the registers that one operation leaves broken by edit, which gets the
// registers before and after the call and the results.
type regMutant struct {
	modelImpl
	op   string
	regs *map[string]any
	edit func(before, after map[string]any, res []any)
}

func (m regMutant) UseRegisters(regs map[string]any) {
	*m.regs = regs
	m.modelImpl.UseRegisters(regs)
}

func (m regMutant) Call(o *code.Op, args []any) []any {
	if o.Name != m.op {
		return m.Model.Call(o, args)
	}
	before := maps.Clone(*m.regs)
	res := m.Model.Call(o, args)
	m.edit(before, *m.regs, res)
	return res
}

// TestOprRegisterMutantsFail is the negative control of what opr.skip and opr.match do to the registers:
// a model that leaves one of them wrong fails a handwritten or a randomized case.
func TestOprRegisterMutantsFail(t *testing.T) {
	for _, mu := range []struct {
		name, op string
		edit     func(before, after map[string]any, res []any)
	}{
		{"opr.match ignores the cut", "opr.match", func(b, a map[string]any, _ []any) { a[exec.Cut] = b[exec.Cut] }},
		{"opr.match clears the cut register", "opr.match", func(_, a map[string]any, _ []any) { a[exec.Cut] = false }},
		{"opr.match stops one short", "opr.match", func(_, a map[string]any, res []any) {
			if res[1].(bool) {
				a[exec.Pos] = a[exec.Pos].(int64) - 1
			}
		}},
		{"opr.skip leaves the position", "opr.skip", func(b, a map[string]any, _ []any) { a[exec.Pos] = b[exec.Pos] }},
		{"opr.skip sets the cut register", "opr.skip", func(_, a map[string]any, _ []any) { a[exec.Cut] = true }},
		{"opr.skip clears the cut register", "opr.skip", func(_, a map[string]any, _ []any) { a[exec.Cut] = false }},
		{"opr.skip leaves the silent depth raised", "opr.skip", func(_, a map[string]any, _ []any) {
			if n, ok := a[exec.Silent].(int64); ok {
				a[exec.Silent] = n + 1
			}
		}},
	} {
		t.Run(mu.name, func(t *testing.T) {
			s := Suite{Name: mu.name, Ops: oprModelOps(), New: func(c ref.Config) Impl {
				return regMutant{modelImpl{ref.New(c)}, mu.op, new(map[string]any), mu.edit}
			}}
			handwritten, random := oprFails(s)
			if !handwritten && !random {
				t.Error("no case detected the mutant")
			}
		})
	}
}
