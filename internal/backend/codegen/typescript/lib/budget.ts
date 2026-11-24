// Portable invocation controls are part of the generated runtime module.

/** A cooperative signal without a required DOM typing dependency. */
export interface CancellationSignal {
  readonly aborted: boolean;
  readonly reason?: unknown;
}

/**
 * Receives the compute units that a parse spent, when the parse ends: after a result, a failure or an
 * interruption alike. The parse sets units to zero when it starts. It needs a parser generated with
 * limit controls, and counts the units whatever the compute limit is.
 */
export interface ComputeReport {
  units: number;
}

export type ParseInterruptionKind = "canceled" | "compute-limit" | "memory-limit" | "input-limit" | "depth-limit";

/** An interrupted invocation returns no partial Node or typed result. */
export class ParseInterruptionError extends Error {
  readonly kind: ParseInterruptionKind;
  readonly limit: number;
  readonly used: number;
  readonly requested: number;
  /** For a compute-limit, the rule that the profile of the parse ranks first (most repeated evaluations at one position and level), or "" without a profile or a repeated evaluation. */
  readonly hotspot: string;

  constructor(kind: ParseInterruptionKind, limit = 0, used = 0, requested = 0, cause: unknown = undefined, hotspot = "") {
    super(kind === "canceled" ? "parse canceled" : kind === "input-limit" ? `input-limit exceeded: limit ${limit}, length ${requested}` : kind === "depth-limit" ? `nesting too deep: more than ${limit} nested recursive rule calls` : kind === "memory-limit" ? `memory-limit exceeded: limit ${limit}, used ${used}` : `${kind} exceeded: limit ${limit}, used ${used}, requested ${requested}${kind === "compute-limit" && hotspot !== "" ? `, hotspot rule ${hotspot}` : ""}`);
    this.name = "ParseInterruptionError";
    this.kind = kind;
    this.limit = limit;
    this.used = used;
    this.requested = requested;
    this.hotspot = hotspot;
    Object.defineProperty(this, "cause", { value: cause, configurable: true });
  }
}
// Interface merging exposes cause with ES2020 or later Error declarations.
export interface ParseInterruptionError { readonly cause: unknown }

export interface ResolvedParseOptions {
  // profiles and traces hold the profiles and trace functions of the options in order, nulls left
  // out; an unobserved invocation has the shared empty arrays noProfiles and noTraces.
  profiles: readonly Profile[];
  profileClock: (() => number) | null;
  traces: readonly TraceFunction[];
  traceRules: readonly string[];
  readonly traceMaxDepth: number;
  readonly unit: Unit;
  readonly maxDepth: number;
  readonly expectations: boolean;
  signal: CancellationSignal | null;
  readonly budget: number;
  readonly memory: number;
  /** The report of the invocation, or null. */
  readonly report: ComputeReport | null;
  /** The limit of missing tokens that the parse may insert (ParseOptions.missingTokens); zero disables insertion. */
  readonly missingTokens: number;
}

// Signal getters are caller code. Keep their thrown identity through the
// parser's Fatal and host-stack-overflow classification boundaries.
export class SignalPanic {
  readonly value: unknown;
  constructor(value: unknown) { this.value = value; }
}

// unitsCheckInterval is the number of compute units after which a check of the counter observes
// cancellation and the memory count (spec/runtime/parsing.md, Checks and Interruptions).
const unitsCheckInterval = 256;

/**
 * BudgetControl holds the limits of a parse with limit controls, which counts logical compute units
 * (spec/runtime/parsing.md, Compute Units): the parser counts the units of the events of the parse in
 * Parser.units, and checks the count only on cycles (calls of recursive rules, steps of growths and
 * loop iterations), after sized events and at the end of the parse. A check compares the count with
 * the compute limit and, once at least unitsCheckInterval units have been counted since the last
 * observation, observes the signal, then the memory count. The end check observes them whatever the
 * interval.
 */
