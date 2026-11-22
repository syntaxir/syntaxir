package print_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
)

func TestGeneratedTSBudgetControlFields(t *testing.T) {
	t.Parallel()
	const width = 48
	names := make([]string, width)
	declarations := make([]string, width)
	values := make([]string, width)
	captures := make([]string, width)
	for i := range names {
		names[i] = "Field" + strings.Repeat("a", 200) + fmt.Sprintf("%02d", i)
		declarations[i] = names[i] + " int"
		values[i] = names[i] + ":1"
		captures[i] = "capture" + strings.Repeat("a", 200) + fmt.Sprintf("%02d", i) + `:"z"`
	}
	src := `type Box = { ` + strings.Join(declarations, ";") + ` }
 rule main Box = x:item {$x.` + names[width-1] + ` == 1} => $x
 rule item Box = "z" => Box{` + strings.Join(values, ",") + `}
 rule fast = "z"`
	// Building a struct, reading its member and converting it count no units of their own, so each
	// route counts the units of the engine's.
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsUnitsOf + `
const item=` + tsEngineUnits(t, src, "item", "z", false) + `,main=` + tsEngineUnits(t, src, "main", "z", false) + `,recognized=` + tsEngineUnits(t, src, "main", "z", true) + `;
function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite field budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
for(const unit of [p.CodePoints,p.Bytes]) {
 const normal=p.parse("z",unit),typed=p.parseValue("z",unit);
 const active=p.parseWithOptions("z",{unit,signal:{aborted:false}}),converted=p.parseValueWithOptions("z",{unit,signal:{aborted:false}});
 check(normal.error===null&&typed.error===null&&active.error===null&&converted.error===null&&stable(active.node)===stable(normal.node)&&stable(converted.value)===stable(typed.value),"field construction, lookup and typed conversion parity");
 check(unitsOf(unit,o=>p.parseRuleWithOptions("item","z",o))===item.get(unit),"field construction counts the engine's units");
 check(unitsOf(unit,o=>p.parseWithOptions("z",o))===main.get(unit)&&unitsOf(unit,o=>p.parseValueWithOptions("z",o))===main.get(unit),"member lookup counts the engine's units");
 check(unitsOf(unit,o=>p.recognizeWithOptions("z",o))===recognized.get(unit),"field-dependent recognition counts the engine's units");
 const parseBudget=minimum(n=>p.parseWithOptions("z",{unit,limits:{compute:n}}).error===null);
 check(p.parseWithOptions("z",{unit,limits:{compute:parseBudget+1024}}).error===null,"same budget finishes parse");
 const conversion=p.parseValueWithOptions("z",{unit,limits:{compute:parseBudget}});
 check(conversion.value!==null&&conversion.error===null,"typed conversion charges nothing beyond the parse");
 const short=p.parseValueWithOptions("z",{unit,limits:{compute:parseBudget-1}});
 check(short.value===null&&short.error instanceof p.ParseInterruptionError&&short.error.kind==="compute-limit","typed parse needs the units of the parse");
 let polls=0;check(p.parseWithOptions("z",{unit,signal:{get aborted(){polls++;return false}}}).error===null,"observe parse checkpoints");
 const cancelAt=polls+1;polls=0;
 const canceled=p.parseValueWithOptions("z",{unit,signal:{get aborted(){return ++polls>=cancelAt},reason:"fields"}});
 // Converting the small result takes fewer steps than a poll interval, so it reaches no poll.
 check(canceled.value!==null&&canceled.error===null,"typed conversion polls only every 256 steps");
 check(p.parseValueWithOptions("z",{unit,limits:{compute:0}}).error===null,"fresh retry after field interruption");
}
`
	runTypedTSFixture(t, src, consumer, common.GenOptions{Start: "main", Types: true, Recognize: true}, "")

	t.Run("capture-attachment", func(t *testing.T) {
		grammar := `rule main = ` + strings.Join(captures, " ") + `
rule fast = "z" #repeat(48)`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="z".repeat(48),ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.error===null&&active.error===null&&stable(active.node)===stable(ordinary.node)&&active.node?.fields.length===48,"capture attachment parity");
 check(p.parseRuleWithOptions("fast",input,{unit,limits:{compute:4096}}).error===null,"matching-only capture baseline fits");
 // Attaching the captures as fields charges nothing (captures are not an item of the cost), so the
 // parse fits the budget of matching alone.
 const attached=p.parseWithOptions(input,{unit,limits:{compute:4096}});
 check(attached.error===null&&stable(attached.node)===stable(ordinary.node),"capture attachment charges nothing");
 check(p.parseWithOptions(input,{unit,limits:{compute:0}}).error===null,"capture attachment fresh retry");
}
`
		runTypedTSFixture(t, grammar, consumer, common.GenOptions{Start: "main", Types: true}, "")
	})
}
