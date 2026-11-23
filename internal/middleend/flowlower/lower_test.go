package flowlower

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
)

// valueFree makes a module of one value-free rule with the body, as the builder does: the
// expectations of its literals and classes are interned after the five fixed ones.
func valueFree(body core.Node) (*core.Module, *core.Rule) {
	m := &core.Module{Exps: []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}}
	core.Walk(body, func(n core.Node) {
		switch n := n.(type) {
		case *core.Lit:
			n.Exp = core.ExpID(len(m.Exps))
			m.Exps = append(m.Exps, `"`+n.Text+`"`)
		case *core.ClassNode:
			n.Class.Exp = core.ExpID(len(m.Exps))
			m.Exps = append(m.Exps, "class")
			m.Classes = append(m.Classes, n.Class)
		}
	})
	r := &core.Rule{ID: 0, Name: "r", Body: body, Policy: core.Policy{Seen: -1}}
	m.Rules = []*core.Rule{r}
	core.CallGraph(m)
	core.Analyze(m)
	return m, r
}

// missingLit returns the #missing site of the literal text, numbered site.
func missingLit(site int, text string) *core.Missing {
	l := &core.Lit{Text: text}
	return &core.Missing{Lit: l, Body: l, Site: site}
}

func alts(bodies ...core.Node) *core.Choice {
	c := &core.Choice{}
	for _, b := range bodies {
		c.Alts = append(c.Alts, &core.Alt{Body: b})
	}
	return c
}

