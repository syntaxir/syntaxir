package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
)

// TestGeneratedTSTypedRecoveredCaptures checks that the typed TypeScript API types a capture inside
// a recovered expression as optional, as the typed Go API does (*Word): a field declared *Word is
// typed exactly Word | null at compile time, which is also how a plain node field is spelled, so the
// two kinds of field agree in TypeScript. A parse that fails the capture gives null, while a name
// captured after the recovery stays set.
func TestGeneratedTSTypedRecoveredCaptures(t *testing.T) {
	t.Parallel()
	src := `type Word = { Text string }
type R = { V *Word; W Word }
rule main R = (v:letter "b") #recover(skip="z") w:letter => R{V: $v, W: $w}
rule letter Word = t:([ac]) => Word{Text: text($t)}`
	consumer := `import { parseValue } from "./parser.ts";
import type { R, Word } from "./types.ts";
function check(x: unknown, why: string): asserts x { if (!x) throw new Error(why); }
type Equals<A, B> = (<T>() => T extends A ? 1 : 2) extends (<T>() => T extends B ? 1 : 2) ? true : false;
const optionalAsGo: Equals<R["V"], Word | null> = true;
check(optionalAsGo, "a *Word capture is typed Word | null");
const ok = parseValue("abc");
check(ok.error === null && ok.value !== null, "parse with the capture made");
check(ok.value?.V?.Text === "a" && ok.value?.W?.Text === "c", "captures made");
const rec = parseValue("zc");
check(rec.value !== null && rec.error !== null, "a recovered parse keeps its value and reports the recovery");
check(rec.value?.V === null && rec.value?.W?.Text === "c", "recovered capture is null, the name after it is set");
`
	runTypedTSFixture(t, src, consumer, common.GenOptions{}, "")
}
