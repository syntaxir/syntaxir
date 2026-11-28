// Package reftest compares the parsers of a build with a reference tag (pass.ReferenceControl) with
// those of the default build. A reference tag selects the reference form of a mechanism for the whole
// program, so the two forms never run in one process: each build writes a transcript, and the
// transcripts are compared (hack/test-reference-tags.sh). A transcript has two parts. Its
// configuration lists the files of the build that a reference tag selects, so it differs between the
// builds, which shows that the tag took effect, also in the copy of the Go library that the generated
// parsers are built with. Its results are those of the parsers of the control's backends on its
// witnesses: trees, typed values, syntax and recovered errors, recognition and exact compute units,
// which must not differ.
package reftest

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/opttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// A Witness is a case that a reference control names (pass.ReferenceControl.Witnesses): a grammar with
// inputs, parsed and recognized from scratch, a stream, whose inputs are read in chunks of several
// sizes, or a Document session, whose edits and parses run on a Document.
type Witness struct {
	Name     string
	Case     codegentest.Case
	Stream   bool
	Document *codegentest.DocumentSession
}

// streamPrefix and documentPrefix begin the names of the witnesses that are streams and Document
// sessions; the other witnesses are the cases of the backend-claims corpus (opttest.Corpus).
const (
	streamPrefix   = "stream "
	documentPrefix = "document "
)

// streams are the stream witnesses: records, with a line that fails and one that recovers, the
// text of a head read after the stream, and a start rule whose action a stream does not evaluate.
var streams = []codegentest.Case{
	{Name: "records", Src: codegentest.Records, Inputs: []string{"#records\n", "#records\na=1\nbc=22\n", "#records\na=1\n=2\nb=3\n", "#records\n" + strings.Repeat("ab=12\n", 40)}},
	{Name: "records recover", Src: codegentest.RecordsRecover, Inputs: []string{"#records\na=1\nzz\nb=2\n", "#records\n??\n", "#records\n" + strings.Repeat("a=1\nx\n", 20)}},
	{Name: "text commit", Src: codegentest.StreamTextCommitGrammar, Inputs: codegentest.StreamTextCommitInputs},
	{Name: "start action", Src: codegentest.StreamStartActionGrammar, Inputs: codegentest.StreamStartActionInputs},
}

// streamChunks are the sizes of the reads of a stream witness: one byte, a few, and the whole input.
var streamChunks = []int{1, 3, 1 << 20}

// Lookup returns the witness of the name.
func Lookup(name string) (Witness, bool) {
	if s, ok := strings.CutPrefix(name, streamPrefix); ok {
		i := slices.IndexFunc(streams, func(c codegentest.Case) bool { return c.Name == s })
		if i < 0 {
			return Witness{}, false
		}
		return Witness{Name: name, Case: streams[i], Stream: true}, true
	}
	if s, ok := strings.CutPrefix(name, documentPrefix); ok {
		i := slices.IndexFunc(codegentest.DocumentSessions, func(d codegentest.DocumentSession) bool { return d.Name == s })
		if i < 0 {
			return Witness{}, false
		}
		return Witness{Name: name, Document: &codegentest.DocumentSessions[i]}, true
	}
	corpus := opttest.Corpus()
	i := slices.IndexFunc(corpus, func(c codegentest.Case) bool { return c.Name == name })
	if i < 0 {
		return Witness{}, false
	}
	return Witness{Name: name, Case: corpus[i]}, true
}

// Control returns the reference control of the tag.
func Control(tag string) (pass.ReferenceControl, bool) {
	rcs := pass.ReferenceControls()
	i := slices.IndexFunc(rcs, func(r pass.ReferenceControl) bool { return r.Tag == tag })
	if i < 0 {
		return pass.ReferenceControl{}, false
	}
	return rcs[i], true
}

