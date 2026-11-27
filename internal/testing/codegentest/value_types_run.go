package codegentest

import (
	"fmt"
	"slices"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime/value"
)

// RuleCall is a successful evaluation of a rule that a traced parse reports at its exit: the rule,
// the range of the input it matched, in bytes, and the value it produced.
type RuleCall struct {
	Rule       string
	Start, End int
	Value      *value.Node
}

// TracedParse parses input from the rule main with the limit of missing tokens missing and
// positions in bytes, reports every successful evaluation of a rule to call as it exits, the
// evaluations that the parse discards later and those it answers from a memo among them, and
// returns the result of the parse.
type TracedParse func(input string, missing int, call func(RuleCall)) (*value.Node, error)

// RuleParse parses input from the rule r with the limit of missing tokens missing and positions in
// bytes. It returns an error for a rule that cannot start a parse.
type RuleParse func(r, input string, missing int) (*value.Node, error)

// ValueTypeLimits returns the limits of missing tokens that the checks of the values of c run: none,
// and one that inserts when the grammar uses #missing.
func ValueTypeLimits(t testing.TB, c Case) []int {
	t.Helper()
	if InsertsMissing(t, c) {
		return []int{0, 8}
	}
	return []int{0}
}

// ValueTypeCheck is what EngineValueTypeMismatches observed of one case.
type ValueTypeCheck struct {
	// Calls and RuleParses count the evaluations checked in the traced parses and the successful
	// parses of rules on the inputs that the evaluations matched.
	Calls, RuleParses int
	Mismatches        []string
}

// EngineValueTypeMismatches checks the values of the rules of c on the engine (ValueTypes): on each
// backend of parses, the value of every successful evaluation that the traced parse of each input of
// c reports, and the result; and, with rule, the value of every successful parse of a rule, on the
// part of the input that an evaluation of it matched, that rule returns. A parse of a rule on such a
// part is a parse like any other, so its value has the type of the rule even where it differs from
// the value in context (an evaluation that read past its end, or under other bindings), and a parse
// that fails is no mismatch. mutate, when not nil, changes each value before the check, for negative
// controls.
func EngineValueTypeMismatches(t *testing.T, c Case, parses map[string]TracedParse, rule RuleParse, mutate func(*value.Node) *value.Node) ValueTypeCheck {
	t.Helper()
	vt := ValueTypesOf(t, c)
	var out ValueTypeCheck
	report := func(format string, args ...any) {
		if len(out.Mismatches) < 20 {
			out.Mismatches = append(out.Mismatches, c.Name+": "+fmt.Sprintf(format, args...))
		}
	}
	check := func(r string, n *value.Node) error {
		if mutate != nil {
			n = mutate(n)
		}
		return vt.CheckNode(r, n)
	}
	type span struct {
		rule, text string
		missing    int
	}
	var spans []span
	seen := map[span]bool{}
	for _, backend := range slices.Sorted(mapKeysOf(parses)) {
		parse := parses[backend]
		for _, in := range c.Inputs {
			for _, missing := range ValueTypeLimits(t, c) {
				n, err := parse(in, missing, func(call RuleCall) {
					out.Calls++
					if err := check(call.Rule, call.Value); err != nil {
						report("%s: input %q, missing tokens %d: the evaluation of %s at [%d, %d): %v", backend, in, missing, call.Rule, call.Start, call.End, err)
					}
					if s := (span{call.Rule, in[call.Start:call.End], missing}); !seen[s] {
						seen[s] = true
						spans = append(spans, s)
					}
				})
				// A parse that recovered from errors returns its value with them.
				if err == nil || n != nil {
					if err := check("main", n); err != nil {
						report("%s: input %q, missing tokens %d: the result: %v", backend, in, missing, err)
					}
				}
			}
		}
	}
	if rule != nil {
		for _, s := range spans {
			n, err := rule(s.rule, s.text, s.missing)
			if err != nil && n == nil {
				continue
			}
			out.RuleParses++
			if err := check(s.rule, n); err != nil {
				report("the parse of %s on %q, missing tokens %d: %v", s.rule, s.text, s.missing, err)
			}
		}
	}
	return out
}

func mapKeysOf[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// RecoveredCaptureCase captures a name both outside and inside an expression that #recover
// annotates: when the expression fails and is recovered, the capture inside it is not made, and the
// name keeps the value of the capture outside, so the struct field it sets is a node and never nil.
var RecoveredCaptureCase = Case{Name: "a capture before a recovered capture of the same name", Src: `type R = { X node[string] }
rule main R = x:"c" (x:"a" "b") #recover(skip="z") => R{X: $x}`, Inputs: []string{"caz", "cab", "cz", "c"}}

// ValueTypeCorpus returns the cases of the checks of the values of rules against their types: the
// typed corpus of the generated parsers, the typed grammars with #missing and #recover, the grammars
// of named node types, value results, casts, constructors, captures and Operators Rules, the
// grammars of testdata, and generated action expressions.
func ValueTypeCorpus(t *testing.T) []Case {
	t.Helper()
	cases := GeneratedTypesCases(t)
	cases = append(cases, TypedInsertionCorpus()...)
	cases = append(cases, RecoveredCaptureCase)
	cases = append(cases, TypedRecoveryCases...)
	cases = append(cases, TypedOperatorsRuleBodyCases...)
	cases = append(cases, NodeTypeCases()...)
	cases = append(cases, ValueResultCases()...)
	cases = append(cases, CastCases()...)
	cases = append(cases, ConstructorCases()...)
	cases = append(cases, CaptureAccessCases()...)
	cases = append(cases, GrammarCases(t)...)
	for _, ac := range ActionCasesWide(7, 64, true) {
		cases = append(cases, ac.Case)
	}
	return cases
}

// ValueTypeCases returns the cases of ValueTypeCorpus that a check of the values of rules runs: in
// the default tier those that witness the rows of the Go table of typed values (TypedRows.Cover),
// which convert a value of every shape of type, and the recovered capture; in the long tier all of
// them.
func ValueTypeCases(t *testing.T) []Case {
	t.Helper()
	corpus := ValueTypeCorpus(t)
	if allCases {
		return corpus
	}
	cases := GoTypedRows.Cover(t, corpus)
	if !slices.ContainsFunc(cases, func(c Case) bool { return c.Name == RecoveredCaptureCase.Name }) {
		cases = append(cases, RecoveredCaptureCase)
	}
	return cases
}
