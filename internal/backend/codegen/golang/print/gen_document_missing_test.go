package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// documentInsertionLimits are the missing-token limits of the generated Document comparisons: one
// and two, which some inputs reach, and one that none reaches.
var documentInsertionLimits = []int{1, 2, 64}

// TestGeneratedDocumentsInsertMissingTokens checks that a generated Document with a missing-token
// limit parses as a parse from scratch of its text does, on the incremental insertion corpus, in
// both units, at several limits: the same tree and error after each edit from one input of a case to
// the next and back to the first, after a parse without an edit, and in an observed parse. A parse
// that reaches the limit fails and leaves nothing that a later parse reuses, and a parse whose
// options change the limit is rejected. Each parse also counts the compute units of the engine's
// Document along the same edits. The Documents resume the runs of their
// repetitions, so that the parses after an edit resume them. With a library that records the runs
// whose elements reached an insertion site, some parse must differ.
func TestGeneratedDocumentsInsertMissingTokens(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	// The Documents record the runs of every repetition, however short, on both sides (the engine's
	// state.Tuning.MinRec): the corpus inputs are short.
	patch := func(name, src, old, new string) string {
		if !strings.Contains(src, old) {
			t.Fatalf("%s does not declare %q", name, old)
		}
		return strings.Replace(src, old, new, 1)
	}
	everyRun := func(name, src string) string {
		if name == "call_exec.go" {
			return patch(name, src, "const minRecordedRun = 16\n", "const minRecordedRun = 1\n")
		}
		return src
	}
	cases := codegentest.IncrementalInsertionCorpus()
	for _, m := range generatedDocumentInsertionMismatches(t, goBin, codegentest.LibCopyWith(t, everyRun, nil), cases, true) {
		t.Error(m)
	}
	// The mutant records a run whatever its elements reached.
	const clean = "func (p *Parser) runClean(r *RunState) bool {\n"
	ignoring := codegentest.LibCopyWith(t, func(name, src string) string {
		if name == "document_runs.go" {
			return patch(name, src, clean, clean+"\tr.reached0 = p.missingReached()\n")
		}
		return everyRun(name, src)
	}, nil)
	if len(generatedDocumentInsertionMismatches(t, goBin, ignoring, cases, false)) == 0 {
		t.Error("runs that ignore insertions: every generated Document parse matched the parse from scratch")
	}
}

// documentEdit is an edit of a Document, in its position unit. Texts are bytes, which JSON keeps
// when they are not UTF-8.
type documentEdit struct {
	Start, End int
	Text       []byte
}

// documentInsertionRun is the Document comparison of one case in one unit at one limit: the
// Document starts with the first text and takes the edits in order.
type documentInsertionRun struct {
	Case  int
	First []byte
	Edits []documentEdit
	Bytes bool
	Limit int
}

// documentInsertionResult is what the generated program reports of a run: the differences of its
// parses from the parses from scratch, and the compute units of each parse.
type documentInsertionResult struct {
	Diffs []string `json:",omitempty"`
	Units []uint64 `json:",omitempty"`
}

// generatedDocumentInsertionMismatches generates a parser with Documents for each case with the
// library copy lib, runs the Document comparisons of every case at documentInsertionLimits, and
// describes each parse that differs from the parse from scratch, and each rejection that does not
// happen. With units, it also describes each run whose parses count other compute units than those
// of the engine's Document along the same edits.
func generatedDocumentInsertionMismatches(t *testing.T, goBin, lib string, cases []codegentest.Case, units bool) []string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", codegentest.GoModAt("docmissing", lib))
	var imports, checks strings.Builder
	var runs []documentInsertionRun
	var names []string
	var want [][]uint64
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: fmt.Sprintf("g%d", i), Start: "main", Document: true, Observability: true, SourceInfo: true})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"docmissing/g%d\"\n", i, i)
		fmt.Fprintf(&checks, `	%[1]d: func(v run) (res result) {
		u := g%[1]d.CodePoints
		if v.Bytes {
			u = g%[1]d.Bytes
		}
		d, err := g%[1]d.NewDocument(string(v.First), g%[1]d.WithUnit(u), sxirruntime.WithMissingTokens(v.Limit))
		if err != nil {
			res.Diffs = append(res.Diffs, err.Error())
			return res
		}
		parse := func(what string, opts ...sxirruntime.ParseOption) {
			var units uint64
			if countUnits {
				opts = append(opts, sxirruntime.WithComputeReport(&units))
			}
			n, err := d.Parse(opts...)
			res.Units = append(res.Units, units)
			bn, berr := g%[1]d.Parse(d.Text(), append(opts, g%[1]d.WithUnit(u), sxirruntime.WithMissingTokens(v.Limit))...)
			if got, want := show(n, err), show(bn, berr); got != want {
				res.Diffs = append(res.Diffs, fmt.Sprintf("%%s, text %%q:\n document     %%s\n from scratch %%s", what, d.Text(), got, want))
			}
		}
		parse("first parse")
		if n, err := d.Parse(sxirruntime.WithMissingTokens(v.Limit + 1)); n != nil || err == nil {
			res.Diffs = append(res.Diffs, fmt.Sprintf("a parse with the limit %%d was not rejected", v.Limit+1))
		}
		for _, e := range v.Edits {
			if err := d.Edit(e.Start, e.End, string(e.Text)); err != nil {
				res.Diffs = append(res.Diffs, err.Error())
				return res
			}
			parse("after an edit")
		}
		parse("again")
		parse("observed", sxirruntime.WithTrace(func(instrument.TraceEvent) {}))
		parse("after the observed parse")
		return res
	},
`, i)
		var prog *engine.Program
		if units {
			prog = mustCompile(t, c.Src, check.Options{NoProjections: true})
		}
		for _, bytes := range []bool{false, true} {
			for _, limit := range documentInsertionLimits {
				v := documentInsertionRun{Case: i, First: []byte(c.Inputs[0]), Bytes: bytes, Limit: limit}
				text := c.Inputs[0]
				for _, next := range append(c.Inputs[1:], c.Inputs[0]) {
					start, end, repl := documentMinimalEdit(text, next, bytes)
					v.Edits = append(v.Edits, documentEdit{start, end, []byte(repl)})
					text = next
				}
				runs = append(runs, v)
				names = append(names, fmt.Sprintf("%s (bytes %v, limit %d)", c.Name, bytes, limit))
				if units {
					want = append(want, engineDocumentInsertionUnits(t, prog, v))
				} else {
					want = append(want, nil)
				}
			}
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
`+imports.String()+`)

