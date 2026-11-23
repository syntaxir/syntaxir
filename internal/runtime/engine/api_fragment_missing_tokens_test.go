package engine_test

import (
	"errors"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestParsePrefixMissingTokenOptionsWithoutMissing checks that a missing-token limit leaves a prefix
// parse of a grammar without #missing as it is without one, and that a negative limit is the
// configuration error of Parse.
func TestParsePrefixMissingTokenOptionsWithoutMissing(t *testing.T) {
	parser, err := compileTestSource(`rule main = "x"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, recognize := range []bool{false, true} {
			traceCalls := 0
			opts := []engine.ParseOption{engine.WithBackend(backend), engine.WithTrace(func(engine.TraceEvent) { traceCalls++ })}
			if recognize {
				opts = append(opts, engine.WithRecognize())
			}
			// A positive limit, also one that a later value sets, and a limit disabled last
			// all parse the prefix as without the option: the grammar has nothing to insert.
			var strictCalls int
			for i, limits := range [][]engine.ParseOption{
				nil,
				{engine.WithMissingTokens(1)},
				{engine.WithMissingTokens(0), engine.WithMissingTokens(1)},
				{engine.WithMissingTokens(1), engine.WithMissingTokens(0)},
			} {
				traceCalls = 0
				options := append(append([]engine.ParseOption{}, opts...), limits...)
				result, err := parser.ParsePrefix("main", "xtail", options...)
				if err != nil || result.Consumed != 1 || result.ConsumedBytes != 1 || recognize != (result.Node == nil) {
					t.Fatalf("%v/recognize=%v: limits %d returned %+v, %v", backend, recognize, i, result, err)
				}
				if i == 0 {
					strictCalls = traceCalls
				} else if traceCalls != strictCalls {
					t.Fatalf("%v/recognize=%v: limits %d traced %d events, without a limit %d", backend, recognize, i, traceCalls, strictCalls)
				}
			}
			// Negative limits retain the existing shared configuration validation.
			options := append(append([]engine.ParseOption{}, opts...), engine.WithMissingTokens(-1))
			_, referenceErr := parser.Parse("x", options...)
			result, err := parser.ParsePrefix("main", "xtail", options...)
			if err == nil || referenceErr == nil || err.Error() != referenceErr.Error() || result != (engine.PrefixResult{}) {
				t.Fatalf("negative limit: prefix=%+v/%v, Parse error=%v", result, err, referenceErr)
			}
		}
	}
}

// TestParsePrefixMissingGrammar checks the prefix parses of a rule that ends in an insertable literal
// beside the whole-input parse that inserts it: with a limit the prefix parse inserts the literal where
// it is absent, and without one, or with a zero limit set last, it parses strictly.
func TestParsePrefixMissingGrammar(t *testing.T) {
	parser, err := compileTestSource(`rule main = token $$
rule token = "x" ";" #missing`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, recognize := range []bool{false, true} {
		opts := []engine.ParseOption{engine.WithBackend(engine.Closure), engine.WithMissingTokens(1)}
		if recognize {
			opts = append(opts, engine.WithRecognize())
		}
		// Whole-input parsing continues to synthesize the missing semicolon.
		node, err := parser.Parse("x", opts...)
		var recovered value.SyntaxErrors
		if !errors.As(err, &recovered) || len(recovered) != 1 || recognize && node != nil || !recognize && node == nil {
			t.Fatalf("whole-input insertion recognize=%v: node=%v error=%v", recognize, node, err)
		}
		// The prefix parse inserts the semicolon where it is absent, as the whole-input parse
		// does, and consumes it where it is present.
		result, err := parser.ParsePrefix("token", "x", opts...)
		if !errors.As(err, &recovered) || len(recovered) != 1 || result.Consumed != 1 || recognize != (result.Node == nil) {
			t.Fatalf("insertion prefix recognize=%v: %+v %v", recognize, result, err)
		}
		result, err = parser.ParsePrefix("token", "x;tail", opts...)
		if err != nil || result.Consumed != 2 || recognize != (result.Node == nil) {
			t.Fatalf("present literal recognize=%v: %+v %v", recognize, result, err)
		}
		// Insertion disabled by omission, an explicit zero or a zero overriding a positive
		// parses strictly: the absent semicolon fails, the present one is consumed.
		for _, limits := range [][]engine.ParseOption{
			nil,
			{engine.WithMissingTokens(0)},
			{engine.WithMissingTokens(1), engine.WithMissingTokens(0)},
		} {
			options := []engine.ParseOption{engine.WithBackend(engine.Closure)}
			if recognize {
				options = append(options, engine.WithRecognize())
			}
			options = append(options, limits...)
			var one *value.SyntaxError
			if result, err := parser.ParsePrefix("token", "x", options...); !errors.As(err, &one) || result != (engine.PrefixResult{}) {
				t.Fatalf("disabled insertion, absent literal, recognize=%v: %+v %v", recognize, result, err)
			}
			if result, err := parser.ParsePrefix("token", "x;tail", options...); err != nil || result.Consumed != 2 || recognize != (result.Node == nil) {
				t.Fatalf("disabled insertion, present literal, recognize=%v: %+v %v", recognize, result, err)
			}
		}
	}
}
