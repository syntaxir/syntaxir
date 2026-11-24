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
  regs?: Regs;
}
// The registers: the position and the silent depth are numbers, the cut flag a boolean.
type Regs = Record<string, number | boolean>;
interface Config {
  rules: number;
  limit: number;
  maxDepth: number;
  vars: number[];
  ruleVars: number[][];
  noExpectations?: boolean;
  ctxRules?: { cycle: number; leader: boolean }[];
  ruleSpecs?: RuleSpec[];
  observed?: boolean;
  document?: boolean;
  oprs?: { novalue: boolean; opType: boolean; prefix: OprOpSpec[]; led: OprOpSpec[]; operands: OprLineSpec[] | null; skip: boolean; trivia?: OprExprSpec[] }[];
  sourceInfo?: boolean;
  fieldNames?: string[] | null;
}
interface OprOpSpec {
  kind: string;
  assoc: string;
  level: number;
  line: OprLineSpec;
}
interface OprLineSpec {
  scope: number;
  expr?: OprExprSpec[] | null;
  shape: "pass" | "tuple" | "shared" | "none" | "wrap" | "unit";
  action: boolean;
  nullAction?: boolean;
}

// oprShapers are the shapers of the values of lines (ref.OprShape), as the printer encodes the static shape of the
// line's expression: an expression of one element, a sequence, a sequence whose tuple node a memo entry shares, an
// operator's expression of no value, and an expression that is not a sequence whose scope's value is the node of its
// range: of one element, and of no valued element.
const oprShapers: Record<OprLineSpec["shape"], number> = {
  pass: p.singlePass << 2,
  tuple: p.shaperElems | p.shaperTuple | (p.singleTuple << 2) | (2 << 5),
  shared: p.shaperElems | p.shaperTuple | p.shaperShared | (p.singleTuple << 2) | (2 << 5),
  none: p.singleNone << 2,
  wrap: p.shaperTuple | (p.singlePass << 2) | (1 << 5),
  unit: p.shaperTuple | (p.singleNone << 2),
};

// oprTypeName is the named node type of the operators of the rule i, if they have one.
const oprTypeName = (i: number): string => "Op" + i;

// oprProbe is the action of a line (ref.OprLineSpec): a node of the range of its input whose elements are the
// locals op, lhs and rhs and whose fields are the captures of its frame.
const oprProbe: p.Action = (c: p.Actx) => {
  const fields: p.NodeField[] = [];
  c.f.forEach((v, k) => {
    if (v !== null) fields.push({ name: "c" + k, value: v });
  });
  const kids = [c.op, c.lhs, c.rhs] as (p.Node | null)[];
  return [new p.Node("tuple", "", c.start, c.end, "", kids, fields, false, false), null];
};
// oprNullProbe is the action of a line with ref.OprLineSpec.NullAction: it returns the null value.
const oprNullProbe: p.Action = () => [null, null];
// OprExprSpec is one evaluation of the grammar code of an Operators Rule (ref.OprExpr).
interface OprExprSpec {
  ok: boolean;
  len: number;
  charge: number;
  errs?: number[];
  captures?: number[] | null;
  cut?: boolean;
}
interface EvalSpec {
  ok: boolean;
  len: number;
  charge: number;
  errs?: number[];
  captures?: number[];
}
interface RuleSpec {
  memo: boolean;
  docMemo: boolean;
  leader: boolean;
  positional: boolean;
  rec: boolean;
  captures: number;
  seen: number;
  body?: EvalSpec[];
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
  regs?: Regs;
  gathered?: number[];
  made?: number[];
  items?: (number[] | null)[];
}

