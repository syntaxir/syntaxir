package print_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// insertionLimits are the missing-token limits of the comparisons of parses from scratch: none, which
// runs the parser's tables and inserts nothing, one that some inputs exceed, and one that none does.
var insertionLimits = []int{0, 2, 8}

// insertionCases are the grammars with #missing of the comparisons of parses from scratch: the typed
// insertion cases, the insertion cases and the shared insertion corpus.
func insertionCases() []codegentest.Case {
	return append(append(codegentest.TypedInsertionCases(), codegentest.InsertionCases()...), codegentest.InsertionCorpus()...)
}

// TestGeneratedTSInsertMissingTokens checks that the generated TypeScript parsers with limit controls
// insert missing tokens as the engine does: the trees, the errors, the recognitions and the compute
// units of the node and the typed parses with a limit of missing tokens, which run the insertion
// tables, and with no limit, which run the parser's tables and insert nothing.
func TestGeneratedTSInsertMissingTokens(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node := tooltest.RequireNode(t)
	for _, limit := range insertionLimits {
		for _, m := range codegentest.TSParsersInsertionMismatches(t, node, insertionCases(), common.GenOptions{}, limit, nil) {
			t.Errorf("limit %d: %s", limit, m)
		}
	}
}

// TestGeneratedTSInsertMissingTokensOrdinary is TestGeneratedTSInsertMissingTokens for the parsers
// without limit controls, which count no units: at a limit that some inputs exceed, and in the long
// tier at every limit.
func TestGeneratedTSInsertMissingTokensOrdinary(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node := tooltest.RequireNode(t)
	limits := insertionLimits[1:2]
	if codegentest.Long() {
		limits = insertionLimits
	}
	for _, limit := range limits {
		for _, m := range codegentest.TSParsersInsertionMismatches(t, node, insertionCases(), common.GenOptions{NoLimitControls: true}, limit, nil) {
			t.Errorf("limit %d: %s", limit, m)
		}
	}
}

// TestGeneratedTSTypedInsertMissingTokens checks that the typed TypeScript parsers convert the missing
// values of the insertions as the Go parsers do: for every rule of the typed insertion cases, at the
// limits of missing tokens that insert nothing, some and all, the values of parseRuleValueWithOptions,
// with their Missing and ErrorValue values, the errors and the compute units equal those of the Go
// ParseRuleValue.
func TestGeneratedTSTypedInsertMissingTokens(t *testing.T) {
	t.Parallel()
	for _, limit := range insertionLimits {
		t.Run(fmt.Sprint("limit ", limit), func(t *testing.T) {
			t.Parallel()
			checkRuleValue(t, codegentest.TypedInsertionCases(), limit)
		})
	}
}

// streamInsertionLimits are the missing-token limits of the stream comparisons: none, which runs the
// parser's tables, one and two, which some inputs reach, and one that none reaches.
var streamInsertionLimits = []int{0, 1, 2, 64}

// streamedInsertionCase returns the grammar src of an insertion case as a stream grammar whose start
// rule main repeats the case's start rule, renamed element, so that an element that inserts follows
// one that does not and the limit spans elements.
func streamedInsertionCase(src string) string {
	src = regexp.MustCompile(`\bmain\b`).ReplaceAllString(strings.ReplaceAll(src, "@start\n", ""), "element")
	return "@start\nrule main = element* #stream\n" + src
}

// TestStreamOracleInsertMissingTokens checks that a TypeScript stream with a missing-token limit
// inserts what the engine's stream parse does, on the incremental insertion corpus, in every chunking
// and in both units: the same elements delivered, the same error and the same compute units. It runs
// the limits 0 and 2, and the second observable too; the long tier runs every limit, observable and
// not.
func TestStreamOracleInsertMissingTokens(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	runs := []streamOptions{{Missing: 0}, {Missing: 2}, {Missing: 2, Observable: true}}
	if codegentest.Long() {
		runs = nil
		for _, limit := range streamInsertionLimits {
			runs = append(runs, streamOptions{Missing: limit}, streamOptions{Missing: limit, Observable: true})
		}
	}
	for _, c := range codegentest.IncrementalInsertionCorpus() {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			for _, d := range runs {
				for _, m := range streamOracleMismatches(t, streamedInsertionCase(c.Src), c.Inputs, d, nil) {
					t.Error(m)
				}
			}
		})
	}
}

