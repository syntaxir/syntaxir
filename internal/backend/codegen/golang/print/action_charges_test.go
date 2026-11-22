package print_test

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime"
)

// actionChargesGoldenFile returns the golden of the charges of the action expressions of the generator; what names it in
// the header.
func actionChargesGoldenFile(path, what, unset string) codegentest.GoldenFile {
	return codegentest.GoldenFile{Path: path, CaseOf: codegentest.GoldenCaseBefore, Digests: true, Group: actionChargeGroup,
		Header: "# The charges of the generated action expression differential cases (" + what + ") on every target: the units the\n" +
			"# engine's closure backend, bytecode VM and iterative VM, the node and the typed generated Go parsers and the\n" +
			"# generated TypeScript parser charge for each input, and the outcome (ok, or the error) each reports. The targets\n" +
			"# charge action expressions in their own ways today, so the columns differ; the golden freezes each, so that a change of an\n" +
			"# action expression's charges on one target is a stated change. " + unset + "\n" + codegentest.GoldenRules + codegentest.GoldenDigestRules +
			"# The part of a case is the target: the last word of the entry's key.\n"}
}

// actionChargeGroup splits the key of an entry, "case | input target", into the case and the target, which
// is the last word of the key (with -recognize for the node parser's Recognize).
func actionChargeGroup(key string) (caseName, target string) {
	caseName = codegentest.GoldenCaseBefore(key)
	return caseName, key[strings.LastIndex(key, " ")+1:]
}

// actionChargeCases returns the action cases of the golden, which the compiler accepts.
func actionChargeCases(t *testing.T, all []codegentest.ActionCase) []codegentest.ActionCase {
	t.Helper()
	var out []codegentest.ActionCase
	for _, c := range all {
		g, err := parseTest(c.Src)
		if err == nil {
			_, err = entry.Compile(g, entry.Options{})
		}
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		out = append(out, c)
	}
	return out
}

// outcome shortens a result to ok or the first line of its error.
func actionOutcome(res string) string {
	res = strings.Trim(res, `"`)
	if res == "<nil>" || res == "" {
		return "ok"
	}
	if i := strings.Index(res, `\n`); i >= 0 {
		res = res[:i]
	}
	return res
}

// TestActionChargesAcrossTargets records what every target charges for the generated action expressions, and checks
// that the golden still holds. It asserts no equality between targets: their counts differ by contract
// today (spec/runtime/parsing.md), and the golden is the baseline against which later stages state the
// changes they make to them.
func TestActionChargesAcrossTargets(t *testing.T) {
	t.Parallel()
	checkActionCharges(t, "testdata/action_charges.golden", "codegentest.ActionCases", "The action expressions leave no struct field unset.", "actioncharges", codegentest.ActionCases(1, 40, false))
}

// TestActionChargesAcrossTargetsWide pins the charges of the action expressions of codegentest.ActionCasesWide, which have
// strings beyond the Basic Multilingual Plane, other literals, tuples, lists of structured values and
// the errors of run time. The action expressions of seeds 5 to 12 leave struct fields unset, which read as their zero values, so
// that the golden sees the operators applied to the zero values. The targets charge them in their own
// ways, like the action expressions above.
func TestActionChargesAcrossTargetsWide(t *testing.T) {
	t.Parallel()
	var all []codegentest.ActionCase
	for seed := uint64(1); seed <= 12; seed++ {
		all = append(all, codegentest.ActionCasesWide(seed, 20, seed > 4)...)
	}
	checkActionCharges(t, "testdata/action_charges_wide.golden", "codegentest.ActionCasesWide, seeds 1 to 12", "Some action expressions leave struct fields unset.", "actionchargeswide", all)
}

// TestActionChargesMember pins the charges of codegentest.MemberActionCases, whose action expressions read every kind of
// member, set fields and the zero values of unset lists, tuples, optionals and structs among them, so
// that a change in how a target reads a member shows as a stated change of the units it charges.
func TestActionChargesMember(t *testing.T) {
	t.Parallel()
	checkActionCharges(t, "testdata/action_charges_member.golden", "codegentest.MemberActionCases", "Some action expressions leave struct fields unset.", "actionchargesmember", codegentest.MemberActionCases())
}