// A Machine is the library in the state at the start of a parse, with the bindings of one family.
interface Machine {
  // call runs the operation op, or makes the handle that the case operation op makes.
  call(op: string, a: any[]): any[];
  // snapshot adds to the answer the state that the harness inspects after each call.
  snapshot(ans: Answer, e: Entry, res: any[]): void;
  // setRegs sets the registers that the call reads, and getRegs returns them after it (a family with registers).
  setRegs?(regs: Regs): void;
  getRegs?(): Regs;
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

// interruption is the interruption of the conformance harness that ends a parse: the nesting limit is
// "nesting", the compute limit "limit" and an evaluation error, a Fatal that is no interruption of the
// parse, "error". The other interruptions are outside the model.
function interruption(x: unknown): string | null {
  if (x instanceof p.Fatal) {
    if (!(x.err instanceof p.ParseInterruptionError)) return "error";
    if (x.err.kind === "compute-limit") return "limit";
    if (x.err.kind === "depth-limit") return "nesting";
  }
  return null;
}

// Values is the Values handle of a conformance identifier or of an environment key: the values, or
// none for an empty key.
interface Values {
  vals: p.Value[] | null;
}

// SharedCtx is the handle that stands for the library's shared empty context (noCtx).
class SharedCtx {}

// Errors is the handle of a list of recovered errors.
class Errors {
  errs: p.Errs;
  constructor(errs: p.Errs) {
    this.errs = errs;
  }
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
  P: p.PrintedParser;
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
    this.P = this.newParser(g, input, cfg);
    if (options?.sparse) this.P.sparse = true;
    if (controlled) {
      this.ctl = new p.BudgetControl({ signal: null, profiles: [], budget: cfg.limit === 0 ? Number.MAX_SAFE_INTEGER : cfg.limit, memory: 0 } as unknown as p.ResolvedParseOptions);
      (this.P as { control: p.BudgetControl | null }).control = this.ctl;
      this.ctl.held = () => this.P.heldBytes();
      this.P.unitsCheck = this.ctl.unitsCheck();
    }
  }

  // newParser makes the parser of the case: the batch Parser with the printed operations.
  newParser(g: p.Grammar, input: string, cfg: Config): p.PrintedParser {
    return new p.PrintedParser(g, input, p.CodePoints, Math.max(cfg.rules, 1), cfg.maxDepth);
  }

  // printed is the table of the printed operations of the parser's class.
  printed(): Record<string, (P: any, a: any[], c: any) => any[]> {
    return p.printedOps;
  }

  used(): number {
    return this.ctl === null ? 0 : this.P.units;
  }

  // errorsOf returns the handle of the errors that a list of the record holds, one for each entry.
  errorsOf(e: p.Errs): Errors {
    if (e === null) return new Errors(null);
    let l = this.lists.get(e);
    if (l === undefined) {
      l = new Errors(e);
      this.lists.set(e, l);
    }
    return l;
  }

  // conv converts the handles of the suite to the values that the printed code takes and returns: the
  // errors of a list are the library's, and a conformance identifier's values are the values themselves.
  conv = {
    in: (type: string, x: any): any => {
      if (type === "Values") return (x as Values).vals;
      if (type === "Ctx" && x instanceof SharedCtx) return p.noCtx;
      if (type === "Errs") return x === null ? null : x === errsMarker ? err(0) : (x as Errors).errs;
      return x;
    },
    out: (type: string, x: any, op: string, a: any[]): any => {
      // An attempt keeps the handle of the errors it was made with: a list of no errors is the null of the library,
      // which has no identity, and the model holds the handle that it was given.
      if (op === "opr.attempt") (x as any).given = a[5];
      if (op === "opr.errs") return (a[0] as any).given;
      if (type === "Values") return { vals: x };
      // The library's context of a call outside every growth is one shared value; the case holds a handle of
      // its own for each call, which stands for it.
      if (type === "Ctx" && x === p.noCtx) return new SharedCtx();
      if (type === "Errs") {
        // The errors of a result of the dependency memo with none are the model's null handle.
        if (op === "ctx.errs" && (x === null || errorCount(x) === 0)) return null;
        return this.errorsOf(x);
      }
      return x;
    },
  };

