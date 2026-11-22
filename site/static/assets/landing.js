// The landing page: the output tabs of the workbench, their link with the targets of the architecture
// diagram, and the live example, which parses the input with the README's grammar on the playground's
// WebAssembly build of the engine.

import { SXIRClient, siteRoot } from "./sxir-client.js";
import { encodeState } from "./state.js";
import { BELOW_RESOLUTION, liveView, tabOfTarget, targetOfTab, targetLabel } from "./landing-view.js";

const $ = (id) => document.getElementById(id);

// Tabs (the WAI-ARIA tabs pattern, with automatic activation).
const tabs = [...document.querySelectorAll('.wb-tabs [role="tab"]')];
const tabName = (tab) => tab.id.replace(/^tab-/, "");

function selectTab(name, { focus = false, fromDiagram = false } = {}) {
  for (const tab of tabs) {
    const on = tabName(tab) === name;
    tab.setAttribute("aria-selected", String(on));
    tab.tabIndex = on ? 0 : -1;
    $(tab.getAttribute("aria-controls")).hidden = !on;
    if (on && focus) tab.focus();
  }
  if (!fromDiagram) highlightTarget(targetOfTab(name));
  if (name === "live") startLive();
}

for (const tab of tabs) {
  tab.addEventListener("click", () => selectTab(tabName(tab)));
  tab.addEventListener("keydown", (e) => {
    const i = tabs.indexOf(tab);
    const next = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: tabs.length - 1 }[e.key];
    if (next === undefined) return;
    e.preventDefault();
    selectTab(tabName(tabs[(next + tabs.length) % tabs.length]), { focus: true });
  });
}

// The architecture diagram: a target highlights its path, and its output tab when it has one. The
// diagram is an image to assistive technology; keyboard users reach the same outputs through the tabs.
const arch = $("arch");
const archStatus = $("arch-status");

function highlightTarget(target) {
  if (!arch) return;
  arch.classList.toggle("has-target", target != null);
  for (const el of arch.querySelectorAll("[data-target]")) el.classList.toggle("is-on", el.dataset.target === target);
  for (const el of arch.querySelectorAll(".d-path")) el.classList.toggle("is-on", target != null && el.classList.contains("p-" + target));
  archStatus.textContent = targetLabel(target);
}

arch?.addEventListener("click", (e) => {
  const el = e.target.closest("[data-target]");
  if (!el) return;
  const target = el.dataset.target;
  const already = el.classList.contains("is-on");
  highlightTarget(already ? null : target);
  const tab = tabOfTarget(target);
  if (!already && tab) selectTab(tab, { fromDiagram: true });
});

// The live example. The parser (about 2 MB compressed) loads only when the example is about to be
// seen or is used.
const bench = $("workbench");
const input = $("live-input");
const output = $("live-output");
const badge = $("live-badge");
const fallback = $("live-fallback");
const open = $("live-open");
const grammar = bench?.dataset.grammar ?? "";
let client = null;
let started = false;
let timer = 0;
let seq = 0;
let linkVersion = 0;

function setBadge(kind, text) {
  badge.textContent = text;
  badge.className = "wb-badge" + (kind ? " is-" + kind : "");
}

function setStats(v) {
  $("live-nodes").textContent = v.nodes;
  $("live-depth").textContent = v.depth;
  $("live-time").textContent = v.time;
  $("live-time").classList.toggle("is-note", v.time === BELOW_RESOLUTION);
}

async function run() {
  const n = ++seq;
  client ??= new SXIRClient(siteRoot() + "playground/");
  try {
    // Five timed parses: the page labels the parse time as their median.
    const res = await client.call("parse", { grammar, input: input.value, runs: 5 }, { key: "parse" });
    if (n !== seq) return;
    const v = liveView(res);
    fallback.hidden = true;
    output.hidden = false;
    output.textContent = v.output;
    setBadge(v.badge.kind, v.badge.text);
    setStats(v);
  } catch (err) {
    if (n !== seq || err?.name === "SupersededError") return;
    console.warn("sxir: the live example is unavailable:", err);
    output.hidden = true;
    fallback.hidden = false;
    setBadge("error", "Parser unavailable");
    setStats({ nodes: "–", depth: "–", time: "–" });
  }
}

async function updateLink() {
  const version = ++linkVersion;
  const text = input.value;
  try {
    const hash = await encodeState({ g: grammar, i: text });
    if (version === linkVersion && input.value === text) open.href = siteRoot() + "playground/#" + hash;
  } catch (err) {
    if (version === linkVersion) console.warn("sxir: cannot update the playground link:", err);
  }
}

function startLive() {
  if (started || !input) return;
  started = true;
  setBadge("", "Loading the parser…");
  run();
  updateLink();
}

if (input) {
  input.addEventListener("input", () => {
    startLive();
    clearTimeout(timer);
    timer = setTimeout(run, 120);
    updateLink();
  });
  input.addEventListener("focus", startLive);
  for (const b of document.querySelectorAll("[data-preset]")) {
    b.addEventListener("click", () => {
      input.value = b.dataset.preset;
      if (!started) startLive();
      else run();
      updateLink();
    });
  }
  $("live-retry").addEventListener("click", () => {
    // The client starts a new worker, compiling the module again, on the call after a failed start.
    setBadge("", "Loading the parser…");
    run();
  });
  if ("IntersectionObserver" in window) {
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        io.disconnect();
        startLive();
      }
    }, { rootMargin: "200px" });
    io.observe(bench);
  } else {
    startLive();
  }
}
