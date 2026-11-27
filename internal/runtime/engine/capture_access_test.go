package engine

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// TestCaptureAccessAcrossBackends checks that every backend returns the values that the specification
// gives to the capture access cases (codegentest.CaptureAccessResults), so that a reading of captures
// that every backend shares is caught too, and that recognition agrees across the backends.
func TestCaptureAccessAcrossBackends(t *testing.T) {
	t.Parallel()
	results := codegentest.CaptureAccessResults()
	for _, fixture := range codegentest.CaptureAccessCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			want, ok := results[fixture.Name]
			if !ok || len(want) != len(fixture.Inputs) {
				t.Fatalf("no result for each of the %d inputs: %d", len(fixture.Inputs), len(want))
			}
			program := mustCompile(t, fixture.Src)
			for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
				for _, recognize := range []bool{false, true} {
					for i, input := range fixture.Inputs {
						baseline := codegentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: recognize}))
						if !recognize && baseline != want[i] {
							t.Fatalf("closure/%s input=%q: %s, want %s", unit, input, baseline, want[i])
						}
						for _, backend := range []Backend{Bytecode, BytecodeIterative} {
							actual := codegentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}))
							if actual != baseline {
								t.Fatalf("%s/%s recognize=%t input=%q: %s / %s", backend, unit, recognize, input, actual, baseline)
							}
						}
					}
				}
			}
		})
	}
}

// A value of a named string node type has the members of a node.
func TestNodeMembersOfNamedStringNodes(t *testing.T) {
	t.Parallel()
	const src = `type Num = node[string]
type Out = { S int; E int; V string }
rule main = "-" n:num $$ => Out{S: $n.Start, E: $n.End, V: $n.Value}
rule num Num = [0-9]+ #atomic`
	program := mustCompile(t, src)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := program.ParseWith("main", "-12", ParseOptions{Backend: backend})
		if err != nil || n.String() != `Out{E: 3, S: 1, V: "12"}` {
			t.Fatalf("%s: %v %v\n%s", backend, n, err, src)
		}
	}
}

// A text struct value reads its text through its declared field.
func TestTextStructFieldRead(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, `type Num = { Text string }
type Out = { V string; N int }
rule main = "-" n:num $$ => Out{V: $n.Text, N: len($n.Text)}
rule num Num = t:([0-9]+ #atomic) => Num{Text: text($t)}`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := program.ParseWith("main", "-12", ParseOptions{Backend: backend})
		if err != nil || n.String() != `Out{N: 2, V: "12"}` {
			t.Fatalf("%s: %v %v", backend, n, err)
		}
	}
}

// x.$y reads capture y of node x and projects a repetition. A member x.y, which the source syntax
// rejects, is rejected by grammar validation with a pointer to .$y when the grammar API builds it.
func TestCaptureAccessMatchesField(t *testing.T) {
	t.Parallel()
	access := mustCompile(t, `rule main = xs:(-"," y:[a-z])* $$ => map($xs, (i) => i.$y)`)
	if a := checkpkg.RuleProjections(access.Static().Model, access.Static().RuleList[0]); a["xs"] != "y" {
		t.Fatalf("projection: %v", a)
	}
	g, err := parseTest(`rule main = xs:(-"," y:[a-z])* $$ => map($xs, (i) => i.$y)`)
	if err != nil {
		t.Fatal(err)
	}
	member := g.Rules()[0].Action.(*ast.Call).Args[1].(*ast.Lambda).Body.(*ast.FieldAccess)
	member.Name = "y"
	if _, err := Compile(g, checkpkg.Options{}); err == nil || !strings.Contains(err.Error(), "a capture is read with .$y") {
		t.Fatalf("field access on a node: %v", err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for input, want := range map[string]string{",a,b": `["a" "b"]`, "": `[]`, ",a,": "error"} {
			n, err := access.ParseWith("main", input, ParseOptions{Backend: backend})
			got := "error"
			if err == nil {
				got = n.String()
			}
			if got != want {
				t.Fatalf("%s %q: %s, want %s", backend, input, got, want)
			}
		}
	}
}

// TestCaptureAccessCompiledFile checks that a runtime of an earlier instruction set refuses a compiled
// file with capture access.
func TestCaptureAccessCompiledFile(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, `rule main = xs:(y:"a" "b")* $$ => $xs[0].$y`)
	data, err := program.MarshalBinary("main")
	if err != nil || data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("capture access ISA: %v", err)
	}
	// A runtime that knows only an earlier instruction set must not load it.
	data[isaOffset] = bytecode.ISAVersion - 1
	binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
	if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("under-declared capture access ISA accepted: %v", err)
	}
}

// TestStreamOutsideCapturedSequences requires #stream to stand outside every captured sequence. A
// stream parse releases the capture trail at each commit, while a captured sequence attaches the
// captures that its elements wrote on the trail since it began; a capture holds only the stream
// repetition itself, which is no sequence and carries no captures.
func TestStreamOutsideCapturedSequences(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = "h" x:(y:"a" item* #stream) $$`,
		`rule main = "h" x:(y:"a" (z:item)* #stream) $$`,
		`rule main = x:("h" (y:"a" item* #stream)) $$`,
		`rule main = x:(y:"h" w:(y:"a" item* #stream)) $$`,
	} {
		_, err := Compile(mustParse(src+"\nrule item = [0-9]"), checkpkg.Options{})
		if err == nil || !strings.Contains(err.Error(), "#stream is only allowed at the top level of a rule body") {
			t.Errorf("%s: got %v; want the #stream placement error", src, err)
		}
	}
	prog := mustCompile(t, "rule main = x:(y:item* #stream) $$\nrule item = [0-9]")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n := 0
		err := prog.ParseStreamWith("main", strings.NewReader(strings.Repeat("1", 5000)), func(*sxirvalue.Node) error { n++; return nil }, ParseOptions{Backend: backend})
		if err != nil || n != 5000 {
			t.Errorf("%s: %d elements, %v; want 5000", backend, n, err)
		}
	}
}
