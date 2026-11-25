package bench

import (
	"encoding/json"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/bench/inputs"
	gcsv "syntaxir.dev/sxir/internal/bench/parsers/csv"
	gcsvdoc "syntaxir.dev/sxir/internal/bench/parsers/csvdoc"
	gminilang "syntaxir.dev/sxir/internal/bench/parsers/minilang"
	gminilangdoc "syntaxir.dev/sxir/internal/bench/parsers/minilangdoc"
	gminilangtdoc "syntaxir.dev/sxir/internal/bench/parsers/minilangtdoc"
	"syntaxir.dev/sxir/internal/runtime/engine"
)

// Document edit latency: the time from an editor's change to the updated tree, one Edit and the
// Parse after it, over a seeded trace of edits at random places of a document of 100,000 lines. An
// editor feels the slow edits, so the benchmarks report the median and the 99th percentile of the
// edits besides their mean.

// documentLines is the size of the documents of the edit-latency benchmarks, in lines.
const documentLines = 100_000

// documentTraceLen is the number of edits of a trace; a benchmark cycles through it.
const documentTraceLen = 4096

// docEdit is one replacement of a trace, in code points of the text before it.
type docEdit struct {
	start, end int
	text       string
}

// docSession is an open Document: edit applies a replacement and parse brings the result up to
// date, returning the tree or the typed value.
type docSession struct {
	edit  func(start, end int, text string) error
	parse func() (any, error)
}

// docTarget is a Document to measure: the engine on a backend or a generated parser. typed is set
// when its parse returns typed values, which have no Node form to compare with a fresh parse.
type docTarget struct {
	name  string
	typed bool
	open  func(input string) (docSession, error)
}

// docGrammar is a grammar of the edit-latency benchmarks: its input of at least a number of lines,
// the numbers whose digits the edits change (a number stays a number whatever digit from 1 to 9
// replaces one of its digits or is inserted before one), its generated Documents and its fresh
// parse, the reference for the incremental trees.
type docGrammar struct {
	name      string
	grammar   string
	input     func(lines int) string
	number    *regexp.Regexp // the numbers to edit, as the first group
	generated []docTarget
	fresh     func(text string) (any, error)
}

var docGrammars = []docGrammar{
	{
		name: "CSV", grammar: "../testdata/grammars/csv/csv.sxir",
		input:  func(lines int) string { return inputs.CSVInput(lines - 1) },
		number: regexp.MustCompile(`,([0-9]+)\.[0-9][0-9],`),
		generated: []docTarget{{name: "generated", open: func(input string) (docSession, error) {
			doc, err := gcsvdoc.NewDocument(input)
			if err != nil {
				return docSession{}, err
			}
			return docSession{doc.Edit, func() (any, error) { return doc.Parse() }}, nil
		}}},
		fresh: func(text string) (any, error) { return gcsv.Parse(text) },
	},
	{
		name: "Minilang", grammar: "../examples/minilang/minilang.sxir",
		input:  minilangLines,
		number: regexp.MustCompile(`= a \* ([0-9]+) \+`),
		generated: []docTarget{
			{name: "generated", open: func(input string) (docSession, error) {
				doc, err := gminilangdoc.NewDocument(input)
				if err != nil {
					return docSession{}, err
				}
				return docSession{doc.Edit, func() (any, error) { return doc.Parse() }}, nil
			}},
			{name: "generated-typed", typed: true, open: func(input string) (docSession, error) {
				doc, err := gminilangtdoc.NewDocument(input)
				if err != nil {
					return docSession{}, err
				}
				return docSession{doc.Edit, func() (any, error) { return doc.ParseValue() }}, nil
			}},
		},
		fresh: func(text string) (any, error) { return gminilang.Parse(text) },
	},
}

// minilangLines returns a Minilang program of at least lines lines: MinilangInput writes 13 lines
// per function.
func minilangLines(lines int) string {
	return inputs.MinilangInput((lines+12)/13, 0)
}

// numberDigits returns the offsets, in code points as Documents take them, of the digits of the
// numbers that g's edits change.
func numberDigits(g *docGrammar, input string) []int {
	var at []int
	byteAt, runeAt := 0, 0
	for _, m := range g.number.FindAllStringSubmatchIndex(input, -1) {
		runeAt += utf8.RuneCountInString(input[byteAt:m[2]])
		byteAt = m[2]
		for i := m[2]; i < m[3]; i++ { // digits are one code point each
			at = append(at, runeAt+i-m[2])
		}
	}
	return at
}

// documentTrace returns a seeded trace of n edits (n even) of input at the digits of g's numbers.
// Same-length edits replace a digit with a digit from 1 to 9. Length-changing edits come in pairs:
// a digit from 1 to 9 inserted before a digit, then removed, so the text is the input again after
// every pair and the trace can be cycled. No edit makes a number start with 0.
func documentTrace(g *docGrammar, input string, lengthChanging bool, n int) []docEdit {
	at := numberDigits(g, input)
	r := rand.New(rand.NewPCG(1, 2))
	trace := make([]docEdit, 0, n)
	for len(trace) < n {
		p := at[r.IntN(len(at))]
		d := string(rune('1' + r.IntN(9)))
		if lengthChanging {
			trace = append(trace, docEdit{p, p, d}, docEdit{p, p + 1, ""})
		} else {
			trace = append(trace, docEdit{p, p + 1, d})
		}
	}
	return trace
}

