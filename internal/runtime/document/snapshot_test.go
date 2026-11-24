package document_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/document"
	. "syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

func TestSnapshotControlledFreezeCancellationAndRetry(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls reference")
	}
	prog := mustCompile(t, `rule main = .* $$`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, _ := prog.NewDocumentWith("main", strings.Repeat("x", 8192), o)
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal(err)
		}
		// A memo-hit parse succeeds with this allowance, and so does the capture of its existing graph
		// and text: freezing is Document work, which no Compute limit counts.
		o.Limits = runtime.Limits{Compute: 100}
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal("memo-hit fixture", err)
		}
		if s, err := d.SnapshotWith(o); s == nil || err != nil || s.Text() != d.Text() {
			t.Fatal("freeze under a compute limit", s, err)
		}
		// Cancellation still interrupts the freezing, which polls the context.
		o.Limits = runtime.Limits{}
		s, err := d.SnapshotWith(cancelDuringCapture(o))
		if s != nil || !errors.Is(err, context.Canceled) || d.Memo().Len() != 0 || d.Runs() != nil || d.Kids() != nil || d.Machine() != nil {
			t.Fatal("freeze interruption", s, err)
		}
		s, err = d.SnapshotWith(o)
		if s == nil || err != nil || s.Text() != d.Text() {
			t.Fatal("retry", err)
		}
		if resultJSON(s.Materialize(), s.Error()) != resultJSON(prog.ParseWith("main", d.Text(), o)) {
			t.Fatal("fresh mismatch")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		o.Context = ctx
		before := d.Memo()
		stats := d.Stats()
		s, err = d.SnapshotWith(o)
		if s != nil || !errors.Is(err, context.Canceled) || d.Memo() != before || d.Stats() != stats {
			t.Fatal("pre-execution cancellation", err)
		}
	})
}

func TestSnapshotDiagnosticCopyAndNilResult(t *testing.T) {
	original := value.SyntaxErrors{&value.SyntaxError{Expected: []string{}, Messages: []string{"problem"}}, nil}
	copy := document.CopySnapshotError(&state.Parser{}, original).(value.SyntaxErrors)
	original[0].Messages[0] = "changed"
	original[0] = nil
	if copy[0].Messages[0] != "problem" || copy[0].Expected == nil || copy[1] != nil {
		t.Fatal("diagnostic copy")
	}
	prog := mustCompile(t, `rule main = "x" => nil`)
	d, _ := prog.NewDocument("main", "x")
	s, err := d.SnapshotWith(ParseOptions{})
	if s == nil || err != nil || s.Root().Valid() || s.Materialize() != nil {
		t.Fatal("nil parse result")
	}
}

func TestSnapshotDocumentVersionsAndConcurrentReads(t *testing.T) {
	prog := mustCompile(t, `rule main = item* $$
rule item = [a-zé]`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		o = withExpectations(o)
		d, err := prog.NewDocumentWith("main", "aé", o)
		if err != nil {
			t.Fatal(err)
		}
		s, err := d.SnapshotWith(o)
		if err != nil {
			t.Fatal(err)
		}
		original := resultJSON(s.Materialize(), s.Error())
		if s.Text() != "aé" || s.Version() != d.Version() || s.Unit() != o.Unit || s.Stats() != d.Stats() {
			t.Fatal("outcome metadata")
		}
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Go(func() {
				for j := 0; j < 100; j++ {
					if resultJSON(s.Materialize(), s.Error()) != original {
						t.Error("concurrent snapshot changed")
					}
					s.Root().Fields()
				}
			})
		}
		if err := d.Edit(0, 1, "z"); err != nil {
			t.Fatal(err)
		}
		latest, err := d.SnapshotWith(o)
		if err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if latest.Text() != "zé" || latest.Version() == s.Version() || s.Text() != "aé" || resultJSON(s.Materialize(), s.Error()) != original {
			t.Fatal("edit changed snapshot")
		}
		if err := d.Edit(0, 1, "!"); err != nil {
			t.Fatal(err)
		}
		failed, err := d.SnapshotWith(o)
		var syntax *value.SyntaxError
		if failed == nil || failed.Root().Valid() || !errors.As(err, &syntax) || failed.Text() != "!é" {
			t.Fatal("failed revision", err)
		}
		before := failed.Error().Error()
		syntax.Expected[0] = "mutated"
		syntax.Pos = 999
		if failed.Error().Error() != before {
			t.Fatal("returned error alias")
		}
		copy := failed.Error().(*value.SyntaxError)
		copy.Expected[0] = "again"
		copy.Line = 999
		if failed.Error().Error() != before {
			t.Fatal("getter error alias")
		}
	})
}

func TestSnapshotFreezeCancellationAndContextPanic(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls reference")
	}
	prog := mustCompile(t, `rule main = .* $$`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, _ := prog.NewDocumentWith("main", strings.Repeat("x", 4096), o)
		if _, err := d.ParseWith(o); err != nil {
			t.Fatal(err)
		}
		for _, panicCase := range []bool{false, true} {
			ctx := &snapshotPhaseContext{Context: context.Background(), done: make(chan struct{})}
			token := &struct{ message string }{"context panic"}
			if panicCase {
				ctx.panicValue = token
			}
			o.Context = ctx
			if panicCase {
				func() {
					defer func() {
						if got := recover(); got != token {
							t.Fatalf("panic identity: %v", got)
						}
					}()
					d.SnapshotWith(o)
				}()
			} else {
				s, err := d.SnapshotWith(o)
				if s != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("freeze cancellation: %v %v", s, err)
				}
			}
			if !ctx.seen || d.Memo().Len() != 0 || d.Runs() != nil || d.Kids() != nil || d.Machine() != nil {
				t.Fatal("freeze cleanup")
			}
			o.Context = nil
			if s, err := d.SnapshotWith(o); s == nil || err != nil {
				t.Fatal("retry", err)
			}
		}
	})
}

