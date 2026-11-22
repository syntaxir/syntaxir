//go:build !sxir_budget_controls_reference

package state_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

func fallbackTextInput(unit input.PositionUnit, base int, bytes []byte, runes []rune, control *state.BudgetControl) state.Input {
	return state.Input{Unit: unit, Base: base, Bs: bytes, In: runes, Control: control}
}

func TestBudgetInputTextNativeParityAndDetachedResult(t *testing.T) {
	t.Parallel()
	byteCases := []int{0, 128, 129, 4096, 4097}
	for _, n := range byteCases {
		source := make([]byte, n+6)
		for i := range source {
			source[i] = byte(i*37 + 11)
		}
		start, end := 3, n+3
		in := fallbackTextInput(input.Bytes, 17, source, nil, state.NewBudgetControl(nil, runtime.Limits{Compute: 100_000}))
		got := in.Text(17+start, 17+end)
		want := string(source[start:end])
		if got != want {
			t.Fatalf("Bytes length %d text mismatch", n)
		}
		// Reading the text of the input is no event: it counts nothing.
		if in.Control.Used != 0 {
			t.Fatalf("Bytes length %d spent=%d, want 0", n, in.Control.Used)
		}
		if n > 0 {
			source[start] ^= 0xff
			if got != want {
				t.Fatalf("Bytes length %d result changed with source buffer", n)
			}
		}
	}

	for _, n := range []int{0, 1, 128, 129, 257} {
		runes := make([]rune, n+4)
		for i := range runes {
			switch i % 7 {
			case 0:
				runes[i] = 0
			case 1:
				runes[i] = 'é'
			case 2:
				runes[i] = '💡'
			case 3:
				runes[i] = -1
			case 4:
				runes[i] = 0xd800
			case 5:
				runes[i] = 0x110000
			default:
				runes[i] = 'x'
			}
		}
		start, end := 2, n+2
		in := fallbackTextInput(input.CodePoints, 9, nil, runes, state.NewBudgetControl(nil, runtime.Limits{Compute: 100_000}))
		got := in.Text(9+start, 9+end)
		want := string(runes[start:end])
		if got != want {
			t.Fatalf("CodePoints length %d text = %q, want %q", n, got, want)
		}
		if in.Control.Used != 0 {
			t.Fatalf("CodePoints length %d spent=%d, want 0", n, in.Control.Used)
		}
		if n > 0 {
			runes[start] = 'z'
			if got != want {
				t.Fatalf("CodePoints length %d result changed with source buffer", n)
			}
		}
	}
}

func TestBudgetInputTextChunkInterruptionLeavesSourceAndRetries(t *testing.T) {
	t.Parallel()
	byteSource := []byte(strings.Repeat("a", 4097))
	byteBefore := append([]byte(nil), byteSource...)
	byteInput := fallbackTextInput(input.Bytes, 0, byteSource, nil, state.NewBudgetControl(nil, runtime.Limits{Compute: 4096}))
	// The copy counts nothing, so a limit below its length does not stop it.
	if err := statetest.CatchFatal(t, func() { _ = byteInput.Text(0, len(byteSource)) }); err != nil {
		t.Fatalf("Bytes copy error = %v, want none", err)
	}
	if !reflect.DeepEqual(byteSource, byteBefore) || byteInput.Control.Used != 0 {
		t.Fatalf("Bytes failed copy mutated source or charged unexpected spend: counter=%d", byteInput.Control.Used)
	}
	byteInput.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 10_000})
	if got, want := byteInput.Text(0, len(byteSource)), string(byteSource); got != want {
		t.Fatal("Bytes retry differs from native conversion")
	}

	runeSource := make([]rune, 129)
	for i := range runeSource {
		runeSource[i] = 'λ'
	}
	runeBefore := append([]rune(nil), runeSource...)
	runeInput := fallbackTextInput(input.CodePoints, 0, nil, runeSource, state.NewBudgetControl(nil, runtime.Limits{Compute: 257}))
	if err := statetest.CatchFatal(t, func() { _ = runeInput.Text(0, len(runeSource)) }); err != nil {
		t.Fatalf("CodePoints encoding error = %v, want none", err)
	}
	if !reflect.DeepEqual(runeSource, runeBefore) || runeInput.Control.Used != 0 {
		t.Fatalf("CodePoints encoding mutated source or counted %d units, want none", runeInput.Control.Used)
	}
	runeInput.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 10_000})
	if got, want := runeInput.Text(0, len(runeSource)), string(runeSource); got != want {
		t.Fatal("CodePoints retry differs from native conversion")
	}
}

