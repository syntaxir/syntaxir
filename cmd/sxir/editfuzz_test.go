package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/editfuzz"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

const editFuzzGrammar = "@start\nrule main = (word / other)* $$\nrule word = [a-z]+ #atomic\nrule other = .\n"

func TestEditFuzz(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "g.sxir")
	os.WriteFile(g, []byte(editFuzzGrammar), 0o644)
	var out strings.Builder
	if err := run([]string{"edit-fuzz", "-g", g, "-sequences", "3", "-operations", "8"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	checkOK(t, out.String(), 3, "closure/codepoints", "closure/bytes", "bytecode/codepoints", "bytecode/bytes",
		"bytecode-iterative/codepoints", "bytecode-iterative/bytes")

	corpus := filepath.Join(dir, "corpus")
	os.Mkdir(corpus, 0o755)
	os.WriteFile(filepath.Join(corpus, "b"), []byte("x\xff"), 0o644)
	os.WriteFile(filepath.Join(corpus, "a"), []byte("é!"), 0o644)
	os.Mkdir(filepath.Join(corpus, "sub"), 0o755)
	os.Symlink(filepath.Join(corpus, "a"), filepath.Join(corpus, "c"))
	os.WriteFile(filepath.Join(corpus, ".hidden"), []byte("no"), 0o644)
	out.Reset()
	if err := run([]string{"edit-fuzz", "-g", g, "-backend", "bytecode-iterative", "-unit", "bytes", "-corpus", corpus, "-seed", "4"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	checkOK(t, out.String(), 100, "bytecode-iterative/bytes")
	texts, err := readCorpus(corpus)
	if err != nil || len(texts) != 3 || string(texts[0]) != "é!" || string(texts[1]) != "x\xff" || string(texts[2]) != "é!" {
		t.Fatalf("corpus %q, %v", texts, err)
	}
}

// checkOK checks that out holds the line of a successful run for each of the backend/unit pairs, in order,
// each with the number of sequences and at least two compared checks for each sequence, at its initial text
// and at its last operation, which is a checkpoint, and no check skipped at the compute limit.
func checkOK(t *testing.T, out string, sequences int, pairs ...string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(pairs) {
		t.Fatalf("got:\n%s", out)
	}
	for i, pair := range pairs {
		var compared int
		if _, err := fmt.Sscanf(lines[i], "ok "+pair+": %d sequences, %d checks compared, 0 skipped at the compute limit", new(int), &compared); err != nil ||
			!strings.HasPrefix(lines[i], fmt.Sprintf("ok %s: %d sequences, ", pair, sequences)) ||
			!strings.HasSuffix(lines[i], " checks compared, 0 skipped at the compute limit") || compared < 2*sequences {
			t.Errorf("line %d: %q, want %s with %d sequences and at least %d compared checks", i, lines[i], pair, sequences, 2*sequences)
		}
	}
}

func TestEditFuzzReplay(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "g.sxir")
	os.WriteFile(g, []byte(editFuzzGrammar), 0o644)
	seq := &editfuzz.Sequence{GrammarSHA256: editfuzz.GrammarHash([]byte(editFuzzGrammar)), Start: "main",
		Backend: editfuzz.Bytecode, Unit: input.Bytes, Initial: []byte("ab"), Operations: []editfuzz.Operation{
			{Kind: editfuzz.Edit, Start: 2, End: 2, Text: []byte("\xc3")}, {Kind: editfuzz.Checkpoint},
			{Kind: editfuzz.Edit, Start: 3, End: 3, Text: []byte("\xa9")}, {Kind: editfuzz.Undo}, {Kind: editfuzz.Checkpoint}}}
	path, err := editfuzz.Save(dir, seq)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := run([]string{"edit-fuzz", "-g", g, "-replay", path}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ok "+path+": 5 operations\n" {
		t.Fatalf("got %q", out.String())
	}
	// A passing sequence cannot be minimized.
	if err := run([]string{"edit-fuzz", "-g", g, "-replay", path, "-minimize"}, nil, &out); err == nil || !strings.Contains(err.Error(), "does not fail") {
		t.Fatalf("got %v", err)
	}
	other := filepath.Join(dir, "other.sxir")
	os.WriteFile(other, []byte(editFuzzGrammar+"\n"), 0o644)
	if err := run([]string{"edit-fuzz", "-g", other, "-replay", path}, nil, &out); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("other grammar: %v", err)
	}
	data, _ := os.ReadFile(path)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	raw["version"] = 2
	data, _ = json.Marshal(raw)
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, data, 0o644)
	if err := run([]string{"edit-fuzz", "-g", g, "-replay", bad}, nil, &out); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("version 2: %v", err)
	}
}

func TestEditFuzzErrors(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "g.sxir")
	os.WriteFile(g, []byte(editFuzzGrammar), 0o644)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "-g is required"},
		{[]string{"-g", g, "x"}, "unexpected arguments"},
		{[]string{"-g", filepath.Join(dir, "none.sxir")}, "no such file"},
		{[]string{"-g", g, "-backend", "default"}, "unknown backend"},
		{[]string{"-g", g, "-unit", "rune"}, "unknown unit"},
		{[]string{"-g", g, "-sequences", "-1"}, "negative"},
		{[]string{"-g", g, "-s", "nope"}, "nope"},
		{[]string{"-g", g, "-corpus", filepath.Join(dir, "none")}, "no such file"},
		{[]string{"-g", g, "-corpus", t.TempDir()}, "no files"},
		{[]string{"-g", g, "-replay", filepath.Join(dir, "none.json")}, "no such file"},
		{[]string{"-g", g, "-minimize"}, "-minimize needs -save-failures"},
		{[]string{"-g", g, "-replay", "x.json", "-backend", "closure", "-seed", "2"}, "-replay does not take -backend, -seed"},
		{[]string{"-g", g, "-replay", "x.json", "-save-failures", dir}, "only with -minimize"},
	} {
		var out strings.Builder
		if err := run(append([]string{"edit-fuzz"}, tc.args...), nil, &out); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want an error containing %q", tc.args, err, tc.want)
		}
	}
}
