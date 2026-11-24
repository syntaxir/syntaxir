// The worker of the node pool (nodepool.go): it reads one job per line from its standard input, runs the
// script of the job in the directory of the job, and answers with one line of JSON. Jobs run one at a time,
// so a job owns the process, its working directory and its output while it runs.
import { createInterface } from "node:readline";
import { join as join_ } from "node:path";
import { pathToFileURL } from "node:url";
import fsModule from "node:fs";
import pathModule from "node:path";
import utilModule from "node:util";
import osModule from "node:os";
import eventsModule from "node:events";
import streamModule from "node:stream";
import bufferModule from "node:buffer";
import { createHook } from "node:async_hooks";

const realArgv0 = process.argv[0];
// The built-ins that the worker itself uses are taken here, before any script runs, so that a script that
// replaces one of them (Array.prototype.push, Reflect.ownKeys, JSON.stringify, console.log...) cannot blind the
// detector or the protocol: the code below that runs after a script uses these and plain loops, no spread,
// destructuring or for-of (which read Array.prototype[Symbol.iterator]).
const uncurry = Function.prototype.bind.bind(Function.prototype.call);
const push = uncurry(Array.prototype.push);
const join = uncurry(Array.prototype.join);
const slice = uncurry(Array.prototype.slice);
const startsWith = uncurry(String.prototype.startsWith);
const mapGet = uncurry(Map.prototype.get);
const mapSet = uncurry(Map.prototype.set);
const mapHas = uncurry(Map.prototype.has);
const mapDelete = uncurry(Map.prototype.delete);
const weakGet = uncurry(WeakMap.prototype.get);
const weakSet = uncurry(WeakMap.prototype.set);
const weakHas = uncurry(WeakMap.prototype.has);
const bufferToString = uncurry(Buffer.prototype.toString);
const ownKeys = Reflect.ownKeys;
const getDescriptor = Object.getOwnPropertyDescriptor;
const getPrototype = Object.getPrototypeOf;
const isExtensible = Object.isExtensible;
const isFrozen = Object.isFrozen;
const bufferFrom = Buffer.from;
const stringify = JSON.stringify;
const parse = JSON.parse;
const now = Date.now;
const StringOf = String;
const NumberOf = Number;
const MapOf = Map;
const WeakMapOf = WeakMap;
const PromiseOf = Promise;
const setTimer = setTimeout;
const setNow = setImmediate;
const clearTimer = clearTimeout;
const getActive = process.getActiveResourcesInfo.bind(process);
const umask = process.umask.bind(process);
const hasCapture = process.hasUncaughtExceptionCaptureCallback.bind(process);
const chdir = process.chdir.bind(process);
const processEventNames = process.eventNames.bind(process);
const processListenerCount = process.listenerCount.bind(process);
const processOn = process.on.bind(process);

const reply = process.stdout.write.bind(process.stdout);
const origExit = process.exit;
const origOutWrite = process.stdout.write;
const origErrWrite = process.stderr.write;

class Exit {
  constructor(code) {
    this.code = code;
  }
}

let out = "";
let err = "";
let failure = null;
// The first request to exit decides the exit code of the script, whether the script caught the Exit that
// process.exit throws or not, and the output written after it is dropped, as the process would have ended.
let exitRequest = null;
const collect = (sink) => (chunk, enc, cb) => {
  if (exitRequest === null) {
    const text = typeof chunk === "string" ? chunk : bufferToString(bufferFrom(chunk));
    if (sink === "out") out += text;
    else err += text;
  }
  const done = typeof enc === "function" ? enc : cb;
  if (typeof done === "function") done();
  return true;
};

// An error that no job awaits (a rejected promise, a throw in a callback) fails the job that is running.
const unhandled = (e) => {
  if (!(e instanceof Exit)) failure ??= e;
};
processOn("uncaughtException", unhandled);
processOn("unhandledRejection", unhandled);