func TestBudgetInputTextPollingCancellationAtCopyAndEncoding(t *testing.T) {
	t.Parallel()
	bytes := []byte(strings.Repeat("b", 8192))
	byteInput := fallbackTextInput(input.Bytes, 0, bytes, nil, state.NewBudgetControl(statetest.NewPollingContext(2), runtime.Limits{}))
	// The copy counts nothing and makes no check, so it does not observe the context.
	if err := statetest.CatchFatal(t, func() { _ = byteInput.Text(0, len(bytes)) }); err != nil {
		t.Fatalf("Bytes copy = %v, want no interruption", err)
	}
	if byteInput.Control.Used != 0 {
		t.Fatalf("Bytes copy counter=%d, want 0", byteInput.Control.Used)
	}

	runes := make([]rune, 512)
	for i := range runes {
		runes[i] = '語'
	}
	runeInput := fallbackTextInput(input.CodePoints, 0, nil, runes, state.NewBudgetControl(statetest.NewPollingContext(3), runtime.Limits{}))
	if err := statetest.CatchFatal(t, func() { _ = runeInput.Text(0, len(runes)) }); err != nil {
		t.Fatalf("CodePoints encoding = %v, want no interruption", err)
	}
	if runeInput.Control.Used != 0 {
		t.Fatalf("CodePoints encoding counter=%d, want 0", runeInput.Control.Used)
	}
}

func TestBudgetInputTextSourceSubstringRemainsConstantBudget(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 8192)
	byteInput := state.Input{Unit: input.Bytes, Bs: []byte(long), Src: long, SrcOK: true, Control: state.NewBudgetControl(nil, runtime.Limits{Compute: 1})}
	if got := byteInput.Text(1024, 7168); got != long[1024:7168] {
		t.Fatal("Bytes source substring mismatch")
	}
	if byteInput.Control.Used != 0 {
		t.Fatalf("Bytes source substring spent budget=%d", byteInput.Control.Used)
	}

	runes := []rune(long)
	offs := make([]int32, len(runes)+1)
	for i := range offs {
		offs[i] = int32(i)
	}
	codePointInput := state.Input{Unit: input.CodePoints, In: runes, Src: long, SrcOK: true, Offs: offs, Control: state.NewBudgetControl(nil, runtime.Limits{Compute: 1})}
	if got := codePointInput.Text(1024, 7168); got != long[1024:7168] {
		t.Fatal("CodePoints source substring mismatch")
	}
	if codePointInput.Control.Used != 0 {
		t.Fatalf("CodePoints source substring spent budget=%d", codePointInput.Control.Used)
	}
}

func TestBudgetTraceTextInterruptionRetriesPooledParser(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = .* $$`)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, unit := range []input.PositionUnit{input.Bytes, input.CodePoints} {
			raw := strings.Repeat("z", 9000)
			if unit == input.CodePoints {
				raw = strings.Repeat("\xff", 9000)
			}
			armed := false
			var observed *state.Parser
			node, err := prog.ParseWith("main", raw, engine.ParseOptions{
				Backend: backend,
				Unit:    unit,
				Limits: runtime.Limits{
					Compute: 1 << 30,
				},
				Trace: func(e engine.TraceEvent) {
					if !armed && e.Kind == engine.TraceExit && e.Rule == "main" && e.Matched {
						armed = true
						observed = state.TraceParser(e)
						if unit == input.Bytes {
							state.TraceParser(e).SrcOK = false
						}
						// The text counts nothing; the check after the trace function observes the
						// context that it canceled.
						ctx, cancel := context.WithCancel(context.Background())
						cancel()
						state.TraceParser(e).Control.Ctx = ctx
						_ = e.Text()
					}
				},
			})
			if !armed || node != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("%v/%v trace Text interruption: armed=%t node=%v err=%v", backend, unit, armed, node, err)
			}
			if observed == nil || observed.Scratch != nil || observed.Control != nil {
				t.Fatalf("%v/%v interrupted parser cleanup: %#v", backend, unit, observed)
			}
			node, err = prog.ParseWith("main", raw, engine.ParseOptions{Backend: backend, Unit: unit})
			if err != nil || node == nil {
				t.Fatalf("%v/%v pooled retry: node=%v err=%v", backend, unit, node, err)
			}
		}
	}
}
