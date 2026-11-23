package engine

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

func TestCastAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range codegentest.CastCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			program := mustCompile(t, fixture.Src)
			for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
				for _, recognize := range []bool{false, true} {
					for _, input := range fixture.Inputs {
						baseline := codegentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: recognize}))
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

func TestCastValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, input, want string }{
		{"rule main = x:(n / w) $$ => $x.(Num)\n" + codegentest.CastRules, "12", `"type":"Num","start":0,"end":2,"fields":{"Text":"12"}`},
		{"rule main = x:(n / w) $$ => $x.(Num)\n" + codegentest.CastRules, "ab", "cannot cast node[[]_] to Num"},
		{"rule main = x:(n+ / w) $$ => $x.([]Num)\n" + codegentest.CastRules, "ab", "cannot cast node[[]_] to []Num"},
		{"rule main = x:(n / w)? $$ => $x.(*Num)\n" + codegentest.CastRules, "", "null"},
		{"rule main = x:(pair / w) $$ => $x.(P).N\n" + codegentest.CastRules, "1=", `"type":"Num","start":0,"end":1,"fields":{"Text":"1"}`},
		{"rule main = \"\\n\" r\nrule r = x:(n / w) $$ => $x.(Num)\n" + codegentest.CastRules, "\nab", "cannot cast node[[]_] to Num at input position 1 (line 2, column 1); cast at grammar position 2:29"},
		{"rule main = x:(n / w) ({$x.(Num).Text != \"\"} \"!\" / \"?\") $$\n" + codegentest.CastRules, "a?", `"value":"?"`},
	} {
		program := mustCompile(t, tc.src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := codegentest.ResultJSON(program.ParseWith("main", tc.input, ParseOptions{Backend: backend})); !strings.Contains(got, tc.want) {
				t.Errorf("%s %q: %s, want %s", backend, tc.input, got, tc.want)
			}
		}
	}
}

func TestCastErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule main = x:"a" => $x.(int)`, "cannot cast node[string] to int"},
		{`rule main = x:"a" => $x.(Nope)`, "Nope"},
		{"rule main = x:n => $x.(P)\n" + codegentest.CastRules, "cannot cast Num to P"},
		{"rule main = x:n => $x.(Num).N\n" + codegentest.CastRules, "N"},
		{`rule main = "a" => (1).(string)`, "cannot cast int to string"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, checkpkg.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// An unknown type is rejected.
	g, err := parseTest(`rule main = x:"a" => $x.(Nope)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, checkpkg.Options{}); err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("unknown cast type: %v", err)
	}
}

// TestCastCompiledFile checks that a cast survives a compiled file and that a runtime of an earlier
// instruction set refuses it.
func TestCastCompiledFile(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, "rule main = x:(n / w) $$ => $x.(Num)\n"+codegentest.CastRules)
	// A cast survives a compiled file.
	data, err := program.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"1", "a"} {
		if got, want := codegentest.ResultJSON(loaded.ParseWith("main", input, ParseOptions{})), codegentest.ResultJSON(program.ParseWith("main", input, ParseOptions{})); got != want {
			t.Fatalf("loaded %q: %s / %s", input, got, want)
		}
	}
	data, err = program.MarshalBinary("main")
	if err != nil || data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("cast ISA: %v", err)
	}
	// A runtime that knows only an earlier instruction set must not load it.
	data[isaOffset] = bytecode.ISAVersion - 1
	binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
	if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("under-declared cast ISA accepted: %v", err)
	}
	// A cast to any checks nothing; it uses the same instruction set.
	data, err = mustCompile(t, "rule main = x:(n / w) $$ => $x.(_)\n"+codegentest.CastRules).MarshalBinary("main")
	if err != nil || data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("cast to any ISA: %v %d", err, data[isaOffset])
	}
}
