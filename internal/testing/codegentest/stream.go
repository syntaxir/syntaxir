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
// before the first element and the text of a tail after the last one. The head is a string node,
// which holds its text, so its text is whole after any number of commits, whatever input the stream
// still holds; the text of a head that is not a string node cannot be taken there (the checker
// rejects it, as TestStreamDivergentReadsRejected of the engine checks). The tail says which head the
// input claims: "!" the head the input has, "?" another one; a predicate that does not hold fails the
// parse. The texts of the head and of the tail count their units.
const StreamTextCommitGrammar = `@start
rule main = h:head item* #stream ("!" {text($h) == "hé>"} / "?" {text($h) == "x>"}) t:tail {text($t) == text($t)}
rule head = ([a-zé]+ ">") #atomic
rule item = [a-zé]+ ";"
rule tail = [!€]*
`

// StreamTextCommitInputs are the inputs of the grammar: no element, one element before a long tail,
// which a stream holding the whole input does not discard, many elements, and two inputs whose
// tails contradict the text of the head, which fail.
var StreamTextCommitInputs = []string{
	"hé>!",
	"hé>a;!",
	"hé>a;!" + strings.Repeat("!€", 300),
	"hé>" + strings.Repeat("é;", 400) + "!",
	"hé>" + strings.Repeat("ab;", 300) + "!" + strings.Repeat("€", 600),
	"hé>a;?",
	"hé>?",
}

// StreamStartActionGrammar has a start rule whose action fails where the rule's list has two
// elements: at the top call for "a;a;", which a stream does not evaluate, and at the nested call for
// "(a;a;)a;", which does not stream and builds its value.
const StreamStartActionGrammar = `rule main = ("(" main ")")? xs:(([a-z] ";")* #stream) => 10 / (len($xs) - 2)
`

// StreamStartActionInputs are the inputs of StreamStartActionGrammar.
var StreamStartActionInputs = []string{"", "a;a;", "(a;)a;a;", "(a;a;)a;", "a;a;a;"}

// StreamTextCommitFails is the number of inputs at the end of StreamTextCommitInputs that fail.
const StreamTextCommitFails = 2

// StreamTextCommitChunks are the read sizes of the chunkings, the last one the whole input.
var StreamTextCommitChunks = []int{1, 2, 3, 5, 8, 64, 1 << 20}
