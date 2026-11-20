//go:build zp_long && !zp_budget_controls_reference

package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/backend/codegen/common"
	"zeroparse.dev/zp/internal/pass"
	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/pkg/runtime/input"
)

// chargeCorpusCases returns the testdata grammars with their samples and prefixes of them.
func chargeCorpusCases(t *testing.T) []chargeCase {
	t.Helper()
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.zp")
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

	zpinput "zeroparse.dev/zp/pkg/runtime/input"
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
	for u, unit := range []zpinput.PositionUnit{zpinput.CodePoints, zpinput.Bytes} {
		for i, in := range inputs {
			run := func(l uint64) string {
				_, err := Parse(in, WithUnit(unit), WithLimits(Limits{Compute: l}))
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

// TestChargeAgreementGenerated compares the units of generated Go and TypeScript parsers, with the
// default optimizations and with every optimization off, with the closure backend on the charge
// cases and the testdata corpora.
func TestChargeAgreementGenerated(t *testing.T) {
	codegentest.NeedsBudgetControls(t)
	cases := append([]chargeCase{}, chargeCases...)
	if os.Getenv("ZP_CHARGE_SMALL") == "" {
		cases = append(cases, chargeCorpusCases(t)...)
	}
	refs := make([]chargeRef, len(cases))
	for k, c := range cases {
		refs[k] = chargeReference(t, c)
	}
	settings := []struct {
		name  string
		noOpt []string
	}{{"default", nil}, {"optimizations off", allOptimizationsOff()}}
	for _, s := range settings {
		t.Run(s.name+"/go", func(t *testing.T) {
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
				code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: pkg, Start: "main", NoOpt: s.noOpt})
				if err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
				inputs, limits := refs[k].literals(c, false)
				consumer := strings.NewReplacer("PKG", pkg, "INPUTS", inputs, "LIMITS", limits).Replace(chargeGoConsumer)
				if err := os.MkdirAll(filepath.Join(dir, pkg), 0o755); err != nil {
					t.Fatal(err)
				}
				for name, src := range map[string]string{"parser.go": string(code), "charges_test.go": consumer} {
					if err := os.WriteFile(filepath.Join(dir, pkg, name), []byte(src), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			for k, c := range cases {
				cmd := exec.Command("go", "test", "-count=1", "-run", "TestCharges", "-v", "./"+fmt.Sprintf("p%d", k))
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s: %v\n%s", c.name, err, codegentest.Clip(string(out), 0))
				}
				refs[k].compare(t, c, "generated Go "+s.name, string(out))
			}
		})
		t.Run(s.name+"/typescript", func(t *testing.T) {
			if _, err := exec.LookPath("node"); err != nil {
				t.Skip("node not found")
			}
			for k, c := range cases {
				g, err := parseTest(c.grammar)
				if err != nil {
					t.Fatal(err)
				}
				inputs, limits := refs[k].literals(c, true)
				script := strings.NewReplacer("INPUTS", inputs, "LIMITS", limits).Replace(chargeTSScript)
				out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", LimitControls: true, Expectations: true, NoOpt: s.noOpt}, codegentest.GenerateTSBundle)
				refs[k].compare(t, c, "generated TypeScript "+s.name, out)
			}
		})
	}
}
