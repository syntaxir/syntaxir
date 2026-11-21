package print_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// textScript is a Document construction followed by edits, as the engine ran it. The texts are bytes
// so that JSON keeps invalid UTF-8 as it is.
type textScript struct {
	Bytes   bool
	Initial []byte
	Edits   []textScriptEdit
}

type textScriptEdit struct {
	Start, End int
	Text       []byte
}

// textStep is the outcome of construction (the first step) or of one edit: whether it was accepted,
// the text after it, and whether a parse of that text succeeded.
type textStep struct {
	Accepted bool
	Text     []byte
	Parsed   bool
}

// textPieces make up initial texts and replacements: valid characters of each width, a literal
// U+FFFD, continuation bytes, incomplete prefixes, the rest of a prefix, an invalid lead byte and an
// encoded surrogate.
var textPieces = []string{"", "a", "b", " ", "\n", "é", "日", "😀", "€", "\uFFFD", "\x80", "\xa9", "\xc3", "\xe6\xbc", "\xe2\x82", "\xac", "\xf0\x9f\x98", "\xff", "\xed\xa0\x80"}

func randomPieces(rng *rand.Rand, n int) string {
	var b strings.Builder
	for range rng.Intn(n + 1) {
		b.WriteString(textPieces[rng.Intn(len(textPieces))])
	}
	return b.String()
}

// engineTextScript runs a random Document session on the engine in unit and returns it with the outcome
// of each step. Edit ranges are drawn from the engine's current text, including ranges outside it and,
// in Bytes, ends inside a character, so that the session also has rejected edits.
func engineTextScript(t *testing.T, prog *engine.Program, rng *rand.Rand, unit input.PositionUnit, edits int) (textScript, []textStep) {
	t.Helper()
	s := textScript{Bytes: unit == input.Bytes, Initial: []byte(randomPieces(rng, 6))}
	d, err := prog.NewDocumentWith("main", string(s.Initial), engine.ParseOptions{Unit: unit})
	if err != nil {
		t.Fatal(err)
	}
	step := func(accepted bool) textStep {
		_, err := d.Parse()
		return textStep{Accepted: accepted, Text: []byte(d.Text()), Parsed: err == nil}
	}
	steps := []textStep{step(true)}
	for range edits {
		length := len(d.Text())
		if unit == input.CodePoints {
			length = utf8.RuneCountInString(d.Text())
		}
		start := rng.Intn(length + 2)
		end := start + rng.Intn(4) - 1
		e := textScriptEdit{Start: start, End: end, Text: []byte(randomPieces(rng, 3))}
		s.Edits = append(s.Edits, e)
		steps = append(steps, step(d.Edit(e.Start, e.End, string(e.Text)) == nil))
	}
	return s, steps
}

// TestGeneratedDocumentTextAgreesWithEngine replays random Document sessions of the engine on a
// generated Go Document and compares, after construction and after each edit, whether the edit was
// accepted, the text that Text returns and whether a parse succeeds. In CodePoints both normalize
// invalid UTF-8 in the initial text and in replacements, one U+FFFD per invalid byte; in Bytes both
// keep the exact bytes.
func TestGeneratedDocumentTextAgreesWithEngine(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedDocumentTextAgreesWithEngine)
}

func testGeneratedDocumentTextAgreesWithEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated Document")
	}
	const src = "rule main = (word / \" \" / \"\\n\")* $$\nrule word = [a-zé日😀€]+"
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	prog := mustCompile(t, src)
	const seeds, edits = 8, 400
	var scripts []textScript
	var want [][]textStep
	for seed := range seeds {
		rng := rand.New(rand.NewSource(int64(seed)))
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			s, steps := engineTextScript(t, prog, rng, unit, edits)
			scripts = append(scripts, s)
			want = append(want, steps)
		}
	}

	dir := t.TempDir()
	code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "probe", Start: "main", Document: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(scripts)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		"go.mod":          []byte(codegentest.GoMod("textprobe")),
		"probe/parser.go": code,
		"main.go":         []byte(textProbeMain),
		"scripts.json":    data,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "scripts.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	var got [][]textStep
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("go run: malformed output: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("generated Document ran %d scripts, want %d", len(got), len(want))
	}

	var compared, rejected, normalized int
	var diffs []string
	for i, s := range scripts {
		unit := input.CodePoints
		if s.Bytes {
			unit = input.Bytes
		}
		for j, w := range want[i] {
			compared++
			if !w.Accepted {
				rejected++
			}
			if j > 0 && w.Accepted && unit == input.CodePoints && !utf8.Valid(s.Edits[j-1].Text) {
				normalized++
			}
			if g := got[i][j]; g.Accepted != w.Accepted || string(g.Text) != string(w.Text) || g.Parsed != w.Parsed {
				diffs = append(diffs, fmt.Sprintf("script %d (%v) step %d: generated accepted=%v text %q parsed=%v; engine accepted=%v text %q parsed=%v",
					i, unit, j, g.Accepted, g.Text, g.Parsed, w.Accepted, w.Text, w.Parsed))
			}
		}
	}
	t.Logf("%d steps compared, %d rejected edits, %d accepted CodePoints edits with invalid UTF-8; %d divergences", compared, rejected, normalized, len(diffs))
	if rejected == 0 || normalized == 0 {
		t.Errorf("the sessions have %d rejected edits and %d normalized replacements; want some of each", rejected, normalized)
	}
	for i, d := range diffs {
		if i == 10 {
			t.Errorf("%d more divergences", len(diffs)-i)
			break
		}
		t.Error(d)
	}
}

// textProbeMain replays the scripts of its argument on the generated Document and prints the steps.
const textProbeMain = `package main

import (
	"encoding/json"
	"os"

	. "textprobe/probe"
)

type script struct {
	Bytes   bool
	Initial []byte
	Edits   []struct {
		Start, End int
		Text       []byte
	}
}

type step struct {
	Accepted bool
	Text     []byte
	Parsed   bool
}

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var scripts []script
	if err := json.Unmarshal(data, &scripts); err != nil {
		panic(err)
	}
	var out [][]step
	for _, s := range scripts {
		unit := CodePoints
		if s.Bytes {
			unit = Bytes
		}
		d, err := NewDocument(string(s.Initial), WithUnit(unit))
		if err != nil {
			panic(err)
		}
		record := func(accepted bool) step {
			_, err := d.Parse()
			return step{Accepted: accepted, Text: []byte(d.Text()), Parsed: err == nil}
		}
		steps := []step{record(true)}
		for _, e := range s.Edits {
			steps = append(steps, record(d.Edit(e.Start, e.End, string(e.Text)) == nil))
		}
		out = append(out, steps)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		panic(err)
	}
}
`
