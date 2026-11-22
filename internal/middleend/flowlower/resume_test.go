package flowlower

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
)

// resumeOps are the operations of the runs that a Document parse resumes.
var resumeOps = []string{flow.OpResumeOn, flow.OpResumeStart, flow.OpResumeMore, flow.OpResumeNext,
	flow.OpResumeBegin, flow.OpResumeEnd, flow.OpResumeFinish}

// resumeOptions are the options of a lowering that resumes runs.
var resumeOptions = lowerdata.Options{Logical: true, Document: true, Node: true, Resume: true}

// resumable returns a repetition of the literal "ab" that a Document can resume, as the builder
// numbers it (core.Repeat.Resume).
func resumable(min, max int) *core.Repeat {
	return &core.Repeat{Min: min, Max: max, Body: &core.Lit{Text: "ab"}, Resume: 1, Shiftable: true}
}

// lowerResume lowers the body in a local function, or in a framed one that builds its value if
// framed, with opts, and returns the module and the listing of its function.
func lowerResume(t *testing.T, body core.Node, framed bool, opts lowerdata.Options) (*code.Module, string) {
	t.Helper()
	m, r := valueFree(body)
	var f *code.Module
	var err error
	if framed {
		f, err = LowerFrame(m, r, r.Body, nil, true, "r", opts)
	} else {
		f, err = Lower(m, r, "r", opts)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := flow.VerifyModule(f); err != nil {
		t.Fatal(err)
	}
	return f, code.FuncStringIn(f, flow.FuncOf(f))
}

// TestLowerResume checks the lowering of a repetition that a Document parse resumes, in local and
// framed functions, with and without a bound and as a scan site: with the options of a lowering for
// Documents that resumes runs, the function runs the run's operations around the element, once each
// where the engine calls them, and the iteration that keeps a reused element continues without a
// step; with any of those options off, or for a repetition that is not resumable, it has none of
// them.
func TestLowerResume(t *testing.T) {
	class := func() core.Node {
		return &core.ClassNode{Class: &core.Class{Ranges: []core.Range{{Lo: 'a', Hi: 'z'}}}}
	}
	for _, c := range []struct {
		name   string
		body   func() core.Node
		framed bool
	}{
		{"star", func() core.Node { return resumable(0, -1) }, false},
		{"bounded plus", func() core.Node { return resumable(1, 3) }, false},
		{"framed star", func() core.Node { return resumable(0, -1) }, true},
		{"framed own scope", func() core.Node {
			r := resumable(1, -1)
			r.Scope = []core.Slot{{Name: "x"}}
			r.Body = &core.Capture{Name: "x", Body: &core.Lit{Text: "ab"}}
			return r
		}, true},
		{"scan site", func() core.Node {
			return &core.Repeat{Min: 0, Max: -1, Body: class(), ScanSite: 1, Resume: 1}
		}, false},
		{"framed scan site", func() core.Node {
			return &core.Repeat{Min: 0, Max: -1, Body: class(), ScanSite: 1, Resume: 1}
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, got := lowerResume(t, c.body(), c.framed, resumeOptions)
			for _, op := range resumeOps {
				want := 1
				if op == flow.OpResumeEnd {
					want = 2 // the element matched, and the element failed
				}
				if n := strings.Count(got, op+"("); n != want {
					t.Errorf("%s called %d times, want %d, in\n%s", op, n, want, got)
				}
			}
			if strings.Contains(got, flow.OpScanReach+"(") && !strings.Contains(got, "!x") {
				t.Errorf("a resuming walk is a walk of the scan site in\n%s", got)
			}
			// The iteration that keeps a reused element takes no step: the call of flow.resumeNext checks the
			// limits in its place, and the iteration continues.
			if strings.Contains(got, "loop loop.1 init") {
				return
			}
			if kept, ok := keepsWithoutStep(flow.FuncOf(f).Body); !kept || !ok {
				t.Errorf("the iteration that keeps a reused element: found %v, continues without a step %v, in\n%s", kept, ok, got)
			}
		})
	}
	off := map[string]lowerdata.Options{
		"without Resume":   {Logical: true, Document: true, Node: true},
		"without Document": {Logical: true, Node: true, Resume: true},
		"typed":            {Logical: true, Document: true, Resume: true},
	}
	for name, opts := range off {
		t.Run(name, func(t *testing.T) {
			_, got := lowerResume(t, resumable(0, -1), false, opts)
			for _, op := range resumeOps {
				if strings.Contains(got, op+"(") {
					t.Errorf("%s in\n%s", op, got)
				}
			}
		})
	}
	for name, body := range map[string]core.Node{
		"not resumable": &core.Repeat{Min: 0, Max: -1, Body: &core.Lit{Text: "ab"}},
		"skip-run site": &core.Repeat{Min: 0, Max: -1, Body: &core.Lit{Text: "ab"}, Site: 1, Resume: 1},
	} {
		t.Run(name, func(t *testing.T) {
			_, got := lowerResume(t, body, false, resumeOptions)
			for _, op := range resumeOps {
				if strings.Contains(got, op+"(") {
					t.Errorf("%s in\n%s", op, got)
				}
			}
		})
	}
}

// TestLowerResumeRunSite checks that a walk that resumes its run is no walk of the run site that the
// repetition also is: it is never hot, and it tells the site nothing where it walked.
func TestLowerResumeRunSite(t *testing.T) {
	rep := resumable(0, -1)
	rep.Run = 1
	_, got := lowerResume(t, rep, true, resumeOptions)
	if !strings.Contains(got, "!x") || !strings.Contains(got, flow.OpRunHot+"([") {
		t.Fatalf("no run site in\n%s", got)
	}
	var hot, walked bool
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, flow.OpRunHot+"([") && strings.Contains(line, "!x") && strings.Contains(line, "&&") {
			hot = true
		}
		if strings.Contains(line, "if !x") {
			walked = true
		}
	}
	if !hot || !walked {
		t.Errorf("hot tested only for walks that do not resume: %v; walks noted only for them: %v, in\n%s", hot, walked, got)
	}
}