// TestActionChargesConvert pins the charges of codegentest.ConvertActionCases, whose action expressions convert string, tuple
// and list nodes and nodes of named node types to a named node type, so that a change in how a target
// converts a value shows as a stated change of the units it charges.
func TestActionChargesConvert(t *testing.T) {
	t.Parallel()
	checkActionCharges(t, "testdata/action_charges_convert.golden", "codegentest.ConvertActionCases", "The action expressions make no struct value.", "actionchargesconvert", codegentest.ConvertActionCases())
}

// TestActionChargesStruct pins the charges of codegentest.StructActionCases, whose action expressions make struct values of
// fields that are nodes, so that a change in how a target makes a struct and sets its range shows as a stated
// change of the units it charges.
func TestActionChargesStruct(t *testing.T) {
	t.Parallel()
	checkActionCharges(t, "testdata/action_charges_struct.golden", "codegentest.StructActionCases", "Some action expressions leave struct fields unset.", "actionchargesstruct", codegentest.StructActionCases())
}

// TestActionChargesCast pins the charges of codegentest.CastActionCases, whose action expressions cast values to types of every
// kind, the lists and tuples of them among them, so that a change in how a target checks a value against a
// type shows as a stated change of the units it charges.
func TestActionChargesCast(t *testing.T) {
	t.Parallel()
	checkActionCharges(t, "testdata/action_charges_cast.golden", "codegentest.CastActionCases", "The action expressions make no struct value.", "actionchargescast", codegentest.CastActionCases())
}

// TestActionChargesHull pins the charges of codegentest.HullActionCases, whose action expressions make list and tuple values and read
// the range that covers their elements, so that a change in how a target makes them and sets the range shows as a
// stated change of the units it charges.
func TestActionChargesHull(t *testing.T) {
	t.Parallel()
	checkActionCharges(t, "testdata/action_charges_hull.golden", "codegentest.HullActionCases", "The action expressions make no struct value that leaves a field unset.", "actionchargeshull", codegentest.HullActionCases())
}

// TestActionChargesIter pins the charges of codegentest.IterActionCases, whose action expressions iterate over lists with map, filter,
// foldl and foldr, so that a change in how a target walks a list, calls the function and keeps or drops the
// elements it makes shows as a stated change of the units it charges.
func TestActionChargesIter(t *testing.T) {
	t.Parallel()
	checkActionCharges(t, "testdata/action_charges_iter.golden", "codegentest.IterActionCases", "Some action expressions read a list field left unset.", "actionchargesiter", codegentest.IterActionCases())
}

// TestActionChargesPush pins the charges of codegentest.PushActionCases, whose action expressions push the elements of the lists and
// tuples they make, from literals, zero values, concat over lists it takes as they are and fused concat, so that a
// change in how a target makes and pushes an element shows as a stated change of the units it charges.
func TestActionChargesPush(t *testing.T) {
	t.Parallel()
	checkActionCharges(t, "testdata/action_charges_push.golden", "codegentest.PushActionCases", "Some action expressions read list and tuple fields left unset.", "actionchargespush", codegentest.PushActionCases())
}

