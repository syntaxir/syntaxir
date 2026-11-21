package bytecode_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/bytecode"
)

// TestSpecInstructionSetVersion checks that spec/bytecode/README.md states the instruction-set version the
// runtime writes and accepts.
func TestSpecInstructionSetVersion(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../spec/bytecode/README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`\| Instruction-set version \| Unsigned integer for this module: (\d+)\.`).FindSubmatch(data)
	if m == nil {
		t.Fatal("spec/bytecode/README.md does not state the instruction-set version of a module")
	}
	if got, _ := strconv.Atoi(string(m[1])); got != bytecode.ISAVersion {
		t.Fatalf("spec/bytecode/README.md states instruction-set version %d; the runtime uses %d", got, bytecode.ISAVersion)
	}
}

// TestDocsInstructionSetVersion checks that the runtime specification of compiled grammars, the
// guide and the release policy give the instruction-set version the runtime writes, as the bytecode
// specification does.
func TestDocsInstructionSetVersion(t *testing.T) {
	t.Parallel()
	for _, d := range []struct{ path, pattern string }{
		{"../../spec/runtime/compiled-grammars.md", `\| Instruction-set version \| (\d+) \|`},
		{"../../docs/guide/compiled-grammars.md", `\| Instruction-set version \| (\d+) \|`},
		{"../../docs/releasing.md", `instruction-set version (\d+)`},
	} {
		data, err := os.ReadFile(d.path)
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(d.pattern).FindSubmatch(data)
		if m == nil {
			t.Errorf("%s does not state the instruction-set version", d.path)
			continue
		}
		if got, _ := strconv.Atoi(string(m[1])); got != bytecode.ISAVersion {
			t.Errorf("%s states instruction-set version %d; the runtime uses %d", d.path, got, bytecode.ISAVersion)
		}
	}
}

// TestSpecMatchInstructions checks that the instruction table of Match Instructions in
// spec/bytecode/README.md gives every instruction of match code its opcode and name, and no other:
// a module stores the opcodes as they are, so a change to either changes the file format.
func TestSpecMatchInstructions(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../spec/bytecode/README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, sec, ok := strings.Cut(string(data), "\n## Match Instructions\n")
	if !ok {
		t.Fatal("spec/bytecode/README.md has no Match Instructions section")
	}
	sec, _, _ = strings.Cut(sec, "\n## ")
	spec := map[bytecode.Op]string{}
	for _, m := range regexp.MustCompile("(?m)^\\| (\\d+) \\| `([A-Z_]+)` \\|").FindAllStringSubmatch(sec, -1) {
		code, _ := strconv.Atoi(m[1])
		op := bytecode.Op(code)
		if name, dup := spec[op]; dup {
			t.Errorf("the spec gives opcode %d to both %s and %s", code, name, m[2])
		}
		spec[op] = m[2]
	}
	for op := bytecode.OpStr; op <= bytecode.OpScanSite; op++ {
		if got, want := spec[op], op.String(); got != want {
			t.Errorf("opcode %d is %s; the spec names it %q", op, want, got)
		}
		delete(spec, op)
	}
	for op, name := range spec {
		t.Errorf("the spec gives opcode %d to %s, which is not an instruction of match code", op, name)
	}
}