// TestVerifyResume checks that the Flow verifier keeps the operations of runs in Node functions lowered
// for Documents, and that a run starts at a positive site.
func TestVerifyResume(t *testing.T) {
	f, _ := lowerResume(t, resumable(0, -1), false, resumeOptions)
	a := flow.AttrsOf(flow.FuncOf(f))
	a.Node = false
	if err := flow.VerifyModule(f); err == nil || !strings.Contains(err.Error(), "outside a Node function lowered for Documents") {
		t.Errorf("a typed function: got %v", err)
	}
	a.Node, a.Document = true, false
	if err := flow.VerifyModule(f); err == nil || !strings.Contains(err.Error(), "outside a Node function lowered for Documents") {
		t.Errorf("a function not lowered for Documents: got %v", err)
	}
	a.Document = true
	code.Walk(flow.FuncOf(f).Body, func(code.Stmt) {}, func(x code.Expr) {
		if c, ok := x.(*code.Call); ok && c.Callee == flow.OpResumeStart {
			r := c.Imm.(prim.Resume)
			r.Site = 0
			c.Imm = r
		}
	})
	if err := flow.VerifyModule(f); err == nil || !strings.Contains(err.Error(), "site 0") {
		t.Errorf("a run at site 0: got %v", err)
	}
}

// keepsWithoutStep finds, in the block b and the blocks in it, the iteration that keeps an element
// that flow.resumeNext reused: the branch on the second result of the call, right after it. It
// reports whether it found one and whether each such branch continues the loop without a step.
func keepsWithoutStep(b *code.Block) (found, ok bool) {
	if b == nil {
		return false, true
	}
	ok = true
	for i, s := range b.Stmts {
		var inner []*code.Block
		switch s := s.(type) {
		case *code.Set:
			if len(s.Values) != 1 || len(s.Dst) != 2 || i+1 == len(b.Stmts) {
				continue
			}
			if c, isCall := s.Values[0].(*code.Call); !isCall || c.Callee != flow.OpResumeNext {
				continue
			}
			br, isIf := b.Stmts[i+1].(*code.If)
			if !isIf {
				continue
			}
			if l, isLocal := br.Cond.(*code.Local); !isLocal || l.Name != s.Dst[1].Name {
				continue
			}
			found = true
			stmts := br.Then.Stmts
			_, cont := stmts[len(stmts)-1].(*code.Continue)
			stepped := false
			for _, t := range stmts {
				if d, isDo := t.(*code.Do); isDo && d.Call.Callee == flow.OpStep {
					stepped = true
				}
			}
			ok = ok && cont && !stepped
		case *code.Block:
			inner = []*code.Block{s}
		case *code.If:
			inner = []*code.Block{s.Then, s.Else}
		case *code.Loop:
			inner = []*code.Block{s.Body}
		}
		for _, in := range inner {
			f, k := keepsWithoutStep(in)
			found, ok = found || f, ok && k
		}
	}
	return found, ok
}
