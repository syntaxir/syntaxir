package state_test

import (
	"fmt"
	"io"
	"testing"

	. "syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// streamChunkReader returns at most size bytes per Read.
type streamChunkReader struct {
	s    string
	size int
}

func (r *streamChunkReader) Read(b []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := min(len(r.s), r.size, len(b))
	copy(b, r.s[:n])
	r.s = r.s[n:]
	return n, nil
}

// TestStreamTextCutAtCommit checks that a text in a stream is cut at the last commit point: the
// outcome, the elements and the compute units of each input are the same for every chunking of the
// reader on every backend, and the predicates of the grammar hold where the cut texts say they do.
// The reference build counts no units, so there the outcome and the elements are compared.
func TestStreamTextCutAtCommit(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, codegentest.StreamTextCommitGrammar)
	for k, in := range codegentest.StreamTextCommitInputs {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			var first string
			for _, b := range append([]Backend{Closure}, vmBackends...) {
				for _, size := range codegentest.StreamTextCommitChunks {
					var units uint64
					o := ParseOptions{Backend: b, Unit: unit, Expectations: true}
					if state.BudgetControlsEnabled {
						o.ComputeReport = &units
					}
					n := 0
					err := prog.ParseStreamWith("main", &streamChunkReader{s: in, size: size}, func(*value.Node) error {
						n++
						return nil
					}, o)
					got := fmt.Sprintf("%d elements, units %d, error %v", n, units, err)
					if first == "" {
						first = got
						if fails := k >= len(codegentest.StreamTextCommitInputs)-codegentest.StreamTextCommitFails; fails != (err != nil) {
							t.Errorf("%.20q (%v): %s", in, unit, got)
						}
					} else if got != first {
						t.Errorf("%.20q (%v, %v, chunks of %d): %s, want %s", in, unit, b, size, got, first)
					}
				}
			}
		}
	}
}

// TestStreamTextUnits pins the compute units of a text in a stream: the units of a grammar whose
// predicate compares the text of its head with a string, less those of the same grammar without the
// predicates, are the length of the cut text plus the comparison's (the bytes of the shorter
// operand). Once an element was delivered the text is empty and both are 0; when none was it is the
// whole head, 3 code points or 4 bytes, compared in 4 bytes; on every backend and chunking.
func TestStreamTextUnits(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("the reference build counts no compute units")
	}
	t.Parallel()
	texts := enginetest.MustCompile(t, `@start
rule main = h:head item* #stream ("!" {text($h) == ""} / "?" {text($h) == "hé>"})
rule head = [a-zé]+ ">"
rule item = [a-zé]+ ";"
`)
	none := enginetest.MustCompile(t, `@start
rule main = h:head item* #stream ("!" {true} / "?" {true})
rule head = [a-zé]+ ">"
rule item = [a-zé]+ ";"
`)
	units := func(prog *Program, in string, b Backend, unit input.PositionUnit, size int) uint64 {
		t.Helper()
		var n uint64
		err := prog.ParseStreamWith("main", &streamChunkReader{s: in, size: size}, func(*value.Node) error { return nil },
			ParseOptions{Backend: b, Unit: unit, ComputeReport: &n})
		if err != nil {
			t.Fatalf("%q (%v, %v, chunks of %d): %v", in, unit, b, size, err)
		}
		return n
	}
	for _, c := range []struct {
		in           string
		points, byts uint64
	}{{"hé>a;!", 0, 0}, {"hé>ab;é;!", 0, 0}, {"hé>?", 3 + 4, 4 + 4}} {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			want := c.points
			if unit == input.Bytes {
				want = c.byts
			}
			for _, b := range append([]Backend{Closure}, vmBackends...) {
				for _, size := range []int{1, 1 << 20} {
					if got := units(texts, c.in, b, unit, size) - units(none, c.in, b, unit, size); got != want {
						t.Errorf("%q (%v, %v, chunks of %d): units of the texts %d, want %d", c.in, unit, b, size, got, want)
					}
				}
			}
		}
	}
}
