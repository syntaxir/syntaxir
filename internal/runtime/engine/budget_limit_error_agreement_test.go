//go:build !sxir_budget_controls_reference

package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// A LimitError of kind LimitCompute reports the check that stopped the parse
// (spec/runtime/parsing.md#checks-and-interruptions): Used is the counter at that check, above the limit
// by at most K + B or the units of the sized event the check follows, and Requested is zero, since a
// check covers many events. Whether a parse ends with the error does not depend on the backend; Used
// may, within the overshoot. These cases run under every compute limit from 1 to max and record what
// each backend reports, as "used/requested" ("ok" for a parse that succeeds, "error" for another
// error), so that the decision, the fields of the error and, among the engine's backends, which count
// the same events at the same checks, the whole report can be compared.
type limitAgreementCase struct {
	name, grammar, input string
	max                  uint64
}

var limitAgreementCases = []limitAgreementCase{
	// A literal of four code points, which one advance counts.
	{name: "literal", grammar: "@start\nrule main = \"abcd\"+", input: "abcdabcdabcd", max: 80},
	// The operators of a level, which the longest match tries one after another.
	{name: "operators", grammar: `@start
rule main = #operators {
    operand [0-9]
    level {
        infix left "+"
        infix left "-"
        infix left "*"
        infix left "/"
        infix left "%"
        infix left "^"
    }
}`, input: "1+2-3*4/5%6^7", max: 220},
	// A list of seven elements and the five that filter keeps, each a sized event.
	{name: "list", grammar: "@start\nrule main int = \"x\" => len(filter(list(1, 2, 3, 4, 5, 6, 7), (e) => e > 2))", input: "x", max: 120},
	// A list of 150 elements, more than a chunk of a copy.
	{name: "long list", grammar: "@start\nrule main int = \"x\" => len(filter(concat(" + strings.TrimSuffix(strings.Repeat("list(1, 2, 3, 4, 5, 6, 7, 8, 9, 10), ", 15), ", ") + "), (e) => e > 2))", input: "x", max: 700},
	// The list of a repetition of five elements, which the parse gathers and takes off without a
	// unit.
	{name: "repetition", grammar: "@start\nrule main = [a-z]*", input: "abcde", max: 60},
}

// limitOvershoot is K + B (spec/runtime/parsing.md#checks-and-interruptions) for the grammars of
// limitAgreementCases, whose literals are shorter than 4096 and whose sized events are smaller than K +
// B: the most that Used may exceed the limit by.
const limitOvershoot = 4096 + 4096

func limitReport(err error, used, requested uint64, isLimit bool) string {
	switch {
	case err == nil:
		return "ok"
	case isLimit:
		return fmt.Sprintf("%d/%d", used, requested)
	}
	return "error"
}

// engineLimitReports returns what the backend b reports at each limit from 1 to c.max.
func engineLimitReports(t *testing.T, c limitAgreementCase, b Backend) []string {
	prog := compileBare(t, c.grammar)
	out := make([]string, c.max+1)
	for l := uint64(1); l <= c.max; l++ {
		_, err := prog.ParseWith("main", c.input, ParseOptions{Backend: b, Limits: sxirruntime.Limits{Compute: l}})
		var le *sxirruntime.LimitError
		ok := errors.As(err, &le)
		if ok {
			out[l] = limitReport(err, le.Used, le.Requested, ok)
		} else {
			out[l] = limitReport(err, 0, 0, false)
		}
	}
	return out
}

// limitAgreementConsumer prints, for each limit, what the generated Parse and ParseValue report.
const limitAgreementConsumer = `package fixture

import (
	"errors"
	"fmt"
	"testing"
)

func report(err error) string {
	var le *LimitError
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &le):
		return fmt.Sprintf("%d/%d", le.Used, le.Requested)
	}
	return "error"
}

func TestReports(t *testing.T) {
	for l := uint64(1); l <= MAX; l++ {
		_, err := Parse(INPUT, WithLimits(Limits{Compute: l}))
		_, terr := ParseValue(INPUT, WithLimits(Limits{Compute: l}))
		fmt.Printf("REPORT %d %s %s\n", l, report(err), report(terr))
	}
}
`

// generatedGoLimitReports returns what the Parse and the ParseValue of the parser generated from c report
// at each limit from 1 to c.max, with the runtime in the module directory runtime.
func generatedGoLimitReports(t *testing.T, c limitAgreementCase, runtime string) (node, typed []string) {
	g, err := parseTest(c.grammar)
	if err != nil {
		t.Fatal(err)
	}
	code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "fixture", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	consumer := strings.NewReplacer("MAX", fmt.Sprint(c.max), "INPUT", fmt.Sprintf("%q", c.input)).Replace(limitAgreementConsumer)
	for name, source := range map[string]string{
		"go.mod":         codegentest.GoModAt("fixture", runtime),
		"parser.go":      string(code),
		"parser_test.go": consumer,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "TestReports", "-v", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	node, typed = make([]string, c.max+1), make([]string, c.max+1)
	for _, line := range strings.Split(string(out), "\n") {
		var l uint64
		var n, ty string
		if _, err := fmt.Sscanf(line, "REPORT %d %s %s", &l, &n, &ty); err == nil && l <= c.max {
			node[l], typed[l] = n, ty
		}
	}
	return node, typed
}

const limitAgreementTSScript = `import { parseWithOptions } from "./parser.ts";
for (let l = 1; l <= MAX; l++) {
  const r = parseWithOptions(INPUT, { limits: { compute: l } });
  const e = r.error;
  const s = e == null ? "ok" : e.kind === "compute-limit" ? e.used + "/" + e.requested : "error";
  console.log("REPORT " + l + " " + s);
}
`

// generatedTSLimitReports returns what the generated TypeScript parser reports at each limit from 1
// to c.max, with patch applied to its bundle.
func generatedTSLimitReports(t *testing.T, c limitAgreementCase, patch func(string) string) []string {
	g, err := parseTest(c.grammar)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.NewReplacer("MAX", fmt.Sprint(c.max), "INPUT", fmt.Sprintf("%q", c.input)).Replace(limitAgreementTSScript)
	bundle := codegentest.GenerateTSBundle
	if patch != nil {
		bundle = func(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
			b, err := codegentest.GenerateTSBundle(g, opts)
			return []byte(patch(string(b))), err
		}
	}
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, bundle)
	reports := make([]string, c.max+1)
	for _, line := range strings.Split(out, "\n") {
		var l uint64
		var s string
		if _, err := fmt.Sscanf(line, "REPORT %d %s", &l, &s); err == nil && l <= c.max {
			reports[l] = s
		}
	}
	return reports
}

