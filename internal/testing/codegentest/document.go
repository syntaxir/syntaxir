package codegentest

// IncrementalGrammar is a grammar with left recursion, an Operators Rule expression, line starts (which
// examine the preceding character), captures, and position-dependent actions.
const IncrementalGrammar = `type Pos = { At int; Name node[string] }
rule main = (line / blank)* $$
rule blank = "\n"
rule line = ^ (assign / list / pos) "\n"
rule assign = n:name " = " e:expr
rule list = list "," item / item
rule item = [a-z]+ #atomic
rule pos Pos = "@" n:name => Pos{At: $n.Start, Name: $n}
rule name = [a-z]+ #atomic
rule expr = #operators {
    operand [0-9]+ #atomic
    operand "(" x:expr ")" => $x
    level { infix left "+" / "-" }
    level { infix left "*" }
    level { prefix "-" }
}`

// GrowthValuesGrammar has growth step results that end at the same position under equal roots and
// differ below them, in children, positions or text. A Document compares such results after edits
// of its texts: one that compared them by their roots alone would give them one identity and
// return the tree of the other.
const GrowthValuesGrammar = `@start
rule main = r0
rule r0 = (r3 !r1 &"c" / r2 level(r9, l2) "b") / r0 (level(r9, l2) (r1 / r3 "c") / r3) / r1 / "a"
rule r1 = ("c" / r3)* !"b" r2 / r9 / r0 "a" / "b"
rule r2 = "c" / &!r4 r1 r3 / r4? / "b"
rule r3 = "c" r4 "c" / r3 / (!r4) #recover(skip=[abc]) ((r0) #recover(skip=[abc]) "a" / r0) "c" / "a"
rule r4 = level(r9, l2) / "a"
rule r9 = #operators {
    operand r1
    operand "b"
    level l1 { infix left "a" }
    level l2 { postfix "c" }
}
`

// GrowthValuesText is a text of GrowthValuesGrammar, and GrowthValuesEdits a series of edits of it
// (start, end, inserted text). After the first, a Document that compared step results by their
// roots alone returns another tree than a parse from scratch.
const GrowthValuesText = "baabcac"

var GrowthValuesEdits = []struct {
	Start, End int
	Text       string
}{{4, 5, "a"}, {0, 1, "ab"}, {5, 5, "bb"}, {2, 3, "cb"}}
