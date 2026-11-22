package corepass_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// structuralFacts returns the structural module of src and its facts, computed with control.
func structuralFacts(t *testing.T, src string, control *cancel.Control) (*core.Module, *corepass.Facts) {
	t.Helper()
	g, err := compiletest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	m, err := elaborate.Structural(compile.StructuralInput(g, g.Rules(), nil), false)
	if err != nil {
		t.Fatal(err)
	}
	return m, corepass.Compute(m, control)
}

// TestSuccessAndCutProofs checks Facts.Succeeds and the loose-cut queries on nodes of each kind
// they treat. Each query answers alike with and without a control, polls the control it has, and
// leaves its node unchanged.
func TestSuccessAndCutProofs(t *testing.T) {
	t.Parallel()
	m, f := structuralFacts(t, "rule known = \"\"\nrule unknown = \"a\"\n", nil)
	id := map[string]int{}
	for _, r := range m.Rules {
		if r != nil {
			id[r.Name] = r.ID
		}
	}
	committed := func() core.Node { return &core.Seq{Items: []core.Node{&core.Cut{}, &core.Lit{Text: "a"}}} }
	for _, test := range []struct {
		name         string
		node         func() core.Node
		success, cut bool
	}{
		{"empty", func() core.Node { return &core.Lit{} }, true, false},
		{"known call", func() core.Node { return &core.Call{Rule: id["known"]} }, true, false},
		{"unknown call", func() core.Node { return &core.Call{Rule: id["unknown"]} }, false, false},
		{"committed failure", committed, false, true},
		{"committed choice", func() core.Node {
			return &core.Choice{Alts: []*core.Alt{{Body: committed()}, {Body: &core.Succeed{}}}}
		}, false, false},
		{"committed optional", func() core.Node { return &core.Opt{Body: committed()} }, false, false},
		{"empty repeat", func() core.Node { return &core.Repeat{Body: committed(), Max: 0} }, true, false},
		{"larger minimum", func() core.Node { return &core.Repeat{Body: &core.Succeed{}, Min: 2, Max: 3} }, false, false},
		{"negative failure", func() core.Node { return &core.Look{Neg: true, Body: &core.FailNode{}} }, true, false},
		{"nested negative", func() core.Node {
			return &core.Look{Neg: true, Body: &core.Look{Neg: true, Body: &core.Succeed{}}}
		}, true, false},
		{"positive", func() core.Node { return &core.Look{Body: &core.Succeed{}} }, true, false},
		{"sequence", func() core.Node { return &core.Seq{Items: []core.Node{&core.Cut{}}} }, true, true},
		{"capture", func() core.Node { return &core.Capture{Body: &core.Cut{}} }, true, true},
		{"terminal", func() core.Node { return &core.Terminal{Body: &core.Cut{}} }, true, true},
		{"drop", func() core.Node { return &core.Drop{Body: &core.Cut{}} }, true, true},
		{"text", func() core.Node { return &core.Text{Body: &core.Cut{}} }, true, true},
		{"label", func() core.Node { return &core.Label{Body: &core.Cut{}} }, true, true},
		{"failing recovery", func() core.Node { return &core.Recover{Body: &core.FailNode{}, Skip: &core.Cut{}} }, false, true},
		{"recovery", func() core.Node { return &core.Recover{Body: &core.Succeed{}, Skip: &core.Cut{}} }, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			n := test.node()
			control := &cancel.Control{Ctx: context.Background()}
			for _, f := range []*corepass.Facts{f, f.WithControl(control)} {
				if got := f.Succeeds(n); got != test.success {
					t.Fatalf("success = %v, want %v", got, test.success)
				}
				if got := f.LooseCut(n); got != test.cut || got != corepass.LooseCut(n) {
					t.Fatalf("loose cut = %v, want %v", got, test.cut)
				}
			}
			if control.Steps == 0 || !reflect.DeepEqual(n, test.node()) {
				t.Fatal("the proofs did not poll or changed their node")
			}
		})
	}
}

// TestProofCancellation checks that the proofs stop on a cancelled control inside a traversal of
// a large node, where no loop around them polls, and leave the node unchanged.
func TestProofCancellation(t *testing.T) {
	t.Parallel()
	const size = 1024
	_, f := structuralFacts(t, "rule main = \"\"\n", nil)
	ctx, stop := context.WithCancel(context.Background())
	stop()
	polled := f.WithControl(&cancel.Control{Ctx: ctx})
	succeeds := func() []core.Node {
		items := make([]core.Node, size)
		for i := range items {
			items[i] = &core.Succeed{}
		}
		return items
	}
	fails := func() []*core.Alt {
		alts := make([]*core.Alt, size)
		for i := range alts {
			alts[i] = &core.Alt{Body: &core.FailNode{}}
		}
		return alts
	}
	labelled := func(n core.Node) core.Node {
		for range size {
			n = &core.Label{Body: n}
		}
		return n
	}
	for _, test := range []struct {
		name  string
		node  func() core.Node
		query func(core.Node)
	}{
		{"success", func() core.Node { return &core.Seq{Items: succeeds()} }, func(n core.Node) { polled.Succeeds(n) }},
		{"failure", func() core.Node { return &core.Look{Neg: true, Body: &core.Choice{Alts: fails()}} }, func(n core.Node) { polled.Succeeds(n) }},
		{"cut", func() core.Node { return &core.Seq{Items: succeeds()} }, func(n core.Node) { polled.LooseCut(n) }},
		{"failure labels", func() core.Node { return &core.Look{Neg: true, Body: labelled(&core.FailNode{})} }, func(n core.Node) { polled.Succeeds(n) }},
		{"cut labels", func() core.Node { return labelled(&core.Succeed{}) }, func(n core.Node) { polled.LooseCut(n) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			n := test.node()
			func() {
				defer func() {
					if c, ok := recover().(cancel.Cancellation); !ok || !errors.Is(c.Err, context.Canceled) {
						t.Fatalf("cancellation was swallowed or changed: %v", c)
					}
				}()
				test.query(n)
				t.Fatal("the proof finished without observing cancellation")
			}()
			if !reflect.DeepEqual(n, test.node()) {
				t.Fatal("the interrupted proof changed its node")
			}
		})
	}
	// The facts of rules off the success fixed point, such as rules on a cycle, still poll.
	var src strings.Builder
	for i := range size {
		fmt.Fprintf(&src, "rule r%d = r%d\n", i, (i+1)%size)
	}
	func() {
		defer func() {
			if c, ok := recover().(cancel.Cancellation); !ok || !errors.Is(c.Err, context.Canceled) {
				t.Fatalf("cancellation of the rule facts was swallowed or changed: %v", c)
			}
		}()
		structuralFacts(t, src.String(), &cancel.Control{Ctx: ctx})
		t.Fatal("the rule facts were computed without observing cancellation")
	}()
}