  // call runs the printed code of the operation, with the handles of the suite converted. The operations
  // of the cases that make a handle, and those that the printer never prints (print.OperationMethods
  // lists them: they are no operation of a composition), run on the library's own state.
  call(op: string, a: any[]): any[] {
    // A charge of the cases counts its units and checks the counter, as the printed count of units does.
    if (op === "budget.charge") {
      if (this.ctl !== null) this.P.unitsSized(Number(a[0]));
      return [];
    }
    const printed = this.printed()[op];
    if (printed !== undefined) return printed(this.P, a, this.conv);
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
      case "test.node":
        return [{}];
      case "test.rule":
        return [this.rules[a[0]]];
      case "test.err":
        return [err(a[0])];
      case "budget.used":
        return [this.used()];
      case "budget.chargeKey":
        // The units of the key of a memoized rule's call (units.key).
        P.keyUnits(a[0] as p.Rule);
        return [];
      case "env.lookup":
        return [P.variable(a[0])];
      case "env.scope":
        return [P.env];
      case "rec.push":
        P.recovered.push(a[0]);
        return [];
    }
    throw new Error("no binding of the operation " + op);
  }

  snapshot(ans: Answer, e: Entry, res: any[]): void {
    ans.used = this.used();
    ans.rec = errorIds(this.P.recovered, []);
    let n = 0;
    for (const x of this.P.recovered) n += unrolled(x);
    ans.unrolled = n;
    if (res.some((r) => r instanceof Errors)) {
      ans.lists = res.map((r) => (r instanceof Errors ? (r.errs === null ? [0, 0] : [unrolled(r.errs), errorCount(r.errs)]) : null)) as [number, number][];
    }
  }
}

// The capture frames. The registers are the position of the parser.
class FrameM extends Exec {
  constructor(cfg: Config, controlled: boolean, options: Record<string, unknown> | null) {
    super(cfg, controlled, options);
    // The parser starts with the shared empty frame, which the model calls no frame: a case reads the
    // current frame before it uses one, and the shared empty frame is also what frame.open(0) returns.
    (this.P as { frame: p.Frame | null }).frame = null;
  }

  setRegs(regs: Regs): void {
    if (regs.pos !== undefined) this.P.pos = regs.pos as number;
  }
  getRegs(): Regs {
    return { pos: this.P.pos };
  }

  call(op: string, a: any[]): any[] {
    if (op === "test.slot") return [(a[0] as p.Frame)[a[1]] ?? null];
    return super.call(op, a);
  }
}

// The expectation record, with the memo operations that keep the expectations of an entry.
class ExpM extends Exec {
  constructor(cfg: Config, controlled: boolean, options: Record<string, unknown> | null) {
    super(cfg, controlled, options);
    this.P.expectations = !cfg.noExpectations;
  }

  call(op: string, a: any[]): any[] {
    switch (op) {
      case "test.exp":
        return [a[0]];
      case "test.placeholder":
        return [p.expFar];
    }
    return super.call(op, a);
  }
}

// The dependency memo of left recursion and the growths. A Ctx is the library's CtxCall; a Grow its
// GrowState; a result is its CtxResult; the conformance operations install growths and lookaheads
// without charging, as the case states them.
class CtxM extends ExpM {
  constructor(cfg: Config, controlled: boolean, options: Record<string, unknown> | null) {
    super(cfg, controlled, options);
    const leaders = new Map<number, number>();
    this.rules = (cfg.ctxRules ?? []).map((cr, i) => {
      const r = p.mkRule(i, "r" + i, [], 0, false, cr.leader, -1, false, []);
      if (cr.cycle !== 0) {
        r.cycle = cr.cycle;
        if (cr.leader) {
          const k = leaders.get(cr.cycle) ?? 0;
          r.leaderIndex = k;
          if (k < 31) r.leaderBit = 1 << k;
          leaders.set(cr.cycle, k + 1);
        }
      }
      return r;
    });
  }

