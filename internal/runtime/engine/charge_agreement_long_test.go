//go:build sxir_long && !sxir_budget_controls_reference

package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// chargeCorpusCases returns the testdata grammars with their samples and prefixes of them.
func chargeCorpusCases(t *testing.T) []chargeCase {
	t.Helper()
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	var out []chargeCase
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, chargeCase{filepath.Base(filepath.Dir(path)), string(src), grammarCorpus(t, path)})
	}
	return out
}

// TestChargeAgreementCorpora compares the units of the engine's backends on the samples of the
// testdata grammars and prefixes of them, in both position units.
func TestChargeAgreementCorpora(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range chargeCorpusCases(t) {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(fmt.Sprintf("%s/%v", c.name, unit), func(t *testing.T) {
				t.Parallel()
				for _, d := range chargeDivergences(engineChargeBackends(t, c.grammar, unit), c.inputs) {
					reportChargeDivergence(t, c.name, d.backend, d.msg)
				}
			})
		}
	}
}

// chargeGoConsumer prints, for each input and unit, the units that the generated Parse reserves
// (found as chargeTotal finds them) and its reports at the limits given.
const chargeGoConsumer = `package PKG

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

func report(err error) string {
	var le *LimitError
	if errors.As(err, &le) && le.Kind == LimitCompute {
		return fmt.Sprintf("%d/%d", le.Used, le.Requested)
	}
	return "ok"
}

func total(run func(uint64) string) uint64 {
	hi := uint64(64)
	for run(hi) != "ok" {
		if hi >= 1<<40 {
			return 0
		}
		hi *= 4
	}
	lo := uint64(0)
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if run(mid) == "ok" {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

var inputs = INPUTS

var limits = LIMITS

func TestCharges(t *testing.T) {
	for u, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
		for i, in := range inputs {
			run := func(l uint64) string {
				_, err := PARSE(in, WithUnit(unit), WithLimits(Limits{Compute: l}))
				return report(err)
			}
			var at []string
			for _, l := range limits[u][i] {
				at = append(at, run(l))
			}
			fmt.Printf("CHARGE %d %d %d %s\n", u, i, total(run), strings.Join(at, ","))
		}
	}
}
`

// chargeTSScript is chargeGoConsumer for a generated TypeScript parser.
const chargeTSScript = `import { parseWithOptions, CodePoints, Bytes } from "./parser.ts";
const inputs = INPUTS;
const limits = LIMITS;
const report = (r) => { const e = r.error; return e != null && e.kind === "compute-limit" ? e.used + "/" + e.requested : "ok"; };
const total = (run) => {
  let hi = 64;
  while (run(hi) !== "ok") { if (hi >= 2 ** 40) return 0; hi *= 4; }
  let lo = 0;
  while (hi - lo > 1) { const mid = lo + Math.floor((hi - lo) / 2); if (run(mid) === "ok") hi = mid; else lo = mid; }
  return hi;
};
[CodePoints, Bytes].forEach((unit, u) => {
  inputs.forEach((input, i) => {
    const run = (l) => report(parseWithOptions(input, { unit, expectations: true, limits: { compute: l } }));
    const at = limits[u][i].map(run);
    console.log("CHARGE " + u + " " + i + " " + total(run) + " " + at.join(","));
  });
});
`

// chargeRef is what the reference backend gives for each unit and input: the units and the reports
// at the limits below them.
type chargeRef struct {
	totals [2][]uint64
	limits [2][][]uint64
	at     [2][][]string
}

func chargeReference(t *testing.T, c chargeCase) chargeRef {
	t.Helper()
	var r chargeRef
	for u, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
		ref := engineChargeBackends(t, c.grammar, unit)[0]
		for _, in := range c.inputs {
			total := chargeTotal(ref.run, in)
			limits := chargeLimits(total)
			at := make([]string, len(limits))
			for k, l := range limits {
				at[k] = ref.run(in, l)
			}
			r.totals[u] = append(r.totals[u], total)
			r.limits[u] = append(r.limits[u], limits)
			r.at[u] = append(r.at[u], at)
		}
	}
	return r
}

