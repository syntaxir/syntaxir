package lib

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// unitsScript compares chargeUnits(n) with n charges of one unit on controls of budget.ts, from every
// distance to a poll and to the budget, with and without a signal that aborts at the next poll.
const unitsScript = `
globalThis.Fatal = class { constructor(e) { this.err = e; } };
const { BudgetControl } = await import("./budget.ts");
function run(pre, n, budget, cancel, units) {
  let polls = 0;
  const signal = { get aborted() { polls++; return cancel && armed; }, reason: "stop" };
  let armed = false;
  const c = new BudgetControl({ signal, budget, memory: 0, profiles: [] });
  for (let i = 0; i < pre; i++) c.charge(1);
  const before = polls;
  armed = true;
  let err = "";
  try {
    if (units) c.chargeUnits(n); else for (let i = 0; i < n; i++) c.charge(1);
  } catch (x) {
    const e = x.err;
    err = e.kind + " " + e.limit + " " + e.used + " " + e.requested;
  }
  return JSON.stringify([c.budgetUsed, c.sinceCheck, polls - before, err]);
}
let bad = 0;
for (const pre of [0, 1, 100, 254, 255, 300, 511])
  for (const n of [1, 2, 255, 256, 257, 600, 1000])
    for (const budget of [0, pre + n - 1, pre + n, pre + n + 5, pre + 3]) {
      if (budget !== 0 && budget < pre) continue;
      for (const cancel of [false, true]) {
        const got = run(pre, n, budget, cancel, true), want = run(pre, n, budget, cancel, false);
        if (got !== want) { bad++; console.log("pre", pre, "n", n, "budget", budget, "cancel", cancel, "chargeUnits", got, "one-unit charges", want); }
      }
    }
const c = new BudgetControl({ signal: null, profiles: [], budget: 2 ** 40, memory: 0 });
c.chargeUnits(2 ** 32);
if (c.budgetUsed !== 2 ** 32) { bad++; console.log("used", c.budgetUsed, "after a run of 2**32 units"); }
process.exit(bad ? 1 : 0);
`

// TestChargeUnitsIsOneUnitCharges pins that BudgetControl.chargeUnits(n) of the library reserves
// what n charges of one unit do: the same units, the same distance to the next poll, the same failure
// (the unit that does not fit, after the units that do), the same polls, and an abort that the first
// poll observes. Its time grows with the polls, not with n.
func TestChargeUnitsIsOneUnitCharges(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	src, err := FS.ReadFile("budget.ts")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string]string{"budget.ts": string(src), "units.mjs": unitsScript} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, "units.mjs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