export class BudgetControl {
  private signal: CancellationSignal | null;
  // limit is the resolved compute limit; 0 sets none. The default limit of a stream rises with the
  // input that its parse demands (raise).
  private limit: number;
  private readonly memory: number;
  private readonly profile: Profile | null;
  // obsAt is the count at which the next check observes the signal and the memory count.
  private obsAt = unitsCheckInterval;
  // steps counts the steps since the last observation (step).
  private steps = 0;
  /** The bytes of storage the parse has taken into use at the sites that allocate it (Parser.alloc). */
  mem = 0;
  /** held returns the bytes the parser's working stacks hold now; the count takes its largest value at a check. */
  held: (() => number) | null = null;
  /**
   * counted returns the units the parser has counted now, which the report of the invocation gives
   * when it ends: also when an error of an action ends the parse before its end check (end). It is
   * null until a parser takes the control, and an invocation without one reports zero.
   */
  counted: (() => number) | null = null;
  private heldPeak = 0;

  constructor(o: ResolvedParseOptions) {
    this.signal = o.signal;
    this.limit = o.budget;
    this.memory = o.memory;
    this.profile = o.profiles[0] ?? null;
  }

  release(): void { this.signal = null; }

  // check observes the signal first, then the memory count. Memory is counted where storage is taken
  // into use and checked here, not reserved before an allocation, so the count can pass the limit by
  // what the parse allocates between two observations.
  check(): void {
    const signal = this.signal;
    if (signal !== null) {
      let aborted: boolean;
      let reason: unknown;
      try {
        aborted = signal.aborted;
        if (aborted) reason = signal.reason;
      } catch (x) { throw new SignalPanic(x); }
      if (aborted) throw new Fatal(new ParseInterruptionError("canceled", 0, 0, 0, reason));
    }
    if (this.memory !== 0) {
      const used = this.memoryUsed();
      if (used > this.memory) throw new Fatal(new ParseInterruptionError("memory-limit", this.memory, used, 0));
    }
  }

  /**
   * retain starts the memory count at the bytes that a Document retains from earlier parses, which
   * retained returns; it reads them only under a finite memory limit. It reports false, counting
   * nothing, when they alone exceed the limit: the Document then drops them and parses from scratch
   * (spec/runtime/documents.md).
   */
  retain(retained: () => number): boolean {
    if (this.memory === 0) return true;
    const n = retained();
    if (n > this.memory) return false;
    this.mem += n;
    return true;
  }

  /** memoryLimited reports whether a memory limit is set. */
  memoryLimited(): boolean { return this.memory !== 0; }

  /** memoryUsed returns the count the memory limit checks: the allocated bytes and the peak of the working stacks. */
  memoryUsed(): number {
    if (this.held !== null) {
      const h = this.held();
      if (h > this.heldPeak) this.heldPeak = h;
    }
    return this.mem + this.heldPeak;
  }

  // unitsCheck returns the count above which the parser checks its counter (Parser.unitsCheck): the
  // compute limit, or the count before the next observation if that comes first.
  unitsCheck(): number {
    let c = this.limit === 0 ? Infinity : this.limit;
    if (this.observes() && this.obsAt - 1 < c) c = this.obsAt - 1;
    return c;
  }

  // observes reports whether the checks observe anything beside the compute limit: a signal or a
  // finite memory limit.
  private observes(): boolean { return this.signal !== null || this.memory !== 0; }

  // observe observes the signal, then the memory count, and starts the interval to the next
  // observation again.
  private observe(units: number): void {
    this.check();
    this.obsAt = units + unitsCheckInterval;
  }

  // observeNow observes the signal, then the memory count, whatever the units counted since the last
  // observation, as a stream does around the delivery of an element, and starts the interval to the
  // next observation again.
  observeNow(units: number): void { this.observe(units); }

  // raise raises the compute limit to limit if that is higher, for the default limit of a stream, and
  // reports whether it did.
  raise(limit: number): boolean {
    if (limit <= this.limit) return false;
    this.limit = limit;
    return true;
  }

