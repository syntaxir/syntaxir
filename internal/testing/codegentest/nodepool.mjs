// The worker of the node pool (nodepool.go): it reads one job per line from its standard input, runs the
// script of the job in the directory of the job, and answers with one line of JSON. Jobs run one at a time,
// so a job owns the process, its working directory and its output while it runs.
import { createInterface } from "node:readline";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

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

const describe = (e) => (e instanceof Error ? (e.stack ?? String(e)) : String(e));

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
  reply(JSON.stringify({ id: job.id, out, err, code }) + "\n");
}