// TestLower checks the functions lowered from small rules: their statements and the variables
// they declare, and that they verify.
func TestLower(t *testing.T) {
	// inline is the number of code points of the inline-literals pass that runs after a case's lowering.
	inline := map[string]int{"choice of literals": 1}
	for _, c := range []struct {
		name string
		body core.Node
		opts lowerdata.Options
		want string
	}{
		{"choice of literals", alts(&core.Lit{Text: "ab"}, &core.Lit{Text: "c"}), lowerdata.Options{}, `
func r() {
  var x1 int
  var ok bool
  var start int
  var v value
  var c ctx
  x1 = $pos
  block L2 {
    block L3 {
      if _, ok = flow.matchLit(["ab"]); !ok {
        exit L3
      }
      exit L2
    }
    $pos = x1
    block L4 {
      if flow.hasLit(["c"]) {
        flow.advance(1)
      } else {
        if _, ok = flow.matchLit(["c"]); !ok {
          exit L4
        }
      }
      exit L2
    }
    $pos = x1
    exit fail
  }
  v = null(value)
}
`},
		{"repetition with work and recovery", &core.Repeat{Min: 1, Max: -1, Body: &core.Lit{Text: "a"}}, lowerdata.Options{Logical: true, Recovers: true}, `
func r() {
  var x1 int
  var x2 int
  var x3 int
  var ok bool
  var start int
  var v value
  var c ctx
  x1 = 0
  loop loop.1 {
    flow.step()
    x2, x3 = $pos, flow.recLen()
    block L4 {
      if _, ok = flow.matchLit(["a"]); !ok {
        exit L4
      }
      x1 = (x1 + 1)
      if ($pos == x2) {
        break loop.1
      }
      continue loop.1
    }
    flow.restore(x2, x3)
    break loop.1
  }
  if (x1 < 1) {
    exit fail
  }
  v = null(value)
}
`},
		{"insertions under a label in a repetition", &core.Repeat{Min: 0, Max: -1, Body: &core.Label{Exp: 1, MissingScope: true,
			Body: missingLit(1, "a")}}, lowerdata.Options{Recovers: true, Inserts: true}, `
func r() {
  var x1 int
  var x2 int
  var x3 int
  var x4 int
  var x6 int
  var x7 expmark
  var x8 int
  var x9 expids
  var x12 int
  var x13 expmark
  var x14 int
  var x15 expids
  var ok bool
  var x18 error
  var x19 bool
  var start int
  var v value
  var c ctx
  x1 = 0
  loop loop.1 {
    x2, x3 = $pos, flow.recLen()
    x4 = flow.missingLen()
    block L5 {
      x6 = flow.missingScope([exp 1])
      x7 = flow.isolate()
      block L11 {
        block L10 {
          x12 = $pos
          x13 = flow.isolate()
          block L17 {
            block L16 {
              if _, ok = flow.matchLit(["a"]); !ok {
                exit L16
              }
              x14, x15 = flow.unisolate(x13)
              flow.mergeExpected(x14, x15)
              exit L17
            }
            x14, x15 = flow.unisolate(x13)
            $pos = x12
            x18, x19 = flow.insertMissing([site 1 "a"], x15)
            if !x19 {
              flow.mergeExpected(x14, x15)
              exit L10
            }
          }
          x8, x9 = flow.unisolate(x7)
          flow.mergeExpected(x8, x9)
          flow.missingScopeEnd(x6)
          exit L11
        }
        x8, x9 = flow.unisolate(x7)
        flow.missingScopeEnd(x6)
        flow.expect([exp 1 message], x8)
        exit L5
      }
      x1 = (x1 + 1)
      if ($pos == x2) {
        break loop.1
      }
      continue loop.1
    }
    flow.restore(x2, x3)
    flow.resetMissing(x4)
    break loop.1
  }
  v = null(value)
}
`},
		{"alternatives after one that cannot fail", alts(&core.Lit{Text: "a"}, &core.Succeed{}, &core.Lit{Text: "b"}), lowerdata.Options{}, `
func r() {
  var x1 int
  var ok bool
  var start int
  var v value
  var c ctx
  x1 = $pos
  block L2 {
    block L3 {
      if _, ok = flow.matchLit(["a"]); !ok {
        exit L3
      }
      exit L2
    }
    $pos = x1
    block L4 {
      exit L2
    }
    $pos = x1
    exit fail
  }
  v = null(value)
}
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, r := valueFree(c.body)
			f, err := lowerFinished(t, m, r, c.opts, inline[c.name])
			if err != nil {
				t.Fatal(err)
			}
			if err := flow.VerifyModule(f); err != nil {
				t.Fatal(err)
			}
			if got := dump(f); got != strings.TrimPrefix(c.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", got, strings.TrimPrefix(c.want, "\n"))
			}
		})
	}
}

// TestLowerFrame checks the framed functions lowered from small bodies.
func TestLowerFrame(t *testing.T) {
	for _, c := range []struct {
		name  string
		body  core.Node
		slots []core.Slot
		want  string
	}{
		{"recovery of a repetition with captures", &core.Recover{
			Body: &core.Repeat{Min: 1, Max: -1, Scope: []core.Slot{{Name: "x"}},
				Body: &core.Capture{Slot: 0, Name: "x", Body: &core.Lit{Text: "a"}}},
			Skip: &core.Any{}}, nil, `
func r() (value, bool) {
  var x1 mark
  var x2 expmark
  var x3 int
  var x4 expids
  var x7 frame
  var x8 frame
  var x9 int
  var x10 mark
  var x12 int
  var x13 int
  var ok bool
  var v14 value
  var v15 value
  var size int
  var x17 error
  block fail {
    // Recovery inlined under the runtime-owned frame.
    x1 = flow.markState([state pos trail rec])
    x2 = flow.isolate()
    block L6 {
      block L5 {
        x9 = 0
        loop loop.1 {
          x10 = flow.markState([state pos trail])
          x8 = flow.frame()
          x7 = flow.newFrame([[x] declared])
          flow.setFrame(x7)
          block L11 {
            x12 = $pos
            x13 = $pos
            if _, ok = flow.matchLit(["a"]); !ok {
              exit L11
            }
            v14 = flow.newMatch(x13, flow.str(["a"]))
            v15 = flow.shapeCapture([{Elems:false K:0 Mode:0 Tuple:false Shared:false}], v14, x12)
            flow.setCapture([slot 0], v15)
            flow.setFrame(x8)
            flow.declareNames([[x] declared])
            flow.dropTrail(x10)
            flow.freeFrame(x7)
            x9 = (x9 + 1)
            if ($pos == flow.markPos(x10)) {
              break loop.1
            }
            continue loop.1
          }
          flow.setFrame(x8)
          flow.reset(x10)
          break loop.1
        }
        if (x9 < 1) {
          exit L5
        }
        x3, x4 = flow.unisolate(x2)
        flow.mergeExpected(x3, x4)
        exit L6
      }
      x3, x4 = flow.unisolate(x2)
      x4 = flow.keep(x4)
      flow.reset(x1)
      block L16 {
        _, size, ok = flow.peek()
        if !ok {
          flow.expect([exp 0], $pos)
          exit L16
        }
        flow.advance(size)
        if ($pos == flow.markPos(x1)) {
          exit L16
        }
        x17 = flow.makeError(x3, x4)
        flow.appendError(x17)
        exit L6
      }
      flow.reset(x1)
      flow.mergeExpected(x3, x4)
      exit fail
    }
    return null(value), true
  }
  return null(value), false
}
`},
		{"cut at the top", &core.Seq{Items: []core.Node{&core.Capture{Slot: 0, Name: "y", Body: &core.Lit{Text: "a"}}, &core.Cut{}}},
			[]core.Slot{{Name: "y"}}, `
func r() (value, bool) {
  var cut1 bool
  var x2 int
  var x3 int
  var ok bool
  var v4 value
  var v5 value
  block fail {
    cut1 = $cut
    x2 = $pos
    x3 = $pos
    if _, ok = flow.matchLit(["a"]); !ok {
      exit fail
    }
    v4 = flow.newMatch(x3, flow.str(["a"]))
    v5 = flow.shapeCapture([{Elems:false K:0 Mode:0 Tuple:false Shared:false}], v4, x2)
    flow.setCapture([slot 0], v5)
    cut1 = true
    $cut = cut1
    return null(value), true
  }
  $cut = cut1
  return null(value), false
}
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, r := valueFree(c.body)
			f, err := LowerFrame(m, r, r.Body, c.slots, false, "r", lowerdata.Options{})
			if err != nil {
				t.Fatal(err)
			}
			finish(t, f, flowpass.Target{}, 0)
			if err := flow.VerifyModule(f); err != nil {
				t.Fatal(err)
			}
			if got := dump(f); got != strings.TrimPrefix(c.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", got, strings.TrimPrefix(c.want, "\n"))
			}
		})
	}
}