// missingConsumerGrammar is a typed grammar with #missing: a missing value in a field of type missing,
// one of type error, and joins with a missing member and without one.
const missingConsumerGrammar = `type Tok ~ node[string] | missing
type Out = { Items []Tok; End missing; Err error }
@start
rule main Out = xs:item* e:end r:(x:"!" #missing => $x.(error)) $$ => Out{Items: $xs.Value, End: $e, Err: $r}
rule item Tok = "a" x:";" #missing => $x
rule end missing = m:"." #missing => $m.(missing)`

// TestGeneratedTSMissingTokensConsumer compiles a consumer of a parser of a grammar with #missing under
// the strictest checks and runs it: the option missingTokens of each entry point, a Document and a
// stream, MissingTokenLimitError with its limit and position, the rejected limits, and the types of
// the typed values: Missing for missing, ErrorValue for error, narrowed by their type, and the joins
// with RecoveredError, Missing and null.
func TestGeneratedTSMissingTokensConsumer(t *testing.T) {
	t.Parallel()
	runTSSplit(t, strings.Replace(missingConsumerGrammar, "rule main Out = xs:item*", "rule main Out = xs:item* #stream", 1), common.GenOptions{Start: "main", Types: true, Recognize: true, Document: true, Stream: true}, `import {parseWithOptions, parseValueWithOptions, recognizeWithOptions, createDocument, createStream, MissingTokenLimitError, SyntaxErrors, marshal} from "./parser.ts";
import type {Missing, ErrorValue, RecoveredError, Tok, Out} from "./parser.ts";
declare const process: {stdout: {write(s: string): void}};
const check = (ok: boolean, what: string): void => { if (!ok) throw new Error(what); };
// No limit, the default, inserts nothing.
check(parseWithOptions("a;a").node === null, "a parse without a limit inserted");
// A limit inserts, and the result has the syntax errors of the insertions.
const r = parseValueWithOptions("a;a", {missingTokens: 3});
check(r.error instanceof SyntaxErrors && r.value !== null, "insertions: " + r.error?.message);
const out: Out = r.value!;
const items: readonly Tok[] = out.Items ?? [];
const last: Tok = items[1] ?? null;
check(last !== null && last.type === "missing" && last.expected === ";" && last.start === 3 && last.end === 3, "the inserted item: " + JSON.stringify(last));
const end: Missing | null = out.End;
check(end !== null && end.expected === "." && end.message === '1:4: syntax error: expected "."', "the inserted end: " + JSON.stringify(end));
const err: ErrorValue | null = out.Err;
if (err !== null && err.type === "missing") {
  const m: Missing = err;
  check(m.expected === "!", "the inserted error value");
} else {
  const e: RecoveredError | null = err;
  check(false, "the error value is no Missing: " + JSON.stringify(e));
}
// The node of an insertion marshals with the literal expected.
check(marshal(parseWithOptions("a", {missingTokens: 3}).node).includes('"expected":";"'), "the marshalled missing node");
// A parse that needs more insertions than the limit fails with MissingTokenLimitError.
const over = parseWithOptions("aaa", {missingTokens: 1});
check(over.node === null && over.error instanceof MissingTokenLimitError, "past the limit: " + over.error?.message);
const limit = over.error as MissingTokenLimitError;
check(limit.limit === 1 && limit.position === 2 && limit.message === "missing token insertion limit 1 exceeded at position 2", "the limit error: " + limit.message);
check(recognizeWithOptions("aaa", {missingTokens: 1}) instanceof MissingTokenLimitError, "recognize past the limit");
check(recognizeWithOptions("a;a", {missingTokens: 3}) instanceof SyntaxErrors, "recognize with insertions");
check(String(parseWithOptions("a", {missingTokens: -1}).error).includes("missing token limit must be nonnegative"), "a negative limit");
check(String(parseWithOptions("a", {missingTokens: 1.5}).error).includes("missing token limit must be nonnegative"), "a fractional limit");
// A Document fixes the limit at its creation; its parses cannot change it.
const made = createDocument("a", {missingTokens: 3});
check(made.document !== null, "createDocument: " + made.error?.message);
const d = made.document!;
check(d.parseValue().value?.Items?.[0]?.type === "missing", "the Document's insertion");
check(String(d.parse({missingTokens: 0}).error).includes("cannot change the missing-token limit"), "a Document parse changed the limit");
// A stream has one limit for all of its elements.
const s = createStream({missingTokens: 2}).stream!;
const fed = s.feed(Uint8Array.of(0x61, 0x61, 0x61), true);
check(fed.state === "failed" && fed.error instanceof MissingTokenLimitError && fed.elements.length === 2, "the stream's limit: " + fed.state + " " + fed.error?.message);
process.stdout.write("ok\n");
`)
}

