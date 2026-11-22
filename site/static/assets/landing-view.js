// What the live example of the landing page shows for a parse response of sxir.wasm. It has no
// dependencies, so that it can be tested in Node (testdata/landing_view_test.mjs).

import { fmtTime } from "../playground/status.js";

// BELOW_RESOLUTION is the parse time when the browser's clock did not advance during the parse: browsers
// coarsen the clock that WebAssembly reads, so a short parse can measure 0 µs.
export const BELOW_RESOLUTION = "Below clock resolution";

// liveView turns a parse response into the output text, the badge ({kind, text}: kind is ok or error)
// and the measured telemetry. Nodes and depth are those of the tree the engine built; time is the
// median of the timed parses (the request asks for five), measured in the browser.
export function liveView(res) {
  const time = res.micros == null ? "–" : res.micros > 0 ? fmtTime(res.micros) : BELOW_RESOLUTION;
  if (res.compile && !res.compile.ok) {
    const output = (res.compile.diagnostics || []).map((d) => `${d.line}:${d.col}: ${d.message}`).join("\n");
    return { output, badge: { kind: "error", text: "Grammar error" }, nodes: "–", depth: "–", time: "–" };
  }
  if (!res.matched) {
    const e = res.errors?.[0];
    const output = e ? `${e.line}:${e.col}: ${e.message}` : res.error || "The input does not match the grammar.";
    return { output, badge: { kind: "error", text: e ? `Syntax error at ${e.line}:${e.col}` : "No match" }, nodes: "–", depth: "–", time };
  }
  if (res.error) {
    return { output: res.error, badge: { kind: "error", text: "Error" }, nodes: "–", depth: "–", time };
  }
  return {
    output: res.sexpr || "",
    badge: { kind: "ok", text: res.recovered ? "Recovered" : "Parsed" },
    nodes: String(res.nodes ?? 0),
    depth: String(res.depth ?? 0),
    time,
  };
}
