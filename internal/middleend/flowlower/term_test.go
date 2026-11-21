package flowlower_test

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/compile"
	irbuild "syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

var update = flag.Bool("update", false, "update golden files")

// TestTermDumpGolden lists the terms of every grammar of testdata/grammars lowered to Exec IR, against
// the goldens in testdata/terms: a review aid that shows what a change of the lowering does to the
// printed programs. A term that Term does not lower is listed by the kinds that it holds and Term
// does not lower. A dump above termDigestAbove bytes is stored as one digest line for each term
// (termDigests); a difference then names the terms that changed, with the digests of the golden and
// of the run and the actual text, and the command that regenerates the goldens.
func TestTermDumpGolden(t *testing.T) {
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".sxir")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			g, err := compiletest.Parse(string(src))
			if err != nil {
				t.Fatal(err)
			}
			prog, err := compiletest.Compile(g, compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			m, err := irbuild.CoreFrom(prog, false)
			if err != nil {
				t.Fatal(err)
			}
			got := dumpTerms(t, m)
			golden := filepath.Join("testdata", "terms", name+".golden")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				out := got
				if len(got) > termDigestAbove {
					out = termDigests(got)
				}
				if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) == got || (strings.HasPrefix(string(want), termDigestHeader) && len(got) > termDigestAbove && termDigests(got) == string(want)) {
				return
			}
			if strings.HasPrefix(string(want), termDigestHeader) {
				for _, d := range termDigestDifferences(string(want), got) {
					t.Error(d)
				}
			}
			t.Errorf("the lowered terms differ from %s; regenerate after review: go test ./internal/middleend/flowlower -run TestTermDumpGolden -update", golden)
		})
	}
}

// termDigestAbove is the size in bytes above which a dump is stored as digests, and termDigestHeader
// is the first line of such a golden.
const (
	termDigestAbove  = 24 << 10
	termDigestHeader = "# digests: term, occurrence, lines, first 10 hex digits of the SHA-256 of the text\n"
)

// termSection is one term of a dump: its heading, which names the rule and the kind of term, and its text.
type termSection struct{ key, text string }

// termSections splits a dump into its terms. The key of a term is its heading and its occurrence among
// the terms with that heading, so that a term added before it does not change its key.
func termSections(dump string) []termSection {
	var out []termSection
	seen := map[string]int{}
	for _, part := range strings.Split(strings.TrimPrefix(dump, "## "), "\n## ") {
		if part == "" {
			continue
		}
		head, _, _ := strings.Cut(part, "\n")
		seen[head]++
		out = append(out, termSection{head + " #" + strconv.Itoa(seen[head]), part})
	}
	return out
}

// termDigest returns the number of lines and the digest of the text of a term.
func termDigest(text string) (lines int, sum string) {
	h := sha256.Sum256([]byte(text))
	return strings.Count(text, "\n") + 1, hex.EncodeToString(h[:])[:10]
}

// termDigests returns the digest golden of a dump: one line for each term, in order.
func termDigests(dump string) string {
	var b strings.Builder
	b.WriteString(termDigestHeader)
	for _, sec := range termSections(dump) {
		n, sum := termDigest(sec.text)
		fmt.Fprintf(&b, "%s\t%d\t%s\n", sec.key, n, sum)
	}
	return b.String()
}