// Configuration returns the files of this module, and of the copy of the Go library that generated
// parsers are built with, that a build constraint naming a tag of the sxir_ family selects in this
// build, one "package file" per line: the build tags of the test process (GOFLAGS) select them, as
// they select the files of the generated parsers' builds. The tags of the test tiers and of the
// feature witness, which every build of these tests sets alike, are left out.
func Configuration(t testing.TB) string {
	t.Helper()
	// The module of the checkout may be in a workspace, which refuses -mod=mod, the flag that the
	// tests add for the temporary modules of generated parsers; the tags stay.
	goflags := slices.DeleteFunc(strings.Fields(os.Getenv("GOFLAGS")), func(f string) bool { return strings.HasPrefix(f, "-mod=") })
	var lines []string
	for _, dir := range []string{moduleRoot(t), codegentest.LibCopy()} {
		cmd := exec.Command(goBin(), "list", "-deps", "-f", "{{.ImportPath}}|{{.Dir}}|{{join .GoFiles \" \"}}", "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS="+strings.Join(goflags, " "))
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list in %s: %v\n%s", dir, err, stderr.String())
		}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f := strings.SplitN(l, "|", 3)
			if len(f) != 3 || !strings.HasPrefix(f[0], "syntaxir.dev/sxir") {
				continue
			}
			for _, name := range strings.Fields(f[2]) {
				if constraint := buildConstraint(t, filepath.Join(f[1], name)); selectsForm(constraint) {
					lines = append(lines, f[0]+" "+name)
				}
			}
		}
	}
	slices.Sort(lines)
	return strings.Join(slices.Compact(lines), "\n") + "\n"
}

// buildConstraint returns the expression of the //go:build line of the file, or "".
func buildConstraint(t testing.TB, path string) string {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if c, ok := strings.CutPrefix(l, "//go:build "); ok {
			return c
		}
		if strings.HasPrefix(l, "package ") {
			break
		}
	}
	return ""
}

// selectsForm reports whether a build constraint names a tag of the sxir_ family other than those
// of the test tiers and of the feature witness.
func selectsForm(constraint string) bool {
	for _, w := range strings.FieldsFunc(constraint, func(r rune) bool { return !(r == '_' || 'a' <= r && r <= 'z' || '0' <= r && r <= '9') }) {
		if strings.HasPrefix(w, "sxir_") && !slices.Contains([]string{"sxir_long", "sxir_release", "sxir_feature_witness"}, w) {
			return true
		}
	}
	return false
}

// Results returns the results of the parsers of the backends of rc on its witnesses, one line for each
// parse, in an order that does not depend on the build.
func Results(t *testing.T, rc pass.ReferenceControl) string {
	t.Helper()
	var b strings.Builder
	var generated []Witness
	for _, name := range rc.Witnesses {
		w, ok := Lookup(name)
		if !ok {
			t.Fatalf("reference control %s: the witness %q is not a case of the corpus, a stream or a Document session", rc.Tag, name)
		}
		for _, be := range engineBackends {
			if rc.Backends.Has(be.backend) {
				engineResults(t, &b, w, be.name, be.engine)
			}
		}
		generated = append(generated, w)
	}
	if rc.Backends&pass.GeneratedGo != 0 {
		goResults(t, &b, generated)
	}
	if rc.Backends.Has(pass.BackendTypeScript) {
		tsResults(t, &b, generated)
	}
	return b.String()
}

// engineBackends are the engine backends with the names that the transcript gives them.
var engineBackends = []struct {
	backend pass.Backend
	name    string
	engine  engine.Backend
}{
	{pass.BackendClosure, "closure", engine.Closure},
	{pass.BackendRecursiveVM, "recursive VM", engine.Bytecode},
	{pass.BackendIterativeVM, "iterative VM", engine.BytecodeIterative},
}

// units are the position units of every parse, with their names.
var units = []struct {
	name string
	unit sxirinput.PositionUnit
}{{"codepoints", sxirinput.CodePoints}, {"bytes", sxirinput.Bytes}}

// engineResults writes the results of the engine backend be on the witness w.
func engineResults(t *testing.T, b *strings.Builder, w Witness, name string, be engine.Backend) {
	t.Helper()
	if w.Document != nil {
		documentResults(t, b, w, name, be)
		return
	}
	prog := enginetest.MustCompile(t, w.Case.Src)
	for _, in := range w.Case.Inputs {
		for _, u := range units {
			at := fmt.Sprintf("%s | %s | %s | %q", w.Name, name, u.name, in)
			if w.Stream {
				for _, size := range streamChunks {
					var spent uint64
					var elems []string
					err := prog.ParseStreamWith("main", chunked(in, size), func(n *value.Node) error {
						elems = append(elems, n.String())
						return nil
					}, engine.ParseOptions{Unit: u.unit, Backend: be, Expectations: true, ComputeReport: &spent})
					fmt.Fprintf(b, "%s | stream %d | %q %v units %d\n", at, size, elems, err, spent)
				}
				continue
			}
			var spent, recognized uint64
			n, err := prog.ParseWith("main", in, engine.ParseOptions{Unit: u.unit, Backend: be, Expectations: true, ComputeReport: &spent})
			fmt.Fprintf(b, "%s | parse | %s views %q units %d\n", at, codegentest.ResultJSON(n, err), codegentest.NodeViews(n), spent)
			_, err = prog.ParseWith("main", in, engine.ParseOptions{Unit: u.unit, Backend: be, Expectations: true, Recognize: true, ComputeReport: &recognized})
			fmt.Fprintf(b, "%s | recognize | %v units %d\n", at, err, recognized)
		}
	}
}

