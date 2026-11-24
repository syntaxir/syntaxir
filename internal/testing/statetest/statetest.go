// Package statetest holds the helpers that tests of the parser state share across test packages:
// fault-injecting callbacks and readers, the leak check, and the run at lowered hot spans. It
// imports no engine code, so the engine's own in-package tests can share it with the tests of the
// state package.
package statetest

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strconv"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// CatchPanic runs f and returns the value it panicked with, or nil.
func CatchPanic(f func()) (got any) {
	defer func() { got = recover() }()
	f()
	return nil
}

// AssertTraceParserDetached checks that a finished parser, which a trace callback exposed, holds no
// reference or string any more and has the Aborted state that the parse ended in.
func AssertTraceParserDetached(t *testing.T, p *state.Parser, aborted bool) {
	t.Helper()
	if p == nil {
		t.Fatal("trace callback did not expose a parser")
	}
	if p.Aborted != aborted {
		t.Fatalf("aborted=%t want %t", p.Aborted, aborted)
	}
	var inspect func(reflect.Value, string)
	inspect = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
			if !v.IsNil() {
				t.Errorf("finished parser retains %s (%s)", path, v.Kind())
			}
		case reflect.String:
			if v.Len() != 0 {
				t.Errorf("finished parser retains %s (string)", path)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				inspect(v.Field(i), path+"."+v.Type().Field(i).Name)
			}
		case reflect.Array:
			for i := range v.Len() {
				inspect(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	inspect(reflect.ValueOf(*p), "parser")
}

// AssertReleased checks that the parser, which a trace callback exposed, was released after a caller
// panic: it holds no scratch and no budget control.
func AssertReleased(t *testing.T, p *state.Parser) {
	t.Helper()
	if p == nil || p.Scratch != nil || p.Control != nil {
		t.Fatalf("parser not released after caller panic: %#v", p)
	}
}

// StreamPanicToken is a runtime error that a stream callback or reader panics with.
type StreamPanicToken struct{ Phase string }

func (p *StreamPanicToken) Error() string { return "caller panic: " + p.Phase }
func (*StreamPanicToken) RuntimeError()   {}

// StreamReader is a reader of Text that calls OnRead once before its first read, then, at the end of
// the text, panics with PanicValue or fails with Err, or reports io.EOF. Closed is set by Close.
type StreamReader struct {
	Text       string
	Offset     int
	PanicValue any
	Err        error
	OnRead     func()
	Closed     bool
}

func (r *StreamReader) Read(b []byte) (int, error) {
	if r.OnRead != nil {
		f := r.OnRead
		r.OnRead = nil
		f()
	}
	if r.Offset < len(r.Text) {
		n := copy(b, r.Text[r.Offset:])
		r.Offset += n
		return n, nil
	}
	if r.PanicValue != nil {
		panic(r.PanicValue)
	}
	if r.Err != nil {
		return 0, r.Err
	}
	return 0, io.EOF
}

// Close records that the reader was closed.
func (r *StreamReader) Close() error { r.Closed = true; return nil }

// CancelledRefillReader cancels its context on the first read and returns First, then reports
// io.EOF, or no bytes at all while First is empty.
type CancelledRefillReader struct {
	Cancel context.CancelFunc
	First  []byte
	Calls  int
}

func (r *CancelledRefillReader) Read(dst []byte) (int, error) {
	r.Calls++
	if r.Calls == 1 {
		r.Cancel()
		return copy(dst, r.First), nil
	}
	if len(r.First) == 0 {
		return 0, nil
	}
	return 0, io.EOF
}

// OneByteReader returns one byte of Source per read and counts the reads.
type OneByteReader struct {
	Source string
	Calls  int
}

func (r *OneByteReader) Read(p []byte) (int, error) {
	r.Calls++
	if len(r.Source) == 0 {
		return 0, io.EOF
	}
	p[0] = r.Source[0]
	r.Source = r.Source[1:]
	return 1, nil
}

// ContextPanicToken is a runtime error that a context panics with.
type ContextPanicToken struct{ Phase string }

func (*ContextPanicToken) Error() string { return "context panic token" }
func (*ContextPanicToken) RuntimeError() {}

var _ runtime.Error = (*ContextPanicToken)(nil)

// ContextErrPanic is a context whose Err panics with Token at its PanicAt-th call once Armed. It
// records the budget Control's Used and SinceCheck at the panic.
type ContextErrPanic struct {
	context.Context
	done    chan struct{}
	Armed   bool
	PanicAt int
	Calls   int
	Token   *ContextPanicToken
	Control *state.BudgetControl
	AtPanic uint64
	Since   uint64
}

// NewContextErrPanic returns a context that panics with token at its panicAt-th Err call once armed.
func NewContextErrPanic(token *ContextPanicToken, panicAt int) *ContextErrPanic {
	return &ContextErrPanic{Context: context.Background(), done: make(chan struct{}), Token: token, PanicAt: panicAt}
}

func (c *ContextErrPanic) Done() <-chan struct{} { return c.done }

func (c *ContextErrPanic) Err() error {
	if !c.Armed {
		return nil
	}
	c.Calls++
	if c.Calls == c.PanicAt {
		if c.Control != nil {
			c.AtPanic, c.Since = c.Control.Used, c.Control.SinceCheck
		}
		panic(c.Token)
	}
	return nil
}

// ContextDonePanic is a context whose Done panics with Token.
type ContextDonePanic struct {
	context.Context
	Token *ContextPanicToken
}

func (c *ContextDonePanic) Done() <-chan struct{} { panic(c.Token) }

// MemoVars returns n variable values: slot i holds i.
func MemoVars(n int) []any {
	vals := make([]any, n)
	for i := range vals {
		vals[i] = i
	}
	return vals
}

// CatchFatal runs f and returns the error of the state.Fatal panic it raises, or nil. Any other panic
// fails the test.
func CatchFatal(t *testing.T, f func()) error {
	t.Helper()
	var got error
	func() {
		defer func() {
			if v := recover(); v != nil {
				fatal, ok := v.(state.Fatal)
				if !ok {
					t.Fatalf("panic = %T(%v), want fatal", v, v)
				}
				got = fatal.Err
			}
		}()
		f()
	}()
	return got
}

// PollingContext reports cancellation from the At-th Err call on (never when At is 0); Done never
// fires, so only polling sees it. Checks counts the Err calls.
type PollingContext struct {
	context.Context
	done   chan struct{}
	Checks int
	At     int
}

// NewPollingContext returns a PollingContext that cancels from its at-th Err call on.
func NewPollingContext(at int) *PollingContext {
	return &PollingContext{Context: context.Background(), done: make(chan struct{}), At: at}
}

func (c *PollingContext) Done() <-chan struct{} { return c.done }

func (c *PollingContext) Err() error {
	c.Checks++
	if c.At > 0 && c.Checks >= c.At {
		return context.Canceled
	}
	return nil
}

// NewBudgetParser returns a parser of an empty program under a budget control with the limits.
func NewBudgetParser(limits sxirruntime.Limits) *state.Parser {
	prog := &state.SharedProgram{}
	state.InitSharedProgram(prog, &program.Program{})
	return &state.Parser{Prog: prog, Input: state.Input{Control: state.NewBudgetControl(nil, limits)}}
}

// MemoParser returns a parser whose variables are MemoVars(n).
func MemoParser(n int) *state.Parser {
	p := &state.Parser{}
	p.SetVars(MemoVars(n))
	return p
}

// MemoPrefix puts 320 entries that hold retained in front of the table's first slot, the entries that
// a growth in progress must release when it stops.
func MemoPrefix(table *state.MemoTable, retained *value.Node) {
	if len(table.Slots) == 0 { // a growth in progress has no entry in the table
		table.Slots = append(table.Slots, nil)
	}
	for i := 0; i < 320; i++ {
		e := table.Alloc()
		*e = state.MemoEntry{Rule: int32(10000 + i), Node: retained, Next: table.Slots[0]}
		table.Slots[0] = e
	}
}

// RequireMemoCleared fails the test if a memo cell of the chunks still holds state.
func RequireMemoCleared(t *testing.T, chunks [][]state.MemoEntry) {
	t.Helper()
	for _, chunk := range chunks {
		for i := range chunk {
			if !reflect.DeepEqual(chunk[i], state.MemoEntry{}) {
				t.Fatalf("used memo cell %d retained state", i)
			}
		}
	}
}

// ActionBudgetParser returns a parser with a budget control under ctx and the limits, counting
// positions in the unit.
func ActionBudgetParser(ctx context.Context, limits sxirruntime.Limits, unit input.PositionUnit) *state.Parser {
	return &state.Parser{Input: state.Input{Unit: unit, Control: state.NewBudgetControl(ctx, limits)}}
}

// hotSpans are the thresholds at which the differential tests of run sites run: the one of the
// engine, at which the short inputs of the tests stay cold, and short ones, at which sites turn hot
// in the middle of a parse and the records answer the walks after it.
var hotSpans = []int{new(state.Tuning).RunHotSpan(), 2, 1}

// AtHotSpans runs f with the run-site and scan-site hot spans lowered to each of hotSpans (the
// test-only settings f passes to its parses), and checks that the sites turn hot at the short ones.
func AtHotSpans(t *testing.T, f func(t *testing.T, tune state.Tuning)) {
	t.Helper()
	for _, span := range hotSpans {
		// The counters are those of this span alone: they see no parse of another test.
		tune := state.Tuning{RunHot: span, ScanHot: span, Stats: new(state.HotStats)}
		t.Run("span"+strconv.Itoa(span), func(t *testing.T) { f(t, tune) })
		if span <= 2 && tune.Stats.RunHotTurns.Load() == 0 {
			t.Errorf("no run site turned hot at span %d", span)
		}
	}
}

// MissingNodes returns the missing-token nodes of a tree, each once, in walk order.
func MissingNodes(t *testing.T, root *value.Node) []*value.Node {
	t.Helper()
	var found []*value.Node
	seen := make(map[*value.Node]bool)
	if err := value.Walk(root, func(v value.WalkVisit) (value.WalkAction, error) {
		if v.Node != nil && v.Node.Type() == valueimpl.TypeMissing && !seen[v.Node] {
			seen[v.Node] = true
			found = append(found, v.Node)
		}
		return value.WalkContinue, nil
	}); err != nil {
		t.Fatal(err)
	}
	return found
}

// InsertionCycle is left-recursive only through an insertion: without insertion, a calls itself
// only after "(" consumed input.
const InsertionCycle = `rule main = a $$
rule a = "(" #missing a "x" / "y"`
