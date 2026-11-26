package codegentest

// StreamWindowCases are streams of a repeated line whose memory count follows the window of the
// memory theorem of the streaming proof (spec/proofs/streaming.md): the largest count after a commit
// is the same, up to the sampling of where node chunks are split, for a stream four times as long.
// They keep the records of skip-run, run and scan sites, and memo entries that hold recovered
// errors, which a stream drops with its memo. Lines is the number of lines of the shorter stream,
// Per the number of elements of a line, and Limit the diagnostic limit, 0 for none.
var StreamWindowCases = []struct {
	Name, Src, Line   string
	Lines, Per, Limit int
}{
	{"memo", `@start
rule main = item* #stream !.
rule item = a:word "," b:word "\n" / a:word "," b:word ";"
rule word = [a-z]+
`, "abc,abc\n", 2048, 1, 0},
	{"recover limited", `@start
rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, "a=1;b;", 2048, 2, 2},
	// The skip expression of the recovery is a skip-run site whose records keep expectations.
	{"skip records limited", `@start
rule main = item* #stream !.
rule item = x / .
rule x = ("b" #recover(skip=(word / [a-z ])*)) "c"
rule word = [a-z]+ !"="
`, "bab abc cxbaa a!c", 2048, 16, 2},
	// list is a run site, which the second alternative walks again from the second word on.
	{"run sites", `@start
rule main = item* #stream !.
rule item = list "x" ";" / word "," list "y" ";"
rule list = (word ",")*
rule word = [a-z]+
`, "ab,cd,ef,gh,ij,kl,mn,op,qr,st,uv,wx,ab,cd,ef,gh,ij,kl,mn,op,qr,st,uv,wx,y;", 512, 1, 0},
	// ws is a scan site: the second alternative walks the spaces again.
	{"scan sites", `@start
rule main = item* #stream !.
rule item = ws "a" ws ";" / ws "b" ws ";"
rule ws = [ ]*
`, "                                        b;", 512, 1, 0},
}

// StreamCountSlack is how far the largest memory count of a stream of StreamWindowCases may rise
// when the stream is four times as long: the counts after the commits are sampled where the chunks
// of nodes are split, which falls at more places in an element on a longer stream. Anything a stream
// kept for each element would add a byte or more for each of the three quarters of the elements of
// the longer stream.
const StreamCountSlack = 256
