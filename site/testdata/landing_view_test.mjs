import { test } from "node:test";
import assert from "node:assert/strict";
import { BELOW_RESOLUTION, liveView, tabOfTarget, targetOfTab, targetLabel } from "../static/assets/landing-view.js";

const ok = { compile: { ok: true, diagnostics: [] } };

test("a parsed tree shows the S-expression and the measured telemetry", () => {
  const v = liveView({ ...ok, matched: true, sexpr: "(Bin 1 + 2)", nodes: 4, depth: 2, micros: 37, runs: 5 });
  assert.deepEqual(v, { output: "(Bin 1 + 2)", badge: { kind: "ok", text: "Parsed" }, nodes: "4", depth: "2", time: "37 µs" });
});

test("a syntax error shows its position and no tree telemetry", () => {
  const v = liveView({ ...ok, matched: false, errors: [{ line: 1, col: 5, message: 'expected "(", [0-9]' }], micros: 1200, runs: 1 });
  assert.equal(v.output, '1:5: syntax error: expected "(", [0-9]');
  assert.deepEqual(v.badge, { kind: "error", text: "Syntax error at 1:5" });
  assert.equal(v.nodes, "–");
  assert.equal(v.time, "1.20 ms");
});

test("a grammar error lists the diagnostics", () => {
  const v = liveView({ compile: { ok: false, diagnostics: [{ line: 2, col: 3, message: "undefined rule X" }] }, matched: false });
  assert.equal(v.output, "2:3: undefined rule X");
  assert.equal(v.badge.text, "Grammar error");
});

test("a recovered tree is marked", () => {
  assert.equal(liveView({ ...ok, matched: true, recovered: true, sexpr: "x", nodes: 1, depth: 1, micros: 2 }).badge.text, "Recovered");
});

test("targets and tabs map both ways; the roadmap has no tab", () => {
  for (const t of ["go", "ts", "vm", "wasm"]) assert.equal(targetOfTab(tabOfTarget(t)), t);
  assert.equal(tabOfTarget("wasm"), "live");
  assert.equal(tabOfTarget("any"), null);
  assert.equal(targetOfTab("cli"), null);
  assert.equal(targetLabel(null), "Showing every target");
  assert.equal(targetLabel("any"), "Showing more targets, on the roadmap");
});

test("a parse faster than the browser's clock says so instead of 0 µs", () => {
  assert.equal(liveView({ ...ok, matched: true, sexpr: "x", nodes: 1, depth: 1, micros: 0, runs: 5 }).time, BELOW_RESOLUTION);
});