// TestLowerNodeValues checks that a Node lowering builds the Node runtime's values, in a function that
// Verify accepts, and a typed lowering builds the typed ones.
func TestLowerNodeValues(t *testing.T) {
	for _, node := range []bool{false, true} {
		m, r := valueFree(&core.Repeat{Min: 1, Max: -1, Body: &core.Lit{Text: "a"}})
		f, err := LowerFrame(m, r, r.Body, nil, true, "r", lowerdata.Options{Node: node})
		if err != nil {
			t.Fatal(err)
		}
		if err := flow.VerifyModule(f); err != nil {
			t.Fatalf("node %v: %v", node, err)
		}
		got := code.FuncStringIn(f, flow.FuncOf(f))
		hasNode := strings.Contains(got, flow.OpNewBranchNode+"(") || strings.Contains(got, flow.OpNewTerminalNode+"(")
		hasTyped := strings.Contains(got, flow.OpNewNode+"(") || strings.Contains(got, flow.OpNewMatch+"(")
		if a := flow.AttrsOf(flow.FuncOf(f)); a.Node != node || hasNode != node || hasTyped == node {
			t.Errorf("node %v: Attrs.Node %v, Node operations %v, typed operations %v in\n%s", node, a.Node, hasNode, hasTyped, got)
		}
	}
}

// TestLowerVerifies checks that lowering returns an error, rather than a function, when the Flow
// verifier rejects the function it built: a Node function that holds a typed value operation is not
// printed.
func TestLowerVerifies(t *testing.T) {
	newLowerer := func() *lowerer {
		m, _ := valueFree(&core.Succeed{})
		return newLowerer(m, core.ActionSitesOf(m), newTables(m), lowerdata.Options{}, "f", &flow.Attrs{Start: "start", Result: "v", Ctx: "c"})
	}
	l := newLowerer()
	l.at.Node = true
	l.set(l.decl("v", prim.TypeValue), call(flow.OpNewMatch, nil, num(0), call(flow.OpStr, prim.Chars(""))))
	if g, err := verified(l.module(nil)); err == nil || g != nil || !strings.Contains(err.Error(), "typed match in a Node function") {
		t.Errorf("module: got %v, %v", g, err)
	}
	if g, err := verified(newLowerer().module(nil)); err != nil || g == nil {
		t.Errorf("module of a valid function: got %v, %v", g, err)
	}
}

