//go:build sxir_long

package engine_test

import "testing"

// TestEnvironmentMutantsFail is the negative control of the conformance of the engine's variable
// environment: each mutant breaks one part of env.bind, env.lookup or env.key. A binding is no event
// of the compute units, so the controlled bind has no charge left to break.
func TestEnvironmentMutantsFail(t *testing.T) {
	const conformance = "^TestEngineExecConformance$"
	core := []string{"syntaxir.dev/sxir/internal/runtime/state"}
	t.Run("bind", func(t *testing.T) {
		const equal = "\tif p.rawValueEqual(p.Env.Get(slot, p.envLevels), val) {\n\t\treturn\n\t}\n"
		runPrintedMutants(t, "internal/runtime/state/budget_memo.go", []printedMutant{
			{"an equal binding makes a new environment", []printedEdit{{equal, ""}}, conformance, core},
			{"a rebind binds nothing", []printedEdit{{"\tp.Env = p.Env.Set(slot, p.envLevels, p.binding(val))\n\tp.chargeValueBytes", "\tp.chargeValueBytes"}},
				conformance, core},
		})
	})
	t.Run("uncontrolled bind", func(t *testing.T) {
		runPrintedMutants(t, "internal/runtime/state/runtime.go", []printedMutant{
			{"an equal binding makes a new environment",
				[]printedEdit{{"\tif p.Env.Get(slot, p.envLevels) == val {\n\t\treturn\n\t}\n", ""}}, conformance, core},
			{"a key keeps the initial values after a binding",
				[]printedEdit{{"if p.Env == p.initEnv && r.InitVals != nil &&", "if r.InitVals != nil &&"}}, conformance, core},
			{"a key leaves out its first variable",
				[]printedEdit{{"return p.keyValues(r.VarSlots)", "return p.keyValues(r.VarSlots[min(1, len(r.VarSlots)):])"}}, conformance, core},
		})
	})
}