// chunked returns a reader of s that returns at most size bytes at a time.
func chunked(s string, size int) io.Reader {
	if size == 1 {
		return iotest.OneByteReader(strings.NewReader(s))
	}
	return &chunkReader{s: s, size: size}
}

type chunkReader struct {
	s    string
	size int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p[:min(len(p), r.size)], r.s)
	r.s = r.s[n:]
	return n, nil
}

// documentResults writes the results of the Document session of w on the engine backend be: for each
// parse, the statistics, the compute units and the result, and the same of a snapshot of the text.
func documentResults(t *testing.T, b *strings.Builder, w Witness, name string, be engine.Backend) {
	t.Helper()
	s := w.Document
	prog := enginetest.MustCompile(t, s.Grammar)
	unit := sxirinput.CodePoints
	if s.Bytes {
		unit = sxirinput.Bytes
	}
	d, err := prog.NewDocumentWith("main", s.Text, engine.ParseOptions{Unit: unit, Backend: be, Expectations: true})
	if err != nil {
		t.Fatalf("%s: %v", w.Name, err)
	}
	k := 0
	for _, step := range s.Steps {
		if !step.Parse {
			for range max(step.Times, 1) {
				if err := d.Edit(step.Start, step.End, step.Text); err != nil {
					t.Fatalf("%s: %v", w.Name, err)
				}
			}
			continue
		}
		at := fmt.Sprintf("%s | %s | parse %d", w.Name, name, k)
		var spent uint64
		n, err := d.ParseWith(engine.ParseOptions{Unit: unit, Backend: be, Expectations: true, ComputeReport: &spent})
		st := d.Stats()
		fmt.Fprintf(b, "%s | %s evaluated %d reused %d units %d\n", at, codegentest.ResultJSON(n, err), st.Evaluated, st.Reused, spent)
		snap, err := d.SnapshotWith(engine.ParseOptions{Unit: unit, Backend: be, Expectations: true})
		if err != nil || snap == nil {
			fmt.Fprintf(b, "%s | snapshot %v\n", at, err)
		} else {
			ss := snap.Stats()
			fmt.Fprintf(b, "%s | snapshot %s evaluated %d reused %d\n", at, codegentest.ResultJSON(snap.Materialize(), snap.Error()), ss.Evaluated, ss.Reused)
		}
		k++
	}
}

// goResults writes the results of the generated Go parsers on the witnesses: of the Node, typed and
// recognizing parsers on the inputs of the cases and streams, as the program of goProgram prints
// them, and of the Documents of the sessions.
func goResults(t *testing.T, b *strings.Builder, ws []Witness) {
	t.Helper()
	var cases []Witness
	for _, w := range ws {
		if w.Document != nil {
			for _, l := range strings.Split(strings.TrimSpace(codegentest.RunDocumentSessionGo(t, *w.Document)), "\n") {
				fmt.Fprintf(b, "%s | Go | %s\n", w.Name, l)
			}
			continue
		}
		cases = append(cases, w)
	}
	if len(cases) > 0 {
		b.WriteString(goProgram(t, cases))
	}
}

// tsResults writes the results of the generated TypeScript parsers on the witnesses.
func tsResults(t *testing.T, b *strings.Builder, ws []Witness) {
	t.Helper()
	node := tooltest.RequireNode(t)
	var cases []codegentest.Case
	for _, w := range ws {
		if w.Document != nil {
			for _, l := range strings.Split(strings.TrimSpace(codegentest.RunDocumentSessionTS(t, *w.Document)), "\n") {
				fmt.Fprintf(b, "%s | TypeScript | %s\n", w.Name, l)
			}
			continue
		}
		cases = append(cases, w.Case)
	}
	if len(cases) > 0 {
		for _, l := range strings.Split(strings.TrimSpace(codegentest.TSParsersOutputs(t, node, cases, common.GenOptions{Types: true}, nil)), "\n") {
			fmt.Fprintf(b, "TypeScript | %s\n", l)
		}
	}
}

