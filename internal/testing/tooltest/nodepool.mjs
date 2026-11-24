// The worker of the node pool (nodepool.go): it reads one job per line from its standard input, runs the
// script of the job in the directory of the job, and answers with one line of JSON. Jobs run one at a time,
// so a job owns the process, its working directory and its output while it runs.
import { createInterface } from "node:readline";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import fsModule from "node:fs";
import pathModule from "node:path";
import utilModule from "node:util";
import osModule from "node:os";

const reply = process.stdout.write.bind(process.stdout);
const realErr = process.stderr.write.bind(process.stderr);
const realExit = process.exit.bind(process);

class Exit {
  constructor(code) {
    this.code = code;
  }
}

let out = "";
let err = "";
let failure = null;
const collect = (sink) => (chunk, enc, cb) => {
  const text = typeof chunk === "string" ? chunk : Buffer.from(chunk).toString();
  if (sink === "out") out += text;
  else err += text;
  const done = typeof enc === "function" ? enc : cb;
  if (typeof done === "function") done();
  return true;
};

// An error that no job awaits (a rejected promise, a throw in a callback) fails the job that is running.
const unhandled = (e) => {
  failure ??= e;
};
process.on("uncaughtException", unhandled);
process.on("unhandledRejection", unhandled);

// The state that a script could leave to the next one: the own properties of the global object, of the
// built-in constructors and prototypes, of process and of the built-in modules a script may patch, with
// what each holds (a primitive by value, anything else by identity), the prototype and the extensibility
// of each object, and the listeners of process. The worker compares it with the state at its start after
// every job; a difference fails that job, and the worker is replaced.
const ids = new WeakMap();
let nextId = 1;
const ident = (v) => {
  if (v === null || (typeof v !== "object" && typeof v !== "function")) return typeof v === "symbol" ? String(v) : typeof v + ":" + String(v);
  if (!ids.has(v)) ids.set(v, nextId++);
  return "#" + ids.get(v);
};
const roots = () => {
  const typed = Object.getPrototypeOf(Uint8Array);
  const named = {
    globalThis, Object, Function, Array, String, Number, Boolean, Symbol, BigInt, Map, Set, WeakMap, Promise, Error,
    RegExp, Date, JSON, Math, Reflect, Intl, Uint8Array, TextEncoder, TextDecoder, URL, Buffer, typed,
    arrayIterator: Object.getPrototypeOf([][Symbol.iterator]()),
    iterator: Object.getPrototypeOf(Object.getPrototypeOf([][Symbol.iterator]())),
    generator: Object.getPrototypeOf(function* () {}),
    asyncFunction: Object.getPrototypeOf(async function () {}),
    fs: fsModule, path: pathModule, util: utilModule, os: osModule,
  };
  for (const k of ["Object", "Function", "Array", "String", "Number", "Boolean", "Symbol", "BigInt", "Map", "Set", "WeakMap", "Promise", "Error", "RegExp", "Date", "Uint8Array", "typed"]) {
    const c = named[k];
    if (typeof c === "function") named[k + ".prototype"] = c.prototype;
  }
  return named;
};
const snapshot = () => {
  const lines = [];
  for (const [name, obj] of Object.entries(roots())) {
    if (obj === undefined || obj === null) continue;
    lines.push(`${name} proto ${ident(Object.getPrototypeOf(obj))} ext ${Object.isExtensible(obj)}`);
    for (const key of Reflect.ownKeys(obj)) {
      const d = Object.getOwnPropertyDescriptor(obj, key);
      const body = "value" in d ? ident(d.value) : `get ${ident(d.get)} set ${ident(d.set)}`;
      lines.push(`${name}.${String(key)} ${d.enumerable}${d.configurable}${d.writable} ${body}`);
    }
  }
  for (const key of Reflect.ownKeys(process)) lines.push(`process.${String(key)}`);
  for (const key of Object.keys(process.env).sort()) lines.push(`env.${key}=${process.env[key]}`);
  for (const ev of process.eventNames()) lines.push(`on ${String(ev)} ${process.listenerCount(ev)}`);
  return lines;
};
const differences = (before, after) => {
  const a = new Set(before);
  const b = new Set(after);
  const out = [];
  for (const l of before) if (!b.has(l)) out.push("-" + l);
  for (const l of after) if (!a.has(l)) out.push("+" + l);
  return out;
};

const describe = (e) => (e instanceof Error ? (e.stack ?? String(e)) : String(e));

// Node defines some globals lazily, and what they set up (a symbol of undici on the global object) appears when
// they are first read, so read them all before the state that scripts must leave is taken.
for (const key of Reflect.ownKeys(globalThis)) {
  try {
    globalThis[key];
  } catch {}
}
const pristine = snapshot();

for await (const line of createInterface({ input: process.stdin })) {
  const job = JSON.parse(line);
  out = "";
  err = "";
  failure = null;
  let code = 0;
  process.stdout.write = collect("out");
  process.stderr.write = collect("err");
  process.exit = (c) => {
    throw new Exit(c ?? 0);
  };
  try {
    process.chdir(job.dir);
    process.argv = [process.argv[0], join(job.dir, job.script), ...job.args];
    await import(pathToFileURL(join(job.dir, job.script)).href);
    // The macrotasks that the script queued run before the job ends.
    await new Promise((r) => setImmediate(r));
  } catch (e) {
    if (e instanceof Exit) code = e.code;
    else failure ??= e;
  }
  if (failure instanceof Exit) {
    code = failure.code;
    failure = null;
  }
  if (failure !== null) {
    err += describe(failure) + "\n";
    code = 1;
  }
  process.stdout.write = reply;
  process.stderr.write = realErr;
  process.exit = realExit;
  const diff = differences(pristine, snapshot());
  const leak = diff.length === 0 ? "" : diff.slice(0, 20).join("\n") + (diff.length > 20 ? `\n... ${diff.length - 20} more` : "");
  reply(JSON.stringify({ id: job.id, out, err, code, leak }) + "\n");
}