// TestRecoveryMutantsFail is the negative control of the tests of the engine's recovered errors: each
// mutant breaks one part of stream.settleErrors, of the shared lists (rec.since and rec.add), of a
// stream's reports of them, of the growths that take them, or of their replay with missing tokens,
// and the tests named for it must fail.
func TestRecoveryMutantsFail(t *testing.T) {
	const conformance = "^TestEngineExecConformance$"
	const scratch = "^TestSettlingKeepsASmallScratch$"
	core := []string{"syntaxir.dev/sxir/internal/runtime/state"}
	t.Run("settle", func(t *testing.T) {
		const keep = "if p.Tr == nil && cap(p.Recovered) <= streamRecoveryScratchMax {"
		runPrintedMutants(t, "internal/runtime/state/stream_ops.go", []printedMutant{
			{"settling keeps the errors",
				[]printedEdit{{"\t\tp.Recovered = p.Recovered[:0]\n\t} else {", "\t} else {"}}, conformance, core},
			{"settling clears a list beyond the scratch", []printedEdit{{keep, "if p.Tr == nil {"}}, scratch, core},
			{"settling releases a list of one slot too many",
				[]printedEdit{{keep, "if p.Tr == nil && cap(p.Recovered) < streamRecoveryScratchMax {"}}, scratch, core},
			{"settling leaves the errors in the slots of the scratch",
				[]printedEdit{{"\t\tclear(p.Recovered[:cap(p.Recovered)])\n", ""}}, scratch, core},
			{"settling leaves the errors counted as delivered",
				[]printedEdit{{"\tp.diagnosed, p.diagnosedSkip, p.diagnosedList = 0, 0, nil\n\tif BudgetControlsEnabled && p.Control != nil {\n\t\t// With a diagnostic",
					"\tif BudgetControlsEnabled && p.Control != nil {\n\t\t// With a diagnostic"}},
				"^TestStreamCommitForgetsTheDiagnosedCount$", core},
			{"settling keeps the bytes of the errors",
				[]printedEdit{{"\t\tp.Control.errs = 0\n", "\t\t_ = 0\n"}}, "^TestMemoryLimitStream$", []string{"."}},
		})
	})
	t.Run("lists", func(t *testing.T) {
		runPrintedMutants(t, "internal/runtime/state/recovered.go", []printedMutant{
			{"since leaves the entries in place",
				[]printedEdit{{"\tp.Recovered = append(p.Recovered[:m], recEntry{list: l})\n", ""}}, conformance, core},
			{"since copies the errors of the lists",
				[]printedEdit{
					{"entries: appendSpend(p, make([]recEntry, 0, len(after)), after),", "entries: flatEntries(p, after),"},
					{"// countRecErrors returns", "func flatEntries(p *Parser, after []RecEntry) []RecEntry {\n\tvar out []RecEntry\n\tfor _, e := range appendRecErrors(nil, after) {\n\t\tp.spendValue(1)\n\t\tout = append(out, RecEntry{err: e})\n\t}\n\treturn out\n}\n\n// countRecErrors returns"},
				}, "^TestRecSinceSharesTheListsItTakes$", core},
			// The control of the bound on unrolling: wrapping the one entry after a mark changes no
			// error and no charge, and only the bound fails.
			{"since wraps the one entry after its mark",
				[]printedEdit{{"\tif len(p.Recovered)-m == 1 {\n\t\tp.spendValue(1)\n\t\treturn p.Recovered[m]\n\t}\n", ""}}, conformance, core},
			{"a list forgets the errors that a stream delivered",
				[]printedEdit{{"c.diagnosed, c.diagnosedSkip, c.diagnosedList = m, skip, l", "c.diagnosed = m\n\t\t_ = skip"}},
				"^TestReportRecoveredOfLists$", core},
		})
		runPrintedMutants(t, "internal/runtime/state/recovered.go", []printedMutant{
			{"a report delivers no errors of a list",
				[]printedEdit{{"errs = appendRecErrors(errs[:0], p.Recovered[p.diagnosed:p.diagnosed+1])",
					"errs = errs[:0]\n\t\tif e := p.Recovered[p.diagnosed]; e.err != nil {\n\t\t\terrs = append(errs, e.err)\n\t\t}"}},
				"^TestStreamDiagnosticsOfSharedLists$", core},
		})
		runPrintedMutants(t, "internal/runtime/state/call_exec.go", []printedMutant{
			{"a growth step keeps the errors of the step before",
				[]printedEdit{{"\t\t\t\tp.growAdopt(&g, v, p.recSince(rec))\n\t\t\t\tp.Recovered = p.Recovered[:rec]\n", "\t\t\t\tp.growAdopt(&g, v, p.recSince(rec))\n"}},
				"^TestMissingTokensLeftRecursion$", core},
			// The growth in a context drops the errors of each step after growKeepStep took them as a parameter.
			{"a growth that grew keeps the errors of the step before",
				[]printedEdit{{"\t\t\t\ts := p.growKeepStep(&g, v, ok, p.recSince(rec))\n\t\t\t\tp.Recovered = p.Recovered[:rec]\n", "\t\t\t\ts := p.growKeepStep(&g, v, ok, p.recSince(rec))\n"}},
				"^TestLeftRecursionDependencyMemoUnexamined$", core},
		})
		runPrintedMutants(t, "internal/runtime/state/runtime.go", []printedMutant{
			{"a growth step of the VM keeps the errors of the step before",
				[]printedEdit{{"\t\tp.growAdopt(g, v, p.recSince(g.Rec))\n\t\tp.Recovered = p.Recovered[:g.Rec]\n", "\t\tp.growAdopt(g, v, p.recSince(g.Rec))\n"}},
				"^TestBudgetDiagnosticRecoveredLists$", []string{"syntaxir.dev/sxir/internal/runtime/vm"}},
			{"a growth step of the VM in a context keeps the errors of the step before",
				[]printedEdit{{"\ts := p.growKeepStep(g, v, ok, errs)\n\tp.Recovered = p.Recovered[:g.Rec]\n", "\ts := p.growKeepStep(g, v, ok, errs)\n"}},
				"^TestLeftRecursionDependencyMemoUnexamined$", core},
		})
		runPrintedMutants(t, "internal/runtime/state/missing_token.go", []printedMutant{
			{"a replay records the errors of the insertions it repeats",
				[]printedEdit{{"\tif duplicate == nil {\n\t\tp.recAdd(errs)", "\tif true {\n\t\tp.recAdd(errs)"}},
				"^TestMissingTokensLedgerSeedBudget$", core},
		})
	})
}
