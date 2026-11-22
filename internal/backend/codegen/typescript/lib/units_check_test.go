package lib

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// checksScript counts sized events on controls of budget.ts as the parser does (Parser.unitsSized:
// count, then poll once the count passes unitsCheck) and compares the outcome with that of counting
// each unit alone and checking after it: the same units at the interruption, the same decision at
// the end. It also checks that a signal raised during the parse is observed within one observation
// interval of the units counted after it, plus the event that crosses it, and that the end check
// observes it whatever the interval.
const checksScript = `
globalThis.Fatal = class { constructor(e) { this.err = e; } };
const { BudgetControl } = await import("./budget.ts");
function run(events, budget, raiseAt, oneByOne) {
  let raised = false;
  const signal = raiseAt < 0 ? null : { get aborted() { return raised; }, reason: "stop" };
  const c = new BudgetControl({ signal, budget, memory: 0, profiles: [] });
  let units = 0, check = c.unitsCheck(), observedAt = -1;
  const count = (n) => {
    units += n;
    if (units > check) check = c.poll(units);
  };
  try {
    for (const n of events) {
      if (!raised && raiseAt >= 0 && units >= raiseAt) raised = true, raisedAt = units;
      if (oneByOne) for (let i = 0; i < n; i++) count(1); else count(n);
    }
    c.end(units);
    return { units, err: "" };
  } catch (x) {
    const e = x.err;
    return { units, err: e.kind + " " + e.limit + " " + e.used + " " + e.requested };
  }
}
let raisedAt = -1;
let bad = 0;
const shapes = [[1, 1, 1], [3, 300, 2, 1], [255, 1, 1, 600], [256, 256, 256], [1000], [7, 500, 9, 1, 1, 40]];
for (const shape of shapes) {
  const events = [];
  for (let r = 0; r < 20; r++) events.push(...shape);
  const total = events.reduce((a, b) => a + b, 0);
  for (const budget of [0, 1, 255, 256, 257, 1000, total - 1, total, total + 1]) {
    const got = run(events, budget, -1, false), want = run(events, budget, -1, true);
    if (budget === 0 || budget >= total) {
      if (got.err !== "" || got.units !== total) { bad++; console.log("shape", shape, "budget", budget, "ends", JSON.stringify(got)); }
      continue;
    }
    // Counting by events interrupts at the event that crosses the limit, one unit by one at the unit.
    if (!got.err.startsWith("compute-limit " + budget + " ") || got.units <= budget || got.units - budget > Math.max(...shape)) {
      bad++; console.log("shape", shape, "budget", budget, "events", JSON.stringify(got), "units", JSON.stringify(want));
    }
    if (want.units !== budget + 1) { bad++; console.log("shape", shape, "budget", budget, "units", JSON.stringify(want)); }
  }
  for (const raiseAt of [0, 1, 300, 1000, total - 1]) {
    raisedAt = -1;
    const got = run(events, 0, raiseAt, false);
    if (raisedAt < 0) {
      // The signal is raised before an event only: past the start of the last there is none.
      if (got.err !== "") { bad++; console.log("shape", shape, "never raised", JSON.stringify(got)); }
      continue;
    }
    if (!got.err.startsWith("canceled") || got.units - raisedAt > 256 + Math.max(...shape)) {
      bad++; console.log("shape", shape, "raised at", raiseAt, raisedAt, JSON.stringify(got));
    }
  }
}
// The end check observes the signal whatever the interval.
{
  let raised = false;
  const c = new BudgetControl({ signal: { get aborted() { return raised; }, reason: "stop" }, budget: 0, memory: 0, profiles: [] });
  c.poll(10);
  raised = true;
  try { c.end(11); bad++; console.log("the end check does not observe the signal"); } catch (x) { if (x.err.kind !== "canceled") { bad++; console.log(x.err.kind); } }
}
process.exit(bad ? 1 : 0);
`

// TestUnitsChecksAgreeWithEachUnit pins the checks of the compute units of the library's
// BudgetControl (spec/runtime/parsing.md, Checks and Interruptions): counting sized events and
// checking after each interrupts at the event that crosses the compute limit, with the count of the
// units counted so far, and a parse within the limit ends with its exact total; a raised signal is
// observed within one observation interval, and at the end of the parse whatever the interval.
func TestUnitsChecksAgreeWithEachUnit(t *testing.T) {
	node := tooltest.RequireNode(t)
	src, err := FS.ReadFile("budget.ts")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string]string{"budget.ts": string(src), "checks.mjs": checksScript} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, "checks.mjs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