  setRegs(regs: Regs): void {
    if (regs.pos !== undefined) this.P.pos = regs.pos as number;
    if (regs.silent !== undefined) this.P.silent = regs.silent as number;
  }
  getRegs(): Regs {
    return { pos: this.P.pos, silent: this.P.silent };
  }

  // seed is the seed that a conformance operation installs.
  seed(ok: boolean, end: number): p.CtxResult {
    return p.newCtxResult(ok ? (null as unknown as p.R) : undefined, ok, end, false, p.noErrs, 0, p.noExp, end);
  }

  call(op: string, a: any[]): any[] {
    const P = this.P;
    switch (op) {
      case "test.grow":
      case "test.reseed":
      case "test.ungrow":
      case "test.lookBegin":
      case "test.lookEnd": {
        // The conformance operations count no units.
        const ctl = P.control;
        const units = P.units;
        (P as { control: p.BudgetControl | null }).control = null;
        try {
          this.testOp(op, a);
        } finally {
          (P as { control: p.BudgetControl | null }).control = ctl;
          P.units = units;
        }
        return [];
      }
    }
    return super.call(op, a);
  }

  testOp(op: string, a: any[]): void {
    const P = this.P;
    switch (op) {
      case "test.grow": {
        const r = a[0] as p.Rule;
        // The conformance cases run outside the missing-token mode, so no growth is silenced (GrowRec.ms).
        P.gamma.push({ rule: r.id, min: a[1], ms: false, pos: P.pos, cycle: r.cycle, bit: r.leaderBit, idx: r.leaderIndex, seed: this.seed(a[2], a[3]), id: a[4] });
        break;
      }
      case "test.reseed": {
        const g = P.gamma[P.gamma.length - 1]!;
        g.seed = this.seed(a[0], a[1]);
        g.id = a[2];
        break;
      }
      case "test.ungrow":
        P.dropTable(P.gamma.length - 1);
        P.gamma.length--;
        if (P.gamma.length === 0) {
          P.interned.clear();
          P.internSeq = 0;
        }
        break;
      case "test.lookBegin":
        P.lookBegin(a[0]);
        break;
      case "test.lookEnd":
        P.lookEnd(a[0]);
        break;
    }
  }
}

// The rules: their descriptors and the invocation of their scripted bodies. A body does what its
// script says (see ref.Eval) and logs the evaluation, which the harness compares with the model's.
class RuleM extends Exec {
  log: string[] = [];
  evals = new Map<number, number>();

  constructor(cfg: Config, controlled: boolean, options: Record<string, unknown> | null) {
    super(cfg, controlled, options);
    this.rules = (cfg.ruleSpecs ?? []).map((s, i) => {
      // Every other rule's value is the tuple of its body's one element, so that the suite also holds the
      // shaping of a value to the cost of the call: shaperTuple with one valued element.
      const scope: string[] = [];
      for (let k = 0; k < s.captures; k++) scope.push("c" + k);
      const r = p.mkRule(i, "r" + i, scope, i % 2 === 1 ? p.shaperTuple | (1 << 5) : 0, s.memo, s.leader, s.seen, false, []);
      r.docMemo = s.docMemo;
      r.positional = s.positional;
      r.rec = s.rec;
      r.body = (P: p.Parser, min: number) => this.evaluate(i, s, P, min);
      return r;
    });
  }

  setRegs(regs: Regs): void {
    if (regs.pos !== undefined) this.P.pos = regs.pos as number;
  }
  getRegs(): Regs {
    return { pos: this.P.pos };
  }