// The state that a script could leave to the next one: everything that can be reached from the global
// object, the modules fs, path, util, os, events, stream and buffer, process and the intrinsics that no global names (the iterator
// and generator prototypes), through own properties and prototypes, with what each property holds (a
// primitive by value, anything else by identity, an accessor by the identity of its functions), the
// prototype and extensibility of each object, the values of process that are read through accessors, the
// environment and the listeners of process. The worker compares it with the state at its start after every
// job; a difference fails that job, and the worker is replaced. Getters are never called.
const ids = new WeakMapOf();
let nextId = 1;
const ident = (v) => {
  if (v === null || (typeof v !== "object" && typeof v !== "function")) return typeof v === "symbol" ? StringOf(v) : typeof v + ":" + StringOf(v);
  if (!weakHas(ids, v)) weakSet(ids, v, nextId++);
  return "#" + weakGet(ids, v);
};
const extraRoots = () => {
  const roots = [];
  const add = (name, v) => push(roots, [name, v]);
  add("fs", fsModule);
  add("path", pathModule);
  add("util", utilModule);
  add("os", osModule);
  add("events", eventsModule);
  add("stream", streamModule);
  add("buffer", bufferModule);
  const arrayIterator = getPrototype([][Symbol.iterator]());
  add("%ArrayIterator%", arrayIterator);
  add("%Iterator%", getPrototype(arrayIterator));
  add("%MapIterator%", getPrototype(new MapOf()[Symbol.iterator]()));
  add("%SetIterator%", getPrototype(new Set()[Symbol.iterator]()));
  add("%StringIterator%", getPrototype(""[Symbol.iterator]()));
  add("%RegExpStringIterator%", getPrototype(/a/[Symbol.matchAll]("a")));
  add("%GeneratorFunction%", getPrototype(function* () {}));
  add("%AsyncFunction%", getPrototype(async function () {}));
  add("%AsyncGeneratorFunction%", getPrototype(async function* () {}));
  add("%AsyncIterator%", getPrototype(getPrototype(getPrototype(async function* () {}).prototype)));
  return roots;
};
// The values of process that only accessors and methods give.
const processState = (lines) => {
  push(lines, ["process.title", StringOf(process.title)]);
  push(lines, ["process.exitCode", StringOf(process.exitCode)]);
  push(lines, ["process.sourceMapsEnabled", StringOf(process.sourceMapsEnabled)]);
  push(lines, ["process.hasUncaughtExceptionCaptureCallback", StringOf(hasCapture())]);
  // The default of every emitter is held in an accessor of the module.
  push(lines, ["events.defaultMaxListeners", StringOf(eventsModule.defaultMaxListeners)]);
  push(lines, ["events.captureRejections", StringOf(eventsModule.captureRejections)]);
  const mask = umask(0);
  umask(mask);
  push(lines, ["process.umask", StringOf(mask)]);
  const names = processEventNames();
  for (let i = 0; i < names.length; i++) push(lines, ["on " + StringOf(names[i]), StringOf(processListenerCount(names[i]))]);
};
const snapshot = () => {
  const state = new MapOf();
  const seen = new WeakMapOf();
  const queue = [];
  const visit = (name, obj) => {
    if (obj === null || (typeof obj !== "object" && typeof obj !== "function") || weakHas(seen, obj)) return;
    weakSet(seen, obj, true);
    push(queue, [name, obj]);
  };
  visit("globalThis", globalThis);
  visit("process", process);
  let q = 0;
  // The roots that no global names come after what the globals reach, so that an object is named by its
  // path from the global object where it has one.
  const drain = () => {
    for (; q < queue.length; q++) {
      const name = queue[q][0];
      const obj = queue[q][1];
      const proto = getPrototype(obj);
      mapSet(state, name + " #proto", ident(proto) + " ext " + isExtensible(obj) + " frozen " + isFrozen(obj));
      const keys = ownKeys(obj);
      for (let k = 0; k < keys.length; k++) {
        const key = keys[k];
        const path = name + "." + StringOf(key);
        // The arguments are set for each script; the list of loaded internal modules grows as node loads them.
        if (startsWith(path, "process.argv") || startsWith(path, "process.moduleLoadList")) continue;
        const d = getDescriptor(obj, key);
        if (d === undefined) continue;
        let body;
        if ("value" in d) {
          body = ident(d.value);
          visit(path, d.value);
        } else {
          body = "get " + ident(d.get) + " set " + ident(d.set);
          visit(path + "<get>", d.get);
          visit(path + "<set>", d.set);
        }
        mapSet(state, path, (d.enumerable ? "e" : "-") + (d.configurable ? "c" : "-") + (d.writable ? "w" : "-") + " " + body);
      }
      // The prototype comes after the own properties, so that an object is named by the shortest path of own properties.
      visit(name + ".__proto__", proto);
    }
  };
  drain();
  const extras = extraRoots();
  for (let i = 0; i < extras.length; i++) visit(extras[i][0], extras[i][1]);
  drain();
  const lines = [];
  processState(lines);
  for (let i = 0; i < lines.length; i++) mapSet(state, lines[i][0], lines[i][1]);
  return state;
};
const differences = (before, after) => {
  const result = [];
  const keys = [];
  before.forEach((_, k) => push(keys, k));
  after.forEach((_, k) => push(keys, k));
  for (let i = 0; i < keys.length; i++) {
    const k = keys[i];
    const a = mapGet(before, k);
    const b = mapGet(after, k);
    if (a === b) continue;
    if (!mapHas(before, k)) push(result, "+" + k + " " + b);
    else if (!mapHas(after, k)) push(result, "-" + k + " " + a);
    else push(result, "~" + k + " " + a + " -> " + b);
  }
  return result;
};

