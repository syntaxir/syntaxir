//go:build unix

package libgo

import (
	"bufio"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// unreadableText returns a string of n bytes over a mapping that faults when read, so a check of its
// length that read it would crash the test rather than pass. The mapping takes address space only.
func unreadableText(t *testing.T, n int) string {
	t.Helper()
	b, err := syscall.Mmap(-1, 0, n, syscall.PROT_NONE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		t.Skipf("no mapping of %d bytes: %v", n, err)
	}
	t.Cleanup(func() { _ = syscall.Munmap(b) })
	return unsafe.String(unsafe.SliceData(b), n)
}

// No input is longer than 2^31−1 units: zero, Unlimited and any larger Input select that bound, the
// text of exactly 2^31−1 bytes passes the check and one more byte fails it, with the bound as the
// limit. The check reads no text, and holds with limit controls and without them.
func TestPrepareTextMaxInput(t *testing.T) {
	if maxInputUnits != 1<<31-1 {
		t.Fatalf("maxInputUnits %d", maxInputUnits)
	}
	at := unreadableText(t, maxInputUnits+1)
	check := func(o parseOptions, text string) (le *sxirruntime.LimitError) {
		defer func() {
			if x := recover(); x != nil {
				f, ok := x.(fatal)
				if !ok {
					panic(x)
				}
				if le, ok = f.err.(*sxirruntime.LimitError); !ok {
					panic(x)
				}
			}
		}()
		prepareText(o, text)
		return nil
	}
	for _, max := range []uint64{0, sxirruntime.Unlimited, maxInputUnits, maxInputUnits + 1, 1 << 40} {
		for _, controls := range []bool{true, false} {
			o := parseOptions{unit: sxirinput.Bytes, limits: sxirruntime.Limits{Input: max}, controls: controls}
			if le := check(o, at[:maxInputUnits]); le != nil {
				t.Errorf("Input %d, controls %v: 2^31−1 bytes failed: %v", max, controls, le)
			}
			le := check(o, at)
			if le == nil || le.Kind != sxirruntime.LimitInput || le.Limit != maxInputUnits || le.Requested != maxInputUnits+1 || le.Used != 0 {
				t.Errorf("Input %d, controls %v: 2^31 bytes gave %+v", max, controls, le)
			}
		}
	}
	// A Document checks the length it keeps for its text in the same way.
	for _, n := range []uint64{maxInputUnits, maxInputUnits + 1} {
		func() {
			defer func() {
				x := recover()
				f, _ := x.(fatal)
				le, _ := f.err.(*sxirruntime.LimitError)
				if (n > maxInputUnits) != (le != nil) || x != nil && le == nil {
					t.Errorf("%d units: %v", n, x)
				}
			}()
			prepareUnits(parseOptions{unit: sxirinput.CodePoints}, n)
		}()
	}
	// A stream holds the bound whatever Input selects it.
	for _, max := range []uint64{0, sxirruntime.Unlimited, 1 << 40} {
		if o := prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Input: max}}, 0, true); o.maxInput != maxInputUnits {
			t.Errorf("Input %d: stream bound %d", max, o.maxInput)
		}
	}
}

// A stream fails when the parser asks for the unit after the 2^31−1st and the stream has one, and
// completes when it ends there. The stream starts just below the bound instead of reading 2 GiB.
func TestStreamFillMaxInput(t *testing.T) {
	stream := func(rest string) *Parser {
		r := bufio.NewReader(strings.NewReader(rest))
		return &Parser{unit: sxirinput.Bytes, Stream: &streamInput{reader: r, base: maxInputUnits - 1, maxInput: maxInputUnits}}
	}
	fill := func(p *Parser, n int) (ok bool, le *sxirruntime.LimitError) {
		defer func() {
			if x := recover(); x != nil {
				f, _ := x.(fatal)
				if le, _ = f.err.(*sxirruntime.LimitError); le == nil {
					panic(x)
				}
			}
		}()
		return p.streamFill(n), nil
	}
	if ok, le := fill(stream("ab"), maxInputUnits); !ok || le != nil {
		t.Errorf("unit 2^31−1: %v %v", ok, le)
	}
	_, le := fill(stream("ab"), maxInputUnits+1)
	if le == nil || le.Kind != sxirruntime.LimitInput || le.Limit != maxInputUnits || le.Requested != maxInputUnits+1 {
		t.Errorf("unit 2^31: %+v", le)
	}
	if ok, le := fill(stream("a"), maxInputUnits+1); ok || le != nil {
		t.Errorf("a stream of 2^31−1 units: %v %v", ok, le)
	}
}

// A Document of exactly 2^31−1 units parses, node and typed, and one of 2^31 units fails before any
// work with a LimitError of kind LimitInput, whatever Input selects, through the entry points of the
// generated Document.Parse and Document.ParseValue. The start rule moves to the end of the input
// without reading it and succeeds there, so that the parse reads no input.
func TestDocumentParseMaxInput(t *testing.T) {
	at := unreadableText(t, maxInputUnits+1)
	node := &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}
	node.Body = func(p *Parser, _ int) (*sxirvalue.Node, bool) {
		p.Pos = p.n
		return nil, true
	}
	typed := &TRule{Rule: &Rule{Id: 0, Name: "main", Memo: true, DocMemo: true, Seen: -1}}
	typed.Body = func(p *TParser, _ int) (any, bool) {
		p.Pos = p.n
		return nil, true
	}
	parsers := map[string]func(text string, o parseOptions) error{
		"node": func(text string, o parseOptions) error {
			_, err := testDocumentState(node, text, sxirinput.Bytes).parse(o)
			return err
		},
		"typed": func(text string, o parseOptions) error {
			d := &Document{State: testDocumentState(typed.Rule, text, sxirinput.Bytes)}
			_, err := TypedDocumentParse(d, typed, o, noChunks{}, func(v any) any { return v })
			return err
		},
	}
	for name, parse := range parsers {
		for _, max := range []uint64{0, sxirruntime.Unlimited, 1 << 40} {
			o := parseOptions{unit: sxirinput.Bytes, maxDepth: defaultMaxDepth, limits: sxirruntime.Limits{Input: max}, controls: true}
			if err := parse(at[:maxInputUnits], o); err != nil {
				t.Errorf("%s, Input %d: 2^31−1 units: %v", name, max, err)
			}
			le, _ := parse(at, o).(*sxirruntime.LimitError)
			if le == nil || le.Kind != sxirruntime.LimitInput || le.Limit != maxInputUnits || le.Requested != maxInputUnits+1 {
				t.Errorf("%s, Input %d: 2^31 units gave %+v", name, max, le)
			}
		}
	}
}