  evaluate(i: number, s: RuleSpec, P: p.Parser, min: number): p.R {
    const body = s.body ?? [];
    const k = this.evals.get(i) ?? 0;
    const e: EvalSpec = body.length > 0 ? body[Math.min(k, body.length - 1)]! : { ok: false, len: 0, charge: 0 };
    this.evals.set(i, k + 1);
    const start = P.pos;
    this.log.push("body r" + i + " level=" + min + " pos=" + start);
    if (e.charge > 0 && this.ctl !== null) this.P.unitsSized(e.charge);
    for (const id of e.errs ?? []) P.recovered.push(err(id));
    for (const c of e.captures ?? []) P.frame[c] = new p.Node(p.typeTupleValue, "", 0, 0, "", [], p.noFields, false, false);
    P.pos = start + e.len;
    if (!e.ok) return undefined;
    return new p.Node(p.typeTupleValue, "", start, P.pos, "", [], p.noFields, true, false);
  }

  call(op: string, a: any[]): any[] {
    const P = this.P;
    switch (op) {
      case "rule.docMemo":
        return [(a[0] as p.Rule).docMemo];
      case "rule.positional":
        return [(a[0] as p.Rule).positional];
    }
    return super.call(op, a);
  }

  snapshot(ans: Answer, e: Entry, res: any[]): void {
    super.snapshot(ans, e, res);
    for (const c of e.obs ?? []) {
      if (c === "rules") ans.obs = { ...(ans.obs ?? {}), rules: [...this.log] };
    }
  }
}

// What action expressions make: the elements gathered on the parser's stack for a list or a tuple, and the values made.
// An element or a struct is an object with a conformance identifier.
interface Ided {
  id: number;
}
const idOf = (x: Ided | null): number => (x === null ? 0 : x.id);

class ActionM extends Exec {
  act: p.Actx | null = null;

  call(op: string, a: any[]): any[] {
    const P = this.P;
    switch (op) {
      case "test.act":
        this.act ??= new p.Actx(P);
        return [this.act];
      case "test.elem":
      case "test.struct":
        return [{ id: a[0] }];
      case "test.items":
        return [Array.from({ length: a[1] }, (_, i) => ({ id: a[0] + i }))];
      // The printer prints neither of these two (it takes the elements of a list off with the parser's kids
      // method, and has no filter list): they run on the library's own stack, elemsAbove as the library's
      // method of the operation, which charges two units for each element.
      case "action.elemsAbove":
        return [P.elemsAbove(a[0])];
      case "action.keep":
        P.kidStack.push(a[0][a[1]]);
        return [];
    }
    return super.call(op, a);
  }

  snapshot(ans: Answer, e: Entry, res: any[]): void {
    super.snapshot(ans, e, res);
    ans.gathered = (this.P.kidStack as unknown as (Ided | null)[]).map(idOf);
    ans.made = (this.P.created as unknown as Ided[]).map(idOf);
    if (res.some((r) => Array.isArray(r))) ans.items = res.map((r) => (Array.isArray(r) ? r.map(idOf) : null));
  }
}

// The Operators Rules: the descriptors that the printed code reads (OperatorsRule).
class OprM extends Exec {
  noOprs: p.OperatorsRuleOp[] = [];
  fieldNames: string[] = [];

  constructor(cfg: Config, controlled: boolean, options: Record<string, unknown> | null) {
    super(cfg, controlled, options);
    (this.P as { frame: p.Frame | null }).frame = null;
    (this.P as { sourceInfo: boolean }).sourceInfo = cfg.sourceInfo ?? false;
    this.fieldNames = cfg.fieldNames ?? [];
    this.rules = (cfg.oprs ?? []).map((s, i) => {
      const r = p.mkRule(i, "r" + i, [], 0, false, false, -1, s.novalue, []);
      if (s.opType) r.opNodeType = oprTypeName(i);
      const line = (l: OprLineSpec) => ({ scope: Array.from({ length: l.scope }, (_, k) => "c" + k), m: this.scripted(l.expr ?? []),
        action: l.nullAction ? oprNullProbe : l.action ? oprProbe : null, value: oprShapers[l.shape] });
      const op = (o: OprOpSpec): p.OperatorsRuleOp => ({ id: 0, kind: o.kind, assoc: o.assoc, level: o.level, line: line(o.line) });
      const list = (os: OprOpSpec[]): p.OperatorsRuleOp[] => ((os ?? []).length === 0 ? this.noOprs : os.map(op));
      r.operatorsRule = { skip: s.skip ? this.scripted(s.trivia ?? []) : null, prefix: list(s.prefix), led: list(s.led),
        operands: (s.operands ?? []).map(line) };
      return r;
    });
  }

