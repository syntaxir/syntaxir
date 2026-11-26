package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A capture inside a recovered expression is the field *Word in the typed Go API, the same Go type
// as a capture made in every parse (struct node fields are pointers), and the typed TypeScript API
// spells both Word | null (gents_recover_typed_test.go), so the two APIs agree on the capture type.
func TestGeneratedTypedRecoveredCaptureField(t *testing.T) {
	t.Parallel()
	const src = `type Word = { Text string }
type R = { V *Word; W Word }
@start
rule main R = (v:letter "b") #recover(skip="z") w:letter => R{V: $v, W: $w}
rule letter Word = t:([ac]) => Word{Text: text($t)}`
	types, err := codegentest.GoTypesOf(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(types, "V *Word") {
		t.Errorf("types.go lacks the optional capture field V *Word:\n%s", types)
	}
	if !strings.Contains(types, "W *Word") {
		t.Errorf("types.go lacks the field W *Word:\n%s", types)
	}
}
