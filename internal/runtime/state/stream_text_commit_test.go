package state_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	. "syntaxir.dev/sxir/internal/runtime/engine"
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

// TestStreamTextCutAtCommit checks that the texts a stream takes after commits do not depend on the
// input it still holds: the outcome, the elements and the compute units of each input are the same
// for every chunking of the reader on every backend, and the predicates of the grammar hold where the
// whole texts say they do. A text of a range before the last commit point, which the stream cuts
// there, is one no grammar can take after the repetition, so no chunking can show the cut.
func TestStreamTextCutAtCommit(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, codegentest.StreamTextCommitGrammar)
	for k, in := range codegentest.StreamTextCommitInputs {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			var first string
			for _, b := range append([]Backend{Closure}, vmBackends...) {
				for _, size := range codegentest.StreamTextCommitChunks {
					var units uint64
					n := 0
					err := prog.ParseStreamWith("main", &streamChunkReader{s: in, size: size}, func(*value.Node) error {
						n++
						return nil
					}, ParseOptions{Backend: b, Unit: unit, Expectations: true, ComputeReport: &units})
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
// predicate compares the text of its head, a string node, with a string, less those of the same
// grammar without the predicates, are the length of the text plus the comparison's (the bytes of
// the shorter operand): the whole head, 3 code points or 4 bytes, compared in 4 bytes, whether or not
// an element was delivered before it; on every backend and chunking. The same grammar with a head
// that is not a string node, whose text the stream cuts at the commit point, is rejected.
func TestStreamTextUnits(t *testing.T) {
	t.Parallel()
	texts := enginetest.MustCompile(t, `@start
rule main = h:head item* #stream ("!" {text($h) == "hé>"} / "?" {text($h) == "hé>"})
rule head = ([a-zé]+ ">") #atomic
rule item = [a-zé]+ ";"
`)
	none := enginetest.MustCompile(t, `@start
rule main = h:head item* #stream ("!" {true} / "?" {true})
rule head = ([a-zé]+ ">") #atomic
rule item = [a-zé]+ ";"
`)
	if msg := enginetest.CompileMessage(t, `@start
rule main = h:head item* #stream ("!" {text($h) == ""} / "?" {text($h) == "hé>"})
rule head = [a-zé]+ ">"
rule item = [a-zé]+ ";"
`); !strings.Contains(msg, "text of capture h, made before a #stream repetition, cannot be taken after it") {
		t.Errorf("a cut head: %s", msg)
	}
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
	for _, in := range []string{"hé>a;!", "hé>ab;é;!", "hé>?"} {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			want := uint64(3 + 4)
			if unit == input.Bytes {
				want = 4 + 4
			}
			for _, b := range append([]Backend{Closure}, vmBackends...) {
				for _, size := range []int{1, 1 << 20} {
					if got := units(texts, in, b, unit, size) - units(none, in, b, unit, size); got != want {
						t.Errorf("%q (%v, %v, chunks of %d): units of the texts %d, want %d", in, unit, b, size, got, want)
					}
				}
			}
		}
	}
}
