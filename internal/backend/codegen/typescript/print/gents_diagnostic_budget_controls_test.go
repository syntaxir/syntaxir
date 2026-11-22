package print_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

const tsDiagnosticMinimum = `function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite diagnostic budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
`

func TestGeneratedTSBudgetControlDiagnosticLineScan(t *testing.T) {
	t.Parallel()
	src := `rule main = "z" #repeat(2048) "!"

rule early = "!"`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="z".repeat(2048),ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.node===null&&ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&stable(active.error)===stable(ordinary.error)&&active.error.message===ordinary.error.message&&active.error.line===1&&active.error.col===2049,"diagnostic line scan parity");
 // A failure at the start of the input counts the units of its matching alone: preparing the input and
 // finding the line and column of the error are no events (spec/runtime/parsing.md, Compute Units).
 const failing=(text:string)=>minimum(n=>p.parseRuleWithOptions("early",text,{unit,limits:{compute:n}}).error instanceof p.SyntaxError);
 check(failing(input)===failing("z"),"diagnostic line reads count no units");
 const stopped=p.parseRuleWithOptions("early",input,{unit,limits:{compute:failing(input)-1}}).error;
 check(stopped instanceof p.ParseInterruptionError&&stopped.kind==="compute-limit","the line scan stops one unit short of its budget");
 const retry=p.parseWithOptions(input,{unit,limits:{compute:0}});check(retry.node===null&&retry.error instanceof p.SyntaxError&&retry.error.message===ordinary.error.message,"fresh line diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, common.GenOptions{Start: "main", Types: true, Recognize: true}, "")
}

func TestGeneratedTSBudgetControlDiagnosticMessage(t *testing.T) {
	t.Parallel()
	message := strings.Repeat("details λ; ", 256)
	src := fmt.Sprintf(`rule main = "z" @error(message=%q)
 rule fast = "z" @error(message="short")`, message)
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const ordinary=p.parse("x",unit),active=p.parseWithOptions("x",{unit,signal:{aborted:false}});
 check(ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&active.error.message===ordinary.error.message&&stable(active.error)===stable(ordinary.error),"long custom diagnostic parity");
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","x",{unit,limits:{compute:n}}).error instanceof p.SyntaxError);
 // The text of a diagnostic is no event: the long message counts what the short one does.
 for(const error of [p.parseWithOptions("x",{unit,limits:{compute:baseline}}).error,p.parseValueWithOptions("x",{unit,limits:{compute:baseline}}).error,p.recognizeWithOptions("x",{unit,limits:{compute:baseline}})])check(error instanceof p.SyntaxError,"custom diagnostic text counts no units");
 for(const error of [p.parseWithOptions("x",{unit,limits:{compute:baseline-1}}).error,p.parseValueWithOptions("x",{unit,limits:{compute:baseline-1}}).error,p.recognizeWithOptions("x",{unit,limits:{compute:baseline-1}})])check(error instanceof p.ParseInterruptionError&&error.kind==="compute-limit","one unit short of the failure interrupts it");
 const retry=p.parseWithOptions("x",{unit,limits:{compute:0}});check(retry.error instanceof p.SyntaxError&&retry.error.message===ordinary.error.message,"fresh custom diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, common.GenOptions{Start: "main", Types: true, Recognize: true}, "")
}

func TestGeneratedTSBudgetControlDiagnosticSort(t *testing.T) {
	t.Parallel()
	const width = 64
	alternatives := make([]string, width)
	for i := range alternatives {
		alternatives[i] = fmt.Sprintf("%q", strings.Repeat("p", 2048)+fmt.Sprintf("%02d", width-i))
	}
	choice := strings.Join(alternatives, " / ")
	src := `rule main = (` + choice + `)
 rule fast = (` + choice + `) @error(message="short")`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const ordinary=p.parse("",unit),active=p.parseWithOptions("",{unit,signal:{aborted:false}});
 check(ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&active.error.expected.length===64&&active.error.message===ordinary.error.message&&stable(active.error)===stable(ordinary.error),"long-prefix expected sorting parity");
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","",{unit,limits:{compute:n}}).error instanceof p.SyntaxError);
 // Sorting and rendering the expected labels are no events: the failure counts what matching counts.
 for(const error of [p.parseWithOptions("",{unit,limits:{compute:baseline}}).error,p.parseValueWithOptions("",{unit,limits:{compute:baseline}}).error,p.recognizeWithOptions("",{unit,limits:{compute:baseline}})])check(error instanceof p.SyntaxError,"expected-label comparisons count no units");
 check(p.parseWithOptions("",{unit,limits:{compute:0}}).error instanceof p.SyntaxError,"fresh sorted diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, common.GenOptions{Start: "main", Types: true, Recognize: true}, "")
}

func TestGeneratedTSBudgetControlRecoveredMessage(t *testing.T) {
	t.Parallel()
	message := strings.Repeat("details λ; ", 256)
	src := fmt.Sprintf(`rule main = ("z" @error(message=%q)) #recover(skip="!")
 rule fast = ("z" @error(message="short")) #recover(skip="!")`, message)
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const ordinary=p.parse("!",unit),active=p.parseWithOptions("!",{unit,signal:{aborted:false}});
 check(ordinary.node!==null&&active.node!==null&&ordinary.error instanceof p.SyntaxErrors&&active.error instanceof p.SyntaxErrors&&active.error.errors.length===1&&active.error.message===ordinary.error.message&&stable(active.node)===stable(ordinary.node),"aggregate recovered diagnostic parity");
 const retained=active.error.errors[0]!;
 // The messages of recovered errors and of their aggregate are no events: the long message counts
 // what the short one does.
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","!",{unit,limits:{compute:n}}).error instanceof p.SyntaxErrors);
 const within=p.parseWithOptions("!",{unit,limits:{compute:baseline}});check(within.node!==null&&within.error instanceof p.SyntaxErrors&&within.error.message===ordinary.error.message,"aggregate recovered text counts no units");
 const typed=p.parseValueWithOptions("!",{unit,limits:{compute:baseline-1}});check(typed.value===null&&typed.error instanceof p.ParseInterruptionError,"interrupted recovery exposes no typed result");
 const retry=p.parseWithOptions("!",{unit,limits:{compute:0}});check(retry.node!==null&&retry.error instanceof p.SyntaxErrors&&retry.error.message===ordinary.error.message&&retained.message===ordinary.error.errors[0]!.message,"retained recovery and fresh retry");
}
`
	runTypedTSFixture(t, src, consumer, common.GenOptions{Start: "main", Types: true, Recognize: true}, "")
}

func TestGeneratedTSBudgetControlDiagnosticParity(t *testing.T) {
	t.Parallel()
	for _, width := range []int{1, 2, 3, 7, 8, 9, 127, 128, 129} {
		alternatives := make([]string, width)
		for i := range alternatives {
			alternatives[i] = fmt.Sprintf("%q", []string{"é", "𐀀", "\uffff", "a"}[(width-i)%4]+fmt.Sprint(i))
		}
		src := `rule main = "é\n" (` + strings.Join(alternatives, " / ") + `)
 rule custom = "é\n" (("a" @error(message="one")) / ("b" @error(message="two")))`
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes])for(const rule of ["main","custom"]) {
 const ordinary=p.parseRule(rule,"é\n!",unit),active=p.parseRuleWithOptions(rule,"é\n!",{unit,signal:{aborted:false}});
 check(ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&ordinary.error.message===active.error.message&&stable(active.error)===stable(ordinary.error)&&active.error.line===2&&active.error.col===1,"diagnostic odd-width Unicode sort and message order parity");
 if(rule==="custom")check(stable(active.error.messages)===stable(["one","two"]),"custom messages preserve choice order");
}
`
			runTypedTSFixture(t, src, consumer, common.GenOptions{Start: "main", Types: true, Recognize: true}, "")
		})
	}
}