// limitDecision is the decision of a report of limitReport: "ok", "limit" or "error".
func limitDecision(r string) string {
	if r == "ok" || r == "error" {
		return r
	}
	return "limit"
}

// limitContractViolations returns the limits from 1 to max at which the report of got breaks the
// contract of a compute LimitError: a Requested that is not zero, or a Used that is not above the limit
// or exceeds it by more than limitOvershoot. A report that is missing is a violation too.
func limitContractViolations(got []string) []string {
	var d []string
	for l := uint64(1); l < uint64(len(got)); l++ {
		if got[l] == "" {
			d = append(d, fmt.Sprintf("limit %d: no report", l))
			continue
		}
		if limitDecision(got[l]) != "limit" {
			continue
		}
		var used, requested uint64
		if _, err := fmt.Sscanf(got[l], "%d/%d", &used, &requested); err != nil {
			d = append(d, fmt.Sprintf("limit %d: report %q", l, got[l]))
			continue
		}
		if requested != 0 || used <= l || used > l+limitOvershoot {
			d = append(d, fmt.Sprintf("limit %d: used %d, requested %d", l, used, requested))
		}
	}
	return d
}

// limitDecisionDisagreements returns the limits from 1 to max at which the decision of got differs
// from that of want.
func limitDecisionDisagreements(want, got []string) []string {
	var d []string
	for l := uint64(1); l < uint64(len(want)); l++ {
		if l >= uint64(len(got)) || limitDecision(got[l]) != limitDecision(want[l]) {
			r := "none"
			if l < uint64(len(got)) {
				r = got[l]
			}
			d = append(d, fmt.Sprintf("limit %d: %s, closure %s", l, r, want[l]))
		}
	}
	return d
}

// limitReportDisagreements returns the limits from 1 to max at which got differs from want.
func limitReportDisagreements(want, got []string) []string {
	var d []string
	for l := uint64(1); l < uint64(len(want)); l++ {
		if got[l] != want[l] {
			d = append(d, fmt.Sprintf("limit %d: %s, closure %s", l, got[l], want[l]))
		}
	}
	return d
}

// TestEngineLimitErrors checks the compute LimitError of the engine's backends under every limit of
// limitAgreementCases: the closure backend's keeps the contract (limitContractViolations) and stops
// some parse of each case, and the bytecode machines, which count the same events and check at the same
// points, report exactly what it reports.
func TestEngineLimitErrors(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range limitAgreementCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			want := engineLimitReports(t, c, Closure)
			for _, d := range limitContractViolations(want) {
				t.Errorf("closure: %s", d)
			}
			if limitDecision(want[1]) != "limit" || want[c.max] != "ok" {
				t.Errorf("closure reports %s at limit 1 and %s at %d: want the limit to stop the parse and the largest to let it end", want[1], want[c.max], c.max)
			}
			for _, b := range []Backend{Bytecode, BytecodeIterative} {
				for _, d := range limitReportDisagreements(want, engineLimitReports(t, c, b)) {
					t.Errorf("%v: %s", b, d)
				}
			}
		})
	}
}

// TestLimitErrorAgreement checks the compute LimitError of generated Go, its typed parser and generated
// TypeScript under every limit of limitAgreementCases against the closure backend's: the decision is
// the same at every limit, and every error keeps the contract (limitContractViolations).
func TestLimitErrorAgreement(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	runtime := codegentest.LibCopy()
	for _, c := range limitAgreementCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			codegentest.NeedsBudgetControls(t)
			want := engineLimitReports(t, c, Closure)
			reports := map[string][]string{}
			reports["generated Go"], reports["generated Go typed"] = generatedGoLimitReports(t, c, runtime)
			// With SKIP_NODE=1 only the TypeScript part is skipped.
			if !tooltest.OptedOut() {
				tooltest.RequireNode(t)
				reports["generated TypeScript"] = generatedTSLimitReports(t, c, nil)
			}
			if os.Getenv("SXIR_LIMIT_AGREEMENT_DUMP") != "" {
				at := func(name string, l uint64) string {
					if r := reports[name]; l < uint64(len(r)) {
						return r[l]
					}
					return "-"
				}
				for l := uint64(1); l <= c.max; l++ {
					t.Logf("%d closure=%s go=%s typed=%s ts=%s", l, want[l], at("generated Go", l), at("generated Go typed", l), at("generated TypeScript", l))
				}
			}
			for name, got := range reports {
				for _, d := range limitDecisionDisagreements(want, got) {
					t.Errorf("%s: %s", name, d)
				}
				for _, d := range limitContractViolations(got) {
					t.Errorf("%s: %s", name, d)
				}
			}
		})
	}
}
