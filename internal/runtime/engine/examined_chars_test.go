package engine

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// TestExaminedCharacterComparisons checks the exact examined end of the call of atom at 0 in each case
// of codegentest.ExaminedChars, as the closure backend reports it.
func TestExaminedCharacterComparisons(t *testing.T) {
	t.Parallel()
	for _, tc := range codegentest.ExaminedChars() {
		t.Run(tc.Name, func(t *testing.T) {
			prog := mustCompile(t, tc.Src())
			unit := input.CodePoints
			if tc.Bytes {
				unit = input.Bytes
			}
			examined, matched, found := 0, false, false
			_, _ = prog.ParseWith("main", tc.Input, ParseOptions{
				Backend: Closure,
				Unit:    unit,
				Trace: func(e TraceEvent) {
					if e.Kind == TraceExit && e.Rule == "atom" && e.Pos == 0 {
						examined, matched, found = e.Examined, e.Matched, true
					}
				},
			})
			if !found {
				t.Fatal("no exit of atom at 0")
			}
			if matched != tc.Matched || examined != tc.Examined {
				t.Errorf("atom@0: matched %v, examined %d; want matched %v, examined %d", matched, examined, tc.Matched, tc.Examined)
			}
		})
	}
}

// TestFlowBodyExaminedCharacterComparisons checks that the Flow bodies of the bytecode VM report the
// examined ends of TestExaminedCharacterComparisons: the guard of a literal (GUARDLIT) compares the
// literal's first character byte by byte, and `$` (ATANCHOR) reads the slot at its position alone.
func TestFlowBodyExaminedCharacterComparisons(t *testing.T) {
	t.Parallel()
	for _, tc := range codegentest.ExaminedChars() {
		t.Run(tc.Name, func(t *testing.T) {
			_, flows, _ := flowProgramsOf(t, tc.Src(), func(string) bool { return true }, nil)
			m, err := flows.src.Module()
			if err != nil {
				t.Fatal(err)
			}
			atom := -1
			for i, r := range m.Rules {
				if m.Strings[r.Name] == "atom" {
					atom = i
				}
			}
			if m.Flows[atom] == nil {
				t.Fatalf("atom has no Flow body:\n%s", m.Disassemble())
			}
			unit := input.CodePoints
			if tc.Bytes {
				unit = input.Bytes
			}
			for _, b := range []Backend{Bytecode, BytecodeIterative} {
				examined, matched, found := 0, false, false
				_, _ = flows.ParseWith("main", tc.Input, ParseOptions{
					Backend: b,
					Unit:    unit,
					Trace: func(e TraceEvent) {
						if e.Kind == TraceExit && e.Rule == "atom" && e.Pos == 0 {
							examined, matched, found = e.Examined, e.Matched, true
						}
					},
				})
				if !found {
					t.Fatalf("%s: no exit of atom at 0", b)
				}
				if matched != tc.Matched || examined != tc.Examined {
					t.Errorf("%s: atom@0: matched %v, examined %d; want matched %v, examined %d", b, matched, examined, tc.Matched, tc.Examined)
				}
			}
		})
	}
}
