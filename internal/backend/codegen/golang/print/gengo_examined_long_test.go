//go:build sxir_long

package print_test

import (
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// examinedMutant leaves out one record of the examined end on one route of an observed parse: in the
// generated code (code) or in a file of the library (file), it replaces each copy of old with new.
// The exactness test must then report a difference on each of the entries. A library mutant states
// the number of copies of old in its file (copies), so that a change of the library that adds a copy
// on another route, or removes one, fails the mutant instead of widening or narrowing what it
// removes; a code mutant replaces every copy in every generated parser, whose number depends on the
// cases, and needs at least one.
type examinedMutant struct {
	name, file, old, new string
	copies               int
	code                 bool
	entries              []string
}

var examinedMutants = []examinedMutant{
	// The reads of the traced typed and recognizer bodies.
	{name: "unrecorded character read of a typed or recognizer body", code: true, old: "p.Peek()", new: "p.PeekOrdinary()",
		entries: []string{"value", "recognize"}},
	// The typed bodies compare literals through Peek.
	{name: "unrecorded literal read of a recognizer body", code: true, old: "p.MatchLiteral(", new: "p.MatchLiteralOrdinary(",
		entries: []string{"recognize"}},
	// The reads of a stream in the traced Node bodies.
	{name: "unrecorded character read of a stream", file: "stream.go", copies: 1, old: "\tp.documentTouch(end)\n\treturn r, size, ok\n", new: "\t_ = end\n\treturn r, size, ok\n",
		entries: []string{"stream"}},
	{name: "unrecorded literal match of a stream", file: "stream.go", copies: 1, old: "\tp.documentTouch(p.Pos)\n\tif !build {\n\t\treturn nil, true\n\t}\n\treturn p.buildNode(Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.LiteralText(start, text)",
		new:     "\tif !build {\n\t\treturn nil, true\n\t}\n\treturn p.buildNode(Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.LiteralText(start, text)",
		entries: []string{"stream"}},
	// The records that the library replays for a result it reuses. The Node and the typed memo hits
	// record the same field, so each mutant names the condition of its hit: the Node hit moves the
	// entry of a Document, the typed hits of a parse without a Document do not.
	{name: "a Node memo hit records nothing", file: "call_exec.go", copies: 1,
		old:     "p.documentMove(e)) && !p.insertsRule(r) {\n\t\tp.Observation.memoHit()\n\t\tif p.track {\n\t\t\tp.documentTouch(e.examined)\n",
		new:     "p.documentMove(e)) && !p.insertsRule(r) {\n\t\tp.Observation.memoHit()\n\t\tif p.track {\n\t\t\tp.documentTouch(0)\n",
		entries: []string{"parse", "recognize"}},
	{name: "a typed memo hit records nothing", file: "call_exec.go", copies: 2,
		old:     "(!e.silent || p.Silent > 0) && !p.insertsRule(r.Rule) {\n\t\tp.Observation.memoHit()\n\t\tif p.track {\n\t\t\tp.documentTouch(e.examined)\n",
		new:     "(!e.silent || p.Silent > 0) && !p.insertsRule(r.Rule) {\n\t\tp.Observation.memoHit()\n\t\tif p.track {\n\t\t\tp.documentTouch(0)\n",
		entries: []string{"value"}},
	{name: "a context replay records nothing", file: "call_exec.go", copies: 4, old: "p.documentTouch(held.examined)", new: "p.documentTouch(0)",
		entries: []string{"parse"}},
	{name: "a seed replay records nothing", file: "call_exec.go", copies: 4, old: "p.documentTouch(seed.examined)", new: "p.documentTouch(0)",
		entries: []string{"parse"}},
	// examinedExtras reaches the last step of a growth taken from the context; the known case that
	// needs its record is a recognizer's (the grammar reduced from random cycle grammar 210).
	{name: "a growth step taken from the context records nothing", file: "context_grow.go", copies: 1, old: "\t\tp.touchRange(s.examined, s.from)\n", new: "\t\t_ = s\n",
		entries: []string{"recognize"}},
	{name: "a skip record reused records nothing", file: "skip_walks.go", copies: 1, old: "p.touchRange(r.examined, r.from)", new: "p.touchRange(0, r.from)",
		entries: []string{"parse"}},
	{name: "a lookahead drops what it examined", file: "context_table.go", copies: 1, old: "p.setExamRange(max(l.examined, examined), min(l.from, from))",
		new:     "p.setExamRange(l.examined, min(l.from, from))",
		entries: []string{"parse"}},
}

// TestObservedExaminedEndsMutants checks that TestObservedExaminedEndsAreExact notices a record of the
// examined end left out on each route: each mutant must change the examined end of some exit of each of
// its entries.
func TestObservedExaminedEndsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	codegentest.PinOrdinary(t)
	cases := examinedMutantCases(t)
	for _, m := range examinedMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			replaced := 0
			lib := codegentest.LibCopy()
			var mutate func([]byte) []byte
			if m.code {
				mutate = func(code []byte) []byte {
					replaced += strings.Count(string(code), m.old)
					return []byte(strings.ReplaceAll(string(code), m.old, m.new))
				}
			} else {
				lib = codegentest.LibCopyWith(t, func(name, src string) string {
					if name != m.file {
						return src
					}
					replaced += strings.Count(src, m.old)
					return strings.ReplaceAll(src, m.old, m.new)
				}, nil)
			}
			reports := runExaminedProbes(t, cases, lib, mutate)
			if replaced == 0 {
				t.Fatalf("no %q in the code", m.old)
			}
			if !m.code && replaced != m.copies {
				t.Fatalf("%s: %s has %d copies of %q, want %d", m.name, m.file, replaced, m.old, m.copies)
			}
			_, diffs := examinedMismatches(t, cases, reports)
			for _, entry := range m.entries {
				if len(diffs[entry]) == 0 {
					t.Errorf("the mutant changes no examined end of %s", entry)
				}
			}
		})
	}
}

// examinedMutantShapes selects the cases of the generated corpus that the mutants need: memoized
// rules, lookaheads, left recursion, cuts and recovery.
var examinedMutantShapes = regexp.MustCompile(`(?i)leader|recurs|look|recover|memo|cut|LR`)

// examinedMutantCases are the cases the mutants run on: those of the generated corpus that
// examinedMutantShapes selects, the shapes of the observation tests, examinedExtras
// and the stream grammars, fewer than the exactness test runs, as each mutant builds every case again.
func examinedMutantCases(t *testing.T) []codegentest.Case {
	t.Helper()
	var cases []codegentest.Case
	for _, c := range codegentest.GenCorpus(t) {
		if examinedMutantShapes.MatchString(c.Name) && !strings.Contains(c.Src, "#stream") {
			cases = append(cases, c)
		}
	}
	cases = append(cases, observeShapes...)
	cases = append(cases, examinedExtras()...)
	for _, c := range streamCases(t) {
		if strings.Contains(c.Src, "#stream") && len(c.Inputs) > 0 {
			cases = append(cases, c)
		}
	}
	return cases
}
