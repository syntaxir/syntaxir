package print_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The lowering decisions of the Core IR passes and of the Flow passes over the corpus are frozen in
// two directories of golden files, a few files per kind: testdata/decisions/typed for the typed
// Go rules (table trules) and testdata/decisions/recognizer for the recognizers (recRules). They are
// the decisions that coreDecisionLines encodes: Core's guard of every alternative, scans and scopes,
// and the terminal programs, class runs and guarded classes that the Flow passes chose in the
// functions the generator lowered (core_flow_decisions_test.go); a rule that no function is lowered
// from has none of the latter. A run of TestCoreTypedDecisionsFrozen or
// TestCoreRecognizerDecisionsFrozen compares every corpus grammar with its golden entry; the negative
// controls of core_passes_test.go break a pass, or the decisions of one, and require the comparison
// to notice.
//
// A golden is a regression freeze, not an oracle: regenerating it makes a changed decision pass.
// Regenerate it only for a change of a pass's decisions that is intended and justified, and review
// the diff. The run prints the changed, added and removed grammars per mode and their counts:
//
//	go test ./internal/runtime/engine -run 'Test(CoreTypedDecisionsFrozen|CoreRecognizerDecisionsFrozen)$' -update
//
// Run it on its own: the other tests of the Core passes read the golden as it was when they began.
//
// A file holds "cond" lines that define the guard conditions by index (in sorted order, so that
// a new condition does not renumber the others), then, for each block, a "grammar" line naming the
// modes whose decisions it holds (modes with the same decisions share it) and the grammar, and the
// decision lines. Rules are identified by their ID, and expressions by their index in a walk of the
// rule's body (typedDecisionsWalk), so the file does not depend on pointers:
//
//	terminal rule expr
//	guard    rule expr ok depth class expectation cond
//	runs     rule expr first:length ...
//	scan     rule expr
//	scope    rule expr slot,...
//
// The grammars whose decisions come to more than fileAbove bytes in a mode have a file of their own and the rest share small.golden. A block whose lines come to more than digestAbove bytes holds a digest line for each rule instead,
//
//	rule ID kind=count:digest ...
//
// with the number of lines of each kind that the rule has and the first digestLength hex digits of
// the SHA-256 of those lines, sorted and joined by newlines. A difference is reported for the rule and
// kind with the golden's count and digest and the actual lines, so that a failing run says what changed
// without the expected text being stored. The grammars of the examples and the small ones keep their lines.
const (
	typedDecisionsDir      = "testdata/decisions/typed"
	recognizerDecisionsDir = "testdata/decisions/recognizer"
)

const (
	// digestAbove is the size in bytes above which the decisions of a grammar are stored as digests.
	digestAbove = 24 << 10
	// fileAbove is the size in bytes above which a grammar has a golden file of its own.
	fileAbove = 2 << 10
	// smallFile is the golden file of the grammars below fileAbove.
	smallFile = "small.golden"
	// digestLength is the number of hex digits of a digest.
	digestLength = 10
	// decisionsRegenerate tells how to regenerate the goldens after a reviewed change.
	decisionsRegenerate = "regenerate after review: go test ./internal/runtime/engine -run 'Test(CoreTypedDecisionsFrozen|CoreRecognizerDecisionsFrozen)$' -update"
)

// decisionsKey identifies the decisions of a grammar in a mode.
type decisionsKey struct{ mode, grammar string }

// ruleKind identifies the decisions of one kind that a rule has.
type ruleKind struct {
	rule int
	kind string
}

// decisionSum is the number and the digest of the decision lines of a rule and kind.
type decisionSum struct {
	n   int
	sum string
}

// decisionsGolden is a directory of golden files of decisions, read once: the sorted lines of each
// mode and grammar, with the guard conditions as quoted text, or for a grammar stored as digests the
// sums of its rules.
type decisionsGolden struct {
	dir, header string
	loaded      bool
	lines       map[decisionsKey][]string
	sums        map[decisionsKey]map[ruleKind]decisionSum
}

var (
	typedGolden = &decisionsGolden{dir: typedDecisionsDir,
		header: "# Typed lowering decisions of the Core IR and Flow passes (see core_typed_decisions_test.go).\n"}
	recognizerGolden = &decisionsGolden{dir: recognizerDecisionsDir,
		header: "# Recognizer lowering decisions of the Core IR and Flow passes (see core_typed_decisions_test.go).\n"}
)

