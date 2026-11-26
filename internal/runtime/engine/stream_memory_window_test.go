package engine

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// streamPruneWindow is the number of units that a stream commits between two drops of its memo
// entries, slots and records (Streaming, Memory).
const streamPruneWindow = 1024

// streamCommitState is what a stream holds when it delivers an element: the commit point before the
// element, the commit point at which it last dropped its memo entries, slots and records, and the
// memory count.
type streamCommitState struct {
	commit, floor int
	mem           uint64
}

// streamCommitStates runs the stream parse of ParseStreamWith with the options o under a memory
// limit that it does not reach, and returns the state at each delivery.
func streamCommitStates(t *testing.T, prog *Program, r io.Reader, o ParseOptions) (states []streamCommitState, err error) {
	t.Helper()
	o.Limits.Memory = runtime.Unlimited - 1
	active, rule, control, err := prog.admitParse("main", o, optionsFamily(o), true, "")
	if err != nil {
		t.Fatal(err)
	}
	p := state.NewControlledStreamParser(&active.sharedProgram, r, o.Unit, control)
	control.AttachMemory(p)
	p.MaxDepth, _ = state.DepthLimit(o, o.Backend)
	p.StreamID = rule.ID
	p.DiagnosticTail.Limit = o.DiagnosticLimit
	p.Emit = func(*value.Node) error {
		states = append(states, streamCommitState{p.TextFloor, p.Memo.Base, control.MemoryUsed()})
		return nil
	}
	p.Descs = active.descs(o.Backend, optionsFamily(o))
	defer func() {
		if x := recover(); x != nil {
			recoverParse(x, active.backend(o.Backend), &err)
		}
	}()
	p.SeedVars()
	if _, ok := p.Call(rule, 0); !ok || !p.AtEOF() {
		return states, p.SyntaxError()
	}
	if len(p.Recovered) > 0 {
		return states, value.SyntaxErrors(p.RecoveredErrors())
	}
	return states, nil
}

// TestStreamMemoryWindow checks the window of the memory theorem of the streaming proof on the
// memory count of the engine's streams, on every backend and position unit. At each delivery, the
// stream last dropped its memo entries, slots and records at the first commit at least
// streamPruneWindow units past the drop before, so that fewer than that many units lie between that
// drop and the commit point. The count follows the window: the largest count at a delivery of a
// stream of a repeated line is the same for a stream four times as long, to the byte, apart from
// the recovered errors, which a stream without a diagnostic limit retains.
func TestStreamMemoryWindow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, line string
		// per is the number of elements of a line, and limit the diagnostic limit, 0 for none.
		per, limit int
		// grows says that the count grows with the length of the stream.
		grows bool
	}{
		{"memo", memoizedStream, "abc,abc\n", 1, 0, false},
		{"left recursion", leftRecursiveStream, "abc,abc\n", 1, 0, false},
		// The lookahead of an element calls num at the start of the next one, past the commit point.
		{"past end", `rule main = item* #stream !.
rule item = n:num ";" &(num ";")?
rule num = d:[0-9]+ {text($d) != ""}
`, "12;", 1, 0, false},
		{"recover limited", `rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, "a=1;b;", 2, 2, false},
		{"recover", `rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, "a=1;b;", 2, 0, true},
	}
	for _, c := range cases {
		prog := mustCompile(t, c.src)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
				t.Run(fmt.Sprintf("%s/%s/%s", c.name, b, unit), func(t *testing.T) {
					t.Parallel()
					var most [2]uint64
					for i, n := range []int{8192, 32768} {
						o := ParseOptions{Backend: b, Unit: unit, DiagnosticLimit: c.limit, DiagnosticLimitSet: c.limit > 0}
						states, err := streamCommitStates(t, prog, strings.NewReader(strings.Repeat(c.line, n)), o)
						if _, ok := err.(value.SyntaxErrors); err != nil && !ok || len(states) != n*c.per {
							t.Fatalf("%d lines: %d elements, error %v", n, len(states), err)
						}
						if problem := streamWindowProblem(states); problem != "" {
							t.Fatalf("%d lines: %s", n, problem)
						}
						for _, s := range states {
							most[i] = max(most[i], s.mem)
						}
					}
					if c.grows != (most[1] != most[0]) {
						t.Errorf("the largest count is %d bytes for 8,192 lines and %d for 32,768", most[0], most[1])
					}
				})
			}
		}
	}
}

// streamWindowProblem returns how the states at the deliveries of a stream break the drops of its
// window, or "".
func streamWindowProblem(states []streamCommitState) string {
	floor, commits := 0, []int{0}
	for i, s := range states {
		commits = append(commits, s.commit)
		if i > 0 {
			// The commit after the previous element is that at s.commit, where the stream drops iff the
			// drop before is at least a window back.
			if prev := states[i-1]; s.commit-prev.floor >= streamPruneWindow {
				floor = s.commit
			}
		}
		if s.floor != floor {
			return fmt.Sprintf("at the delivery after the commit at %d, the stream last dropped at %d, want %d", s.commit, s.floor, floor)
		}
		if !slices.Contains(commits, s.floor) || s.commit-s.floor >= streamPruneWindow {
			return fmt.Sprintf("at the delivery after the commit at %d, the stream last dropped at %d", s.commit, s.floor)
		}
	}
	return ""
}