const countUnits = `+fmt.Sprint(units)+`

type edit struct {
	Start, End int
	Text       []byte
}

type run struct {
	Case  int
	First []byte
	Edits []edit
	Bytes bool
	Limit int
}

type result struct {
	Diffs []string `+"`json:\",omitempty\"`"+`
	Units []uint64 `+"`json:\",omitempty\"`"+`
}

func show(n any, err error) string {
	data, _ := json.Marshal(n)
	return fmt.Sprintf("%s %v", data, err)
}

var checks = map[int]func(run) result{
`+checks.String()+`}

func main() {
	var runs []run
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, &runs); err != nil {
		panic(err)
	}
	for _, v := range runs {
		b, _ := json.Marshal(checks[v.Case](v))
		fmt.Println(string(b))
	}
}
`)
	data, _ := json.Marshal(runs)
	write("runs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "runs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(runs) {
		t.Fatalf("got %d results, want %d\n%s", len(lines), len(runs), out)
	}
	var mismatches []string
	for k, line := range lines {
		var res documentInsertionResult
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		for _, d := range res.Diffs {
			mismatches = append(mismatches, names[k]+": "+d)
		}
		if units && fmt.Sprint(res.Units) != fmt.Sprint(want[k]) {
			mismatches = append(mismatches, fmt.Sprintf("%s: compute units of the parses\n generated %v\n engine    %v", names[k], res.Units, want[k]))
		}
	}
	return mismatches
}

// engineDocumentInsertionUnits runs v with an engine Document, parse for parse as the generated
// program does, and returns the compute units of each parse.
func engineDocumentInsertionUnits(t *testing.T, prog *engine.Program, v documentInsertionRun) []uint64 {
	t.Helper()
	o := engine.ParseOptions{Unit: input.CodePoints, Expectations: true, MissingTokens: v.Limit, Tune: state.Tuning{MinRec: 1}}
	if v.Bytes {
		o.Unit = input.Bytes
	}
	d, err := prog.NewDocumentWith("main", string(v.First), o)
	if err != nil {
		t.Fatalf("new document: %v", err)
	}
	var out []uint64
	parse := func(opts ...sxirruntime.ParseOption) {
		var units uint64
		po := o
		engine.Apply(&po, append(opts, sxirruntime.WithComputeReport(&units)))
		d.ParseWith(po)
		out = append(out, units)
	}
	parse()
	for _, e := range v.Edits {
		if err := d.Edit(e.Start, e.End, string(e.Text)); err != nil {
			t.Fatalf("edit %+v: %v", e, err)
		}
		parse()
	}
	parse()
	parse(sxirruntime.WithTrace(func(instrument.TraceEvent) {}))
	parse()
	return out
}

// documentMinimalEdit returns the edit, in bytes or code points, that replaces the characters
// between the common prefix and the common suffix of from and to. Every invalid UTF-8 byte is a
// character.
func documentMinimalEdit(from, to string, bytes bool) (start, end int, text string) {
	chars := func(s string) []string {
		var out []string
		for s != "" {
			_, n := utf8.DecodeRuneInString(s)
			out, s = append(out, s[:n]), s[n:]
		}
		return out
	}
	a, b := chars(from), chars(to)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	pos := func(chars []string) int {
		if bytes {
			return len(strings.Join(chars, ""))
		}
		return len(chars)
	}
	return pos(a[:p]), pos(a[:len(a)-s]), strings.Join(b[p:len(b)-s], "")
}
