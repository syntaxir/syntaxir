// Command document-retained measures the heap that a parsed Document keeps against the heap of the
// tree that Parse returns for the same text: the Document memory of an editor, which holds the text,
// its tables and the memo entries besides the tree, over the tree alone.
//
// It builds one of the two, -what tree or -what document, for a subject of -lines lines, and prints
// the heap that it keeps after forced collections as JSON. Run each measurement in a process of its
// own with GOGC=off and GOMAXPROCS=1 (go run ./bench/metrics collect -parts retained does), so that
// the collector runs only when forced and no per-P cache holds a value in one run and not in another.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"syntaxir.dev/sxir/internal/bench/inputs"
	gcsvdoc "syntaxir.dev/sxir/internal/bench/parsers/csvdoc"
	gminilangdoc "syntaxir.dev/sxir/internal/bench/parsers/minilangdoc"
	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// settingsGrammar is the grammar of "key = value" lines of the incremental parsing guide
// (docs/guide/incremental-performance.md), one rule per line, on the engine.
const settingsGrammar = `@start
rule main =  (setting / blank)* $$
rule setting =  key " = " value "\n"
rule key =  [a-z_]+ #atomic
rule value =  [^\n]+ #atomic
rule blank =  "\n" `

// subject is a grammar to measure: its text of at least a number of lines, and how to parse it into
// a tree and into a Document.
type subject struct {
	input    func(lines int) string
	parse    func(text string) (any, error)
	document func(text string) (sxirruntime.Document, error)
}

func subjects() (map[string]subject, error) {
	settings, err := compiler.Compile(grammar.FromString("settings.sxir", settingsGrammar), compiler.WithStart("main"))
	if err != nil {
		return nil, err
	}
	return map[string]subject{
		// The engine's Document, on the closure backend, the subject of the guide.
		"settings": {
			input: func(lines int) string {
				var b strings.Builder
				for i := range lines {
					fmt.Fprintf(&b, "key_%c = value %d\n", 'a'+i%26, i)
				}
				return b.String()
			},
			parse:    func(text string) (any, error) { return settings.Parse(text) },
			document: func(text string) (sxirruntime.Document, error) { return settings.NewDocument(text) },
		},
		// The generated Documents of the Document edit-latency benchmarks (bench/document_latency_bench_test.go).
		"csv": {
			input:    func(lines int) string { return inputs.CSVInput(lines - 1) },
			parse:    func(text string) (any, error) { return gcsvdoc.Parse(text) },
			document: func(text string) (sxirruntime.Document, error) { return gcsvdoc.NewDocument(text) },
		},
		"minilang": {
			input:    func(lines int) string { return inputs.MinilangInput((lines+12)/13, 0) },
			parse:    func(text string) (any, error) { return gminilangdoc.Parse(text) },
			document: func(text string) (sxirruntime.Document, error) { return gminilangdoc.NewDocument(text) },
		},
	}, nil
}

type result struct {
	Subject         string `json:"subject"`
	What            string `json:"what"`
	Lines           int    `json:"lines"`
	InputBytes      int    `json:"input_bytes"`
	RetainedBytes   int64  `json:"retained_bytes"`
	RetainedObjects int64  `json:"retained_objects"`
}

func run() error {
	name := flag.String("subject", "settings", "settings, csv or minilang")
	what := flag.String("what", "document", "tree (the result of Parse) or document (a Document and the tree of its Parse)")
	lines := flag.Int("lines", 100000, "lines of the text")
	flag.Parse()
	r, err := measure(*name, *what, *lines)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}

// measure builds the tree or the Document of a subject's text of lines lines and returns the heap
// that it keeps after a collection, beyond the heap before it, which holds the text.
func measure(name, what string, lines int) (result, error) {
	all, err := subjects()
	if err != nil {
		return result{}, err
	}
	s, ok := all[name]
	if !ok {
		return result{}, fmt.Errorf("invalid subject %q", name)
	}
	if what != "tree" && what != "document" {
		return result{}, fmt.Errorf("invalid -what %q", what)
	}
	if lines < 1 {
		return result{}, fmt.Errorf("-lines must be positive")
	}
	text := s.input(lines)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var keep []any
	if what == "tree" {
		t, err := s.parse(text)
		if err != nil {
			return result{}, err
		}
		keep = []any{t}
	} else {
		doc, err := s.document(text)
		if err != nil {
			return result{}, err
		}
		t, err := doc.Parse()
		if err != nil {
			return result{}, err
		}
		keep = []any{doc, t}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(text)
	return result{Subject: name, What: what, Lines: lines, InputBytes: len(text),
		RetainedBytes:   int64(after.HeapAlloc) - int64(before.HeapAlloc),
		RetainedObjects: int64(after.HeapObjects) - int64(before.HeapObjects)}, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
