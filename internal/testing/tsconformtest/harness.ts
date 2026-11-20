// The bindings of the TypeScript library to the operations of the Exec IR conformance suite. Go
// (internal/testing/tsconformtest) sends one trace of calls per case, as a line of JSON, and this
// program replays it on the library and answers with a line of JSON: what each call returned and the
// state that the harness inspects (see trace.go). A binding calls the library's own function or does
// what the library's printed composition does where the operation is inline code there.
import { createInterface } from "node:readline";
import * as p from "./parser.ts";

type Arg = number | boolean | string | null | { h: number } | { errs: true };
interface Entry {
  op: string;
  args: Arg[];
  res: (number | null)[];
  obs?: string[];
  regs?: Record<string, number>;
}
interface Config {
  rules: number;
  limit: number;
  maxDepth: number;
  vars: number[];
  ruleVars: number[][];
}
interface Request {
  family: string;
  config: Config;
  controlled: boolean;
  options: Record<string, unknown> | null;
  entries: Entry[];
}
interface Answer {
  res: unknown[];
  int?: string;
  err?: string;
  used: number;
  rec?: number[];
  unrolled?: number;
  lists?: ([number, number] | null)[];
  obs?: Record<string, string[]>;
  regs?: Record<string, number>;
}

// A Machine is the library in the state at the start of a parse, with the bindings of one family.
interface Machine {
  // call runs the operation op, or makes the handle that the case operation op makes.
  call(op: string, a: any[]): any[];
  // snapshot adds to the answer the state that the harness inspects after each call.
  snapshot(ans: Answer, e: Entry, res: any[]): void;
  // setRegs sets the registers that the call reads, and getRegs returns them after it (a family with registers).
  setRegs?(regs: Record<string, number>): void;
  getRegs?(): Record<string, number>;
}

// The handles: the value that the library returned for each result of a trace, and the identity of
// each value of the library that the harness compares.
class Handles {
  private byId = new Map<number, any>();
  private ids = new Map<any, number>();
  private next = 0;
  // bind records that the handle id of the trace is v.
  bind(id: number, v: any): void {
    this.byId.set(id, v);
  }
  get(id: number): any {
    if (!this.byId.has(id)) throw new Error("the trace uses handle " + id + " before a call returned it");
    return this.byId.get(id);
  }
  // identity returns the identity of v: the same for the same object or the same scalar.
  identity(v: any): number {
    if (v === null || v === undefined) return 0;
    let i = this.ids.get(v);
    if (i === undefined) {
      i = ++this.next;
      this.ids.set(v, i);
    }
    return i;
  }
}

// The value of a conformance identifier (ref.Value): an integer below 1000; from 1000 a string of
// id-1000 "a"; from 2000 a string of id-2000 "b".
function datum(id: number): p.Value {
  if (id < 1000) return id;
  if (id < 2000) return "a".repeat(id - 1000);
  return "b".repeat(id - 2000);
}

function interruption(x: unknown): string | null {
  if (x instanceof p.Fatal) {
    const kind = (x.err as { kind?: string }).kind;
    if (kind === "compute-limit") return "limit";
    if (kind === "depth-limit") return "nesting";
  }
  return null;
}

// Values is the Values handle of a conformance identifier or of an environment key: the values, or
// none for an empty key.
interface Values {
  vals: p.Value[] | null;
}

// Errors is the handle of a list of recovered errors.
interface Errors {
  errs: p.Errs;
}

// A SyntaxError of a conformance identifier is at the position id.
function err(id: number): p.SyntaxError {
  return new p.SyntaxError(id, 1, 1, [], []);
}

function unrolled(e: p.RecEntry): number {
  if (e instanceof p.ErrList) {
    let n = 1;
    for (const x of e.entries) n += unrolled(x);
    return n;
  }
  return 1;
}

function errorCount(e: p.RecEntry): number {
  if (e instanceof p.ErrList) {
    let n = 0;
    for (const x of e.entries) n += errorCount(x);
    return n;
  }
  return 1;
}

function errorIds(entries: readonly p.RecEntry[], out: number[]): number[] {
  for (const e of entries) {
    if (e instanceof p.ErrList) errorIds(e.entries, out);
    else out.push(e.pos);
  }
  return out;
}

// The memo, the deferred memoization, the budget, the environment and the recovered errors.
class Exec implements Machine {
  P: p.Parser;
  ctl: p.BudgetControl | null = null;
  rules: p.Rule[] = [];
  keys = new Map<number, Values>();
  lists = new Map<p.RecEntry, Errors>();

