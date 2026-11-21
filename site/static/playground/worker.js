// Web Worker that runs sxir.wasm, so that a long parse never freezes the page. The page sends
// {type: "init", base, wasm, module?} once, then {id, method, req}; each call is answered with
// {id, out} (the JSON response) or {id, error}.

let ready = null;

// wasmBytes returns the WebAssembly binary of a response, decompressing a gzip body (the site's
// sxir-<hash>.wasm.gz); it is the function of the same name in assets/sxir-client.js, which a classic
// worker script cannot import.
async function wasmBytes(resp) {
  const data = new Uint8Array(await resp.arrayBuffer());
  if (data[0] !== 0x1f || data[1] !== 0x8b) return data;
  return new Response(new Blob([data]).stream().pipeThrough(new DecompressionStream("gzip"))).arrayBuffer();
}

async function init(msg) {
  let runtime = msg.base + "wasm_exec.js";
  const wasmURL = new URL(msg.wasm);
  if (/\/wasm\/sxir-[0-9a-f]{16}\.wasm(\.gz)?$/.test(wasmURL.pathname)) {
    const resp = await fetch(msg.base + "assets.json", { cache: "no-cache" });
    if (!resp.ok) throw new Error(`cannot load playground assets: ${resp.status}`);
    const manifest = await resp.json();
    const asset = [manifest.current, manifest.previous, manifest.tinygoCurrent, manifest.tinygoPrevious].find((a) =>
      a && new URL(a.wasm, msg.base).href === wasmURL.href);
    if (manifest.version !== 1 || !asset || !/^runtime\/wasm_exec-[0-9a-f]{16}\.js$/.test(asset.runtime)) {
      throw new Error("the requested parser is no longer available; reload this page");
    }
    runtime = new URL(asset.runtime, msg.base).href;
  }
  importScripts(runtime);
  const go = new Go();
  let instance;
  if (msg.module) {
    instance = await WebAssembly.instantiate(msg.module, go.importObject);
  } else {
    const resp = await fetch(msg.wasm);
    if (!resp.ok) throw new Error(`cannot load ${msg.wasm}: ${resp.status}`);
    instance = (await WebAssembly.instantiate(await wasmBytes(resp), go.importObject)).instance;
  }
  go.run(instance).then(() => {
    self.postMessage({ type: "exit" });
  });
  if (!self.sxir) throw new Error("sxir.wasm did not start");
  const v = JSON.parse(self.sxir.version("{}"));
  self.postMessage({ type: "ready", version: v });
}

self.onmessage = async (e) => {
  const msg = e.data;
  if (msg.type === "init") {
    ready = init(msg).catch((err) => {
      self.postMessage({ type: "failed", error: String(err && err.message ? err.message : err) });
      throw err;
    });
    return;
  }
  try {
    await ready;
    const out = self.sxir[msg.method](JSON.stringify(msg.req ?? {}));
    self.postMessage({ id: msg.id, out });
  } catch (err) {
    self.postMessage({ id: msg.id, error: String(err && err.message ? err.message : err) });
  }
};
