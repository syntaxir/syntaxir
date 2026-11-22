package libgo

import (
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// The first text of a code-point input containing invalid UTF-8 decodes it once; later texts are
// substrings of the decoded input.
func TestTextDecodesInvalidInputOnce(t *testing.T) {
	source := strings.Repeat("é\xff", 1000)
	want := strings.ToValidUTF8(source, "�")
	for _, controlled := range []bool{false, true} {
		p := &Parser{unit: sxirinput.CodePoints}
		if controlled {
			p.setControl(newBudgetControl(sxirruntime.Limits{Memory: 1 << 30}))
		}
		p.setSource(source)
		if p.offs != nil {
			t.Fatal("invalid input has an offset table before any text")
		}
		if got := p.Text(0, 2000); got != want {
			t.Fatalf("controlled=%t: text differs", controlled)
		}
		if got := p.Text(1, 1999); got != want[len("é"):len(want)-len("�")] {
			t.Fatalf("controlled=%t: decoded substring differs", controlled)
		}
		// A new source discards the decoded one.
		p.setSource("a\xfeb")
		if p.decoded || p.offs != nil {
			t.Fatal("a new source kept the decoded one")
		}
		if got := p.Text(0, 3); got != "a�b" {
			t.Fatalf("text of a new source %q", got)
		}
	}
}