  // scripted returns the grammar code of a line or of the trivia of a rule that runs the script: its k-th
  // evaluation does the k-th entry, or the last once k passes it, and an empty script fails at once.
  scripted(script: OprExprSpec[]): (P: p.Parser) => p.R {
    let k = 0;
    return (P: p.Parser): p.R => {
      const e: OprExprSpec = script.length > 0 ? script[Math.min(k, script.length - 1)]! : { ok: false, len: 0, charge: 0 };
      k++;
      const start = P.pos;
      if (e.charge > 0 && this.ctl !== null) this.P.unitsSized(e.charge);
      for (const id of e.errs ?? []) P.recovered.push(err(id));
      // A capture of the line is a write to a slot of the current frame, as the grammar code's is.
      for (const c of e.captures ?? []) this.call("frame.set", [c, new p.Node("tuple", "", start, start, "", [], p.noFields, true, false)]);
      if (e.cut) P.cut = true;
      P.pos = start + e.len;
      if (!e.ok) return undefined;
      return new p.Node("tuple", "", start, P.pos, "", [], p.noFields, true, false);
    };
  }

  setRegs(regs: Regs): void {
    if (regs.pos !== undefined) this.P.pos = regs.pos as number;
    if (regs.cut !== undefined) this.P.cut = regs.cut as boolean;
    if (regs.silent !== undefined) this.P.silent = regs.silent as number;
  }
  getRegs(): Regs {
    return { pos: this.P.pos, cut: this.P.cut, silent: this.P.silent };
  }

  call(op: string, a: any[]): any[] {
    if (op === "test.frame" || op === "test.mark") return [{}];
    if (op === "test.slot") return [(a[0] as p.Frame)[a[1]] ?? null];
    // A value that a rule has taken: an empty node at the start of the input.
    if (op === "test.node") return [new p.Node("tuple", "", 0, 0, "", [], p.noFields, false, false)];
    const n = a[0] as p.Node;
    switch (op) {
      case "test.valueStart":
        return [n.start];
      case "test.valueEnd":
        return [n.end];
      case "test.valueKids":
        return [n.children.length];
      case "test.valueKid":
        return [n.children[a[1]] ?? null];
      case "test.valueField":
        return [n.fields.find((f) => f.name === this.fieldNames[a[1]])?.value ?? null];
      case "test.valueRule":
        return [this.rules.findIndex((r) => r.name === n.rule && n.rule !== "")];
      case "test.valueType":
        return [this.rules.findIndex((_, i) => n instanceof p.TypedNode && n.type === oprTypeName(i))];
    }
    return super.call(op, a);
  }
}

// The observation of a parse: the library's Observation, with a trace function that lists the events as
// the model does.
class ObsM extends Exec {
  events: string[] = [];

  constructor(cfg: Config, controlled: boolean, options: Record<string, unknown> | null) {
    super(cfg, controlled, options);
    if (cfg.observed) {
      const trace = (e: p.TraceEvent): void => {
        if (e.kind === "enter") this.events.push("enter " + e.rule + " pos=" + e.pos + " level=" + e.level + " depth=" + e.depth);
        else if (e.kind === "exit") {
          this.events.push("exit " + e.rule + " matched=" + e.matched + " end=" + e.end + " examined=" + e.examined + " memo=" + e.memo + " depth=" + e.depth);
        }
      };
      const o = { profiles: [], profileClock: null, traces: [trace], traceRules: [], traceMaxDepth: 0, unit: p.CodePoints, maxDepth: cfg.maxDepth, expectations: false, signal: null, budget: 0, memory: 0 };
      this.P.observation = new p.Observation(o as unknown as p.ResolvedParseOptions, this.P);
    }
  }

