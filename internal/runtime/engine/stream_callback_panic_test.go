package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	value "syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

func TestStreamCallerPanicIdentity(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, codegentest.Records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			for _, traced := range []bool{false, true} {
				for _, buffered := range []bool{false, true} {
					for _, phase := range []string{"reader", "emit"} {
						for _, runtimeValue := range []bool{false, true} {
							t.Run(fmt.Sprintf("%s/%s/trace=%t/buffered=%t/%s/runtime=%t", b, u, traced, buffered, phase, runtimeValue), func(t *testing.T) {
								var token any = &struct{ phase string }{phase}
								if runtimeValue {
									token = &statetest.StreamPanicToken{Phase: phase}
								}
								r := &statetest.StreamReader{Text: "#records\na=1\nbc=22\n"}
								if phase == "reader" {
									r.PanicValue = token
								}
								var reader io.Reader = r
								if buffered {
									reader = bufio.NewReader(r)
								}
								opts := ParseOptions{Backend: b, Unit: u}
								var event TraceEvent
								if traced {
									opts.Trace = func(e TraceEvent) {
										e.LineCol(e.Pos)
										e.Text()
										e.Failure()
										if e.Kind == TraceEnter && e.Rule == "main" {
											event = e
										}
									}
								}
								var err error
								var nodes []*sxirvalue.Node
								var before []string
								got := statetest.CatchPanic(func() {
									err = prog.ParseStreamWith("main", reader, func(n *sxirvalue.Node) error {
										nodes = append(nodes, n)
										before = append(before, codegentest.ResultJSON(n, nil))
										if phase == "emit" && len(nodes) == 2 {
											panic(token)
										}
										return nil
									}, opts)
								})
								if got != token || err != nil || len(nodes) != 2 {
									t.Fatalf("panic=%#v error=%v emitted=%d; want exact caller panic and two records", got, err, len(nodes))
								}
								if r.Closed {
									t.Fatal("caller reader was closed")
								}
								if traced && (event.Rule != "main" || event.Depth != 1 || event.Pos != 0) {
									t.Fatal("saved event metadata changed")
								}
								if value.D(nodes[0].Field("k").(*sxirvalue.Node)).Text != "a" || value.D(nodes[1].Field("v").(*sxirvalue.Node)).Text != "22" {
									t.Fatal("delivered fields changed")
								}
								retryCount := 0
								if err := prog.ParseStreamWith("main", strings.NewReader("#records\nxy=3\n"), func(*sxirvalue.Node) error { retryCount++; return nil }, ParseOptions{Backend: b, Unit: u}); err != nil || retryCount != 1 {
									t.Fatalf("retry error=%v emitted=%d", err, retryCount)
								}
								for i, n := range nodes {
									if codegentest.ResultJSON(n, nil) != before[i] {
										t.Fatal("panic or retry changed a delivered prefix")
									}
								}
							})
						}
					}
				}
			}
		}
	}
}

func TestStreamCallerReturnedErrorIdentity(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, codegentest.Records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			for _, traced := range []bool{false, true} {
				for _, phase := range []string{"reader", "emit"} {
					t.Run(fmt.Sprintf("%s/%s/trace=%t/%s", b, u, traced, phase), func(t *testing.T) {
						stop := errors.New("caller stopped")
						r := &statetest.StreamReader{Text: "#records\na=1\nbc=22\n"}
						if phase == "reader" {
							r.Err = stop
						}
						opts := ParseOptions{Backend: b, Unit: u}
						if traced {
							opts.Trace = func(e TraceEvent) { e.LineCol(e.Pos) }
						}
						count := 0
						var err error
						got := statetest.CatchPanic(func() {
							err = prog.ParseStreamWith("main", r, func(*sxirvalue.Node) error {
								count++
								if phase == "emit" && count == 2 {
									return stop
								}
								return nil
							}, opts)
						})
						if got != nil || err != stop || !errors.Is(err, stop) || count != 2 || r.Closed {
							t.Fatalf("panic=%v error=%v count=%d closed=%t", got, err, count, r.Closed)
						}
					})
				}
			}
		}
	}
}

