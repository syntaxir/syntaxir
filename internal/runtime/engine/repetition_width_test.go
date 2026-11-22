package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestWideRepetitionInstructionSet(t *testing.T) {
	t.Parallel()
	ordinary := mustCompile(t, `rule main = "a"* $$`)
	data, err := ordinary.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("ordinary module instruction set %d", data[isaOffset])
	}
	if strconv.IntSize < 64 {
		return
	}
	wide := mustCompile(t, `rule main = "a" #repeat(0, 4294967296) $$`)
	data, err = wide.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("wide module instruction set %d", data[isaOffset])
	}
	if !strings.Contains(wide.Module().Disassemble(), "= 4294967296\n") {
		t.Fatal("disassembly must display the complete bound")
	}
	// A valid checksum does not make a false instruction-set declaration valid.
	data[isaOffset] = bytecode.ISAVersion - 1
	data = binary.LittleEndian.AppendUint32(data[:len(data)-4], crc32.ChecksumIEEE(data[:len(data)-4]))
	if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("wide instruction in old ISA: %v", err)
	}
}

func TestWideRepetitionBounds(t *testing.T) {
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("bounds exceed a 32-bit implementation's source integer range")
	}
	for _, c := range codegentest.WideRepetitionCases() {
		t.Run(c.Name, func(t *testing.T) {
			for _, options := range []checkpkg.Options{{}, {DisableMemo: true}} {
				prog := mustCompile(t, c.Src, options)
				for _, input := range c.Inputs {
					checkBackends(t, prog, "main", input)
				}
				for _, mode := range valueModes {
					data, err := prog.Marshal("main", mode)
					if err != nil {
						t.Fatal(err)
					}
					loaded, _, err := LoadProgram(data)
					if err != nil {
						t.Errorf("load: %v", err)
						continue
					}
					for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
						if backend == Closure {
							_, err := loaded.ParseWith("main", c.Inputs[0], servedBy(mode, ParseOptions{Backend: backend}))
							wantClosureRefused(t, err)
							continue
						}
						for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
							for _, input := range c.Inputs {
								want, wantErr := prog.ParseWith("main", input, ParseOptions{Unit: unit})
								got, gotErr := loaded.ParseWith("main", input, servedBy(mode, ParseOptions{Backend: backend, Unit: unit}))
								if codegentest.ResultJSON(got, gotErr) != codegentest.ResultJSON(want, wantErr) {
									t.Errorf("loaded %s/%s on %q: got %s, want %s", backend, unit, input, codegentest.ResultJSON(got, gotErr), codegentest.ResultJSON(want, wantErr))
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestWideRepetitionDocumentsAndStreams(t *testing.T) {
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("bounds exceed a 32-bit implementation's source integer range")
	}
	for _, bound := range []string{"2147483648", "4294967296", "9223372036854775807"} {
		prog := mustCompile(t, fmt.Sprintf(`rule main = "é" #repeat(0,%s) #stream $$`, bound))
		modes := map[*Program]CompiledMode{prog: CompiledParse}
		programs := []*Program{prog}
		for _, mode := range valueModes {
			saved := saveLoad(t, prog, "main", mode)
			modes[saved] = mode
			programs = append(programs, saved)
		}
		for _, p := range programs {
			mode, loaded := p.Mode()
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if loaded && backend == Closure {
					_, err := p.ParseWith("main", "é", servedBy(mode, ParseOptions{Backend: backend}))
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					opts := ParseOptions{Backend: backend, Unit: unit}
					count := 0
					err := p.ParseStreamWith("main", strings.NewReader("éé"), func(n *value.Node) error {
						if nodes.D(n).Text != "é" {
							t.Errorf("unexpected stream element: %s", codegentest.ResultJSON(n, nil))
						}
						count++
						return nil
					}, servedBy(modes[p], opts))
					if err != nil || count != 2 {
						t.Errorf("%s %s/%s: emitted %d, error %v", bound, backend, unit, count, err)
					}
					// Document repetitions use run-site metadata as well as the VM operands.
					d, err := p.NewDocumentWith("main", "é", opts)
					if loaded && mode != CompiledDocument {
						if err == nil || !strings.Contains(err.Error(), "documents need document mode") {
							t.Errorf("Document on a %s-mode file: %v", mode, err)
						}
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					for range 2 {
						n, err := d.Parse()
						want, wantErr := prog.ParseWith("main", d.Text(), ParseOptions{Unit: unit})
						if codegentest.ResultJSON(n, err) != codegentest.ResultJSON(want, wantErr) {
							t.Errorf("Document %s/%s: got %s, want %s", backend, unit, codegentest.ResultJSON(n, err), codegentest.ResultJSON(want, wantErr))
						}
						if err := d.Edit(0, 0, "é"); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		}
	}
}