// On each route the diagnostic of a failure finds the line and column of the error, which counts no
// compute units: the units counted inside makeError, measured per route, are none whatever the length
// of the input, and a compute limit one unit short of the units of the whole failure interrupts it.
func TestGeneratedTSBudgetControlDiagnosticLineScanPerRoute(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = "z"+ "!"`)
	if err != nil {
		t.Fatal(err)
	}
	out := codegentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
type Hooked = { makeError(...a: unknown[]): unknown; units: number };
const proto = (p as unknown as { Parser: { prototype: Hooked } }).Parser.prototype;
const original = proto.makeError;
let scanned = -1;
proto.makeError = function (this: Hooked, ...a: unknown[]) {
  const before = this.units;
  try { return original.apply(this, a); } finally { scanned = this.units - before; }
};
const routes: [string, (text: string, n: number) => Error | null][] = [
  ["parse", (text, n) => p.parseWithOptions(text, { limits: { compute: n } }).error],
  ["parseValue", (text, n) => p.parseValueWithOptions(text, { limits: { compute: n } }).error],
  ["recognize", (text, n) => p.recognizeWithOptions(text, { limits: { compute: n } })],
];
function fail(why: string): never { throw new Error(why); }
for (const [name, run] of routes) {
  const counted = (len: number): number => {
    scanned = -1;
    if (!(run("z".repeat(len), Number.MAX_SAFE_INTEGER - 1) instanceof p.SyntaxError)) fail(name + " does not fail at the end");
    return scanned;
  };
  const short = counted(256), long = counted(2048);
  if (short !== 0 || long !== 0) fail(name + " counts " + short + " then " + long + " units for the line scan");
  const input = "z".repeat(512);
  let lo = 0, hi = 1;
  while (run(input, hi) instanceof p.ParseInterruptionError) { lo = hi; hi *= 2; }
  while (lo + 1 < hi) { const mid = Math.floor((lo + hi) / 2); if (run(input, mid) instanceof p.ParseInterruptionError) lo = mid; else hi = mid; }
  const stopped = run(input, hi - 1);
  if (!(stopped instanceof p.ParseInterruptionError) || stopped.kind !== "compute-limit") fail(name + " does not stop one unit short of its budget");
  if (hi < 512) fail(name + " needs only " + hi + " for 512 characters");
}
console.log("ok");
`, common.GenOptions{Start: "main", Types: true, Recognize: true})
	if out != "ok\n" {
		t.Fatalf("got %q", out)
	}
}
