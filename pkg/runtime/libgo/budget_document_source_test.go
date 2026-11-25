package libgo

import (
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// The preparation of the input is free (spec/runtime/parsing.md, Compute Units): decoding the source of
// a Document, or of a batch parse, counts no unit under any compute limit, and stores every character.
func TestSourcePreparationCountsNoUnits(t *testing.T) {
	long := strings.Repeat("abcdefghij", 300)
	for _, s := range []string{"", "abc", "é", "日本語", "a\xffb", "\xe6\x97", long, long + "é日" + long, long[:100] + "\xff\xfe" + long[:100]} {
		for _, document := range []bool{false, true} {
			p := &Parser{}
			if document {
				p.Document = &documentState{}
			}
			p.startUnits(prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Compute: 1}}, 0, false))
			p.setSource(s)
			if p.UnitsNow() != 0 {
				t.Fatalf("%q document=%t: preparation counted %d units", s, document, p.UnitsNow())
			}
			if want := []rune(s); string(p.In) != string(want) {
				t.Fatalf("%q document=%t: stored %q", s, document, p.In)
			}
		}
	}
}