  constructor(cfg: Config, controlled: boolean, options: Record<string, unknown> | null) {
    const g = p.mkGrammar(cfg.maxDepth, false, false, controlled);
    g.initialVars = (cfg.vars ?? []).map((id, i) => ["v" + i, datum(id)]);
    for (let i = 0; i < (cfg.ruleVars ?? []).length; i++) {
      this.rules.push(p.mkRule(i, "r" + i, [], 0, false, false, -1, false, cfg.ruleVars[i]!));
    }
    const input = "x".repeat(Number(options?.input ?? 0));
    // The parser is made without a control: making it charges for the input, which a case does not.
    this.P = new p.Parser(g, input, p.CodePoints, Math.max(cfg.rules, 1), cfg.maxDepth);
    if (options?.sparse) this.P.sparse = true;
    if (controlled) {
      this.ctl = new p.BudgetControl({ signal: null, budget: cfg.limit === 0 ? Number.MAX_SAFE_INTEGER : cfg.limit, memory: 0 } as unknown as p.ResolvedParseOptions);
      (this.P as { control: p.BudgetControl | null }).control = this.ctl;
      this.ctl.held = () => this.P.heldBytes();
    }
  }

  used(): number {
    return this.ctl === null ? 0 : (this.ctl as unknown as { budgetUsed: number }).budgetUsed;
  }

  // errorsOf returns the handle of the errors that a list of the record holds, one for each entry.
  errorsOf(e: p.Errs): Errors {
    if (e === null) return { errs: null };
    let l = this.lists.get(e);
    if (l === undefined) {
      l = { errs: e };
      this.lists.set(e, l);
    }
    return l;
  }

  call(op: string, a: any[]): any[] {
    const P = this.P;
    switch (op) {
      case "test.values": {
        let k = this.keys.get(a[0]);
        if (k === undefined) {
          k = { vals: [a[0]] };
          this.keys.set(a[0], k);
        }
        return [k];
      }
      case "test.datum":
        return [datum(a[0])];
      case "test.rule":
        return [this.rules[a[0]]];
      case "test.err":
        return [err(a[0])];
      case "memo.lookup": {
        const e = P.memoGet(a[0], a[1], a[2], (a[3] as Values).vals);
        return [e, e !== null];
      }
      case "memo.store": {
        const errs = a[8] === null ? null : a[8] === errsMarker ? err(0) : (a[8] as Errors).errs;
        const e = p.newEntry(undefined, a[4], a[5], a[6], errs);
        return [P.memoPut(a[0], a[1], a[2], (a[3] as Values).vals, e)];
      }
      case "memo.ok":
        return [(a[0] as p.MemoEntry).ok];
      case "memo.end":
        return [(a[0] as p.MemoEntry).end];
      case "memo.silent":
        return [(a[0] as p.MemoEntry).silent];
      case "seen.first":
        return [P.seenFirst(a[0], a[1])];
      case "budget.charge":
        this.ctl!.charge(a[0]);
        return [];
      case "budget.used":
        return [this.used()];
      case "env.lookup":
        return [P.variable(a[0])];
      case "env.bind":
        P.bindValue(a[0], a[1]);
        return [];
      case "env.scope":
        return [P.env];
      case "env.restore":
        P.env = a[0];
        return [];
      case "env.key": {
        const vals = P.envValues((a[0] as p.Rule).vars);
        return [{ vals }];
      }
      case "rec.mark":
        return [P.recovered.length];
      case "rec.push":
        P.recovered.push(a[0]);
        return [];
      case "rec.truncate":
        P.recovered.length = a[0];
        return [];
      case "rec.since": {
        const e = P.recSince(a[0]);
        return [this.errorsOf(e)];
      }
      case "rec.add":
        P.recAdd((a[0] as Errors).errs);
        return [];
      case "rec.none":
        return [(a[0] as Errors | null) === null || (a[0] as Errors).errs === null];
    }
    throw new Error("no binding of the operation " + op);
  }

  snapshot(ans: Answer, e: Entry, res: any[]): void {
    ans.used = this.used();
    ans.rec = errorIds(this.P.recovered, []);
    let n = 0;
    for (const x of this.P.recovered) n += unrolled(x);
    ans.unrolled = n;
    if (e.op === "rec.since" && res.length > 0) {
      const l = res[0] as Errors;
      ans.lists = [l.errs === null ? [0, 0] : [unrolled(l.errs), errorCount(l.errs)]];
    }
  }
}

