package libgo

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
)

// The push table (conform.PushCases) on the compositions push and pushItems of the node and the typed library
// (ActionPush and ActionPushItems, printed from Exec IR), as a generated parser calls them for the
// elements of list, map, a tuple literal and concat.

func TestValuePushTable(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		// The library's pushItems counts no unit: the code of an action counts the elements that concat
		// builds itself (UNITSOF, units.items), which the action charge goldens check. So the table's units,
		// which a target whose pushItems counts them charges, are not checked here, and under limit
		// controls the library must count nothing.
		c, vals := nodeBuiltinFixture(controlled)
		if err := conform.RunPushes("node", false, func(call, value string) (string, int, int) {
			base := len(c.P.KidStack)
			var before uint64
			if controlled {
				before = c.P.UnitsNow()
			}
			got := memberRead(func() any {
				if call == conform.CallPush {
					c.fxPush(conform.PushFn(call), vals[value])
				} else {
					c.fxPushItems(vals[value])
				}
				return nil
			}, memberCanon)
			if units := c.P.UnitsNow() - before; controlled && units != 0 {
				t.Errorf("node: %s(%s): counted %d units, want 0", call, value, units)
			}
			units := 0
			pushed := slices.Clone(c.P.KidStack[base:])
			c.P.DropKids(base)
			if got == "nil" {
				got = memberRead(func() any {
					if call == conform.CallPush {
						if len(pushed) != 1 || pushed[0] == nil {
							return nil
						}
						return pushed[0]
					}
					return c.listNode(pushed)
				}, memberCanon)
			}
			return got, len(pushed), units
		}); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
		tc, tvals := typedBuiltinFixture(controlled)
		if err := conform.RunPushes("typed", false, func(call, value string) (string, int, int) {
			base := len(tc.P.KidStack)
			var before uint64
			if controlled {
				before = tc.P.UnitsNow()
			}
			got := memberRead(func() any {
				if call == conform.CallPush {
					tc.fxPush(conform.PushFn(call), tvals[value])
				} else {
					tc.fxPushItems(tvals[value])
				}
				return nil
			}, tmemberCanon)
			if units := tc.P.UnitsNow() - before; controlled && units != 0 {
				t.Errorf("typed: %s(%s): counted %d units, want 0", call, value, units)
			}
			units := 0
			pushed := slices.Clone(tc.P.KidStack[base:])
			tc.P.DropKids(base)
			if got == "nil" {
				if call == conform.CallPush {
					got = "nil"
					if len(pushed) == 1 && pushed[0] != nil {
						e := pushed[0]
						if k := valKind(e); k == valKindInt || k == valKindString || k == valKindBool {
							// The typed library keeps a scalar element as it is, where the node library boxes it.
							got = "box " + tmemberCanon(e)
						} else {
							got = tmemberCanon(e)
						}
					}
				} else {
					got = tmemberCanon(&TSeq{Typ: TypeListValue, Kids: pushed})
				}
			}
			return got, len(pushed), units
		}); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}