// decisionSums returns the sums of the lines, which are tab-separated: the kind, the rule and the rest.
func decisionSums(lines []string) map[ruleKind]decisionSum {
	groups := map[ruleKind][]string{}
	for _, l := range lines {
		f := strings.SplitN(l, "\t", 3)
		id, err := strconv.Atoi(f[1])
		if err != nil {
			panic("malformed decision line " + l)
		}
		k := ruleKind{id, f[0]}
		groups[k] = append(groups[k], l)
	}
	sums := make(map[ruleKind]decisionSum, len(groups))
	for k, ls := range groups {
		sort.Strings(ls)
		h := sha256.Sum256([]byte(strings.Join(ls, "\n")))
		sums[k] = decisionSum{len(ls), hex.EncodeToString(h[:])[:digestLength]}
	}
	return sums
}

// sumsLine is the digest line of a rule: its sums in the order of the kinds.
func sumsLine(rule int, sums map[ruleKind]decisionSum) string {
	var parts []string
	for _, kind := range []string{"terminal", "guard", "runs", "scan", "scope"} {
		if s, ok := sums[ruleKind{rule, kind}]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d:%s", kind, s.n, s.sum))
		}
	}
	return fmt.Sprintf("rule\t%d\t%s", rule, strings.Join(parts, " "))
}

// slugRE matches what a file name replaces.
var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// decisionsFile returns the name of the file of a grammar, which is its name with the characters that
// are not letters or digits turned into dashes.
func decisionsFile(grammar string) string {
	return strings.Trim(slugRE.ReplaceAllString(strings.ToLower(grammar), "-"), "-") + ".golden"
}

// typedDecisionsWalk returns the expressions of the rule's body in walk order, and their indexes.
func typedDecisionsWalk(r *common.Rule) ([]ast.Expr, map[ast.Expr]int) {
	var exprs []ast.Expr
	idx := map[ast.Expr]int{}
	if e := r.BodyExpr(); e != nil {
		astfacts.WalkExpr(e, func(x ast.Expr) {
			if _, ok := idx[x]; !ok {
				idx[x] = len(exprs)
			}
			exprs = append(exprs, x)
		})
	}
	return exprs, idx
}

// typedDecisionsBit is the 0 or 1 that a boolean is written as.
func typedDecisionsBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

// swapLastField replaces the last tab-separated field of the line.
func swapLastField(line, last string) string {
	i := strings.LastIndexByte(line, '\t')
	return line[:i+1] + last
}

// read reads the golden files, once. It panics if a file is malformed.
func (gd *decisionsGolden) read() {
	if gd.loaded {
		return
	}
	gd.loaded = true
	gd.lines = map[decisionsKey][]string{}
	gd.sums = map[decisionsKey]map[ruleKind]decisionSum{}
	files, err := filepath.Glob(filepath.Join(gd.dir, "*.golden"))
	if err != nil {
		panic(err)
	}
	for _, path := range files {
		gd.readFile(path)
	}
	for _, ls := range gd.lines {
		sort.Strings(ls)
	}
}

