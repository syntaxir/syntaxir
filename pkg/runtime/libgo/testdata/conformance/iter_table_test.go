package libgo

import (
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
)

// The iteration table (conform.IterCases) on the compositions itemAt and filterKeep of the node and the typed
// library (ActionItemAt and ActionFilterKeep, printed from Exec IR), through the helpers of compose_test.go
// for foldl, foldr, map and filter.

// iterCall calls the built-in function of the iteration table's call on v through the helpers of compose_test.go:
// fold, mapList and filterList are those of the node or the typed library.
func iterCall(call string, v any, fold func(fn string, right bool, acc, list any, f func(acc, item any) any) any,
	mapList, filterList func(list any, f func(item any) any) any) any {
	switch call {
	case conform.CallFoldl, conform.CallFoldr:
		return fold(call, call == conform.CallFoldr, nil, v, func(_, item any) any { return item })
	case conform.CallMap:
		return mapList(v, func(item any) any { return item })
	case conform.CallFilter:
		return filterList(v, func(item any) any { return item == any(1) })
	}
	return filterList(v, func(item any) any { return item })
}

func TestValueIterTable(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		// A step of an iteration of the library is no logical event: the code of an action counts each
		// element that map, filter or a fold visits itself (actionlower.Options.Visits), which the action
		// charge goldens check. So the table's steps, which a target whose iteration charges them checks, are
		// not checked here, and under limit controls the library must count nothing.
		c, vals := nodeBuiltinFixture(controlled)
		if err := conform.RunIters("node", false, func(call, value string) (string, int) {
			var before uint64
			if controlled {
				before = c.P.UnitsNow()
			}
			got := memberRead(func() any { return iterCall(call, vals[value], c.fxFold, c.fxMap, c.fxFilter) }, memberCanon)
			if units := c.P.UnitsNow() - before; controlled && units != 0 {
				t.Errorf("node: %s(%s): counted %d units, want 0", call, value, units)
			}
			return got, 0
		}); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
		tc, tvals := typedBuiltinFixture(controlled)
		if err := conform.RunIters("typed", false, func(call, value string) (string, int) {
			var before uint64
			if controlled {
				before = tc.P.UnitsNow()
			}
			got := memberRead(func() any { return iterCall(call, tvals[value], tc.fxFold, tc.fxMap, tc.fxFilter) }, tmemberCanon)
			if units := tc.P.UnitsNow() - before; controlled && units != 0 {
				t.Errorf("typed: %s(%s): counted %d units, want 0", call, value, units)
			}
			return got, 0
		}); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}