func TestSnapshotRecoveredAndDepthOutcomes(t *testing.T) {
	for _, fixture := range []struct {
		grammar, text string
		depth         int
		recovered     bool
	}{
		{`rule main = x* $$
rule x = "b" #recover(skip="a")`, "a", 0, true},
		{`rule main = x
rule x = "(" x ")" / "a"`, "((a))", 1, false},
	} {
		prog := mustCompile(t, fixture.grammar)
		snapshotVariants(t, func(t *testing.T, o ParseOptions) {
			d, err := prog.NewDocumentWith("main", fixture.text, o)
			if err != nil {
				t.Fatal(err)
			}
			o.Limits.Depth = uint64(fixture.depth)
			s, err := d.SnapshotWith(o)
			if s == nil || err == nil || s.Text() != fixture.text || s.Version() != d.Version() || s.Error() == nil {
				t.Fatal("snapshot error outcome", s, err)
			}
			if fixture.recovered {
				var received value.SyntaxErrors
				if !errors.As(err, &received) || !s.Root().Valid() || len(received) == 0 {
					t.Fatal("recovered outcome", err)
				}
				before := s.Error().Error()
				received[0].Pos = 999
				received[0].Expected = []string{"changed"}
				received[0].Messages = []string{"changed"}
				if s.Error().Error() != before {
					t.Fatal("returned recovered alias")
				}
				copy := s.Error().(value.SyntaxErrors)
				copy[0].Pos = 888
				copy[0].Messages = []string{"other"}
				if s.Error().Error() != before {
					t.Fatal("getter recovered alias")
				}
			} else if s.Root().Valid() {
				t.Fatal("depth error retained root")
			}
		})
	}
}

func TestSnapshotTracePanicCleanup(t *testing.T) {
	prog := mustCompile(t, `rule main = x $$
rule x = x "a" / "a"`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", "aa", o)
		if err != nil {
			t.Fatal(err)
		}
		marker := &struct{ label string }{"trace panic"}
		o.Trace = func(e TraceEvent) {
			if e.Kind == TraceExit && e.Rule == "x" && e.Memo && e.Matched {
				panic(marker)
			}
		}
		func() {
			defer func() {
				if got := recover(); got != marker {
					t.Fatalf("panic identity: %v", got)
				}
			}()
			d.SnapshotWith(o)
		}()
		if d.Memo().Len() != 0 || d.Runs() != nil || d.Kids() != nil || d.Machine() != nil {
			t.Fatal("trace panic retained reusable state")
		}
		o.Trace = nil
		s, err := d.SnapshotWith(o)
		if s == nil || err != nil || resultJSON(s.Materialize(), s.Error()) != resultJSON(prog.ParseWith("main", d.Text(), o)) {
			t.Fatal("trace panic retry", err)
		}
	})
}

// withExpectations returns o with expectation recording on.
func withExpectations(o ParseOptions) ParseOptions {
	o.Expectations = true
	return o
}

func TestSnapshotUnsupportedLegacyFieldPreservesParseError(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "x"`)
	d, _ := prog.NewDocument("main", "x")
	legacy, err := d.Parse()
	if err != nil {
		t.Fatal(err)
	}
	nodes.D(legacy).Fields = valueimpl.Fields{{Name: "bad", Value: []int{1}}}
	s, err := d.SnapshotWith(ParseOptions{})
	var capture *runtime.SnapshotCaptureError
	if s != nil || !errors.As(err, &capture) || capture.ValueType != reflect.TypeOf([]int{}) || !strings.Contains(capture.Path, "bad") || capture.ParseError != nil {
		t.Fatal("unsupported legacy field", s, err)
	}
	if d.Text() != "x" || d.Version().Sequence() != 1 {
		t.Fatal("capture failure mutated document")
	}
	nodes.D(legacy).Fields = nil
	if s, err := d.SnapshotWith(ParseOptions{}); s == nil || err != nil {
		t.Fatal("retry", err)
	}
}

// A boxed value of a type that a snapshot cannot hold is rejected like a field of that type, never
// dropped; a boxed int is kept.
func TestSnapshotUnsupportedBoxIsRejected(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "x"`)
	d, _ := prog.NewDocument("main", "x")
	legacy, err := d.Parse()
	if err != nil {
		t.Fatal(err)
	}
	nodes.D(legacy).Kind = valueimpl.TupleValueKind
	nodes.SetNodeElems(legacy, []*value.Node{nodes.NewNode(value.KindValue, 0, 0, "", nil, "", nil, 5)})
	s, err := d.SnapshotWith(ParseOptions{})
	if err != nil || s.Root().Child(0).Value() != 5 {
		t.Fatal("boxed int", s, err)
	}
	nodes.SetNodeElems(legacy, []*value.Node{nodes.NewNode(value.KindValue, 0, 0, "", nil, "", nil, []int{1})})
	s, err = d.SnapshotWith(ParseOptions{})
	var capture *runtime.SnapshotCaptureError
	if s != nil || !errors.As(err, &capture) || capture.ValueType != reflect.TypeOf([]int{}) || !strings.Contains(capture.Path, "Children[0]") {
		t.Fatal("unsupported boxed element", s, err)
	}
}