// TestLowerSuspend checks that lowerdata.Options.Suspend adds one flow.inputDemand before each literal match and
// nothing else: without the option the lowering has no demand, and with it the lowering minus its demand lines is the
// lowering without the option, so the results and the charges of the parse do not move.
func TestLowerSuspend(t *testing.T) {
	m := &core.Module{Rules: []*core.Rule{nil}, Exps: []string{`"a"`, `"b"`}}
	body := &core.Seq{Items: []core.Node{&core.Lit{Text: "a"}, &core.Lit{Text: "b"}}}
	r := &core.Rule{Name: "main", Body: body}
	m.Rules[0] = r
	lower := func(suspend bool) string {
		f, err := verified(lowerFunc(m, core.ActionSitesOf(m), newTables(m), lowerdata.Func{Form: lowerdata.Frame, Rule: r, Name: "main", Body: body, Build: true}, lowerdata.Options{Node: true, Suspend: suspend}))
		if err != nil {
			t.Fatal(err)
		}
		if err := flow.VerifyModule(f); err != nil {
			t.Fatal(err)
		}
		return code.FuncStringIn(f, flow.FuncOf(f))
	}
	plain, suspended := lower(false), lower(true)
	if strings.Contains(plain, flow.OpInputDemand+"(") {
		t.Errorf("the lowering without Suspend has %q:\n%s", flow.OpInputDemand, plain)
	}
	if got := strings.Count(suspended, flow.OpInputDemand+"("); got != 2 {
		t.Errorf("the suspended lowering has %d demands, want one per literal:\n%s", got, suspended)
	}
	var kept []string
	for _, line := range strings.Split(suspended, "\n") {
		if !strings.Contains(line, flow.OpInputDemand+"(") {
			kept = append(kept, line)
		}
	}
	if strings.Join(kept, "\n") != plain {
		t.Errorf("the suspended lowering differs from the plain one beyond its demands:\n%s\n--- plain:\n%s", suspended, plain)
	}
}

// TestLowerStream checks the lowering of a #stream repetition in a framed Node function: with lowerdata.Func.Stream
// it starts the stream and delivers each element instead of gathering it, guarded by StreamOn; without the
// option it is the batch repetition, which has none of these operations.
func TestLowerStream(t *testing.T) {
	m := &core.Module{Rules: []*core.Rule{nil}, Exps: []string{`"a"`}}
	rep := &core.Repeat{Min: 0, Max: -1, Body: &core.Lit{Text: "a"}}
	body := &core.Stream{Repeat: rep}
	r := &core.Rule{Name: "main", Body: body}
	m.Rules[0] = r
	lower := func(stream bool) string {
		f, err := verified(lowerFunc(m, core.ActionSitesOf(m), newTables(m), lowerdata.Func{Form: lowerdata.Frame, Rule: r, Name: "main", Body: body, Build: true, Stream: stream}, lowerdata.Options{Node: true}))
		if err != nil {
			t.Fatal(err)
		}
		if err := flow.VerifyModule(f); err != nil {
			t.Fatal(err)
		}
		return code.FuncStringIn(f, flow.FuncOf(f))
	}
	batch, stream := lower(false), lower(true)
	for _, op := range []string{flow.OpStreamStart + "(", flow.OpEmitStream + "(", flow.OpStreamOn + "("} {
		if strings.Contains(batch, op) {
			t.Errorf("the batch lowering has %q:\n%s", op, batch)
		}
		if !strings.Contains(stream, op) {
			t.Errorf("the stream lowering lacks %q:\n%s", op, stream)
		}
	}
	if strings.Count(stream, flow.OpPushKid+"(") != strings.Count(batch, flow.OpPushKid+"(") {
		t.Errorf("the stream lowering keeps the gathering of elements for the parses that do not stream:\n%s", stream)
	}
}

