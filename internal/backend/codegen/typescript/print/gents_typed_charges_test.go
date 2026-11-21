package print_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
)

// TestGeneratedTSTypedParseChargesWhatParseCharges checks that parseValue, which converts the Node
// result of the parse, charges the units the parse charges: the values a parse builds, in any
// representation, charge nothing. The grammars convert a struct with a list of structs, a node type,
// a list of nodes and open values.
func TestGeneratedTSTypedParseChargesWhatParseCharges(t *testing.T) {
	t.Parallel()
	grammars := []struct {
		name, src string
		inputs    []string
	}{
		{"struct list", `type Leaf = { Text string }
type Branch = { Leaves []Leaf }
rule main Branch = xs:(x:leaf)* => Branch{Leaves: map($xs, (x) => x.$x)}
rule leaf Leaf = t:"z" #atomic => Leaf{Text: text($t)}`, []string{"", "z", "zzzz", strings.Repeat("z", 512)}},
		{"node repetition", `rule main = "z"*`, []string{"", "z", "zzzz", strings.Repeat("z", 512)}},
		{"captures", `rule main = item*
rule item = k:[a-z]+ "=" v:[0-9]+ ";"`, []string{"", "a=1;", "ab=12;cd=3;", strings.Repeat("key=42;", 64)}},
	}
	for _, c := range grammars {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var in []string
			for _, s := range c.inputs {
				in = append(in, fmt.Sprintf("%q", s))
			}
			consumer := strings.ReplaceAll(tsTypedChargeConsumer, "INPUTS", "["+strings.Join(in, ",")+"]")
			runTypedTSFixture(t, c.src, consumer, common.GenOptions{Start: "main", LimitControls: true}, "")
		})
	}
}

// tsTypedChargeConsumer compares, for each input and unit, the units of parseWithOptions and
// parseValueWithOptions: the smallest compute limit under which each succeeds, and the used units
// of the interruption at limits below it.
const tsTypedChargeConsumer = `import * as p from "./parser.ts";
const inputs: string[] = INPUTS;
const report = (error: Error | null): string => {
  if (error instanceof p.ParseInterruptionError && error.kind === "compute-limit") return error.used + "/" + error.requested;
  return error === null ? "ok" : "error";
};
const units = (run: (limit: number) => string): [number, string] => {
  let hi = 16;
  while (run(hi) !== "ok") { if (hi >= 2 ** 40) return [0, ""]; hi *= 2; }
  let lo = 0;
  while (hi - lo > 1) { const mid = lo + Math.floor((hi - lo) / 2); if (run(mid) === "ok") hi = mid; else lo = mid; }
  const at: string[] = [];
  for (let l = 1; l < hi; l += 1 + Math.floor(hi / 40)) at.push(run(l));
  return [hi, at.join(",")];
};
const failures: string[] = [];
for (const unit of [p.CodePoints, p.Bytes]) {
  inputs.forEach((input, i) => {
    const [node, nodeAt] = units(l => report(p.parseWithOptions(input, { unit, limits: { compute: l } }).error));
    const [typed, typedAt] = units(l => report(p.parseValueWithOptions(input, { unit, limits: { compute: l } }).error));
    if (node !== typed || nodeAt !== typedAt) {
      failures.push("unit " + unit + " input " + i + ": parseValue charges " + typed + " units, parse " + node + "\nparse at limits below:      " + nodeAt + "\nparseValue at limits below: " + typedAt);
    }
  });
}
if (failures.length !== 0) throw new Error(failures.join("\n"));
`