// docTargets returns the Documents of g to measure: the engine on every backend, then g's generated
// parsers.
func docTargets(tb testing.TB, g *docGrammar) []docTarget {
	p := load(tb, g.grammar)
	var ts []docTarget
	for _, be := range backends {
		ts = append(ts, docTarget{name: be.name, open: func(input string) (docSession, error) {
			doc, err := p.NewDocument(input, engine.WithBackend(be.b))
			if err != nil {
				return docSession{}, err
			}
			return docSession{doc.Edit, func() (any, error) { return doc.Parse() }}, nil
		}})
	}
	return append(ts, g.generated...)
}

// TestDocumentTrace checks the edit-latency benchmarks on reduced documents: the inputs reach
// documentLines lines, the length-changing trace returns to the input, and after every edit of both
// traces every Document parses the edited text, into the tree of a fresh parse where it returns
// trees.
func TestDocumentTrace(t *testing.T) {
	for i := range docGrammars {
		g := &docGrammars[i]
		t.Run(g.name, func(t *testing.T) {
			if n := strings.Count(g.input(documentLines), "\n"); n < documentLines {
				t.Errorf("the input has %d lines, want at least %d", n, documentLines)
			}
			input := g.input(400)
			digits := numberDigits(g, input)
			if len(digits) == 0 {
				t.Fatal("no digits to edit")
			}
			runes := []rune(input)
			for _, p := range digits {
				if runes[p] < '0' || runes[p] > '9' {
					t.Fatalf("code point %d is %q, not a digit", p, runes[p])
				}
			}
			for _, lengthChanging := range []bool{false, true} {
				trace := documentTrace(g, input, lengthChanging, 60)
				for _, target := range docTargets(t, g) {
					doc, err := target.open(input)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := doc.parse(); err != nil {
						t.Fatal(err)
					}
					text := []rune(input)
					for step, e := range trace {
						if err := doc.edit(e.start, e.end, e.text); err != nil {
							t.Fatalf("%s, edit %d: %v", target.name, step, err)
						}
						text = slices.Concat(text[:e.start], []rune(e.text), text[e.end:])
						got, err := doc.parse()
						if err != nil {
							t.Fatalf("%s, edit %d: %v", target.name, step, err)
						}
						if target.typed {
							continue
						}
						want, err := g.fresh(string(text))
						if err != nil {
							t.Fatalf("%s, edit %d: fresh parse: %v", target.name, step, err)
						}
						gj, _ := json.Marshal(got)
						wj, _ := json.Marshal(want)
						if string(gj) != string(wj) {
							t.Fatalf("%s, edit %d: the Document's tree differs from a fresh parse", target.name, step)
						}
					}
					if lengthChanging && string(text) != input {
						t.Errorf("%s: the length-changing trace does not return to the input", target.name)
					}
				}
			}
		})
	}
}

// BenchmarkDocumentEdit measures an Edit and the Parse after it on a document of documentLines
// lines, over the seeded traces of documentTrace: same-length edits and length-changing ones, with
// the engine on every backend and with the generated parsers (generated with -document). ns/op is
// the mean of an edit with its reparse; p50-ns/op and p99-ns/op are the median and the 99th
// percentile over the run's edits, which need at least a hundred edits (a run of about a second)
// to mean anything.
func BenchmarkDocumentEdit(b *testing.B) {
	for i := range docGrammars {
		g := &docGrammars[i]
		input := g.input(documentLines)
		targets := docTargets(b, g)
		for _, kind := range []struct {
			name           string
			lengthChanging bool
		}{{"same-length", false}, {"length-changing", true}} {
			trace := documentTrace(g, input, kind.lengthChanging, documentTraceLen)
			for _, target := range targets {
				b.Run(g.name+"/"+kind.name+"/"+target.name, func(b *testing.B) {
					doc, err := target.open(input)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := doc.parse(); err != nil {
						b.Fatal(err)
					}
					// One latency per edit, in a slice that does not grow inside the loop.
					lat := make([]time.Duration, 0, 1<<20)
					b.ReportAllocs()
					n := 0
					for b.Loop() {
						e := trace[n%len(trace)]
						n++
						start := time.Now()
						if err := doc.edit(e.start, e.end, e.text); err != nil {
							b.Fatal(err)
						}
						if _, err := doc.parse(); err != nil {
							b.Fatal(err)
						}
						if len(lat) < cap(lat) {
							lat = append(lat, time.Since(start))
						}
					}
					slices.Sort(lat)
					b.ReportMetric(float64(percentile(lat, 50)), "p50-ns/op")
					b.ReportMetric(float64(percentile(lat, 99)), "p99-ns/op")
				})
			}
		}
	}
}

// percentile returns the p-th percentile of sorted by the nearest rank, 0 for no values.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100 // ceil(p * n / 100)
	return sorted[max(rank, 1)-1]
}
