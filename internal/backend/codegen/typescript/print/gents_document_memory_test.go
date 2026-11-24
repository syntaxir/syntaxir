package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// documentMemoryScript checks that the memory count of a Document parse starts at the bytes the
// Document keeps from earlier parses (spec/runtime/documents.md): a parse that reuses the memo of
// the parse before counts what the Document kept, and a Document whose
// kept bytes alone pass the limit drops them and parses from scratch, with the result and the
// evaluations of a first parse, under the limit that a first parse passes.
const documentMemoryScript = `import * as p from "./parser.ts";
declare const process: { exit(code: number): never };
function fail(why: string): never { console.log(why); return process.exit(0); }
const input = (n: number): string => {
  let s = "";
  for (let i = 0; i < n; i++) s += String.fromCharCode(97 + i % 26) + "=" + i + ";";
  return s;
};
const doc = (s: string): p.Document => {
  const { document, error } = p.createDocument(s);
  if (document === null) return fail("createDocument: " + error);
  return document;
};
// least returns the smallest memory limit under which ok holds, which holds from it on.
function least(what: string, ok: (memory: number) => boolean): number {
  let lo = 1, hi = 1 << 30;
  if (!ok(hi)) fail(what + ": fails under 2^30");
  while (lo < hi) {
    const mid = Math.floor((lo + hi) / 2);
    if (ok(mid)) hi = mid; else lo = mid + 1;
  }
  return lo;
}
const small = input(20);
const first = least("a first parse", memory => doc(small).parse({ limits: { memory } }).error === null);

// retained reads the bytes that a Document keeps, which the library does not export.
const retained = (d: p.Document): number => (d as unknown as { memo: { retainedBytes(): number } }).memo.retainedBytes();
// kept recounts what a Document keeps by the size model of the library, walking its slots and runs:
// a slot array of n slots is 48 + 8n bytes (32 when empty), an object of k fields 24 + 8k, a memo
// entry 120, and each record of a run and each of its elements an object, the elements in an array.
type Memo = { slots: ({ next: unknown } | null | undefined)[]; edits: unknown[]; runs: Map<number, Map<number, { elems: unknown[] }>> | null };
const arrayBytes = (n: number): number => n === 0 ? 32 : 48 + 8 * n;
const objectBytes = (k: number): number => 24 + 8 * k;
const kept = (d: p.Document): number => {
  const m = (d as unknown as { memo: Memo }).memo;
  let n = m.slots.length === 0 ? 0 : arrayBytes(m.slots.length);
  for (const head of m.slots) {
    for (let e = head; e; e = e.next as typeof head) n += 120;
  }
  for (const t of m.runs?.values() ?? []) {
    for (const r of t.values()) n += objectBytes(2) + arrayBytes(r.elems.length) + r.elems.length * objectBytes(7);
  }
  return n + (m.edits.length === 0 ? 0 : arrayBytes(m.edits.length) + m.edits.length * objectBytes(3));
};
const check = (what: string, d: p.Document): void => {
  if (retained(d) !== kept(d)) fail(what + ": the Document counts " + retained(d) + " bytes kept, the recount " + kept(d));
};
{
  // The count follows the entries that parses store, that edits drop and that lookups drop when they
  // lag behind more edits than a lookup applies, and the runs of the last parse.
  const d = doc(small);
  check("a new Document", d);
  if (d.parse().error !== null) fail("the first parse fails");
  if ((d as unknown as { memo: Memo }).memo.runs === null) fail("the parse recorded no run");
  check("after the first parse", d);
  // A parse counts the storage it takes into use, not only the peak of its working stacks: the least
  // limit of a first parse is at least what the parse stored in the Document, which the count of a
  // parse that counted nothing but its stacks would fall below.
  if (first < kept(d)) fail("a first parse counts " + first + " bytes, below the " + kept(d) + " it stored");
  if (d.edit(4, 8, "x=99;") !== null) fail("edit");
  check("after an edit", d);
  d.parse();
  check("after the parse after the edit", d);
  for (let i = 0; i < 130; i++) {
    if (d.edit(small.length, small.length + (i % 2), i % 2 === 0 ? ";" : "") !== null) fail("edit at the end");
  }
  check("after edits that the entries lag behind", d);
  d.parse();
  check("after the parse that drops the lagging entries", d);
}

// A parse that reuses the memo of the parse before counts what the Document kept: two Documents of
// the same text whose parses reuse the same results, one of which keeps more slots and an edit
// from a longer text before, differ in the count of that parse by what they keep.
const settled = (longer: boolean): p.Document => {
  const d = doc(longer ? small + input(30) : small);
  if (d.parse().error !== null) fail("the first parse fails");
  if (longer) {
    const e = d.edit(small.length, small.length + input(30).length, "");
    if (e !== null) fail("edit: " + e);
    if (d.parse().error !== null) fail("the parse after the edit fails");
  }
  return d;
};
const reuse = (longer: boolean): number => least("a parse that reuses the memo", memory => {
  const d = settled(longer);
  const r = d.parse({ limits: { memory } });
  return r.error === null && d.stats().evaluated === 0;
});
const [a, b] = [settled(false), settled(true)];
if (retained(a) === 0 || retained(b) <= retained(a)) fail("kept bytes " + retained(a) + " and " + retained(b));
// Each count is what its Document keeps and what the parse takes into use, the same for both, and one
// below it fails reporting it.
for (const [longer, d] of [[false, a], [true, b]] as const) {
  const count = reuse(longer);
  const own = count - retained(d);
  if (own <= 0 || own !== reuse(false) - retained(a)) fail("a parse that reuses the memo counts " + count + " with " + retained(d) + " bytes kept");
  const below = settled(longer).parse({ limits: { memory: count - 1 } }).error;
  if (!(below instanceof p.ParseInterruptionError) || below.kind !== "memory-limit" || below.used !== count) fail("one below the count " + count + ": " + below);
}

// A Document whose kept bytes alone pass the limit drops them: the parse after the edit runs from
// scratch under the limit of a first parse of the same text.
const large = input(2000);
const d = doc(large);
if (d.parse().error !== null) fail("the parse of the large text fails");
const e = d.edit(0, large.length, small);
if (e !== null) fail("edit: " + e);
const after = d.parse({ limits: { memory: first } });
if (after.error !== null) fail("the parse after the edit, under the limit of a first parse: " + after.error);
const s = d.stats();
const f = doc(small);
const fresh = f.parse();
if (s.memoHits !== 0 || s.resumedElements !== 0 || s.evaluated !== f.stats().evaluated) fail("the parse after the edit reused what it dropped: " + JSON.stringify(s) + ", a first parse " + JSON.stringify(f.stats()));
if (String(after.node) !== String(fresh.node)) fail("the parse after the edit: " + after.node + ", want " + fresh.node);
console.log("ok");
`

// TestTSDocumentMemoryRetained checks the memory count of the Document parses of a generated
// TypeScript parser against what the Document keeps between them.
func TestTSDocumentMemoryRetained(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = item* !.
rule item = k:[a-z]+ "=" v:[0-9]+ ";"`)
	if err != nil {
		t.Fatal(err)
	}
	out := codegentest.RunTSGrammarScriptWith(t, g, documentMemoryScript, common.GenOptions{Start: "main", Document: true})
	if out != "ok\n" {
		t.Fatalf("got %q", out)
	}
}
