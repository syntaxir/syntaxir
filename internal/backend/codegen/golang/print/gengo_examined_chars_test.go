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

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// TestGeneratedGoExaminedCharacterComparisons checks the examined end of the call of atom at 0 in each
// case of codegentest.ExaminedChars, as the typed parse, the Node parse and the recognizer of the
// generated Go parser report it, and the typed parse of its Document.
func TestGeneratedGoExaminedCharacterComparisons(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	chars := codegentest.ExaminedChars()
	var cases []codegentest.Case
	for _, c := range chars {
		cases = append(cases, codegentest.Case{Name: c.Name, Src: c.Src(), Inputs: []string{c.Input}})
	}
	codegentest.EachMode(t, func(t *testing.T) {
		seen := map[string]int{}
		check := func(c codegentest.ExaminedChar, entry string, events []instrument.TraceEvent) {
			seen[entry]++
			found := false
			for _, e := range events {
				if e.Kind == instrument.TraceExit && e.Rule == "atom" && e.Pos == 0 {
					found = true
					if e.Matched != c.Matched || e.Examined != c.Examined {
						t.Errorf("%s, %s: atom@0 matched %v, examined %d; want matched %v, examined %d", c.Name, entry, e.Matched, e.Examined, c.Matched, c.Examined)
					}
				}
			}
			if !found {
				t.Errorf("%s, %s: no exit of atom at 0", c.Name, entry)
			}
		}
		for _, r := range runExaminedProbes(t, cases, codegentest.LibCopy(), nil) {
			if c := chars[r.Case]; r.Bytes == c.Bytes {
				check(c, r.Entry, r.Events)
			}
		}
		for i, events := range runExaminedCharDocuments(t, chars) {
			check(chars[i], "document", events)
		}
		for _, entry := range []string{"value", "parse", "recognize", "document"} {
			if seen[entry] != len(chars) {
				t.Errorf("%s ran on %d cases, want %d", entry, seen[entry], len(chars))
			}
		}
	})
}

// examinedCharDocumentMain parses each case through the typed Document of its package, in its unit,
// and prints the exit and skipped events of the observed parse as one JSON line.
const examinedCharDocumentMain = `package main

import (
	"encoding/json"
	"fmt"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"
IMPORTS)

type run func(...sxirruntime.ParseOption) error

func main() {
	for _, r := range []run{
RUNS	} {
		var events []sxirinstrument.TraceEvent
		_ = r(sxirruntime.WithTrace(func(ev sxirinstrument.TraceEvent) {
			if ev.Kind != sxirinstrument.TraceEnter {
				events = append(events, ev)
			}
		}))
		b, _ := json.Marshal(events)
		fmt.Println(string(b))
	}
}

func unit(bytes bool) sxirruntime.ParseOption {
	if bytes {
		return sxirruntime.WithUnit(sxirinput.Bytes)
	}
	return sxirruntime.WithUnit(sxirinput.CodePoints)
}
`

// runExaminedCharDocuments generates each case with a typed Document and observability, parses its
// input once through the Document, and returns the events of each parse.
func runExaminedCharDocuments(t *testing.T, chars []codegentest.ExaminedChar) [][]instrument.TraceEvent {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoModAt("examinedchardoc", codegentest.LibCopy())), 0o644); err != nil {
		t.Fatal(err)
	}
	var imports, runs strings.Builder
	for i, c := range chars {
		g, err := parseTest(c.Src())
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		pkg := fmt.Sprintf("d%d", i)
		codegentest.GeneratePackageIn(t, dir, "examinedchardoc", pkg, g, common.GenOptions{Start: "main", Types: true, Document: true, Observability: true})
		fmt.Fprintf(&imports, "\t%s \"examinedchardoc/%[1]s\"\n", pkg)
		fmt.Fprintf(&runs, `		func(o ...sxirruntime.ParseOption) error {
			d, err := %s.NewDocument(%q, unit(%v))
			if err != nil {
				return err
			}
			_, err = d.ParseValue(append(o, unit(%[3]v))...)
			return err
		},
`, pkg, c.Input, c.Bytes)
	}
	main := strings.NewReplacer("IMPORTS", imports.String(), "RUNS", runs.String()).Replace(examinedCharDocumentMain)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(chars) {
		t.Fatalf("go run: %d reports, want %d\n%s", len(lines), len(chars), out)
	}
	all := make([][]instrument.TraceEvent, len(lines))
	for i, l := range lines {
		if err := json.Unmarshal([]byte(l), &all[i]); err != nil {
			t.Fatalf("go run: malformed report %q: %v", l, err)
		}
	}
	return all
}