// literals returns the Go, or with js the JavaScript, source of the inputs and the limits.
func (r chargeRef) literals(c chargeCase, js bool) (inputs, limits string) {
	open, close := "{", "}"
	if js {
		open, close = "[", "]"
	}
	var in []string
	for _, s := range c.inputs {
		if js {
			in = append(in, jsString(s))
		} else {
			in = append(in, fmt.Sprintf("%q", s))
		}
	}
	var units []string
	for u := range r.limits {
		var per []string
		for _, ls := range r.limits[u] {
			var nums []string
			for _, l := range ls {
				nums = append(nums, fmt.Sprint(l))
			}
			per = append(per, open+strings.Join(nums, ",")+close)
		}
		units = append(units, open+strings.Join(per, ",")+close)
	}
	limits = open + strings.Join(units, ",") + close
	if js {
		return "[" + strings.Join(in, ",") + "]", limits
	}
	return "[]string{" + strings.Join(in, ",") + "}", "[][][]uint64" + limits
}

func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteString(`\` + string(r))
		case r < 0x20 || r > 0x7e:
			if r > 0xffff {
				r -= 0x10000
				fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// compare checks the CHARGE lines of a generated parser against the reference.
func (r chargeRef) compare(t *testing.T, c chargeCase, name, out string) {
	t.Helper()
	seen := 0
	for _, line := range strings.Split(out, "\n") {
		var u, i int
		var total uint64
		var at string
		n, _ := fmt.Sscanf(line, "CHARGE %d %d %d %s", &u, &i, &total, &at)
		if n < 3 {
			continue
		}
		seen++
		unit := []string{"codepoints", "bytes"}[u]
		if total != r.totals[u][i] {
			reportChargeDivergence(t, c.name, name, fmt.Sprintf("%s: input %d (%d bytes): %s %d, closure %d", unit, i, len(c.inputs[i]), name, total, r.totals[u][i]))
			continue
		}
		got := strings.Split(at, ",")
		for k, l := range r.limits[u][i] {
			if k >= len(got) || got[k] != r.at[u][i][k] {
				g := "-"
				if k < len(got) {
					g = got[k]
				}
				reportChargeDivergence(t, c.name, name, fmt.Sprintf("%s: input %d (%d bytes): limit %d: %s %s, closure %s", unit, i, len(c.inputs[i]), l, name, g, r.at[u][i][k]))
				break
			}
		}
	}
	if seen != 2*len(c.inputs) {
		t.Fatalf("%s: %d reports for %d inputs\n%s", name, seen, len(c.inputs), codegentest.Clip(out, 0))
	}
}

// allOptimizationsOff names every optimization, for GenOptions.NoOpt.
func allOptimizationsOff() []string {
	var out []string
	for _, f := range pass.Flags() {
		out = append(out, f.Name)
	}
	return out
}

// chargeSettings are the optimization settings under which generated parsers are compared.
var chargeSettings = []struct {
	name  string
	noOpt []string
}{{"default", nil}, {"optimizations off", allOptimizationsOff()}}

// generatedGoCharges generates a Go parser for each case, with the optimizations noOpt off, and
// returns the output of its consumer: the CHARGE lines for the inputs of the case and the limits of
// its reference. With typed, the parser is the typed parser and the consumer runs ParseValue, whose
// conversion of the result charges nothing, so that its units are those of the typed parse.
func generatedGoCharges(t *testing.T, cases []chargeCase, refs []chargeRef, noOpt []string, typed bool) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoModAt("fixture", codegentest.LibCopy())), 0o644); err != nil {
		t.Fatal(err)
	}
	for k, c := range cases {
		g, err := parseTest(c.grammar)
		if err != nil {
			t.Fatal(err)
		}
		pkg := fmt.Sprintf("p%d", k)
		code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: pkg, Start: "main", NoOpt: noOpt, Types: typed})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if typed && !strings.Contains(string(code), "TParse(") {
			t.Fatalf("%s: no typed parser", c.name)
		}
		parse := "Parse("
		if typed {
			parse = "ParseValue("
		}
		inputs, limits := refs[k].literals(c, false)
		consumer := strings.NewReplacer("PKG", pkg, "INPUTS", inputs, "LIMITS", limits, "PARSE(", parse).Replace(chargeGoConsumer)
		if err := os.MkdirAll(filepath.Join(dir, pkg), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, src := range map[string]string{"parser.go": string(code), "charges_test.go": consumer} {
			if err := os.WriteFile(filepath.Join(dir, pkg, name), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The consumers build and run a few at a time; a failure is reported for the first case that failed.
	outs := make([]string, len(cases))
	errs := make([]error, len(cases))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for k := range cases {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			cmd := exec.Command("go", "test", "-count=1", "-run", "TestCharges", "-v", "./"+fmt.Sprintf("p%d", k))
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			outs[k], errs[k] = string(out), err
		})
	}
	wg.Wait()
	for k, c := range cases {
		if errs[k] != nil {
			t.Fatalf("%s: %v\n%s", c.name, errs[k], codegentest.Clip(outs[k], 0))
		}
	}
	return outs
}

// generatedTSCharges is generatedGoCharges for a generated TypeScript parser of one case.
func generatedTSCharges(t *testing.T, c chargeCase, ref chargeRef, noOpt []string) string {
	t.Helper()
	g, err := parseTest(c.grammar)
	if err != nil {
		t.Fatal(err)
	}
	inputs, limits := ref.literals(c, true)
	script := strings.NewReplacer("INPUTS", inputs, "LIMITS", limits).Replace(chargeTSScript)
	return codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Expectations: true, NoOpt: noOpt}, codegentest.GenerateTSBundle)
}

// TestChargeAgreementGenerated compares the units of generated Go parsers (Parse, and ParseValue of
// the typed parser) and TypeScript parsers, with the default optimizations and with every
// optimization off, with the closure backend on the charge cases and the testdata corpora.
func TestChargeAgreementGenerated(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	cases := append([]chargeCase{}, chargeCases...)
	if os.Getenv("SXIR_CHARGE_SMALL") == "" {
		cases = append(cases, chargeCorpusCases(t)...)
	}
	refs := make([]chargeRef, len(cases))
	for k, c := range cases {
		refs[k] = chargeReference(t, c)
	}
	for _, s := range chargeSettings {
		t.Run(s.name+"/go", func(t *testing.T) {
			t.Parallel()
			for k, out := range generatedGoCharges(t, cases, refs, s.noOpt, false) {
				refs[k].compare(t, cases[k], "generated Go "+s.name, out)
			}
		})
		t.Run(s.name+"/go typed", func(t *testing.T) {
			t.Parallel()
			for k, out := range generatedGoCharges(t, cases, refs, s.noOpt, true) {
				refs[k].compare(t, cases[k], "generated Go typed "+s.name, out)
			}
		})
		t.Run(s.name+"/typescript", func(t *testing.T) {
			if _, err := exec.LookPath("node"); err != nil {
				t.Skip("node not found")
			}
			t.Parallel()
			for k, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					refs[k].compare(t, c, "generated TypeScript "+s.name, generatedTSCharges(t, c, refs[k], s.noOpt))
				})
			}
		})
	}
}

// chargeTotalsOf returns the units of the CHARGE lines of out, by position unit and input.
func chargeTotalsOf(t *testing.T, out string, inputs int) [2][]uint64 {
	t.Helper()
	totals := [2][]uint64{make([]uint64, inputs), make([]uint64, inputs)}
	seen := 0
	for _, line := range strings.Split(out, "\n") {
		var u, i int
		var total uint64
		if n, _ := fmt.Sscanf(line, "CHARGE %d %d %d", &u, &i, &total); n == 3 {
			totals[u][i] = total
			seen++
		}
	}
	if seen != 2*inputs {
		t.Fatalf("%d reports for %d inputs\n%s", seen, inputs, codegentest.Clip(out, 0))
	}
	return totals
}

// TestIterationsChargeOneUnitEachGenerated is TestIterationsChargeOneUnitEach for generated Go
// parsers (Parse and typed ParseValue) and TypeScript parsers, with the default optimizations and
// with every optimization off.
func TestIterationsChargeOneUnitEachGenerated(t *testing.T) {
	t.Parallel()
	var vs []unitVariant
	for _, v := range iterationVariants {
		vs = append(vs, unitVariant{v.name, v.grammar, v.unrolled, v.input, v.iterations})
	}
	checkVariantsGenerated(t, "its iterations", vs)
}

// TestCallsChargeTheirUnitsGenerated is TestCallsChargeTheirUnits for generated Go parsers (Parse
// and typed ParseValue) and TypeScript parsers, with the default optimizations and with every
// optimization off.
func TestCallsChargeTheirUnitsGenerated(t *testing.T) {
	t.Parallel()
	var vs []unitVariant
	for _, v := range callVariants {
		vs = append(vs, unitVariant{v.name, v.grammar, v.reference, v.input, v.calls})
	}
	checkVariantsGenerated(t, "its calls", vs)
}

// unitVariant is a grammar whose units on an input exceed those of a reference grammar by more.
type unitVariant struct {
	name, grammar, reference, input string
	more                            uint64
}

// checkVariantsGenerated checks the variants on generated Go and TypeScript parsers, with the default
// optimizations and with every optimization off; what names the units that the variants differ by.
func checkVariantsGenerated(t *testing.T, what string, vs []unitVariant) {
	codegentest.NeedsBudgetControls(t)
	var cases []chargeCase
	for _, v := range vs {
		cases = append(cases, chargeCase{v.name, v.grammar, []string{v.input}}, chargeCase{v.name + " reference", v.reference, []string{v.input}})
	}
	// No limits below the units: only the units are compared.
	refs := make([]chargeRef, len(cases))
	for k := range refs {
		refs[k].limits = [2][][]uint64{{nil}, {nil}}
	}
	// checkOne checks variant k against its reference, from the outputs of the two.
	checkOne := func(t *testing.T, name string, k int, out, ref string) {
		t.Helper()
		v := vs[k]
		got, base := chargeTotalsOf(t, out, 1), chargeTotalsOf(t, ref, 1)
		for u, unit := range []string{"codepoints", "bytes"} {
			if got[u][0] != base[u][0]+v.more {
				t.Errorf("%s: %s: %s: %q: %d units, %d more than the reference, want %d more for %s\n%s\nreference:\n%s", name, v.name, unit, v.input, got[u][0], int64(got[u][0])-int64(base[u][0]), v.more, what, v.grammar, v.reference)
			}
		}
	}
	check := func(t *testing.T, name string, outs []string) {
		t.Helper()
		for k := range vs {
			checkOne(t, name, k, outs[2*k], outs[2*k+1])
		}
	}
	for _, s := range chargeSettings {
		t.Run(s.name+"/go", func(t *testing.T) {
			t.Parallel()
			check(t, "generated Go "+s.name, generatedGoCharges(t, cases, refs, s.noOpt, false))
		})
		t.Run(s.name+"/go typed", func(t *testing.T) {
			t.Parallel()
			check(t, "generated Go typed "+s.name, generatedGoCharges(t, cases, refs, s.noOpt, true))
		})
		t.Run(s.name+"/typescript", func(t *testing.T) {
			if _, err := exec.LookPath("node"); err != nil {
				t.Skip("node not found")
			}
			t.Parallel()
			for k, v := range vs {
				t.Run(v.name, func(t *testing.T) {
					t.Parallel()
					out := generatedTSCharges(t, cases[2*k], refs[2*k], s.noOpt)
					ref := generatedTSCharges(t, cases[2*k+1], refs[2*k+1], s.noOpt)
					checkOne(t, "generated TypeScript "+s.name, k, out, ref)
				})
			}
		})
	}
}
