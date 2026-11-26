package print

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// runStreamMemoryLib runs the checks of the memory count of a stream on the TypeScript library, with
// its source changed by mutate if not nil, and returns the output of node and whether it failed.
func runStreamMemoryLib(t *testing.T, node string, mutate func(string) string) (string, bool) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(libDir, tsLibFileName))
	if err != nil {
		t.Fatal(err)
	}
	helpers, err := os.ReadFile(filepath.Join(libDir, "budget.ts"))
	if err != nil {
		t.Fatal(err)
	}
	lib := string(helpers) + string(source)
	if mutate != nil {
		changed := mutate(lib)
		if changed == lib {
			t.Fatal("the mutant changes nothing")
		}
		lib = changed
	}
	path := filepath.Join(t.TempDir(), "stream_memory.ts")
	if err := os.WriteFile(path, []byte(lib+streamMemoryLibTest), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := tooltest.NodeCombinedOutput(t, node, filepath.Dir(path), filepath.Base(path))
	return string(out), err != nil
}

// TestStreamMemoryHeld checks that the memory count of a TypeScript stream returns no storage that the
// parse still references (spec/runtime/streaming.md, Memory): the values built before the #stream
// repetition stay counted for the whole stream; a commit returns the values of its elements only at a
// memo prune that discards every memo entry and skip record stored by then, since those may reference
// them; and the recovered errors that memo entries, skip records or the record hold stay counted until
// a prune recounts what the holders that remain reach, whatever the diagnostic tail retains.
func TestStreamMemoryHeld(t *testing.T) {
	node := tooltest.RequireNode(t)
	if out, failed := runStreamMemoryLib(t, node, nil); failed {
		t.Fatalf("node:\n%s", out)
	}
}

// TestStreamMemoryHeldMutants is the negative control of TestStreamMemoryHeld: a library that returns
// storage that is still referenced, on each path, fails it.
func TestStreamMemoryHeldMutants(t *testing.T) {
	node := tooltest.RequireNode(t)
	mutants := []struct{ name, old, new, want string }{
		{"prefix", "  streamStart(): void {\n    this.values = 0;\n  }", "  streamStart(): void {}", "prefix:"},
		{"release at commit", "if (this.values > 0) this.retired.push({ bytes: this.values, high: this.memoHigh });",
			"if (this.control !== null) this.control.mem -= this.values;", "retire:"},
		{"release past holders", "this.retired[n]!.high < pos", "true", "retire:"},
		{"skip record holders", "if (r !== null && pos > this.memoHigh) this.memoHigh = pos;", "", "retire:"},
		{"settle", "    this.memoHeld += this.errs;\n    this.errs = 0;\n", "    this.control!.mem -= this.errs;\n    this.errs = 0;\n", "held:"},
		{"recount memo", "        add(e.errs);\n", "", "held:"},
		{"tail", "if (c !== null) c.mem += syntaxErrorBytes(e) - (dropped === null ? 0 : syntaxErrorBytes(dropped));",
			"if (c !== null && dropped !== null) c.mem -= syntaxErrorBytes(dropped);", "tail:"},
		{"record over a record", "return made + (old === undefined ? skipRecordBytes() : 0);", "return made + skipRecordBytes();", "skip:"},
		{"record beside one with values", "values records it, and never after.\n      return made;", "values records it, and never after.\n      return made + skipRecordBytes();", "skip:"},
		{"map of a hash", "        if (recs.size === 0) {\n          s.recs.delete(h);\n          freed += mapBytes;\n        }\n", "", "skip:"},
		{"emptied slots", "(drop === slots.length ? 0 : arrayBytes(slots.length - drop))", "arrayBytes(slots.length - drop)", "slots:"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			out, failed := runStreamMemoryLib(t, node, func(s string) string { return strings.Replace(s, m.old, m.new, 1) })
			if !failed {
				t.Fatalf("the mutant passes:\n%s", out)
			}
			if !strings.Contains(out, m.want) {
				t.Fatalf("the mutant fails another check than %q:\n%s", m.want, out)
			}
		})
	}
}