func TestStreamReaderPanicInsideTraceKeepsOriginalValue(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, codegentest.Records)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(b.String()+"/"+u.String(), func(t *testing.T) {
				token := &statetest.StreamPanicToken{Phase: "nested reader"}
				r := &statetest.StreamReader{PanicValue: token}
				var err error
				got := statetest.CatchPanic(func() {
					err = prog.ParseStreamWith("main", r, nil, ParseOptions{Backend: b, Unit: u, Trace: func(e TraceEvent) {
						if e.Kind == TraceEnter && e.Rule == "main" {
							state.TraceParser(e).Fill(1)
						}
					}})
				})
				if got != token || err != nil {
					t.Fatalf("panic=%#v error=%v", got, err)
				}
			})
		}
	}
}

func TestStreamNormalCallerReturnKeepsVMFaultAttribution(t *testing.T) {
	t.Parallel()
	for _, b := range []Backend{Bytecode, BytecodeIterative} {
		for _, u := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			for _, phase := range []string{"reader", "emit"} {
				t.Run(fmt.Sprintf("%s/%s/%s", b, u, phase), func(t *testing.T) {
					prog := mustCompile(t, "rule main = item* #stream $$\nrule item = \"a\"")
					if _, err := prog.rule(b, "main", state.Inlined, nil); err != nil {
						t.Fatal(err)
					}
					// Corrupt the prepared module after a normal caller return. This fault is
					// engine-owned and must retain the VM's invalid-module classification.
					damage := func() { corruptVMLiteral(t, prog) }
					r := &statetest.StreamReader{Text: "aa"}
					if phase == "reader" {
						r.OnRead = damage
					}
					count := 0
					var err error
					got := statetest.CatchPanic(func() {
						err = prog.ParseStreamWith("main", r, func(*sxirvalue.Node) error {
							count++
							if phase == "emit" {
								damage()
							}
							return nil
						}, ParseOptions{Backend: b, Unit: u})
					})
					if got != nil || err == nil || !strings.Contains(err.Error(), "invalid bytecode:") {
						t.Fatalf("panic=%#v error=%v", got, err)
					}
					if phase == "emit" && count != 1 {
						t.Fatalf("emitted %d before VM fault", count)
					}
				})
			}
		}
	}
}

func TestStreamKeepsCallerBufferedReader(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, codegentest.Records)
	for _, u := range []input.PositionUnit{input.CodePoints, input.Bytes} {
		reader := bufio.NewReader(strings.NewReader("#records\na=1\n"))
		if p := state.NewStreamParser(&prog.sharedProgram, reader, u); p.Reader != reader {
			t.Fatal("stream parser replaced caller's buffered reader")
		}
	}
}

// corruptVMLiteral points the literal of every literal instruction of the Flow and stream bodies of the module
// of prog past the string table, bypassing the saved-module validator, so the VM faults when it runs
// one. It returns a function that restores the instructions.
func corruptVMLiteral(t *testing.T, prog *Program) (restore func()) {
	t.Helper()
	m := prog.Module()
	var undo []func()
	for _, f := range append(slices.Collect(maps.Values(m.Streams)), slices.Collect(maps.Values(m.Flows))...) {
		if f == nil {
			continue
		}
		for i, in := range f.Code {
			switch x := in.Imm.(type) {
			case bytecode.FlowLitOf:
				x.Str = len(m.Strings)
				f.Code[i].Imm = x
			case bytecode.FlowLitNodeOf:
				x.Lit.Str = len(m.Strings)
				f.Code[i].Imm = x
			default:
				continue
			}
			undo = append(undo, func() { f.Code[i] = in })
		}
	}
	if len(undo) == 0 {
		t.Fatal("module has no literal instruction")
	}
	return func() {
		for _, u := range undo {
			u()
		}
	}
}
