package engine

import "testing"

// literalExpectedCases are literals with the text that a syntax error shows for each of them.
var literalExpectedCases = []struct{ src, text string }{
	{`"a"`, `"a"`},
	{`"\""`, `"\""`},
	{`"\\"`, `"\\"`},
	{`"\n"`, `"\n"`},
	{`"\t"`, `"\t"`},
	{`"\r"`, `"\r"`},
	{`"\0"`, `"\u{0}"`},
	{`"\f\v\a\b"`, `"\u{c}\u{b}\u{7}\u{8}"`},
	{`"\u{7f}"`, `"\u{7f}"`},
	{`"\u0080"`, `"\u{80}"`},
	{`"\u{a0}"`, `"\u{a0}"`},
	{`"\u{200b}"`, `"\u{200b}"`},
	{`"\u{1f600}"`, `"😀"`},
	{`"é"`, `"é"`},
	{`"\u{fffd}"`, `"�"`},
	{`"Ab"i`, `"Ab"i`},
	{`"a\"b"i`, `"a\"b"i`},
}

// TestLiteralExpectedText checks the text of a literal as an expected item: the spelling the grammar
// writes, with \" and \\, \n \t \r, \u{h} for every other character that is not a letter, mark,
// number, punctuation or symbol (or a space), and the suffix i.
func TestLiteralExpectedText(t *testing.T) {
	t.Parallel()
	for _, c := range literalExpectedCases {
		check(t, "rule main = "+c.src+"\n", fails("!", "1:1: syntax error: expected "+c.text))
	}
}