// A Document parse records how far it examined the input in the primitives that read it, so a Document function
// reads a literal in place only where the primitive follows as the alternative, and Verify rejects the other
// forms: an in-place read that is part of a larger condition or has no primitive to fall back to.
func TestVerifyDocumentReads(t *testing.T) {
	lit := &core.Lit{Text: "ab"}
	m, r := valueFree(lit)
	build := func(document bool) *code.Module {
		f, err := lowerFinished(t, m, r, lowerdata.Options{Document: document}, 4)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	f := build(true)
	if err := flow.VerifyModule(f); err != nil {
		t.Fatalf("a Document function with the primitive as the alternative of its in-place read: %v", err)
	}
	// find returns the If whose condition is the in-place read.
	find := func(m *code.Module) *code.If {
		var read *code.If
		code.Walk(flow.FuncOf(m).Body, func(s code.Stmt) {
			if s, ok := s.(*code.If); ok && read == nil {
				if c, ok := s.Cond.(*code.Call); ok && c.Callee == flow.OpHasLit {
					read = s
				}
			}
		}, nil)
		return read
	}
	read := find(f)
	if read == nil {
		t.Fatalf("no in-place read in:\n%s", code.FuncStringIn(f, flow.FuncOf(f)))
	}
	saved := *read
	check := func(name, want string) {
		t.Run(name, func(t *testing.T) {
			err := flow.VerifyModule(f)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("got %v, want an error containing %q", err, want)
			}
		})
		*read = saved
	}
	read.Else = nil
	check("no primitive", "without the recording primitive")
	read.Cond = code.Bin("||", saved.Cond, code.Not(saved.Cond))
	check("larger condition", "part of a larger condition")
	if err := flow.VerifyModule(f); err != nil {
		t.Fatalf("restored: %v", err)
	}
	// Outside a Document function the same read stands alone.
	g := build(false)
	if read := find(g); read == nil {
		t.Fatalf("no in-place read in:\n%s", code.FuncStringIn(g, flow.FuncOf(g)))
	} else {
		read.Else = nil
		if err := flow.VerifyModule(g); err != nil && strings.Contains(err.Error(), "recording primitive") {
			t.Errorf("a function that is not a Document function: %v", err)
		}
	}
}

// dump returns the listing of the function of the module m.
func dump(m *code.Module) string { return code.FuncStringIn(m, flow.FuncOf(m)) }

// TestLowerInsertionFrame checks a framed Node function with an insertion inside a recovery: its
// marks save the path of insertions with the recovered errors, and the insertion builds the missing
// node of the Node runtime.
func TestLowerInsertionFrame(t *testing.T) {
	m, r := valueFree(&core.Recover{Body: missingLit(1, "a"), Skip: &core.Any{}})
	f, err := LowerFrame(m, r, r.Body, nil, true, "r", lowerdata.Options{Node: true, Recovers: true, Inserts: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := flow.VerifyModule(f); err != nil {
		t.Fatal(err)
	}
	got := code.FuncStringIn(f, flow.FuncOf(f))
	for _, want := range []string{"flow.markState([state pos rec missing])", "flow.insertMissing([site 1 \"a\"]", "flow.newMissingNode([site 1 \"a\"]"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}

// TestLowerWithoutInsertion checks that a lowering for parses that never insert lowers a #missing site
// under a label as its literal, with none of the operations of insertion.
func TestLowerWithoutInsertion(t *testing.T) {
	m, r := valueFree(&core.Label{Exp: 1, MissingScope: true, Body: missingLit(1, "a")})
	f, err := LowerFrame(m, r, r.Body, nil, true, "r", lowerdata.Options{Node: true, Recovers: true})
	if err != nil {
		t.Fatal(err)
	}
	got := code.FuncStringIn(f, flow.FuncOf(f))
	for _, op := range []string{flow.OpInserting, flow.OpInsertMissing, flow.OpNewMissingNode, flow.OpMissingScope, flow.OpMissingLen, flow.OpResetMissing, "missing]"} {
		if strings.Contains(got, op) {
			t.Errorf("%s in\n%s", op, got)
		}
	}
	if !strings.Contains(got, flow.OpMatchLit) {
		t.Errorf("no %s in\n%s", flow.OpMatchLit, got)
	}
}