// readFile reads one golden file into gd.
func (gd *decisionsGolden) readFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	var conds []string
	var block []decisionsKey
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "\t", 4)
		switch fields[0] {
		case "cond":
			c, err := strconv.Unquote(fields[2])
			if err != nil {
				panic(err)
			}
			conds = append(conds, c)
		case "grammar":
			g, err := strconv.Unquote(fields[2])
			if err != nil {
				panic(err)
			}
			block = block[:0]
			for _, m := range strings.Split(fields[1], ",") {
				k := decisionsKey{m, g}
				block = append(block, k)
				gd.lines[k] = nil
			}
		case "rule":
			sums := map[ruleKind]decisionSum{}
			id, err := strconv.Atoi(fields[1])
			if err != nil {
				panic("malformed digest line " + line)
			}
			for _, part := range strings.Fields(strings.SplitN(line, "\t", 3)[2]) {
				var kind string
				var s decisionSum
				eq, colon := strings.IndexByte(part, '='), strings.IndexByte(part, ':')
				if eq < 0 || colon < eq {
					panic("malformed digest line " + line)
				}
				kind, s.sum = part[:eq], part[colon+1:]
				if s.n, err = strconv.Atoi(part[eq+1 : colon]); err != nil {
					panic("malformed digest line " + line)
				}
				sums[ruleKind{id, kind}] = s
			}
			for _, k := range block {
				if gd.sums[k] == nil {
					gd.sums[k] = map[ruleKind]decisionSum{}
					delete(gd.lines, k)
				}
				for rk, s := range sums {
					gd.sums[k][rk] = s
				}
			}
		default:
			if fields[0] == "guard" {
				i := strings.LastIndexByte(line, '\t')
				n, err := strconv.Atoi(line[i+1:])
				if err != nil || n < 0 || n >= len(conds) {
					panic("malformed guard line " + line)
				}
				line = swapLastField(line, strconv.Quote(conds[n]))
			}
			for _, k := range block {
				gd.lines[k] = append(gd.lines[k], line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}
}

// has reports whether the golden has an entry for the grammar in the mode.
func (gd *decisionsGolden) has(k decisionsKey) bool {
	_, lines := gd.lines[k]
	_, sums := gd.sums[k]
	return lines || sums
}

// differences compares the decision lines of a grammar in a mode with the golden's and returns what
// differs, one entry per decision line for a grammar stored as lines and one per rule and kind for
// one stored as digests, each prefixed with its aspect. ruleName names a rule by its ID.
func (gd *decisionsGolden) differences(k decisionsKey, lines []string, ruleName func(id int) string) []string {
	gd.read()
	var errs []string
	if !gd.has(k) {
		errs = append(errs, fmt.Sprintf("golden: no entry for this grammar in %s (%s)", gd.dir, decisionsRegenerate))
	}
	if want, ok := gd.sums[k]; ok {
		got := decisionSums(lines)
		byKind := map[ruleKind][]string{}
		for _, l := range lines {
			f := strings.SplitN(l, "\t", 3)
			id, _ := strconv.Atoi(f[1])
			byKind[ruleKind{id, f[0]}] = append(byKind[ruleKind{id, f[0]}], l)
		}
		keys := map[ruleKind]bool{}
		for rk := range want {
			keys[rk] = true
		}
		for rk := range got {
			keys[rk] = true
		}
		for rk := range keys {
			w, g := want[rk], got[rk]
			if w == g {
				continue
			}
			actual := byKind[rk]
			sort.Strings(actual)
			more := ""
			if len(actual) > 12 {
				actual, more = actual[:12], fmt.Sprintf(" ... and %d more", len(byKind[rk])-12)
			}
			errs = append(errs, fmt.Sprintf("%s: rule %s: golden has %d lines with digest %s, Core has %d with digest %s: %s%s",
				decisionAspects[rk.kind], ruleName(rk.rule), w.n, orNone(w.sum), g.n, orNone(g.sum), strings.Join(actual, " | "), more))
		}
	} else {
		count := map[string]int{}
		for _, l := range lines {
			count[l]++
		}
		for _, l := range gd.lines[k] {
			count[l]--
		}
		for l, n := range count {
			if n == 0 {
				continue
			}
			f := strings.Split(l, "\t")
			id, _ := strconv.Atoi(f[1])
			who, other := "Core", "golden"
			if n < 0 {
				who, other = other, who
			}
			errs = append(errs, fmt.Sprintf("%s: rule %s: %s %s, %s none", decisionAspects[f[0]], ruleName(id), who, strings.Join(f[2:], " "), other))
		}
	}
	if len(errs) > 0 {
		errs = append(errs, "golden: "+decisionsRegenerate)
	}
	return errs
}

func orNone(sum string) string {
	if sum == "" {
		return "none"
	}
	return sum
}

// block is the text of the lines of a grammar with the conditions replaced by their indexes.
func decisionsBlock(lines []string, condIndex map[string]int) string {
	var b strings.Builder
	for _, l := range lines {
		if strings.HasPrefix(l, "guard\t") {
			i := strings.LastIndexByte(l, '\t')
			l = swapLastField(l, strconv.Itoa(condIndex[l[i+1:]]))
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

// write writes a golden file for every grammar that ran in any mode in modes, with the decisions of
// the grammar in each mode, removes the files of other grammars, and logs the grammars that changed
// against the golden it replaces.
func (gd *decisionsGolden) write(t *testing.T, modes []string, fresh map[string]map[string][]string) {
	t.Helper()
	old := map[decisionsKey]string{}
	if _, err := os.Stat(gd.dir); err == nil {
		gd.read()
		for k := range gd.lines {
			old[k] = strings.Join(gd.lines[k], "\n")
		}
		for k, sums := range gd.sums {
			old[k] = fmt.Sprint(sumsLines(sums))
		}
	}
	names := map[string]bool{}
	for _, m := range modes {
		for name := range fresh[m] {
			names[name] = true
		}
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	if err := os.MkdirAll(gd.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A grammar whose decisions come to more than fileAbove bytes in a mode has a file of its own, and
	// the others share smallFile, so that the corpus of small generated grammars does not make hundreds
	// of files. The grammars of a file are in sorted order.
	type plan struct {
		sums bool
		file string
	}
	plans := map[string]plan{}
	files := map[string][]string{}
	for _, name := range sorted {
		size := 0
		for _, m := range modes {
			n := 0
			for _, l := range fresh[m][name] {
				n += len(l) + 1
			}
			size = max(size, n)
		}
		file := smallFile
		if size > fileAbove {
			file = decisionsFile(name)
		}
		plans[name] = plan{size > digestAbove, file}
		files[file] = append(files[file], name)
	}
	grammars, digested := 0, 0
	var fileNames []string
	for file := range files {
		fileNames = append(fileNames, file)
	}
	sort.Strings(fileNames)
	for _, file := range fileNames {
		if len(files[file]) > 1 && file != smallFile {
			t.Fatalf("the grammars %q have the same golden file %s", files[file], file)
		}
		var b strings.Builder
		b.WriteString(gd.header)
		condSet := map[string]bool{}
		for _, name := range files[file] {
			if plans[name].sums {
				continue
			}
			for _, m := range modes {
				for _, l := range fresh[m][name] {
					if strings.HasPrefix(l, "guard\t") {
						condSet[l[strings.LastIndexByte(l, '\t')+1:]] = true
					}
				}
			}
		}
		var conds []string
		for c := range condSet {
			conds = append(conds, c)
		}
		sort.Strings(conds)
		condIndex := map[string]int{}
		for i, c := range conds {
			condIndex[c] = i
			fmt.Fprintf(&b, "cond\t%d\t%s\n", i, c)
		}
		for _, name := range files[file] {
			byBody := map[string][]string{}
			var bodies []string
			for _, m := range modes {
				ls, ok := fresh[m][name]
				if !ok {
					continue
				}
				var blk string
				if plans[name].sums {
					blk = strings.Join(sumsLines(decisionSums(ls)), "\n") + "\n"
					digested++
				} else {
					blk = decisionsBlock(ls, condIndex)
				}
				if _, dup := byBody[blk]; !dup {
					bodies = append(bodies, blk)
				}
				byBody[blk] = append(byBody[blk], m)
				grammars++
			}
			for _, blk := range bodies {
				fmt.Fprintf(&b, "grammar\t%s\t%s\n", strings.Join(byBody[blk], ","), strconv.Quote(name))
				b.WriteString(blk)
			}
		}
		if err := os.WriteFile(filepath.Join(gd.dir, file), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	written := map[string]bool{}
	for _, file := range fileNames {
		written[file] = true
	}
	entries, err := os.ReadDir(gd.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !written[e.Name()] && strings.HasSuffix(e.Name(), ".golden") {
			if err := os.Remove(filepath.Join(gd.dir, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	var changed, added, removed []string
	for _, m := range modes {
		for name, ls := range fresh[m] {
			k := decisionsKey{m, name}
			was, ok := old[k]
			now := strings.Join(ls, "\n")
			if _, digest := gd.sums[k]; digest {
				now = fmt.Sprint(sumsLines(decisionSums(ls)))
			}
			switch {
			case !ok:
				added = append(added, m+" | "+name)
			case was != now:
				changed = append(changed, m+" | "+name)
			}
		}
		for k := range old {
			if _, ok := fresh[k.mode][k.grammar]; k.mode == m && !ok {
				removed = append(removed, m+" | "+k.grammar)
			}
		}
	}
	list := func(label string, xs []string) {
		sort.Strings(xs)
		more := ""
		if len(xs) > 40 {
			xs, more = xs[:40], fmt.Sprintf(" ... and %d more", len(xs)-40)
		}
		t.Logf("update %s: %s %d: %s%s", gd.dir, label, len(xs), strings.Join(xs, "; "), more)
	}
	t.Logf("update %s: %d grammar entries in %d modes (%d as digests); %d changed, %d added, %d removed", gd.dir, grammars, len(modes), digested, len(changed), len(added), len(removed))
	if len(changed) > 0 {
		list("changed", changed)
	}
	if len(added) > 0 {
		list("added", added)
	}
	if len(removed) > 0 {
		list("removed", removed)
	}
}

// sumsLines returns the digest lines of the rules in sums, in the order of the rules.
func sumsLines(sums map[ruleKind]decisionSum) []string {
	seen := map[int]bool{}
	var rules []int
	for rk := range sums {
		if !seen[rk.rule] {
			seen[rk.rule] = true
			rules = append(rules, rk.rule)
		}
	}
	sort.Ints(rules)
	lines := make([]string, len(rules))
	for i, r := range rules {
		lines[i] = sumsLine(r, sums)
	}
	return lines
}

// keys returns the keys of the entries of the golden, whether stored as lines or as digests.
func (gd *decisionsGolden) keys() []decisionsKey {
	var ks []decisionsKey
	for k := range gd.lines {
		ks = append(ks, k)
	}
	for k := range gd.sums {
		ks = append(ks, k)
	}
	return ks
}

// staleEntries returns the grammars of the golden in the mode that are not among the grammars
// that ran (a stale entry), in sorted order. A grammar the tier left out of the run (skipped) is
// not stale: it was not checked.
func (gd *decisionsGolden) staleEntries(mode string, ran map[string][]string, skipped map[string]bool) []string {
	var stale []string
	for _, k := range gd.keys() {
		if _, ok := ran[k.grammar]; k.mode == mode && !ok && !skipped[k.grammar] {
			stale = append(stale, k.grammar)
		}
	}
	sort.Strings(stale)
	return stale
}

// freeze runs one frozen-decisions test over the cases of each mode: it compares the cases with the
// golden (compareCoreDecisions) and the grammars of the golden with the cases (a missing or a stale
// entry fails), or with -update rewrites the golden from the cases. cases builds the Core IR of the
// grammars of a mode; check is run for every mode afterwards with the counts of its decisions.
func (gd *decisionsGolden) freeze(t *testing.T, modes []coreGenMode, cases func(t *testing.T, mode coreGenMode) []*coreGenCase, check func(t *testing.T, mode coreGenMode, n *coreDecisionCounts)) {
	fresh := map[string]map[string][]string{}
	var names []string
	for _, mode := range modes {
		names = append(names, mode.name)
		t.Run(mode.name, func(t *testing.T) {
			fresh[mode.name] = map[string][]string{}
			var n coreDecisionCounts
			for _, c := range cases(t, mode) {
				for _, err := range compareCoreDecisions(c, &n) {
					t.Errorf("%s: %s", c.name, err)
				}
				fresh[mode.name][c.name] = c.lines
			}
			if !*codegentest.UpdateGoldens {
				gd.read()
				for _, g := range gd.staleEntries(mode.name, fresh[mode.name], codegentest.CorpusWithParsersSkipped(t)) {
					t.Errorf("%s: %s: stale entry in %s", mode.name, g, gd.dir)
				}
			}
			check(t, mode, &n)
		})
	}
	if *codegentest.UpdateGoldens {
		if len(fresh) != len(modes) || t.Failed() {
			t.Fatalf("not rewriting %s: the run did not complete in every mode", gd.dir)
		}
		gd.write(t, names, fresh)
	}
}

// typedRules returns the rules of the program of the generator by ID, value-free twins included.
func typedRules(g *print.Generator) []*common.Rule {
	return append(append([]*common.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...)
}

// recognizerRules returns the rules of the recognizer program of the generator by ID, value-free
// twins included.
func recognizerRules(g *print.Generator) []*common.Rule {
	rec, err := recognizerOf(g)
	if err != nil {
		panic(err)
	}
	return append(append([]*common.Rule(nil), rec.RuleList...), rec.Twins...)
}