// goProgram generates, for each witness, a typed Go parser with limit controls (and a stream parser
// for a stream) and a recognize-only parser, builds a program that runs them on its inputs in both
// units, and returns what it prints: the node, its error and compute units, the typed value with its
// dynamic type, its error and units, the recognitions, and the elements of each chunked stream read.
func goProgram(t *testing.T, ws []Witness) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoMod("reftest")), 0o644); err != nil {
		t.Fatal(err)
	}
	var imports, runs strings.Builder
	var inputs [][]string
	for i, w := range ws {
		g, err := compiletest.Parse(w.Case.Src)
		if err != nil {
			t.Fatalf("%s: %v", w.Name, err)
		}
		codegentest.GeneratePackageIn(t, dir, "reftest", fmt.Sprintf("g%d", i), g, common.GenOptions{Start: "main", Types: true, Recognize: true, Stream: w.Stream})
		codegentest.GeneratePackageIn(t, dir, "reftest", fmt.Sprintf("r%d", i), g, common.GenOptions{Start: "main", RecognizeOnly: true})
		fmt.Fprintf(&imports, "\tg%d \"reftest/g%[1]d\"\n\tr%[1]d \"reftest/r%[1]d\"\n", i)
		stream := ""
		if w.Stream {
			stream = fmt.Sprintf(`
		for _, size := range []int{%s} {
			var spent uint64
			var elems []string
			err := g%d.ParseStream(chunked(s, size), func(n *g%[2]d.Node) error { elems = append(elems, n.String()); return nil }, g%[2]d.WithUnit(u), sxirruntime.WithComputeReport(&spent))
			fmt.Printf("%%s | stream %%d | %%q %%v units %%d\n", at, size, elems, err, spent)
		}`, strings.Trim(strings.Join(strings.Fields(fmt.Sprint(streamChunks)), ", "), "[]"), i)
		}
		fmt.Fprintf(&runs, `	func(at, s string, b bool) {
		u, ru := g%d.CodePoints, r%[1]d.CodePoints
		if b {
			u, ru = g%[1]d.Bytes, r%[1]d.Bytes
		}
		var spent, typed, recognized, only uint64
		n, err := g%[1]d.Parse(s, g%[1]d.WithUnit(u), sxirruntime.WithComputeReport(&spent))
		fmt.Printf("%%s | parse | %%s units %%d\n", at, result(n, err), spent)
		v, err := g%[1]d.ParseValue(s, g%[1]d.WithUnit(u), sxirruntime.WithComputeReport(&typed))
		fmt.Printf("%%s | value | %%T %%s units %%d\n", at, v, result(v, err), typed)
		err = g%[1]d.Recognize(s, g%[1]d.WithUnit(u), sxirruntime.WithComputeReport(&recognized))
		fmt.Printf("%%s | recognize | %%v units %%d\n", at, err, recognized)
		err = r%[1]d.Recognize(s, r%[1]d.WithUnit(ru), sxirruntime.WithComputeReport(&only))
		fmt.Printf("%%s | recognize only | %%v units %%d\n", at, err, only)%s
	},
`, i, stream)
		inputs = append(inputs, w.Case.Inputs)
	}
	var names strings.Builder
	for _, w := range ws {
		fmt.Fprintf(&names, "\t%q,\n", w.Name)
	}
	var data strings.Builder
	for _, in := range inputs {
		fmt.Fprintf(&data, "\t{")
		for _, s := range in {
			fmt.Fprintf(&data, "%q, ", s)
		}
		fmt.Fprintf(&data, "},\n")
	}
	main := `package main

import (
	"encoding/json"
	"fmt"
	"io"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
` + imports.String() + `)

var names = []string{
` + names.String() + `}

var inputs = [][]string{
` + data.String() + `}

var runs = []func(at, s string, b bool){
` + runs.String() + `}

// result is the JSON of a value and its error.
func result(v any, err error) string {
	out := map[string]any{"value": v}
	if err != nil {
		out["err"] = err.Error()
	}
	data, _ := json.Marshal(out)
	return string(data)
}

type chunkReader struct {
	s    string
	size int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p[:min(len(p), r.size)], r.s)
	r.s = r.s[n:]
	return n, nil
}

func chunked(s string, size int) io.Reader { return &chunkReader{s: s, size: size} }

func main() {
	for i, in := range inputs {
		for _, s := range in {
			for _, b := range []bool{false, true} {
				unit := "codepoints"
				if b {
					unit = "bytes"
				}
				runs[i](fmt.Sprintf("%s | Go | %s | %q", names[i], unit, s), s, b)
			}
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBin(), "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	return string(out)
}

// goBin is the go command of the toolchain that runs the test.
func goBin() string { return filepath.Join(runtime.GOROOT(), "bin", "go") }

// moduleRoot returns the root of this module.
func moduleRoot(t testing.TB) string {
	t.Helper()
	out, err := exec.Command(goBin(), "env", "GOMOD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(strings.TrimSpace(string(out)))
}