const describe = (e) => (e instanceof Error ? (e.stack ?? StringOf(e)) : StringOf(e));

// Node defines some globals lazily, and what they set up (a symbol of undici on the global object) appears when
// they are first read, so read them all before the state that scripts must leave is taken.
const globalKeys = ownKeys(globalThis);
for (let i = 0; i < globalKeys.length; i++) {
  try {
    globalThis[globalKeys[i]];
  } catch {}
}
const pristine = snapshot();

const resourceCounts = () => {
  const counts = new MapOf();
  const list = getActive();
  for (let i = 0; i < list.length; i++) mapSet(counts, list[i], (mapHas(counts, list[i]) ? mapGet(counts, list[i]) : 0) + 1);
  return counts;
};
// The resources (timers, handles, requests) that the script left running beyond those it found.
const leftover = (before) => {
  const after = resourceCounts();
  const names = [];
  after.forEach((n, k) => {
    if (n > (mapHas(before, k) ? mapGet(before, k) : 0)) push(names, k);
  });
  return names;
};

// How often the worker looks, while a script's import is pending, whether anything can still finish it.
const idleCheckMs = 20;
// The asynchronous resources that a job started and that have not been destroyed, other than promises and the
// kinds that process.getActiveResourcesInfo lists (timers and immediates). Work that runs on node's thread pool
// (zlib, crypto's pbkdf2, randomBytes and subtle.digest, file system requests, getaddrinfo) is not in that list
// while it is pending, so a script that awaits it would look like one that waits for nothing; here it is tracked
// from the init of its resource to its destroy, which node emits when the request is done. The hook is enabled
// only while a job runs. Its init is called for every promise too, so it returns at once for them; a microbenchmark of
// 2 million awaits measured 0.13 s without a hook and 0.9 s with one (about 0.4 us per await), a cost that the
// scripts of the pool, which await thousands of times, do not feel.
const pending = new MapOf();
let pendingCount = 0;
const untracked = (type) => type === "PROMISE" || type === "TickObject" || type === "Microtask" || type === "Timeout" || type === "Immediate" || type === "TIMERWRAP";
const asyncHook = createHook({
  init(id, type) {
    if (untracked(type) || mapHas(pending, id)) return;
    mapSet(pending, id, type);
    pendingCount++;
  },
  destroy(id) {
    if (mapHas(pending, id)) {
      mapDelete(pending, id);
      pendingCount--;
    }
  },
});
// watchIdle reports to the pool, as soon as it is certain, a job whose import is pending and that waits for
// nothing: a top-level await of a promise that nothing can settle (node itself ends such a script with exit code
// 13 "unsettled top-level await"). The pool then fails the job at once, whatever the load does to the time.
//
// "Waits for nothing" is exact for everything that can run a callback: nothing on the event loop but what was there
// before the job (the pool's own handles), as process.getActiveResourcesInfo lists it, so no timer, request, child
// process or socket can run a callback that settles the promise, and nothing the script started on node's thread
// pool is still pending (the hook above), so no other thread of the script can either. The check runs in a
// setImmediate callback, which node calls in the check phase after the timers and the I/O callbacks of the
// round, with the queues of microtasks and of process.nextTick empty: no promise reaction of the script is
// still to run, which could start a timer or a request, so an empty list cannot be a moment between two
// steps. A timer, not a repeating immediate, starts each check, so that a script that waits for a long timer
// does not make the worker busy. It returns the function that ends the watch.
//
// What this does not see, and the CPU time and wall clock limits of the pool then bound: a resource that stays
// undestroyed after its work is done (a zlib stream whose destroy waits for the garbage collector), which delays
// the report, never makes a false one; and work that no async resource stands for, such as Atomics.waitAsync and
// messages from a worker thread of the script that this worker did not create through a tracked resource.
const watchIdle = (job, before) => {
  let stopped = false;
  let timer = null;
  const check = () => {
    if (stopped) return;
    if (pendingCount === 0 && leftover(before).length === 0) {
      stopped = true;
      reply(stringify({ id: job.id, idle: "its import is pending and nothing is left on the event loop to settle it" }) + "\n");
      return;
    }
    timer = setTimer(tick, idleCheckMs);
  };
  const tick = () => {
    if (!stopped) setNow(check);
  };
  timer = setTimer(tick, idleCheckMs);
  return () => {
    stopped = true;
    clearTimer(timer);
  };
};
const sleep = (ms) => new PromiseOf((r) => setTimer(r, ms));