  call(op: string, a: any[]): any[] {
    const P = this.P;
    switch (op) {
      case "obs.active":
        return [P.observation !== null];
      case "obs.enter":
        P.observation!.enter((a[0] as p.Rule).name, a[1], a[2]);
        return [];
      case "obs.exit":
        P.observation!.exit(a[0], a[1], a[2]);
        return [];
    }
    return super.call(op, a);
  }

  snapshot(ans: Answer, e: Entry, res: any[]): void {
    super.snapshot(ans, e, res);
    for (const c of e.obs ?? []) ans.obs = { ...(ans.obs ?? {}), [c]: [...this.events] };
  }
}

// The doc operations. The parser of a case is the library's parser of its parse: a DocumentParser for a
// Document parse, an ObservedParser for an observed parse without one, and the batch Parser otherwise,
// each with the operations as the printer prints them in that class. The registers are the position.
class DocM extends Exec {
  newParser(g: p.Grammar, input: string, cfg: Config): p.PrintedParser {
    const n = Math.max(cfg.rules, 1);
    if (cfg.document) return new p.PrintedDocumentParser(g, input, p.CodePoints, n, cfg.maxDepth) as unknown as p.PrintedParser;
    if (cfg.observed) return new p.PrintedObservedParser(g, input, p.CodePoints, n, cfg.maxDepth) as unknown as p.PrintedParser;
    return super.newParser(g, input, cfg);
  }

  printed(): Record<string, (P: any, a: any[], c: any) => any[]> {
    if (this.P instanceof p.DocumentParser) return p.printedDocOps;
    if (this.P instanceof p.ObservedParser) return p.printedObsOps;
    return p.printedOps;
  }

  setRegs(regs: Record<string, number>): void {
    if (regs.pos !== undefined) this.P.pos = regs.pos;
  }
  getRegs(): Record<string, number> {
    return { pos: this.P.pos };
  }

  // The operations that a class does not print (print.OperationMethods reports them) run on the library's
  // own state: the batch Parser reads no Document and tracks nothing, and an observed parse keeps no
  // start of the examined input.
  call(op: string, a: any[]): any[] {
    const P = this.P as unknown as p.Parser;
    if (this.printed()[op] !== undefined) return super.call(op, a);
    switch (op) {
      case "test.docStat": {
        const d = P as unknown as p.DocumentParser;
        return [a[0] === 0 ? d.reused : d.evaluated];
      }
      case "doc.active":
        return [P instanceof p.DocumentParser];
      case "doc.tracking":
        return [P.track];
      case "doc.touch":
        P.documentTouch(a[0]);
        return [];
      case "doc.touchFrom":
        P.documentTouchFrom(a[0]);
        return [];
    }
    return super.call(op, a);
  }
}

const errsMarker = { errs: true };

function make(family: string, cfg: Config, controlled: boolean, options: Record<string, unknown> | null): Machine {
  switch (family) {
    case "exec":
      return new Exec(cfg, controlled, options);
    case "action":
      return new ActionM(cfg, controlled, options);
    case "opr":
      return new OprM(cfg, controlled, options);
    case "obs":
      return new ObsM(cfg, controlled, options);
    case "rule":
      return new RuleM(cfg, controlled, options);
    case "ctx":
      return new CtxM(cfg, controlled, options);
    case "exp":
      return new ExpM(cfg, controlled, options);
    case "frame":
      return new FrameM(cfg, controlled, options);
    case "doc":
      return new DocM(cfg, controlled, options);
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