const streamMemoryLibTest = `

const limited = { signal: null, budget: 0, memory: 2 ** 40, profiles: [] } as any;
function stream(limit: number): StreamParser {
  const p = new StreamParser(mkGrammar(100, true, false), CodePoints, 1, 100, new BudgetControl(limited), 0);
  p.n = 4000;
  if (limit > 0) p.tail = new DiagnosticTail(limit);
  return p;
}
// fail records a failure of the check what; the checks all run, and the script fails at the end.
const failures: string[] = [];
function fail(what: string, msg: string): void { failures.push(what + ": " + msg); }
function entry(errs: Errs, env: Value[] | null): MemoEntry {
  return { node: null, errs, expected: [], next: null, env, end: 0, far: 0, rule: 0, min: 0, ok: true, silent: false } as any;
}
// put stores a memo entry at pos whose key holds n values, charged as memoKey charges a key.
function put(p: StreamParser, pos: number, errs: Errs, n: number): void {
  const env = n === 0 ? null : new Array<Value>(n).fill(pos);
  if (n > 0) p.chargeHeld(arrayBytes(n));
  p.memoPut(0, pos, 0, env, entry(errs, env));
}
// slotsFreed returns the bytes of the memo slots that a prune at pos releases.
function slotsFreed(p: StreamParser, pos: number): number {
  const len = p.slots.length;
  const drop = Math.min(pos - p.memoFloor, len);
  return drop <= 0 ? 0 : arrayBytes(len) - (drop === len ? 0 : arrayBytes(len - drop));
}
// commit runs what a commit at pos does with the memory count, as streamCommit composes it.
function commit(p: StreamParser, pos: number, prune: boolean): void {
  p.streamRelease();
  if (prune) {
    p.streamDropRecords(pos);
    p.memoPrune(pos);
  }
  if (p.streamTail()) p.streamSettleErrors();
}

// prefix: the values built before the #stream repetition stay counted.
{
  const p = stream(0);
  const c = p.control!;
  p.allocValue(500);
  p.streamStart();
  p.allocValue(200);
  commit(p, 100, false);
  const before = c.mem;
  const freed = slotsFreed(p, 1100);
  commit(p, 1100, true);
  if (c.mem !== before - 200 - freed) fail("prefix", "a prune returns " + (before - c.mem - freed) + " bytes of values, want the 200 of the element");
}

// retire: an entry stored at 1500, by a lookahead of the element that ends at 500, before its commit,
// may reference the element's values until a prune discards it.
for (const holder of ["memo", "skip"]) {
  const p = stream(0);
  const c = p.control!;
  p.streamStart();
  if (holder === "memo") put(p, 1500, null, 0);
  else {
    p.skips[p.skipN++] = { table: 0, slots: undefined } as any;
    p.skipSlot(1500, noKey, { end: 1600, far: 0, exp: [], from: 0, examined: 0, list: false, key: null, bind: null, kids: null, ki: 0, errs: null });
    p.skipN--;
    // A memo entry stored before the element, which the first prune discards.
    put(p, 10, null, 0);
  }
  p.allocValue(640);
  let before = c.mem;
  commit(p, 500, false);
  if (c.mem !== before) fail("retire", holder + ": a commit without a prune returns " + (before - c.mem) + " bytes");
  // The prune at 1100 discards the entries before it, but the holder at 1500 remains and may
  // reference the element.
  before = c.mem;
  let freed = slotsFreed(p, 1100) + (holder === "memo" ? 0 : entryBytes);
  commit(p, 1100, true);
  if (c.mem !== before - freed) fail("retire", holder + ": a prune that keeps a holder stored before the commit returns " + (before - freed - c.mem) + " bytes of values");
  // By the prune at 2200 the holder is gone.
  before = c.mem;
  freed = slotsFreed(p, 2200) + (holder === "memo" ? entryBytes : 0);
  const recBefore = before;
  commit(p, 2200, true);
  const returned = before - c.mem - freed;
  if (holder === "memo" && returned !== 640) fail("retire", holder + ": a prune past every holder returns " + returned + " bytes of values, want 640");
  if (holder === "skip" && returned < 640) fail("retire", holder + ": a prune past every holder returns " + returned + " bytes of values, want at least 640");
  void recBefore;
}

// held: the lists and keys of the memo entries, the skip records and the record, and under a
// diagnostic limit the errors themselves, stay counted across a commit, and a prune replaces their
// count with what the holders that remain reach, each shared object once.
for (const limit of [0, 4]) {
  const p = stream(limit);
  const c = p.control!;
  p.streamStart();
  const built = (...pos: number[]): SyntaxError[] => pos.map(n => {
    const e = p.makeError(n, [], true);
    p.recovered.push(e);
    return e;
  });
  const e12 = built(1, 2);
  const kept = p.recSince(0) as ErrList;
  put(p, 1500, kept, 2);
  built(3, 4);
  const gone = p.recSince(1) as ErrList;
  p.recovered.length = 1;
  put(p, 100, gone, 3);
  const e56 = built(5, 6);
  const recordOnly = p.recSince(1) as ErrList;
  put(p, 1600, e56[0]!, 1);
  // skipOnly is held only by a skip record.
  const e78 = built(7, 8);
  const skipOnly = p.recSince(2) as ErrList;
  p.recovered.length = 2;
  p.skips[p.skipN++] = { table: 0, slots: undefined } as any;
  p.skipSlot(1700, noKey, { end: 1700, far: 0, exp: [], from: 0, examined: 0, list: false, key: null, bind: null, kids: null, ki: 0, errs: skipOnly });
  p.skipN--;
  const keys = arrayBytes(2) + arrayBytes(1);
  const lists = errListBytes(kept) + errListBytes(recordOnly) + errListBytes(skipOnly);
  const all = lists + errListBytes(gone) + keys + arrayBytes(3);
  if (p.memoHeld !== all) fail("held", "limit " + limit + ": the lists and keys built are counted as " + p.memoHeld + " bytes, want " + all);
  let before = c.mem;
  commit(p, 500, false);
  if (c.mem < before) fail("held", "limit " + limit + ": a commit without a prune returns " + (before - c.mem) + " bytes");
  let want = lists + keys;
  if (limit > 0) {
    // The settling at 500 emptied the record: only the memo and the skip record hold the lists kept
    // and skipOnly and the errors that remain, and the errors are counted too.
    want = keys + errListBytes(kept) + errListBytes(skipOnly);
    for (const e of [e12[0]!, e12[1]!, e56[0]!, e78[0]!, e78[1]!]) want += syntaxErrorBytes(e);
  }
  before = c.mem;
  const heldBefore = p.memoHeld;
  const freed = slotsFreed(p, 1100) + entryBytes;
  commit(p, 1100, true);
  if (p.memoHeld !== want) fail("held", "limit " + limit + ": the prune leaves " + p.memoHeld + " bytes counted, want " + want);
  if (c.mem !== before - freed - (heldBefore - want)) fail("held", "limit " + limit + ": the count moves by " + (c.mem - before) + ", not with the recount");
}

// tail: an error that the tail no longer retains stays counted while a memo entry holds it.
{
  const p = stream(1);
  const c = p.control!;
  p.streamStart();
  const e1 = p.makeError(1, [], true);
  const e2 = p.makeError(2, [], true);
  put(p, 1500, e1, 0);
  const before = c.mem;
  p.recovered.push(e1, e2);
  p.streamReport();
  if (c.mem !== before + syntaxErrorBytes(e2)) fail("tail", "reporting two errors to a tail of one moves the count by " + (c.mem - before) + ", want the " + syntaxErrorBytes(e2) + " of the error it retains");
  commit(p, 500, false);
  const freed = slotsFreed(p, 1100);
  const mid = c.mem;
  commit(p, 1100, true);
  // The prune keeps e1, which the entry at 1500 holds, and returns e2, which only the tail holds now.
  if (c.mem !== mid - freed - syntaxErrorBytes(e2)) fail("tail", "the prune moves the count by " + (c.mem - mid + freed) + ", want -" + syntaxErrorBytes(e2));
}

// skip: a record stored over the record of its key, or beside one with values, adds no storage, and
// dropping the records returns them with the map of their hash, leaving the table as it was made.
{
  const p = stream(0);
  const c = p.control!;
  p.streamStart();
  const rec = (list: boolean): SkipRecord => ({ end: 1600, far: 0, exp: [], from: 0, examined: 0, list, key: null, bind: null, kids: null, ki: 0, errs: null });
  p.skips[p.skipN++] = { table: 0, slots: undefined } as any;
  const before = c.mem;
  p.skipSlot(1500, noKey, rec(false));
  const made = c.mem;
  for (const list of [false, true, false, true]) p.skipSlot(1500, noKey, rec(list));
  p.skipN--;
  if (c.mem !== made) fail("skip", "storing over a record moves the count by " + (c.mem - made));
  p.streamDropRecords(2000);
  const table = objectBytes(3) + arrayBytes(0) + mapBytes;
  if (c.mem !== before + table) fail("skip", "dropping the records leaves " + (c.mem - before) + " bytes counted, want the " + table + " of the table");
}

// slots: a prune that empties the memo slots returns them whole, as the store after it makes them anew.
{
  const p = stream(0);
  const c = p.control!;
  p.streamStart();
  const before = c.mem;
  for (const pos of [4001, 8002, 12003]) {
    p.n = pos - 1; // the slots reach pos - 1, so the prune at pos empties them
    put(p, pos - 1000, null, 0);
    commit(p, pos, true);
    if (c.mem !== before) fail("slots", "after the prune at " + pos + " that empties the slots, " + (c.mem - before) + " bytes stay counted");
  }
}

if (failures.length > 0) {
  console.log(failures.join("\n"));
  process.exit(1);
}
`