// termDigestDifferences lists the terms whose digest in the golden differs from the dump's, with the
// actual text of the terms of the dump (the first 40 lines of each).
func termDigestDifferences(golden, dump string) []string {
	want := map[string]string{}
	for _, l := range strings.Split(golden, "\n") {
		if f := strings.Split(l, "\t"); len(f) == 3 {
			want[f[0]] = f[1] + " lines, digest " + f[2]
		}
	}
	var out []string
	got := map[string]bool{}
	for _, sec := range termSections(dump) {
		got[sec.key] = true
		n, sum := termDigest(sec.text)
		have := fmt.Sprintf("%d lines, digest %s", n, sum)
		if w, ok := want[sec.key]; !ok || w != have {
			if !ok {
				w = "none"
			}
			text := strings.Split(sec.text, "\n")
			more := ""
			if len(text) > 40 {
				text, more = text[:40], "\n..."
			}
			out = append(out, fmt.Sprintf("term %s: golden has %s, the lowering has %s:\n%s%s", sec.key, w, have, strings.Join(text, "\n"), more))
		}
	}
	var gone []string
	for k := range want {
		if !got[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(gone)
	for _, k := range gone {
		out = append(out, fmt.Sprintf("term %s: golden has %s, the lowering has none", k, want[k]))
	}
	return out
}

// dumpTerms lists the terms of the rules of m in the order of the rules: their actions, the actions of
// the lines of Operators Rules, and the terms of predicates and bindings.
func dumpTerms(t *testing.T, m *core.Module) string {
	var b strings.Builder
	dump := func(rule, what string, x core.ActionExpr) {
		if x == nil {
			return
		}
		fmt.Fprintf(&b, "## %s %s\n", rule, what)
		l, err := flowlower.Term(x, flowlower.TermOptions{})
		if err != nil {
			t.Fatalf("%s %s: %v", rule, what, err)
		}
		b.WriteString(code.FuncString(l.Func))
		b.WriteString("\n")
	}
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		if r.Finish.Kind == core.FinishAction {
			dump(r.Name, "action", r.Finish.Action)
		}
		core.Walk(r.Body, func(n core.Node) {
			switch n := n.(type) {
			case *core.Pred:
				dump(r.Name, "predicate", n.Term)
			case *core.BindNode:
				dump(r.Name, "binding "+n.Var, n.Term)
			case *core.OperatorsRule:
				for i, l := range n.Lines() {
					dump(r.Name, fmt.Sprintf("line %d", i), l.Action)
				}
			}
		})
	}
	return b.String()
}

// TestTermRejectsMalformedTerms checks that Term reports an error, and no lowered function, for terms
// that the front end does not build, so that a generator fails loudly instead of printing them.
func TestTermRejectsMalformedTerms(t *testing.T) {
	one := &core.Const{Value: 1}
	f := &core.Lambda{Params: []string{"e"}}
	e := &core.Local{Kind: core.LocalParam, Name: "e", Lambda: f}
	f.Body = e
	mapped := &core.Builtin{Func: "map", Args: []core.ActionExpr{&core.Builtin{Func: "list", Args: []core.ActionExpr{one}}, f}}
	for _, x := range []core.ActionExpr{
		e, // a parameter outside its lambda
		&core.Binary{Op: "+", L: mapped, R: e},
		&core.Builtin{Func: "nope", Args: []core.ActionExpr{one}},
		&core.Builtin{Func: "len", Args: []core.ActionExpr{one, one}},
		&core.Builtin{Func: "map", Args: []core.ActionExpr{one, one}},
		&core.Lambda{Body: one},
		&core.Binary{Op: "+", L: one, R: &core.Lambda{Params: []string{"e"}, Body: one}},
	} {
		if l, err := flowlower.Term(x, flowlower.TermOptions{}); err == nil || l != nil {
			t.Errorf("Term(%s) = %v, %v; want an error", core.ActionExprString(x), l, err)
		}
	}
	if _, err := flowlower.Term(&core.Builtin{Func: "len", Args: []core.ActionExpr{one}}, flowlower.TermOptions{}); err != nil {
		t.Errorf("Term(len(1)): %v", err)
	}
	if _, err := flowlower.Term(mapped, flowlower.TermOptions{}); err != nil {
		t.Errorf("Term(%s): %v", core.ActionExprString(mapped), err)
	}
}

// TestTermLowersNoFunctionValue pins the closed set of built-ins and where a lambda may stand, which is
// why a lowered term never yields a function and the targets keep no check for one: a lambda is accepted
// only as the last argument of map, filter, foldl and foldr, with one parameter for the first two and two
// for the folds, and no other built-in takes one.
func TestTermLowersNoFunctionValue(t *testing.T) {
	one := &core.Const{Value: 1}
	list := &core.Builtin{Func: "list", Args: []core.ActionExpr{one}}
	lambda := func(params ...string) *core.Lambda { return &core.Lambda{Params: params, Body: one} }
	b := func(fn string, args ...core.ActionExpr) core.ActionExpr { return &core.Builtin{Func: fn, Args: args} }
	for _, c := range []struct {
		name string
		term core.ActionExpr
		ok   bool
	}{
		{"len", b("len", one), true},
		{"text", b("text", one), true},
		{"list", b("list", one, one), true},
		{"concat", b("concat", list, list), true},
		{"map", b("map", list, lambda("e")), true},
		{"filter", b("filter", list, lambda("e")), true},
		{"foldl", b("foldl", one, list, lambda("a", "e")), true},
		{"foldr", b("foldr", one, list, lambda("a", "e")), true},
		{"len of a lambda", b("len", lambda("e")), false},
		{"text of a lambda", b("text", lambda("e")), false},
		{"list of a lambda", b("list", lambda("e")), false},
		{"list with a lambda among its elements", b("list", one, lambda("e")), false},
		{"concat of a lambda", b("concat", lambda("e")), false},
		{"map of a lambda", b("map", lambda("e"), lambda("e")), false},
		{"map with a function of two parameters", b("map", list, lambda("a", "e")), false},
		{"filter with a function of two parameters", b("filter", list, lambda("a", "e")), false},
		{"foldl with a function of one parameter", b("foldl", one, list, lambda("e")), false},
		{"foldr with a lambda as the seed", b("foldr", lambda("a", "e"), list, lambda("a", "e")), false},
		{"lambda returning a lambda", b("map", list, &core.Lambda{Params: []string{"e"}, Body: lambda("x")}), false},
		{"a built-in that is not one", b("apply", list, lambda("e")), false},
		{"a lambda alone", lambda("e"), false},
	} {
		l, err := flowlower.Term(c.term, flowlower.TermOptions{})
		if (err == nil) != c.ok || (err == nil) != (l != nil) {
			t.Errorf("%s: Term(%s) = %v, %v; want ok %v", c.name, core.ActionExprString(c.term), l, err, c.ok)
		}
	}
}
