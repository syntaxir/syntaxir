package bytecode_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
)

// TestModuleCompilesCorpus checks that every test grammar compiles to bytecode and that all
// branch targets of its Flow bodies are within the body.
func TestModuleCompilesCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.GenCorpus(t) {
		m := enginetest.MustCompile(t, c.Src).Module()
		for rule, f := range m.Flows {
			for i, in := range f.Code {
				if s, _ := in.Op.Shape(); s.Target && (in.Target < 0 || in.Target >= len(f.Code)) {
					t.Errorf("%s: rule %d: instruction %d: jump to %d", c.Name, rule, i, in.Target)
				}
			}
		}
		if !strings.Contains(m.Disassemble(), "RETURN") {
			t.Errorf("%s: no RETURN", c.Name)
		}
	}
}