// TestGeneratedTSMissingTokensTypedDeclarations checks the declarations of types.ts: a grammar with
// #missing declares Missing and ErrorValue, the type of error, and reserves the name ErrorValue; a
// grammar without it keeps RecoveredError for error and leaves the name to the grammar. A grammar that
// only names the type missing has the declarations, and its parser takes a limit.
func TestGeneratedTSMissingTokensTypedDeclarations(t *testing.T) {
	t.Parallel()
	const recovered = "type S = { E error }\n@start\nrule main S = s:(\"a\" #recover(skip=[^;]*)) \";\" $$ => S{E: $s.(error)}\n"
	inserted := strings.Replace(recovered, `";"`, `";" #missing`, 1)
	types := func(src string) (string, error) {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		files, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Types: true})
		if err != nil {
			return "", err
		}
		for _, f := range files {
			if f.Path == "types.ts" {
				return string(f.Content), nil
			}
		}
		t.Fatalf("no types.ts in %d files", len(files))
		return "", nil
	}
	code, err := types(inserted)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\nexport interface Missing extends Span {\n  readonly type: \"missing\";\n  readonly expected: string;\n  readonly message: string;\n}\n", "\nexport type ErrorValue = RecoveredError | Missing;\n", "  readonly \"E\": ErrorValue | null;\n"} {
		if !strings.Contains(code, want) {
			t.Errorf("the types of a grammar with #missing lack %q:\n%s", want, code)
		}
	}
	code, err = types(recovered)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, "Missing") || strings.Contains(code, "ErrorValue") || !strings.Contains(code, "  readonly \"E\": RecoveredError | null;\n") {
		t.Errorf("the types of a grammar without #missing:\n%s", code)
	}
	const decl = "type ErrorValue = { F node[string] }\n"
	if _, err := types(decl + inserted); err == nil || !strings.Contains(err.Error(), "ErrorValue") || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("ErrorValue in a grammar with #missing: %v", err)
	}
	if _, err := types(decl + recovered); err != nil {
		t.Errorf("ErrorValue in a grammar without #missing: %v", err)
	}
	code, err = types("type Item ~ node[string] | missing\n@start\nrule main Item = x:\"a\" => $x\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(code, "\nexport type ErrorValue = RecoveredError | Missing;\n") || !strings.Contains(code, "export type Item = RecoveredError | Missing | null | StringNode;\n") {
		t.Errorf("the types of a grammar that names missing:\n%s", code)
	}
}

// TestGeneratedTSMissingTokensLimitNeedsMissing checks that only a parser of a grammar with #missing,
// or one that names the type missing, takes a positive limit of missing tokens; every parser takes 0.
func TestGeneratedTSMissingTokensLimitNeedsMissing(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		src  string
		want string
	}{
		{"@start\nrule main = \"a\"\n", "the grammar of this parser has no #missing: a positive missing token limit needs a grammar that uses #missing"},
		{"type Item ~ node[string] | missing\n@start\nrule main Item = x:\"a\" => $x\n", "null"},
	} {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		out := codegentest.RunTSGrammarScript(t, g, `import {parseWithOptions} from "./parser.ts";
console.log(parseWithOptions("a", {missingTokens: 0}).error === null, String(parseWithOptions("a", {missingTokens: 1}).error?.message ?? null));
`)
		if want := "true " + c.want + "\n"; out != want {
			t.Errorf("%q: %q, want %q", c.src, out, want)
		}
	}
}