func checkActionCharges(t *testing.T, golden, what, unset, prefix string, all []codegentest.ActionCase) {
	t.Helper()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	actionExprs := actionChargeCases(t, all)
	got := map[string]string{}
	key := func(i int, input, target string) string {
		return actionChargeKey(prefix, actionExprs, i, input, target)
	}
	// The engine.
	for i, c := range actionExprs {
		g, _ := parseTest(c.Src)
		prog, err := entry.Compile(g, entry.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, in := range c.Inputs {
			for _, b := range []struct {
				name string
				be   engine.Backend
			}{{"engine-closure", engine.Closure}, {"engine-vm", engine.Bytecode}, {"engine-iterative", engine.BytecodeIterative}} {
				var used uint64
				_, err := prog.ParseWith("main", in, engine.ParseOptions{Backend: b.be, Limits: runtime.Limits{Compute: 1 << 50},
					Trace: func(e engine.TraceEvent) { used = state.TraceParser(e).Control.Used }})
				got[key(i, in, b.name)] = fmt.Sprintf("%d %s", used, actionOutcome(fmt.Sprint(err)))
			}
		}
	}
	// Generated Go: the node parser's Parse and Recognize, and the typed parser's ParseValue.
	maps.Copy(got, goActionCharges(t, prefix, actionExprs, nil))
	// Generated TypeScript.
	for i, c := range actionExprs {
		g, _ := parseTest(c.Src)
		var ins []string
		for _, in := range c.Inputs {
			ins = append(ins, fmt.Sprintf("%q", in))
		}
		out := codegentest.RunTSGrammarScriptWith(t, g, tsChargeScript(ins), common.GenOptions{Start: "main"})
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != len(c.Inputs) {
			t.Fatalf("action %d: %d lines for %d inputs:\n%s", i, len(lines), len(c.Inputs), out)
		}
		for j, in := range c.Inputs {
			n, res, _ := strings.Cut(lines[j], " ")
			got[key(i, in, "ts-node")] = n + " " + actionOutcome(res)
		}
	}
	actionChargesGoldenFile(golden, what, unset).Check(t, prefix+"/", got)
}

// actionChargeKey is the key of the golden entry of action expression i of action expressions for the input on the target.
func actionChargeKey(prefix string, actionExprs []codegentest.ActionCase, i int, input, target string) string {
	return fmt.Sprintf("%s/action %d: %s | %s %s", prefix, i, actionExprs[i].Action, input, target)
}

// goActionCharges returns the golden entries of the generated Go targets for action expressions: the charges of the node
// parser's Parse and Recognize and of the typed parser's ParseValue, in compute units. mutate, if not nil, edits each generated parser before it
// is built; it is given the target the parser is built for, "go-node" or "go-typed".
func goActionCharges(t *testing.T, prefix string, actionExprs []codegentest.ActionCase, mutate func(target string, src []byte) []byte) map[string]string {
	t.Helper()
	cases := make([]codegentest.Case, len(actionExprs))
	for i, c := range actionExprs {
		cases[i] = c.Case
		cases[i].Name = fmt.Sprintf("action %d", i)
	}
	got := map[string]string{}
	rt := codegentest.LibCopy()
	for _, run := range []struct {
		name  string
		opts  common.GenOptions
		parse string
	}{{"go-node", common.GenOptions{}, "Parse"}, {"go-typed", common.GenOptions{Types: true}, "ParseValue"}} {
		var edit func([]byte) []byte
		if mutate != nil {
			edit = func(src []byte) []byte { return mutate(run.name, src) }
		}
		totals, problems := budgetTotalsWith(t, rt, prefix, cases, "main", edit, run.opts, run.parse)
		for _, p := range problems {
			t.Error(p)
		}
		for k, v := range totals {
			m := regexp.MustCompile(`^` + prefix + `/action (\d+) \| (\d+) (\w+)$`).FindStringSubmatch(k)
			if m == nil {
				t.Fatalf("unexpected key %q", k)
			}
			var i, j int
			fmt.Sscan(m[1], &i)
			fmt.Sscan(m[2], &j)
			f := strings.SplitN(v, " ", 3)
			target := run.name
			if m[3] == "recognize" {
				target += "-recognize"
			}
			got[actionChargeKey(prefix, actionExprs, i, actionExprs[i].Inputs[j], target)] = f[0] + " " + actionOutcome(f[2])
		}
	}
	return got
}

// tsChargeScript is a TypeScript consumer that prints, for each input, the least compute limit under which
// the parse is not interrupted, and its outcome.
func tsChargeScript(inputs []string) string {
	return `import * as p from "./parser.ts";
const inputs: string[] = [` + strings.Join(inputs, ", ") + `];
for (const s of inputs) {
  const run = (n: number) => p.parseWithOptions(s, { limits: { compute: n } });
  const limited = (n: number) => { const e = run(n).error; return e instanceof p.ParseInterruptionError && e.kind === "compute-limit"; };
  let hi = 1;
  while (limited(hi)) hi *= 2;
  let lo = 1;
  while (lo < hi) { const mid = (lo + hi) >> 1; if (limited(mid)) lo = mid + 1; else hi = mid; }
  const e = run(lo).error;
  console.log(lo + " " + JSON.stringify(e === null ? "<nil>" : String(e.message).split("\n")[0]));
}
`
}
