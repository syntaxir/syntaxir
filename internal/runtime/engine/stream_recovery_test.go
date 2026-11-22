package engine

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// skipStreamRemark returns the program of src, a grammar whose start rule main has one stream body,
// with the jumps that end an element retargeted past the marks each element saves after its STEP,
// instead of back to the start of the loop: the next element then restores, when a choice or a
// recovery backtracks, the state the previous element saved before it was emitted. A jump back is a
// jump from past the STEP to an instruction between the STREAMSTART and the STEP, where a Document
// parse reuses the elements of the previous parse.
func skipStreamRemark(t *testing.T, src string) *Program {
	t.Helper()
	jumps := func(op bytecode.FlowOp) bool { sh, _ := op.Shape(); return sh.Target }
	p := mustCompile(t, src)
	m := p.Module()
	if len(m.Streams) != 1 {
		t.Fatalf("%s: %d stream bodies, want 1", src, len(m.Streams))
	}
	for _, f := range m.Streams {
		start, step := -1, -1
		for pc := range f.Code {
			if f.Code[pc].Op == bytecode.FlowStreamStart && start < 0 {
				start = pc
			}
			if f.Code[pc].Op == bytecode.FlowStep && start >= 0 {
				step = pc
				break
			}
		}
		past := step + 1
		for past < len(f.Code) && (f.Code[past].Op == bytecode.FlowMark || f.Code[past].Op == bytecode.FlowMove) {
			past++
		}
		back := 0
		for pc := step + 1; step >= 0 && pc < len(f.Code); pc++ {
			if in := &f.Code[pc]; jumps(in.Op) && in.Target > start && in.Target <= step {
				in.Target = past
				back++
			}
		}
		if step < 0 || past == step+1 || back == 0 {
			t.Fatalf("%s: stream body shape changed (step %d, past %d, %d loop backs):\n%s", src, step, past, back, m.Disassemble())
		}
	}
	return p
}

// TestCompiledStreamCommitRejected checks that a compiled grammar loads only if no path of a stream
// body restores a state saved before an EMITSTREAM that ran since (bytecode.Flow.RestoreAcrossCommit):
// the emitted input and recovered errors are committed, so a backtrack of a choice or of a recovery
// into them would parse input the stream already gave away.
func TestCompiledStreamCommitRejected(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = ("ab" #recover(skip=[^a]+))+ #stream $$`,
		`rule main = ("a" "b" / "a" "c")* #stream $$`,
	} {
		for _, mode := range valueModes {
			data, err := mustCompile(t, src).Marshal("main", mode)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := LoadProgram(data); err != nil {
				t.Errorf("%s %s mode: the generated module does not load: %v", src, mode, err)
			}
			data, err = skipStreamRemark(t, src).Marshal("main", mode)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "before an EMITSTREAM") {
				t.Errorf("%s %s mode: restore across a commit loaded: %v", src, mode, err)
			}
		}
	}
}

func TestStreamElementRecovery(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`("ab" #recover(skip=[^a]+))`, `(("ab" #recover(skip=[^a]+)) #recover(skip="!"))`, `item`} {
		p := mustCompile(t, `rule main = `+body+`+ #stream $$
rule item = "ab" @error(message="record") #recover(skip=[^a]+)`)
		for _, mode := range valueModes {
			data, err := p.Marshal("main", mode)
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data)
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if backend == Closure {
					err := loaded.ParseStreamWith("main", strings.NewReader("abX!ab"), func(*value.Node) error { return nil }, servedBy(mode, ParseOptions{Backend: backend}))
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
					count := 0
					err := loaded.ParseStreamWith("main", strings.NewReader("abX!ab"), func(*value.Node) error { count++; return nil }, servedBy(mode, ParseOptions{Backend: backend, Unit: unit}))
					var recovered value.SyntaxErrors
					if !errors.As(err, &recovered) || len(recovered) != 1 || count != 3 {
						t.Errorf("%s %s mode %s/%s: count=%d error=%v", body, mode, backend, unit, count, err)
					}
					again, err := loaded.Marshal("main", mode)
					if err != nil || !bytes.Equal(data, again) {
						t.Errorf("roundtrip: %v", err)
					}
				}
			}
		}
	}
}

func BenchmarkStreamRecoveryPreparation(b *testing.B) {
	for _, n := range []int{1, 100} {
		var src strings.Builder
		src.WriteString("rule main = item+ #stream $$\n")
		for i := range n {
			name := fmt.Sprintf("r%d", i)
			if i == 0 {
				name = "item"
			}
			fmt.Fprintf(&src, "rule %s = \"ab\" #recover(skip=[^a]+)\n", name)
		}
		g, err := parseTest(src.String())
		if err != nil {
			b.Fatal(err)
		}
		p, err := Compile(g, checkpkg.Options{})
		if err != nil {
			b.Fatal(err)
		}
		data, err := p.MarshalBinary("main")
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("Compile/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Compile(g, checkpkg.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("LoadBare/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := LoadProgram(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
