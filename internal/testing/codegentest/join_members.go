package codegentest

// JoinMemberGrammar has a join of every member form: a struct type, a list node, a tuple node, a
// string node and error, besides a named node type that is no member (Num, a string node), which
// converts to the string node member; a join with node[_], which takes every node besides its struct
// member; and an undeclared join of a struct type and a list node.
const JoinMemberGrammar = `type Item = { V string }
type Num = node[string]
type J ~ Item | node[[]Item] | node[(node[string], Item)] | node[string] | error
type K ~ Item | node[_]
type R = { A []J; B *(Item | node[[]Item]); C []K }
rule main R = "{" xs:elem* "}" b:(item / lst)? "|" ks:kel* => R{A: $xs.Value, B: $b, C: $ks.Value}
rule elem J = !"}" v:val ";" => $v
rule val J = (item / lst / tup / str / num) #recover(skip=[^;|]+)
rule kel K = item / lst / tup / str
rule item Item = "i" t:[a-z] => Item{V: text($t)}
rule lst = "[" xs:item* "]" => $xs
rule tup = "<" t:([a-z] item) ">" => $t
rule str = "'" s:[a-z] => $s
rule num Num = [0-9]`

// JoinMemberInputs are inputs of JoinMemberGrammar with each member, a recovered error, and none.
var JoinMemberInputs = []string{"{}|", "{ia;}|", "{[iaib];<kic>;'x;7;?;}[ia]|ia[ib]<kic>'z", "{?;ia;}ia|", "{[];}|"}