  // exceeded ends the parse if the count units exceeds the compute limit.
  private exceeded(units: number): void {
    if (this.limit !== 0 && units > this.limit) {
      throw new Fatal(new ParseInterruptionError("compute-limit", this.limit, units, 0, undefined, this.profile === null ? "" : hotspotOf(this.profile)));
    }
  }

  // poll is the check of the count units that the parser makes when the count passes unitsCheck: it
  // observes the signal, then the memory count, once at least unitsCheckInterval units have been
  // counted since the last observation, and ends the parse if the count exceeds the limit; it returns
  // the next unitsCheck. Cancellation wins over the memory limit, and both over the compute limit.
  poll(units: number): number {
    if (this.observes() && units >= this.obsAt) this.observe(units);
    this.exceeded(units);
    return this.unitsCheck();
  }

  // step is a step of the parse that counts no unit, such as the reuse of an element of a Document's
  // run: every unitsCheckInterval steps it observes the signal, then the memory count, so that work
  // which counts no units still observes a cancellation.
  step(): void {
    if (++this.steps >= unitsCheckInterval) {
      this.steps = 0;
      this.check();
    }
  }

  // end is the check at the end of a parse that counted units: it observes the signal and the memory
  // count whatever the interval, then compares the count with the limit.
  end(units: number): void {
    this.observe(units);
    this.exceeded(units);
  }
}

// The memory limit counts bytes by a model of the objects as V8 lays them out on a 64-bit host
// without pointer compression, measured on Node.js 24 as the growth of the heap over 200000 live
// objects of each kind: an object takes 24 bytes plus 8 for each field; an array 32 bytes, and
// with elements a backing store of 16 bytes plus 8 for each; a typed array about 200 bytes plus its
// bytes; a Map 184 bytes, growing by about 28 for each entry; a string of up to 12 one-byte units
// copied out of another takes 16 bytes plus its units rounded up to 8 (one unit is cached, so 0),
// and a longer one a slice of 32 bytes. Two-byte strings and the allocator's rounding are not
// modelled: the count estimates the storage and is the same on every run of a parse.

/** objectBytes is the size of an object with n fields. */
export function objectBytes(n: number): number { return 24 + 8 * n; }
/** nodeBytes is the size of a Node (11 fields). */
export const nodeBytes = 24 + 8 * 11;
/** entryBytes is the size of a memo entry (12 fields). */
export const entryBytes = 24 + 8 * 12;
/** arrayBytes is the size of an array of n elements allocated at its length. */
export function arrayBytes(n: number): number { return n === 0 ? 32 : 48 + 8 * n; }
/** typedBytes is the size of a typed array of n bytes. */
export function typedBytes(n: number): number { return 200 + n; }
/** mapBytes is the size of an empty Map, and mapEntryBytes what each entry adds. */
export const mapBytes = 184;
export const mapEntryBytes = 28;
/** sliceBytes is the size of a string of n units copied or sliced out of another. */
export function sliceBytes(n: number): number { return n <= 1 ? 0 : n < 13 ? (16 + n + 7) & ~7 : 32; }
/** stringBytes is the size of a flat string of n units, as a joined string holds once read. */
export function stringBytes(n: number): number { return n <= 1 ? 0 : (16 + n + 7) & ~7; }

// withBudgetControl runs body with the control of the limits of o, or null when o sets none, and
// returns what interrupted makes of the error of an interruption. The parse that body runs makes the
// end check (BudgetControl.end); this makes the check at the start.
export function withBudgetControl<T>(o: ResolvedParseOptions, body: (control: BudgetControl | null) => T, interrupted: (error: Error) => T): T {
  if (o.signal === null && o.budget === 0 && o.memory === 0 && o.report === null) return body(null);
  let control: BudgetControl | null = null;
  try {
    control = new BudgetControl(o);
    o.signal = null;
    control.check();
    return body(control);
  } catch (x) {
    if (x instanceof SignalPanic) throw x.value;
    if (x instanceof Fatal) return interrupted(x.err);
    throw x;
  } finally {
    // Teardown must neither count nor consult caller getters.
    control?.release();
    o.signal = null;
    if (o.report !== null) o.report.units = control?.counted?.() ?? 0;
  }
}