for await (const line of createInterface({ input: process.stdin })) {
  const job = parse(line);
  out = "";
  err = "";
  failure = null;
  exitRequest = null;
  let code = 0;
  let stuck = "";
  process.stdout.write = collect("out");
  process.stderr.write = collect("err");
  process.exitCode = undefined;
  process.exit = (c) => {
    exitRequest ??= { code: c === undefined ? (process.exitCode === undefined ? 0 : NumberOf(process.exitCode) | 0) : NumberOf(c) | 0 };
    throw new Exit(exitRequest.code);
  };
  const resources = resourceCounts();
  pending.clear();
  pendingCount = 0;
  asyncHook.enable();
  const unwatch = watchIdle(job, resources);
  try {
    chdir(job.dir);
    process.argv.length = 0;
    push(process.argv, realArgv0, join_(job.dir, job.script));
    for (let i = 0; i < job.args.length; i++) push(process.argv, job.args[i]);
    await import(pathToFileURL(join_(job.dir, job.script)).href);
  } catch (e) {
    if (!(e instanceof Exit)) failure ??= e;
  }
  unwatch();
  asyncHook.disable();
  // The script has finished when what it started has: node runs a script until nothing is pending (timers,
  // requests), so the output, the throws and the exits of the callbacks belong to it. What still runs after
  // the time that job.drainMs allows is not a finished script, and fails the job.
  const deadline = now() + job.drainMs;
  await new PromiseOf((r) => setNow(r));
  while (leftover(resources).length > 0 && now() < deadline) await sleep(2);
  await new PromiseOf((r) => setNow(r));
  await new PromiseOf((r) => setNow(r));
  const remaining = leftover(resources);
  if (remaining.length > 0) stuck = "the script left active resources running: " + join(remaining, ", ");
  if (exitRequest !== null) code = exitRequest.code;
  else if (failure !== null) {
    err += describe(failure) + "\n";
    code = 1;
  } else code = process.exitCode === undefined ? 0 : NumberOf(process.exitCode) | 0;
  process.stdout.write = origOutWrite;
  process.stderr.write = origErrWrite;
  process.exit = origExit;
  process.exitCode = undefined;
  const diff = differences(pristine, snapshot());
  if (stuck !== "") push(diff, stuck);
  const leak = diff.length === 0 ? "" : join(slice(diff, 0, 20), "\n") + (diff.length > 20 ? "\n... " + (diff.length - 20) + " more" : "");
  reply(stringify({ id: job.id, out, err, code, leak }) + "\n");
}
