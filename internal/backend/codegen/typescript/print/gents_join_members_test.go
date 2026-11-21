package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A join of struct types, node types and error is a union of its members whatever the values of its
// node members, and narrows by type: a string, tuple or list node has its kind as its type, also a
// node of a named node type that converts to the member of its kind.
func TestGeneratedTSJoinMembers(t *testing.T) {
	t.Parallel()
	consumer := `import { parseValue } from "./parser.ts";
import type { Item, J, K, R, ListNode, TupleNode, StringNode, RecoveredError, TNode } from "./types.ts";
` + tsTypedAssert + `
type Eq<A, B> = (<T>() => T extends A ? 1 : 2) extends (<T>() => T extends B ? 1 : 2) ? true : false;
const j: Eq<J, RecoveredError | null | Item | ListNode<readonly (Item | null)[] | null> | TupleNode<readonly [StringNode | null, Item | null]> | StringNode> = true;
const k: Eq<K, RecoveredError | null | Item | TNode<unknown>> = true;
const b: Eq<R["B"], RecoveredError | null | Item | ListNode<readonly (Item | null)[] | null>> = true;
check(j && k && b, "union types");
function describe(x: J): string {
  if (x === null) return "null";
  switch (x.type) {
    case "Item": return "item " + x.V;
    case "list": return "list " + (x.value ?? []).map(i => i?.V).join(",");
    case "tuple": return "tuple " + x.value[0]?.value + " " + x.value[1]?.V;
    case "string": return "string " + x.value;
    case "error": return "error " + x.start;
  }
}
const r = parseValue("{[iaib];<kic>;'x;7;?;}[ia]|ia[ib]<kic>'z");
check(r.value !== null, "parses: " + stable(r.error)); // with the recovered error at 19
const got = r.value!.A!.map(describe).join("|");
check(got === "list a,b|tuple k c|string x|string 7|error 19", got);
check(r.value!.B?.type === "list", "anonymous join member: " + stable(r.value!.B));
const kinds = r.value!.C!.map(x => x === null ? "null" : x.type).join(",");
check(kinds === "Item,list,tuple,string", kinds); // node[_] takes the other nodes with their kinds
`
	runTypedTSFixture(t, codegentest.JoinMemberGrammar, consumer, common.GenOptions{}, "")
}
