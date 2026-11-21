package libgo

import (
	"errors"
	"reflect"
	"testing"

	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

func TestWalkEdgeOrderAndSharedParents(t *testing.T) {
	shared := nodeOfData(&Data{Text: "shared"})
	left := nodeOfData(&Data{Children: []*sxirvalue.Node{shared}})
	right := nodeOfData(&Data{Fields: valueimpl.Fields{{Name: "nested", Value: []any{shared, nil, 7}}}})
	root := nodeOfData(&Data{Children: []*sxirvalue.Node{left, right, nil}, Fields: valueimpl.Fields{{Name: "z", Value: false}, {Name: "a", Value: shared}}})
	var got []string
	var parents []*sxirvalue.Node
	err := sxirvalue.Walk(root, func(v sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) {
		switch v.Edge.Kind {
		case sxirvalue.WalkRoot:
			got = append(got, "root")
		case sxirvalue.WalkChild:
			got = append(got, "child")
		case sxirvalue.WalkField:
			got = append(got, v.Edge.FieldName)
		case sxirvalue.WalkListElement:
			got = append(got, "list")
		}
		if v.Node == shared {
			parents = append(parents, v.Parent)
		}
		return sxirvalue.WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"root", "child", "child", "child", "nested", "list", "list", "list", "child", "a", "z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("visits = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(parents, []*sxirvalue.Node{left, right, root}) {
		t.Fatal("shared edge parents lost")
	}
	if D(root).Fields[0].Name != "z" {
		t.Fatal("walk sorted fields in place")
	}
}

func TestWalkCyclesAndControls(t *testing.T) {
	n := nodeOfData(&Data{})
	D(n).Children = []*sxirvalue.Node{n}
	var cycle *sxirvalue.CycleError
	err := sxirvalue.Walk(n, func(sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) { return sxirvalue.WalkContinue, nil })
	if !errors.As(err, &cycle) || len(cycle.Path) != 2 || cycle.Path[1].Kind != sxirvalue.WalkChild {
		t.Fatalf("cycle = %#v, %v", cycle, err)
	}
	list := make([]any, 1)
	list[0] = list
	n = nodeOfData(&Data{Fields: valueimpl.Fields{{Name: "x", Value: list}}})
	err = sxirvalue.Walk(n, func(sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) { return sxirvalue.WalkContinue, nil })
	if !errors.As(err, &cycle) || len(cycle.Path) != 3 {
		t.Fatalf("list cycle = %v", err)
	}
	sentinel := errors.New("callback")
	if err := sxirvalue.Walk(n, func(sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) { return sxirvalue.WalkContinue, sentinel }); err != sentinel {
		t.Fatal(err)
	}
	for _, action := range []sxirvalue.WalkAction{sxirvalue.WalkSkip, sxirvalue.WalkStop} {
		count := 0
		if err := sxirvalue.Walk(n, func(sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) { count++; return action, nil }); err != nil || count != 1 {
			t.Fatalf("control %d = %d, %v", action, count, err)
		}
	}
	count := 0
	if err := sxirvalue.Walk(nil, func(v sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) {
		count++
		if v.Value != nil || v.Node != nil || v.Depth != 0 {
			t.Fatal(v)
		}
		return sxirvalue.WalkContinue, nil
	}); err != nil || count != 1 {
		t.Fatal(err, count)
	}
}

func TestWalkDeep(t *testing.T) {
	n := nodeOfData(&Data{})
	const depth = 20000
	for i := 0; i < depth; i++ {
		n = nodeOfData(&Data{Fields: valueimpl.Fields{{Name: "next", Value: []any{n}}}})
	}
	count := 0
	if err := sxirvalue.Walk(n, func(v sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) {
		if v.Node != nil {
			count++
		}
		return sxirvalue.WalkContinue, nil
	}); err != nil || count != depth+1 {
		t.Fatal(count, err)
	}
}

func TestCursorBoundariesAndUncontainedChildren(t *testing.T) {
	for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
		a, b, z := nodeOfData(&Data{Start: 1, End: 3}), nodeOfData(&Data{Start: 3, End: 4}), nodeOfData(&Data{Start: 3, End: 3})
		root := nodeOfData(&Data{Start: 20, End: 21, Children: []*sxirvalue.Node{a, b, z, a}})
		c := sxirvalue.Cursor{Root: root, Unit: unit}
		var got []*sxirvalue.Node
		callback := func(v sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) {
			got = append(got, v.Node)
			return sxirvalue.WalkContinue, nil
		}
		if err := c.At(3, callback); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []*sxirvalue.Node{b, z}) {
			t.Fatalf("point = %v", got)
		}
		got = nil
		if err := c.Range(1, 3, callback); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []*sxirvalue.Node{a, a}) {
			t.Fatal("range must preserve shared edges and exclude upper bound")
		}
		got = nil
		if err := c.Range(3, 3, callback); err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
		if err := c.Range(4, 3, callback); err == nil {
			t.Fatal("reversed range accepted")
		}
		if err := c.At(-1, callback); err == nil {
			t.Fatal("negative position accepted")
		}
	}
	if err := (sxirvalue.Cursor{Unit: sxirinput.PositionUnit(99)}).At(0, func(sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) { return sxirvalue.WalkContinue, nil }); err == nil {
		t.Fatal("invalid unit accepted")
	}
}

func TestWalkSliceIdentityIgnoresCapacity(t *testing.T) {
	x := make([]any, 1, 2)
	x[0] = x[:1:1]
	root := nodeOfData(&Data{Fields: valueimpl.Fields{{Name: "x", Value: x}}})
	visits := 0
	err := sxirvalue.Walk(root, func(sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) {
		visits++
		return sxirvalue.WalkContinue, nil
	})
	var cycle *sxirvalue.CycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("capacity-limited slice cycle = %v, want CycleError", err)
	}
	want := []sxirvalue.WalkEdge{{Kind: sxirvalue.WalkRoot}, {Kind: sxirvalue.WalkField, FieldName: "x"}, {Kind: sxirvalue.WalkListElement, ListIndex: 0}}
	if visits != 2 || !reflect.DeepEqual(cycle.Path, want) {
		t.Fatalf("cycle detected after %d visits at %v, want 2 visits at %v", visits, cycle.Path, want)
	}
}

// A capture that labels an element of a node with source information is visited as that element
// only; a capture that is not an element is visited as a field.
func TestWalkVisitsCapturedElementsOnce(t *testing.T) {
	elem := nodeOfData(&Data{Text: "e"})
	beside := nodeOfData(&Data{Text: "b"})
	root := nodeOfData(&Data{Labeled: true, Children: []*sxirvalue.Node{elem}, Fields: valueimpl.Fields{{Name: "x", Value: elem}, {Name: "s", Value: beside}}})
	visits := map[*sxirvalue.Node]int{}
	err := sxirvalue.Walk(root, func(v sxirvalue.WalkVisit) (sxirvalue.WalkAction, error) {
		visits[v.Node]++
		return sxirvalue.WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if visits[elem] != 1 || visits[beside] != 1 || visits[root] != 1 {
		t.Fatalf("visits = elem %d, beside %d, root %d, want 1 each", visits[elem], visits[beside], visits[root])
	}
}
