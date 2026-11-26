package engine

import (
	"fmt"
	"io"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// streamPruneWindow is the number of units that a stream commits between two drops of its memo
// entries, slots and records (Streaming, Memory).
const streamPruneWindow = 1024

// streamCountSlack is how far the largest memory count of a stream may rise when the stream is four
// times as long: the counts at the deliveries are sampled where the chunks of nodes are split, which
// falls at more places in an element on a longer stream. Anything a stream kept for each element
// would add a byte or more for each of the three quarters of the elements of the longer stream.
const streamCountSlack = 256

// streamCommitState is what a stream holds after a commit: the commit point, the floor of its memo
// (the commit point at which it last dropped its memo entries, slots and records, or 0), and the
// memory count.
type streamCommitState struct {
	commit, floor int
	mem           uint64
}

// streamCommitStates runs the stream parse of ParseStreamWith with the options o under a memory
// limit that it does not reach, and returns the state at each delivery, after the commit before
// the element, and the state after the parse, after the commit of the last element.
func streamCommitStates(t *testing.T, prog *Program, r io.Reader, o ParseOptions) (states []streamCommitState, err error) {
	t.Helper()
	o.Limits.Memory = runtime.Unlimited - 1
	active, rule, control, err := prog.admitParse("main", o, optionsFamily(o), true, "")
	if err != nil {
		t.Fatal(err)
	}
	p := state.NewControlledStreamParser(&active.sharedProgram, r, o.Unit, control)
	p.SetTuning(o.Tune)
	control.AttachMemory(p)
	p.MaxDepth, _ = state.DepthLimit(o, o.Backend)
	p.StreamID = rule.ID
	p.DiagnosticTail.Limit = o.DiagnosticLimit
	observe := func() {
		states = append(states, streamCommitState{p.TextFloor, p.Memo.Base, control.MemoryUsed()})
	}
	p.Emit = func(*value.Node) error {
		observe()
		return nil
	}
	p.Descs = active.descs(o.Backend, optionsFamily(o))
	defer func() {
		if x := recover(); x != nil {
			recoverParse(x, active.backend(o.Backend), &err)
		}
	}()
	p.SeedVars()
	_, ok := p.Call(rule, 0)
	observe()
	if !ok || !p.AtEOF() {
		return states, p.SyntaxError()
	}
	if len(p.Recovered) > 0 {
		return states, value.SyntaxErrors(p.RecoveredErrors())
	}
	return states, nil
}

// TestStreamMemoryWindow checks the window of the memory theorem of the streaming proof on the
// memory count of the engine's streams, on every backend and position unit. After each commit, the
// floor of the memo is a commit point fewer than streamPruneWindow units before the commit point, and
// it moved there from at least that many units back (streamWindowProblem). The count follows the
// window: the largest count at the commits of a stream of a repeated line rises by at most
// streamCountSlack for a stream four times as long, apart from the recovered errors, which a stream
// without a diagnostic limit retains. Each case whose sites keep records (skip-run, run or scan
// sites) counts otherwise without them (Tuning.SkipOff), so the records it checks are held: more with
// a record than without, or less where a scan record stands for memo entries of its rule.
func TestStreamMemoryWindow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, line string
		// lines is the number of lines of the shorter stream, per the number of elements of a line,
		// and limit the diagnostic limit, 0 for none.
		lines, per, limit int
		// grows says that the count grows with the length of the stream, and records that sites of
		// the grammar keep records (the skip expressions of the recoveries are skip-run sites).
		grows, records bool
	}{
		{"memo", memoizedStream, "abc,abc\n", 8192, 1, 0, false, false},
		{"left recursion", leftRecursiveStream, "abc,abc\n", 8192, 1, 0, false, false},
		// The lookahead of an element calls num at the start of the next one, past the commit point.
		{"past end", `rule main = item* #stream !.
rule item = n:num ";" &(num ";")?
rule num = d:[0-9]+ {text($d) != ""}
`, "12;", 8192, 1, 0, false, false},
		{"recover limited", `rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, "a=1;b;", 8192, 2, 2, false, true},
		{"recover", `rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, "a=1;b;", 8192, 2, 0, true, true},
		// The skip expression of the recovery is a skip-run site whose records keep expectations.
		{"skip records limited", `rule main = item* #stream !.
rule item = x / .
rule x = ("b" #recover(skip=(word / [a-z ])*)) "c"
rule word = [a-z]+ !"="
`, "bab abc cxbaa a!c", 512, 16, 2, false, true},
		// list is a run site, which the second alternative walks again from the second word on.
		{"run sites", `rule main = item* #stream !.
rule item = list "x" ";" / word "," list "y" ";"
rule list = (word ",")*
rule word = [a-z]+
`, "ab,cd,ef,gh,ij,kl,mn,op,qr,st,uv,wx,ab,cd,ef,gh,ij,kl,mn,op,qr,st,uv,wx,y;", 512, 1, 0, false, true},
		// ws is a scan site: the second alternative walks the spaces again.
		{"scan sites", `rule main = item* #stream !.
rule item = ws "a" ws ";" / ws "b" ws ";"
rule ws = [ ]*
`, "                                        b;", 2048, 1, 0, false, true},
	}
	for _, c := range cases {
		prog := mustCompile(t, c.src)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
				t.Run(fmt.Sprintf("%s/%s/%s", c.name, b, unit), func(t *testing.T) {
					t.Parallel()
					most := func(n int, tune state.Tuning) uint64 {
						o := ParseOptions{Backend: b, Unit: unit, DiagnosticLimit: c.limit, DiagnosticLimitSet: c.limit > 0, Tune: tune}
						states, err := streamCommitStates(t, prog, &lineReader{line: c.line, n: n}, o)
						if _, ok := err.(value.SyntaxErrors); err != nil && !ok || len(states) != n*c.per+1 {
							t.Fatalf("%d lines: %d commits, error %v", n, len(states), err)
						}
						if problem := streamWindowProblem(states); problem != "" {
							t.Fatalf("%d lines: %s", n, problem)
						}
						var m uint64
						for _, s := range states {
							m = max(m, s.mem)
						}
						return m
					}
					short, long := most(c.lines, state.Tuning{}), most(4*c.lines, state.Tuning{})
					if c.grows != (long > short+streamCountSlack) || long < short {
						t.Errorf("the largest count is %d bytes for %d lines and %d for %d", short, c.lines, long, 4*c.lines)
					}
					if off := most(c.lines, state.Tuning{SkipOff: true}); c.records != (off != short) {
						t.Errorf("the largest count is %d bytes with records and %d without", short, off)
					}
				})
			}
		}
	}
}

// streamWindowProblem returns how the states after the commits of a stream break the window of its
// memo, or "": the floor is a commit point at most the commit point and fewer than streamPruneWindow
// units before it, and it moves only to a commit point at least that many units past it.
func streamWindowProblem(states []streamCommitState) string {
	floor, commits := 0, []int{0}
	for _, s := range states {
		commits = append(commits, s.commit)
		switch {
		case !slices.Contains(commits, s.floor) || s.floor > s.commit:
			return fmt.Sprintf("after the commit at %d, the floor %d is not a commit point so far", s.commit, s.floor)
		case s.commit-s.floor >= streamPruneWindow:
			return fmt.Sprintf("after the commit at %d, the floor %d is a window or more back", s.commit, s.floor)
		case s.floor < floor || s.floor > floor && s.floor-floor < streamPruneWindow:
			return fmt.Sprintf("after the commit at %d, the floor moved from %d to %d", s.commit, floor, s.floor)
		}
		floor = s.floor
	}
	return ""
}
