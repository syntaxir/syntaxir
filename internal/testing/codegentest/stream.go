package codegentest

import "strings"

const Records = `rule main = ^^ header items:record* #stream $$
rule header = "#records\n"
rule record = ^ k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n"`

// RecordsConcat is Records whose elements are struct values with strings concatenated by actions.
const RecordsConcat = `type Rec = { K string; V string }
rule main = ^^ header items:record* #stream $$
rule header = "#records\n"
rule record = ^ k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" => Rec{K: text($k) + "!", V: text($v)}`

// RecordsRecover is Records that recovers from a malformed line by skipping it.
const RecordsRecover = `rule main = ^^ header items:record* #stream $$
rule header = "#records\n"
rule record = ^ (k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n") #recover(skip=[^\n]* "\n")`

// StreamTextCommitGrammar reads, in the start rule after its #stream repetition, the text of a head
// before the first element: text cuts a range at the last commit point, so the head has no text once
// an element was delivered and its whole text when none was, whatever input the stream still holds.
// The tail says which: "!" after elements, "?" after none; a predicate that does not hold fails the
// parse. The texts of the head and of the tail count their units.
const StreamTextCommitGrammar = `@start
rule main = h:head item* #stream ("!" {text($h) == ""} / "?" {text($h) == "hé>"}) t:tail {text($t) == text($t)}
rule head = [a-zé]+ ">"
rule item = [a-zé]+ ";"
rule tail = [!€]*
`

// StreamTextCommitInputs are the inputs of the grammar: no element, one element before a long tail,
// which a stream holding the whole input does not discard, many elements, and two inputs whose
// tails contradict the texts the commit leaves, which fail.
var StreamTextCommitInputs = []string{
	"hé>?",
	"hé>a;!",
	"hé>a;!" + strings.Repeat("!€", 300),
	"hé>" + strings.Repeat("é;", 400) + "!",
	"hé>" + strings.Repeat("ab;", 300) + "!" + strings.Repeat("€", 600),
	"hé>a;?",
	"hé>!",
}

// StreamTextCommitFails is the number of inputs at the end of StreamTextCommitInputs that fail.
const StreamTextCommitFails = 2

// StreamTextCommitChunks are the read sizes of the chunkings, the last one the whole input.
var StreamTextCommitChunks = []int{1, 2, 3, 5, 8, 64, 1 << 20}
