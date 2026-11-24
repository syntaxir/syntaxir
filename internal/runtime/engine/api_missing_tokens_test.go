//go:build !sxir_budget_controls_reference

package engine_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	gensrc "syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestMissingTokensPublicOptions(t *testing.T) {
	p, err := compileTestSource(`rule main = ";" #missing $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]engine.ParseOption{nil, {engine.WithMissingTokens(0)}, {engine.WithMissingTokens(2), engine.WithMissingTokens(0)}} {
		if n, err := p.Parse("", options...); n != nil || err == nil {
			t.Fatal("disabled insertion accepted input", n, err)
		}
	}
	calls := 0
	base := engine.WithMissingTokens(2)
	counted := engine.ParseOption(func(o *engine.ParseOptions) { calls++; base(o) })
	if _, err := p.Parse(";", counted); err != nil || calls != 1 {
		t.Fatal("options did not resolve exactly once", calls, err)
	}
	n, err := p.Parse("", engine.WithMissingTokens(-1), engine.WithMissingTokens(2))
	var recovered value.SyntaxErrors
	if n == nil || !errors.As(err, &recovered) || len(recovered) != 1 {
		t.Fatal(n, err)
	}
	if _, err := p.Parse(";", engine.WithMissingTokens(2), engine.WithMissingTokens(-1)); err == nil || !strings.Contains(err.Error(), "nonnegative") {
		t.Fatal(err)
	}
	if _, err := p.Parse(";", engine.WithMissingTokens(-1)); err == nil {
		t.Fatal("invalid limit accepted")
	}
	if n, err := p.Parse("", engine.WithRecognize(), engine.WithMissingTokens(2)); n != nil || !errors.As(err, &recovered) {
		t.Fatal("recognition", n, err)
	}
	if n, err := p.Parse(";", engine.WithMissingTokens(2)); n == nil || err != nil {
		t.Fatal("ordinary match", n, err)
	}
}

func TestMissingTokensConcurrentReentrantAndRetry(t *testing.T) {
	p, err := compileTestSource(`rule main = ";" #missing $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				n, err := p.Parse("", engine.WithMissingTokens(1))
				var recovered value.SyntaxErrors
				if n == nil || !errors.As(err, &recovered) || len(recovered) != 1 {
					t.Error(n, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	entered := false
	_, err = p.Parse("", engine.WithMissingTokens(1), engine.WithTrace(func(engine.TraceEvent) {
		if entered {
			return
		}
		entered = true
		n, err := p.Parse("", engine.WithMissingTokens(1))
		var recovered value.SyntaxErrors
		if n == nil || !errors.As(err, &recovered) {
			t.Error("reentrant invocation shared a ledger", err)
		}
	}))
	var recovered value.SyntaxErrors
	if !entered || !errors.As(err, &recovered) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Parse("", engine.WithMissingTokens(1), engine.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n, err := p.Parse("", engine.WithMissingTokens(1)); n == nil || !errors.As(err, &recovered) {
		t.Fatal("cancelled invocation poisoned retry", n, err)
	}
}

// TestMissingTokensPublicPaths checks that every public path of a parse inserts missing tokens as
// the parse of the whole input does: every backend, streams, iterators, Documents, compiled grammars
// and the generated Go and TypeScript parsers, which run the tables of the insertion program.
func TestMissingTokensPublicPaths(t *testing.T) {
	p, err := compileTestSource(`rule main = (";" #missing)* #stream $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	// A parse of the whole input inserts on every backend, as the Closure does.
	want, wantErr := p.Parse(";", engine.WithMissingTokens(2))
	for _, backend := range []engine.Backend{engine.Bytecode, engine.BytecodeIterative} {
		if n, err := p.Parse(";", engine.WithBackend(backend), engine.WithMissingTokens(0)); n == nil || err != nil {
			t.Fatal(backend, n, err)
		}
		n, err := p.Parse(";", engine.WithBackend(backend), engine.WithMissingTokens(2))
		if fmt.Sprint(n) != fmt.Sprint(want) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
			t.Fatal(backend, n, err, "want", want, wantErr)
		}
	}
	// Stream parses, iterators and Documents insert as the parse of the whole input does.
	var streamed []string
	streamErr := p.ParseStream(strings.NewReader(";"), func(n *value.Node) error {
		streamed = append(streamed, fmt.Sprint(n))
		return nil
	}, engine.WithMissingTokens(2))
	if fmt.Sprint(streamErr) != fmt.Sprint(wantErr) || len(streamed) == 0 {
		t.Fatal("stream", streamed, streamErr, "want", wantErr)
	}
	it, err := p.Iterate(strings.NewReader(";"), engine.WithMissingTokens(2))
	if err != nil {
		t.Fatal(err)
	}
	var iterated []string
	for {
		n, ok, err := it.Next()
		if !ok {
			if fmt.Sprint(err) != fmt.Sprint(wantErr) {
				t.Fatal("iterator", err, "want", wantErr)
			}
			break
		}
		iterated = append(iterated, fmt.Sprint(n))
	}
	if fmt.Sprint(iterated) != fmt.Sprint(streamed) {
		t.Fatal("iterator", iterated, "stream", streamed)
	}
	doc, err := p.NewDocument(";", engine.WithMissingTokens(2))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); fmt.Sprint(n) != fmt.Sprint(want) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
		t.Fatal("document", n, err, "want", want, wantErr)
	}
	// A compiled grammar keeps #missing: the loaded parser inserts as the source does.
	data, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := engine.LoadParser(data)
	if err != nil {
		t.Fatal(err)
	}
	n, err := loaded.Parse(";", engine.WithMissingTokens(2))
	if fmt.Sprint(n) != fmt.Sprint(want) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
		t.Fatal("loaded", n, err, "want", want, wantErr)
	}
	// The generated Go and TypeScript parsers run their parses with insertion on the tables of the
	// insertion program.
	files, err := gensrc.Go(entry.Grammar(p.Program()), common.GenOptions{Module: "example", Start: "main"})
	if err != nil {
		t.Fatal("Go generator refused #missing:", err)
	}
	if f := files[0]; f.Path != "parser.go" || !bytes.Contains(f.Content, []byte("insRules")) {
		t.Fatalf("the generated Go parser %s has no insertion table", f.Path)
	}
	tsFiles, err := gensrc.TypeScript(entry.Grammar(p.Program()), common.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal("TypeScript generator refused #missing:", err)
	}
	if !slices.ContainsFunc(tsFiles, func(f common.GeneratedFile) bool {
		return f.Path == "parser.ts" && bytes.Contains(f.Content, []byte("setInsertion("))
	}) {
		t.Fatal("the generated TypeScript parser has no insertion table")
	}
	strict, err := compileTestSource(`rule main = ";" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	// A grammar without #missing has nothing to insert: a limit changes no backend's parse.
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		if n, err := strict.Parse(";", engine.WithBackend(backend), engine.WithMissingTokens(1)); n == nil || err != nil {
			t.Fatal(backend, "limit on a grammar without #missing", n, err)
		}
	}
	d, err := strict.NewDocument(";")
	if err != nil {
		t.Fatal(err)
	}
	// A Document fixes the limit at construction.
	if n, err := d.Parse(engine.WithMissingTokens(1)); n != nil || err == nil {
		t.Fatal("existing Document changed its missing-token limit", n, err)
	}
}
