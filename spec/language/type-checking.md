# Type Checking

Every rule has a type, and the checker verifies, when a grammar is compiled,
that the values flowing through the grammar are of the types they are used as.
This chapter defines the type of each Rule Expression, how the type of a rule
without an explicit Type Expression is inferred, and the errors that the checker
reports. The types themselves and the relation "assignable" are defined in the
[Type System](types.md).

## Rule Expression Types

| Rule Expression | Type |
|:--|:--|
| Literal, character class, `.`, `a #atomic` | `node[string]` |
| `a b` | `node[(A, B)]`, a tuple node of the types of the elements; the elements of a group that is not captured are elements of the enclosing sequence |
| `a / b` | `A \| B`, the join of the types of `a` and `b`; when one of them has no value and the other has one, the choice is `nil` where the one without a value matched, so its type is optional, `*(A \| B)` |
| `a*`, `a+`, `a #repeat(...)` | `node[[]A]`, a list node of the type of the element |
| `a?` | `*A`, where `A` is the type of `a` |
| `&a`, `!a`, `_` | `node[()]`, a node of the empty tuple |
| Rule call | The declared type of the called rule, or its inferred type |

A node type also records the captures made in the node, which
[`x.$y`](actions.md#field-access) reads; diagnostics show them after the
value, as in `node[(node[string], num), {"k": 1}]`.

### Types of Captures

The type of a capture `name:a` is the type of `a`. If the capture is made only
in some alternatives of a choice, inside an optional expression, or inside an
expression that [`#recover`](pragmas.md#recover) recovers, it may not match,
and its type is `*T` instead. A name captured more than once in a match
has the join of the types of its captures, since the last capture that matches
gives its value; when at least one of those captures is always made, the join is
taken without nil, and otherwise each capture that may not be made contributes nil,
so the type is optional, `*T`. When `T` is the [type that is not
known](#types-that-are-not-known), `*T` is that type itself.

```sxir
rule r = x:"a"? y:("b" / z:"c") // $x: *node[string], $y: node[string], $z: *node[string]
rule s = k:num "=" k:("x" "y")  // $k: num | node[(node[string], node[string])]
rule t = (v:"a" "b") #recover(skip="z") "c"   // $v: *node[string]
rule u = (v:"a" "b") #recover(skip="z") v:"c" // $v: node[string], the second v is always made
```

## Action Expression Types

Action Expressions in [Value Actions](actions.md) and [Predicate Actions](predicates.md#predicate-actions)
have the following types.

| Action Expression | Type |
|:--|:--|
| Integer literal, `len(x)`, `x.Start`, `x.End` | `int` |
| `x.Value` | `T` for a node of type `node[T]` or of a named node type over it: `string` for a string node, `(A, B)` for a tuple node and `[]A` for a list node |
| String literal, `text(x)` | `string` |
| `true`, `false`, a comparison, `&&`, `\|\|`, `!` | `bool` |
| `nil` | `nil`, assignable to any optional type |
| `$label` | The type of the capture (see above) |
| `T{Field: value, ...}` | Struct Type `T` |
| `[]T{a, b, ...}` | `[]T`; every element must have a known Type assignable to `T` |
| `x.Name` | The type of the field `Name`; for a join type, every member MUST declare the field, and the type is the join of the field types; a recovered value (`error` or `missing`) among the members need not, and makes the type optional (`*T`) ([Field Access](actions.md#field-access)) |
| `x.$y` | The join of the types of capture `y` of the node types `x` may have |
| `x[i]` | The element type of `x`, which MUST be a list, a tuple, or a node whose value is a list or a tuple; `i` MUST be an `int`, and a constant index into a tuple selects that element's type |
| `x.(T)` | `T`; the type of `x` or one of its alternatives MUST be assignable to `T` or one of its alternatives, or the other way around |
| `foldl(init, l, f)`, `foldr(init, l, f)` | The type of `init`, widened to the join of it and the type of `f`'s result |
| `map(l, f)` | `[]T`, where `T` is the type of `f`'s result |
| `list(a, b, ...)` | `[]T`, where `T` is the join of the types of the arguments |
| `concat(l1, l2, ...)` | `[]T`, where `T` is the join of the element types of the arguments |

The list functions treat `nil` as an empty list, which contributes no element
Type: `concat(nil, l)` has the Type of `concat(l)`, and `filter(nil, f)` is
assignable to every list Type.

For `[]T{...}`, the checker validates every element against the explicit element
Type `T`. It reports non-assignable elements and elements whose Type is unknown,
including unknown values nested in a typed list. An empty literal still has
the declared `[]T` Type. Nil is allowed only if `T` permits nil.

A struct field value whose Type is `_` or [not known](#types-that-are-not-known),
or a list with elements of such a Type, is reported when the field's Type is
more specific: `cannot use _ as Ident in field Name of Decl: the type of the
value is not known; convert it with a cast, as in value.(Ident)`, which names a
value of the type that is not known `unknown`. A cast (see [Casts](actions.md#casts)) is the check
that runs; see [Values and Constructors](actions.md#values-and-constructors). A value
whose Type is known and assignable needs no check, and a constructor checks nothing when
it runs.

Likewise, a rule value whose Type is `_` or not known, from an action, a called rule or
an Operators Rule line, is reported when the rule declares a Type it does not fit:
`rule term produces _, which is not assignable to Expr: the type of the value is not
known; convert it with a cast, as in value.(Expr)`, and for a value of the type that is
not known `cannot use unknown as Expr in result of rule term` with the same hint (see [Types and
Errors](actions.md#types-and-errors)). In an Operators Rule with a declared Type and no
whole-rule action, the captures `lhs` and `rhs` have that Type; with a
whole-rule action, they have the folded type (see [Operators Rule
Types](operators-rule.md#types)).

Diagnostics print a type as written in a grammar: `node[string]` for a string
node, `node[(node[string], node[string])]` for a tuple node of two string nodes,
`(node[string], node[string])` for its value and `[]node[string]` for a list
value. A reference to a rule without a declared type prints the rule's name,
also where a recursive type refers to itself, as in `[]*expr`. Run-time errors
name a value the same way, writing `_` for elements they do not examine:
`node[(_, _)]` for a tuple node of two elements, `node[[]_]` for a list node
and `error` for an error value.

A named node type is printed by its name, as `Num`, and `cannot convert X to N` names both types.

A cast tests a value as the value types describe it. `node[T]` takes
a node whose value `T` takes, so `node[string]` takes a string node,
`node[(A, B)]` a tuple node of two elements of types `A` and `B`, and
`node[[]A]` a list node of elements of type `A`; `node[_]` takes every string, tuple or list node; a
tuple Type takes a tuple value of as many elements; a list Type takes a list
value, such as one an action builds, and not the node of a repetition, whose
list is its `Value`; `[]_` and `[]any` take every list value; and an error
value from recovery fits only a Type that admits `error`. A cast in a predicate that fails
makes the predicate fail.

The argument of `len` and `text` MUST be a string, a node or a recovered value (possibly `nil`);
the list arguments of `foldl`, `foldr`, `map`, `filter` and `concat` MUST be lists, and
an error value (from [error recovery](pragmas.md#recover)) counts as an empty list.
A function (`(acc, item) => ...`) can appear only as an argument of `foldl`,
`foldr` or `map`, and it has the number of parameters those functions require.

## Type Inference

The type of a rule without a declared type is inferred from its body, or from
its action if it has one. Dependencies are inferred before their callers, so
finite acyclic chains do not depend on definition order or an iteration cap.
Recursive dependency groups are iterated until their types no longer change;
join-member ordering does not count as a type change. Operators Rule operator actions
also depend on their rule's type through the `lhs` and `rhs` values, or on the
folded type when the rule has a whole-rule action.

A call of a rule has the called rule's type as a reference, as in `[]*expr`, so
recursion changes no type until an action reads into the value of a rule of the
group, with `x.Value`, a field, a capture, an index or a list function. Such a group
is solved as a least fixed point: its rules start with no value, `_|_`, and each
round computes every rule of the group from the types of the previous round,
joined with them, so that neither the types nor the errors depend on the order
of the rules. A type that keeps growing ends the rounds: from the ninth round on,
a rule whose type changes is widened to the [type that is not
known](#types-that-are-not-known) for the rest of inference. The rounds end with
the first round in which no type changes, so every rule of the group has the
type of its body or action under the final types of the group. They do end: a
widened rule absorbs every join and does not change again, so each round from
the ninth on that changes a type widens one more rule, and a group of `n` rules
takes at most `n + 9` rounds. Rules whose types
do not change, acyclic rules and large types that are stable keep their types.
This is a resource limit, not a proof of infinite growth: a finite type that is
still growing in the ninth round is widened too. Declare recursive result types
when precise checking is required.

```sxir
rule num = [0-9]+ #atomic // node[string]
rule pair = k:num "=" v:num // node[(num, node[string], num), {"k": 0, "v": 2}]
```

The type of the accumulator of `foldl` and `foldr` is widened to the join of
the type of the initial value and the type of the result of the function. The checker widens
three times; if the result of the function still does not fit the accumulator, the call is a
compile-time error (`the accumulator type does not converge`), and the result of the function
must be cast. A result that has the [type that is not known](#types-that-are-not-known) where
the accumulator's type is more specific does not fit it, and widens the accumulator to the type
that is not known: `foldl(0, $x, (a, e) => e)` over a list `$x` of the type that is not known
has that type, so a rule declared `int` must cast it.

## Types That Are Not Known

The **type that is not known** is the type of a value about which the checker
knows nothing. No Type Expression denotes it, and diagnostics print it as
`unknown`. Like `_`, it admits every value, and its join with any type is
itself: a join with a member of the type that is not known, and an optional
type of it, are the type that is not known, which admits `nil` as well. Unlike
`_`, it is accepted by every operation that reads into it, so the check moves to
run time.

In a grammar that compiles, a value has the type that is not known only in these
cases:

| Case | Where it is defined |
|:--|:--|
| The value of a rule of a recursive group whose type kept changing, and so the value of a call of the rule | [Type Inference](#type-inference) |
| `$lhs` and `$rhs` of an Operators Rule with a whole-rule action whose folded type keeps growing when some line produces a value other than a node, such as the error value of a line with `#recover` | [Operators Rule Types](operators-rule.md#types) |
| A capture or lambda parameter that holds such a value, and an element of a list of such elements | [Types of Captures](#types-of-captures) |
| A join with such a member, and an optional type of it: the type of `g?`, of a choice with an alternative of the type that is not known, and of a capture that may not be made of such a value, as `x` in `x:g?` | [Types of Captures](#types-of-captures) |
| The accumulator of `foldl` and `foldr` when the result of the function has the type that is not known where the accumulator's type is more specific | [Type Inference](#type-inference) |
| The result of `x.Name`, `x.$y`, `x.Value`, `x.Start`, `x.End` or `x[i]`, and of `+`, when `x` or an operand of `+` has the type that is not known | [Action Expression Types](#action-expression-types) |
| The elements that `map`, `filter`, `foldl`, `foldr` and `concat` read from a list argument of the type that is not known, which `filter` and `concat` return in a list | [Built-In Functions](actions.md#built-in-functions) |

A value of the type that is not known is accepted by every operation that reads
into it: as the operand of every operator, as the argument of every built-in
function, as the receiver of every member access and as a list or an index. The result has the type the operation gives for an
operand of the right kind: `int` for `-`, `*`, `/`, `%`, `len` and the unary
`-`, `bool` for a comparison, `&&`, `||` and `!`, `string` for `text`, and the
type that is not known in the cases of the table. When the operation runs, an
operand of another kind raises a run-time action error, as `$x && true` does
with `invalid operand int for &&` when `$x` holds an `int`; the error follows
the surrounding action ([Types and Errors](actions.md#types-and-errors)).

Where a value meets a declared Type, a value of the type that is not known is
treated as a value of type `_`, which is assignable only to `_`: as the value of
a rule with a declared Type, a field of a struct, the value of a variable
binding or a value converted to a named node type, it is a compile-time error
unless that Type is `_`, and a cast, which is checked when it runs, makes it
fit. An element of a typed list `[]T{...}` of the type that is not known is a
compile-time error even when `T` is `_`. A part of a type counts as well: a list
of elements of the type that is not known is not assignable to `[]int`, while
a part where the declared Type has `_` needs no cast: a node whose value has
such a part is assignable to `node[_]`, and a tuple `(x, 1)` whose `x` is of the
type that is not known to `(_, int)`.

The checker follows a type into its lists, optional types, nodes, joins and
tuples to 32 levels. A type nested deeper is taken to have a part of the type
that is not known where it meets a declared Type, so the value must be cast,
and an operand of `==` or `!=` whose type is nested deeper is checked when the
comparison runs.

A value of type `_`, written in a Type Expression or taken from a `node[_]`
value, admits every value as well, but the checker accepts it only where `_` is
accepted: as the operand of `==` and `!=`, which are checked when they run, and of a cast.
Every other operation on it is a compile-time error, as `cannot index _`.

```sxir
rule g = ("(" x:g ")" => list(list($x[0]))) / ("a" => list(1)) // g: unknown
rule h = x:g => $x[0] + 1                                       // h: unknown
rule n int = x:g => len($x[0])                                  // checked when it runs
```

## Checked Errors

The type checker reports the following errors:

- an undefined type, a type alias that refers to itself, and a duplicate field;
- a field that the struct type does not declare, in a struct constructor or a field access,
  and a field value that is not assignable to the type of the field;
- a typed-list element that is unknown or not assignable to its explicit element Type;
- a rule whose value is not assignable to its declared type;
- a constructor `N{...}` of a named node type with other than one positional argument, or a positional argument
  to a struct type;
- a value that cannot be converted to a named node type where a conversion is required (`N{x}` or a rule's
  declared type), including `_`, `node[_]`, `error`, `nil` and scalars;
- a cast to a named node type that the static types rule out (see [Casts](actions.md#casts)).
- a field access on a value that may be `nil`;
- an operand of an operator or an argument of a built-in function with an
  invalid type;
- a variable that is read or bound without a declaration, a binding whose value
  is not assignable to the declared type of the variable, and a variable
  declaration with an unsupported type or an initial value of another type
  (see [Variables](predicates.md#variables)).

A variable has the type its `var` declaration gives it, so a read of a variable
is checked like any other value of that type: `x + 1` and `V{N: x}` are checked
when the grammar is compiled. A binding is checked only statically: it is a compile-time error
unless the Type of the value is known to be assignable to the declared type of the variable,
`cannot use <value type> as <type> in the binding of variable <name>`. A value whose Type the
checker does not know, such as a rule value of unknown Type, is rejected too, with a hint
to convert it explicitly: `x := $d.(int)`. The cast is an ordinary cast (see
[Casts](actions.md#casts)); when it fails, that is an evaluation error of the binding, which
makes the predicate fail. Nothing is checked when a binding runs, so a variable always holds a value of its
declared type.
