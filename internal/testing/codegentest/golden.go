package codegentest

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// UpdateGoldens rewrites the entries of the key/value goldens (GoldenFile.check) that a run checks, instead
// of comparing them; entries of the checked scope that the run no longer produces are removed:
//
//	go test <package> -run <test> -update
//
// Review the diff of a rewritten golden: a regenerated golden makes a regression pass.
var UpdateGoldens = flag.Bool("update", false, "rewrite the entries of the golden files that the run checks")

// ReadGolden reads the file of "key<TAB>value" lines.
func ReadGolden(t testing.TB, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("%s: malformed line %q", path, line)
		}
		out[key] = value
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// writeGolden writes the golden with its header comment and its entries in key order.
func writeGolden(t testing.TB, path, header string, entries map[string]string) {
	t.Helper()
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(header)
	for _, k := range keys {
		b.WriteString(k + "\t" + entries[k] + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// GoldenRules is the part of the header of every key/value golden that says how it is kept.
const GoldenRules = "# A run checks every entry of its scope: an entry that is missing, stale or different fails. -update replaces the\n" +
	"# entries of the scope, drops stale ones and logs a summary per case. Review the diff: it is a regression freeze.\n"

// GoldenDigestRules is the part of the header of a golden of Digests that says what its lines hold.
const GoldenDigestRules = "# One line for each case, then for each part of it: the number of entries, the first 10 hex digits of the SHA-256 of\n" +
	"# the sorted \"key<TAB>value\" lines of those entries and, for a golden of totals, the sums of the two totals. A\n" +
	"# failing run prints the actual entries of the part whose number or digest differs.\n"

// GoldenFile describes a key/value golden: its path, its header, how a key maps to the case it belongs to
// (for the update summary; nil means the key is the case) and, for goldens of measured totals, the two
// totals of a value that the summary sums (nil for none).
//
// A golden of Digests keeps, instead of the entries, one line for each case with the number of entries and
// a digest of them for each part of the case (see Digest). Group names the case and the part of an entry's
// key; the default is CaseOf(key) and no part. A run compares the numbers and the digests; a difference
// names the case and part and lists the actual entries, and the golden does not hold the expected ones.
// The golden then holds a few dozen bytes for each case, however many inputs the case has.
type GoldenFile struct {
	Path, Header string
	CaseOf       func(key string) string
	Totals       func(value string) (a, b uint64)
	TotalNames   [2]string
	Digests      bool
	Group        func(key string) (caseName, part string)
}

// digestLength is the number of hex digits of a digest.
const digestLength = 10

// goldenSum is the number of entries of a part of a case, the digest of the entries and, for a golden
// with totals, their sums.
type goldenSum struct {
	n      int
	digest string
	a, b   uint64
}

func (s goldenSum) String() string {
	return fmt.Sprintf("%d:%s:%d:%d", s.n, s.digest, s.a, s.b)
}

func parseGoldenSum(text string) (goldenSum, bool) {
	f := strings.Split(text, ":")
	if len(f) != 4 {
		return goldenSum{}, false
	}
	n, err1 := strconv.Atoi(f[0])
	a, err2 := strconv.ParseUint(f[2], 10, 64)
	b, err3 := strconv.ParseUint(f[3], 10, 64)
	return goldenSum{n, f[1], a, b}, err1 == nil && err2 == nil && err3 == nil
}

// group returns the case and the part of the key of an entry.
func (g GoldenFile) group(key string) (string, string) {
	if g.Group != nil {
		return g.Group(key)
	}
	if g.CaseOf != nil {
		return g.CaseOf(key), ""
	}
	return key, ""
}

// goldenParts is the sums of the parts of the cases of a digest golden, by case and part.
type goldenParts map[string]map[string]goldenSum

// digestParts groups the entries by case and part and returns their sums, and the entries of each.
func (g GoldenFile) digestParts(entries map[string]string) (goldenParts, map[[2]string][]string) {
	lines := map[[2]string][]string{}
	for k, v := range entries {
		c, p := g.group(k)
		lines[[2]string{c, p}] = append(lines[[2]string{c, p}], k+"\t"+v)
	}
	parts := goldenParts{}
	for cp, ls := range lines {
		sort.Strings(ls)
		h := sha256.Sum256([]byte(strings.Join(ls, "\n")))
		sum := goldenSum{n: len(ls), digest: hex.EncodeToString(h[:])[:digestLength]}
		if g.Totals != nil {
			for _, l := range ls {
				_, v, _ := strings.Cut(l, "\t")
				a, b := g.Totals(v)
				sum.a, sum.b = sum.a+a, sum.b+b
			}
		}
		if parts[cp[0]] == nil {
			parts[cp[0]] = map[string]goldenSum{}
		}
		parts[cp[0]][cp[1]] = sum
	}
	return parts, lines
}

// readDigests reads a digest golden: the lines "case<TAB>part=sum<TAB>part=sum ...".
func readDigests(t testing.TB, path string) goldenParts {
	t.Helper()
	out := goldenParts{}
	for c, v := range ReadGolden(t, path) {
		out[c] = map[string]goldenSum{}
		for _, f := range strings.Split(v, "\t") {
			part, text, ok := strings.Cut(f, "=")
			s, ok2 := parseGoldenSum(text)
			if !ok || !ok2 {
				t.Fatalf("%s: malformed digest %q of %q", path, f, c)
			}
			out[c][part] = s
		}
	}
	return out
}

func writeDigests(t testing.TB, path, header string, parts goldenParts) {
	t.Helper()
	entries := map[string]string{}
	for c, ps := range parts {
		var names []string
		for p := range ps {
			names = append(names, p)
		}
		sort.Strings(names)
		var fs []string
		for _, p := range names {
			fs = append(fs, p+"="+ps[p].String())
		}
		entries[c] = strings.Join(fs, "\t")
	}
	writeGolden(t, path, header, entries)
}

// Differences returns what differs between the entries of got and the golden, for the golden of Digests
// as "case" or "case | part" of each part whose number of entries or digest differs or that the golden
// lacks, and for another golden as the keys whose value differs or that the golden lacks.
// Mutant checks use it to see that a change shows in the golden.
func (g GoldenFile) Differences(t testing.TB, got map[string]string) []string {
	t.Helper()
	var out []string
	if !g.Digests {
		want := ReadGolden(t, g.Path)
		for k, v := range got {
			if w, ok := want[k]; !ok || w != v {
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return out
	}
	have, _ := g.digestParts(got)
	want := readDigests(t, g.Path)
	for c, ps := range have {
		for p, s := range ps {
			if w, ok := want[c][p]; !ok || w != s {
				out = append(out, partName(c, p))
			}
		}
	}
	sort.Strings(out)
	return out
}

func partName(c, p string) string {
	if p == "" {
		return c
	}
	return c + " | " + p
}

// GoldenCaseBefore returns a caseOf for keys "case | entry": the part before the last " | ".
func GoldenCaseBefore(key string) string {
	if i := strings.LastIndex(key, " | "); i >= 0 {
		return key[:i]
	}
	return key
}

// Check compares the entries of got, which are all the keys with the prefix scope that a complete run
// produces, with the golden. A key of got that the golden lacks, a value that differs and a key of the
// golden with the prefix scope that the run did not produce (a case that was removed or renamed, or a
// report that is missing) are errors. A run that filters its entries must not call check. With -update
// the entries with the prefix scope are replaced by got, the others stay, and the summary of what changed
// is logged for each case.
func (g GoldenFile) Check(t *testing.T, scope string, got map[string]string) {
	t.Helper()
	g.check(t, scope, got, false)
}

// CheckSampled is Check for a run over Representative(cases): in the default tier it compares the
// entries that the run produced with the golden and does not require an entry for every case of the
// golden; in the long tier, where every case runs, it is Check. With -update it must run in the long
// tier, which writes every entry.
func (g GoldenFile) CheckSampled(t *testing.T, scope string, got map[string]string) {
	t.Helper()
	if *UpdateGoldens && !allCases {
		t.Fatalf("%s: -update rewrites the golden from a sample of the corpus; run it with -tags sxir_long", t.Name())
	}
	g.check(t, scope, got, !allCases)
}

func (g GoldenFile) check(t *testing.T, scope string, got map[string]string, partial bool) {
	t.Helper()
	if len(got) == 0 {
		t.Errorf("%s: nothing to compare with %s", t.Name(), g.Path)
		return
	}
	for k := range got {
		if !strings.HasPrefix(k, scope) {
			t.Fatalf("%s: key %q is outside the scope %q", t.Name(), k, scope)
		}
	}
	if g.Digests {
		g.checkDigests(t, scope, got, partial)
		return
	}
	want := ReadGolden(t, g.Path)
	if *UpdateGoldens {
		g.logSummary(t, scope, want, got)
		for k := range want {
			if strings.HasPrefix(k, scope) {
				delete(want, k)
			}
		}
		for k, v := range got {
			want[k] = v
		}
		writeGolden(t, g.Path, g.Header, want)
		return
	}
	var problems []string
	for _, k := range sortedKeys(got) {
		w, ok := want[k]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("no entry for %q", k))
		case w != got[k]:
			problems = append(problems, fmt.Sprintf("%q differs:\n got  %s\n want %s", k, got[k], w))
		}
	}
	for _, k := range sortedKeys(want) {
		if _, ok := got[k]; !ok && !partial && strings.HasPrefix(k, scope) {
			problems = append(problems, fmt.Sprintf("the run produced no entry for %q", k))
		}
	}
	for i, p := range problems {
		if i == 20 {
			t.Errorf("%s: and %d more differences from %s", t.Name(), len(problems)-i, g.Path)
			break
		}
		t.Errorf("%s: %s in %s (run with -update after review)", t.Name(), p, g.Path)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// logSummary logs, for each case with a change under scope, the number of changed, added and removed
// entries and, for a golden with totals, the sums of the totals before and after.
func (g GoldenFile) logSummary(t *testing.T, scope string, old, got map[string]string) {
	t.Helper()
	type delta struct {
		changed, added, removed int
		before, after           [2]uint64
	}
	deltas := map[string]*delta{}
	of := func(k string) *delta {
		c := k
		if g.CaseOf != nil {
			c = g.CaseOf(k)
		}
		d := deltas[c]
		if d == nil {
			d = &delta{}
			deltas[c] = d
		}
		return d
	}
	sum := func(arr *[2]uint64, v string) {
		if g.Totals != nil {
			a, b := g.Totals(v)
			arr[0], arr[1] = arr[0]+a, arr[1]+b
		}
	}
	for k, v := range old {
		if !strings.HasPrefix(k, scope) {
			continue
		}
		d := of(k)
		sum(&d.before, v)
		if nv, ok := got[k]; !ok {
			d.removed++
		} else if nv != v {
			d.changed++
		}
	}
	for k, v := range got {
		d := of(k)
		sum(&d.after, v)
		if _, ok := old[k]; !ok {
			d.added++
		}
	}
	var names []string
	for c, d := range deltas {
		if d.changed+d.added+d.removed != 0 {
			names = append(names, c)
		}
	}
	sort.Strings(names)
	var tc, ta, tr int
	for _, c := range names {
		d := deltas[c]
		tc, ta, tr = tc+d.changed, ta+d.added, tr+d.removed
		line := fmt.Sprintf("update %s: %s: %d changed, %d added, %d removed", g.Path, c, d.changed, d.added, d.removed)
		if g.Totals != nil {
			line += fmt.Sprintf("; %s %d -> %d, %s %d -> %d", g.TotalNames[0], d.before[0], d.after[0], g.TotalNames[1], d.before[1], d.after[1])
		}
		t.Log(line)
	}
	t.Logf("update %s scope %q: %d cases changed (%d entries changed, %d added, %d removed) of %d entries", g.Path, scope, len(names), tc, ta, tr, len(got))
}

// checkDigests is check for a golden of Digests.
func (g GoldenFile) checkDigests(t *testing.T, scope string, got map[string]string, partial bool) {
	t.Helper()
	have, lines := g.digestParts(got)
	want := readDigests(t, g.Path)
	if *UpdateGoldens {
		g.logDigestSummary(t, scope, want, have)
		for c := range want {
			if strings.HasPrefix(c, scope) {
				delete(want, c)
			}
		}
		for c, ps := range have {
			want[c] = ps
		}
		writeDigests(t, g.Path, g.Header, want)
		return
	}
	var problems []string
	var cases []string
	for c := range have {
		cases = append(cases, c)
	}
	sort.Strings(cases)
	for _, c := range cases {
		var parts []string
		for p := range have[c] {
			parts = append(parts, p)
		}
		sort.Strings(parts)
		for _, p := range parts {
			s := have[c][p]
			w, ok := want[c][p]
			if ok && w == s {
				continue
			}
			expect := "no entry"
			if ok {
				expect = fmt.Sprintf("%d entries with digest %s", w.n, w.digest)
			}
			actual := lines[[2]string{c, p}]
			more := ""
			if len(actual) > 12 {
				actual, more = actual[:12], fmt.Sprintf("\n ... and %d more", s.n-12)
			}
			problems = append(problems, fmt.Sprintf("%q differs: the golden has %s, the run has %d entries with digest %s:\n %s%s",
				partName(c, p), expect, s.n, s.digest, strings.Join(actual, "\n "), more))
		}
	}
	if !partial {
		var gone []string
		for c, ps := range want {
			if !strings.HasPrefix(c, scope) {
				continue
			}
			for p := range ps {
				if _, ok := have[c][p]; !ok {
					gone = append(gone, partName(c, p))
				}
			}
		}
		sort.Strings(gone)
		for _, k := range gone {
			problems = append(problems, fmt.Sprintf("the run produced no entry for %q", k))
		}
	}
	for i, p := range problems {
		if i == 20 {
			t.Errorf("%s: and %d more differences from %s", t.Name(), len(problems)-i, g.Path)
			break
		}
		t.Errorf("%s: %s in %s (regenerate after review: go test <package> -run <test> -update, with -tags sxir_long)", t.Name(), p, g.Path)
	}
}

// logDigestSummary logs, for each case with a change under scope, the number of changed, added and
// removed parts and, for a golden with totals, the sums before and after.
func (g GoldenFile) logDigestSummary(t *testing.T, scope string, old, have goldenParts) {
	t.Helper()
	var names []string
	seen := map[string]bool{}
	for c := range old {
		if strings.HasPrefix(c, scope) && !seen[c] {
			seen[c] = true
			names = append(names, c)
		}
	}
	for c := range have {
		if !seen[c] {
			seen[c] = true
			names = append(names, c)
		}
	}
	sort.Strings(names)
	var cases, tc, ta, tr int
	for _, c := range names {
		var changed, added, removed int
		var before, after [2]uint64
		for p, s := range old[c] {
			before[0], before[1] = before[0]+s.a, before[1]+s.b
			if n, ok := have[c][p]; !ok {
				removed++
			} else if n != s {
				changed++
			}
		}
		for p, s := range have[c] {
			after[0], after[1] = after[0]+s.a, after[1]+s.b
			if _, ok := old[c][p]; !ok {
				added++
			}
		}
		if changed+added+removed == 0 {
			continue
		}
		cases++
		tc, ta, tr = tc+changed, ta+added, tr+removed
		line := fmt.Sprintf("update %s: %s: %d parts changed, %d added, %d removed", g.Path, c, changed, added, removed)
		if g.Totals != nil {
			line += fmt.Sprintf("; %s %d -> %d, %s %d -> %d", g.TotalNames[0], before[0], after[0], g.TotalNames[1], before[1], after[1])
		}
		t.Log(line)
	}
	t.Logf("update %s scope %q: %d cases changed (%d parts changed, %d added, %d removed) of %d cases", g.Path, scope, cases, tc, ta, tr, len(have))
}