// A Mark is a backtracking point: the position, the environment and the lengths of the recovered errors
// and of the trail when frame.mark took it, as the printed composition keeps them in local variables.
interface Mark {
  pos: number;
  env: unknown;
  trail: number;
  rec: number;
}

// The capture frames. The registers are the position of the parser.
class FrameM extends Exec {
  constructor(cfg: Config, controlled: boolean, options: Record<string, unknown> | null) {
    super(cfg, controlled, options);
    // The parser starts with the shared empty frame, which the model calls no frame: a case reads the
    // current frame before it uses one, and the shared empty frame is also what frame.open(0) returns.
    (this.P as { frame: p.Frame | null }).frame = null;
  }

  setRegs(regs: Record<string, number>): void {
    if (regs.pos !== undefined) this.P.pos = regs.pos;
  }
  getRegs(): Record<string, number> {
    return { pos: this.P.pos };
  }

  call(op: string, a: any[]): any[] {
    const P = this.P;
    switch (op) {
      case "test.node":
        return [{}];
      case "frame.mark":
        return [{ pos: P.pos, env: P.env, trail: P.trail.length, rec: P.recovered.length } as Mark];
      case "frame.markPos":
        return [(a[0] as Mark).pos];
      case "frame.markEnv":
        return [(a[0] as Mark).env];
      case "frame.markTrail":
        return [(a[0] as Mark).trail];
      case "frame.markRec":
        return [(a[0] as Mark).rec];
      case "frame.undo": {
        for (let t = P.trail.length - 1; t >= a[0]; t--) {
          P.spend();
          const u = P.trail[t]!;
          u.f[u.slot] = u.old;
        }
        P.trail.length = a[0];
        return [];
      }
      case "frame.current":
        return [P.frame];
      case "frame.use":
        P.frame = a[0];
        return [];
      case "frame.set":
        P.setCapture(a[0], a[1]);
        return [];
      case "frame.close":
        return [];
      case "frame.open":
        return [P.newFrame(a[0])];
      case "frame.slots":
        return [(a[0] as p.Frame).length];
      case "frame.save": {
        P.saved.length = 0;
        for (const x of a[0] as p.Frame) {
          P.spend();
          P.saved.push(x);
        }
        return [];
      }
      case "frame.restore": {
        const f = a[0] as p.Frame;
        for (let t = 0; t < f.length; t++) {
          P.spend();
          f[t] = P.saved[t]!;
        }
        return [];
      }
      case "test.slot":
        return [(a[0] as p.Frame)[a[1]] ?? null];
    }
    return super.call(op, a);
  }
}

const errsMarker = { errs: true };

function make(family: string, cfg: Config, controlled: boolean, options: Record<string, unknown> | null): Machine {
  switch (family) {
    case "exec":
      return new Exec(cfg, controlled, options);
    case "frame":
      return new FrameM(cfg, controlled, options);
  }
  throw new Error("no family " + family);
}

function decode(h: Handles, a: Arg): any {
  if (a !== null && typeof a === "object") {
    if ("h" in a) return h.get(a.h);
    return errsMarker;
  }
  return a;
}

function run(req: Request): { answers: Answer[] } {
  const m = make(req.family, req.config, req.controlled, req.options);
  const h = new Handles();
  const answers: Answer[] = [];
  for (const e of req.entries) {
    const ans: Answer = { res: [], used: 0 };
    let res: any[] = [];
    try {
      if (e.regs && m.setRegs) m.setRegs(e.regs);
      res = m.call(e.op, e.args.map((a) => decode(h, a)));
      ans.res = res.map((v, i) => {
        const id = e.res[i];
        if (id === null || id === undefined) return v;
        h.bind(id, v);
        return { h: h.identity(v) };
      });
    } catch (x) {
      const kind = interruption(x);
      if (kind !== null) ans.int = kind;
      else ans.err = x instanceof Error ? (x.stack ?? x.message) : String(x);
    }
    if (e.regs && m.getRegs) ans.regs = m.getRegs();
    m.snapshot(ans, e, ans.err === undefined && ans.int === undefined ? res : []);
    answers.push(ans);
    if (ans.err !== undefined || ans.int !== undefined) break;
  }
  return { answers };
}

const lines = createInterface({ input: process.stdin });
for await (const line of lines) {
  let out: object;
  try {
    out = run(JSON.parse(line) as Request);
  } catch (x) {
    out = { error: x instanceof Error ? (x.stack ?? x.message) : String(x) };
  }
  process.stdout.write(JSON.stringify(out) + "\n");
}
