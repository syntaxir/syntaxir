package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
)

// No input is longer than 2^31−1 units in generated TypeScript, with limit controls or without:
// zero, Unlimited and any larger input limit select that bound. The inputs are proxies that know only
// their length and throw when anything else is read, so an input of 2^31 bytes fails before it is
// read, and one of 2^31−1 units passes the check and fails only when the parse reads it.
func TestGeneratedTSLongestInput(t *testing.T) {
	t.Parallel()
	consumer := `import * as p from "./parser.ts";
function check(x: unknown, why: string): asserts x { if (!x) throw new Error(why); }
const read = new Error("the input was read");
const sized = (length: number) => new Proxy(new Uint8Array(0), {
  get(_target, key) {
    if (key === "length") return length;
    throw read;
  },
}) as Uint8Array;
const outcome = (f: () => Error | null): Error | null => {
  try {
    return f();
  } catch (e) {
    if (e === read) return read;
    throw e;
  }
};
const max = 2 ** 31 - 1;
for (const unit of [p.CodePoints, p.Bytes]) {
  for (const input of [undefined, 0, p.Unlimited, max + 1, 2 ** 40]) {
    const o: p.ParseOptions = input === undefined ? { unit } : { unit, limits: { input } };
    const entries: [string, (x: Uint8Array) => Error | null][] = [
      ["parse", x => p.parseWithOptions(x, o).error],
      ["rule", x => p.parseRuleWithOptions("main", x, o).error],
      ["recognize", x => p.recognizeWithOptions(x, o)],
      ["typed", x => p.parseValueWithOptions(x, o).error],
      ["document", x => p.createDocument(x, o).error],
    ];
    for (const [name, run] of entries) {
      const what = name + " " + unit + " input " + input;
      check(outcome(() => run(sized(max))) === read, what + ": 2^31−1 units pass the check");
      if (unit !== p.Bytes) continue; // counting the code points of a longer input reads it
      const over = outcome(() => run(sized(max + 1)));
      check(over instanceof p.ParseInterruptionError && over.kind === "input-limit" && over.limit === max && over.requested === max + 1,
        what + ": 2^31 bytes: " + over);
    }
  }
}
`
	for _, controls := range []bool{true, false} {
		runTypedTSFixture(t, tsBudgetFormsGrammar, consumer, common.GenOptions{Start: "main", Types: true, Recognize: true, Document: true, NoLimitControls: !controls}, "")
	}
}
