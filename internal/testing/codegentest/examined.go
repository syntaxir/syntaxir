package codegentest

import "strings"

// ExaminedShapes are the shapes on which every producer's examined ends are checked against the
// independent reference (package examinedref), besides the generated corpus: growths of left
// recursion, Operators Rules, recovery, token insertion, predicates and variables, values of actions,
// #ignorecase, inline actions, input that is not valid UTF-8, the bytes that literals and anchors
// examine, and walks of hot scan sites. Each mutant of the reference must fail one of them.
func ExaminedShapes() []Case {
	const ops = "rule e = #operators {\n skip \" \"*\n operand [0-9]+ #atomic\n operand \"(\" e \" \"* \")\"\n" +
		" level add { infix left \"+\" / \"-\" }\n level cmp { infix none \"<\" }\n level neg { prefix \"-\" }\n" +
		" level mul { infix left \"*\" }\n level pow { infix right \"**\" }\n level post { postfix \"!\"; postfix \"[\" e \"]\" }\n}"
	return []Case{
		{Name: "direct growth", Src: "rule main = e $$\nrule e = e \"+\" n / n\nrule n = [0-9]+",
			Inputs: []string{"1", "1+2", "1+2+3", "1+2+", "1x"}},
		{Name: "indirect growth", Src: "rule main = a $$\nrule a = b \"1\" / b \"2\" / \"a\"\nrule b = a \"x\" / b \"y\" / \"b\"",
			Inputs: []string{"b1", "by2", "ax2", "byy1x2", "b1x1y2", "byyyyyy2x1"}},
		{Name: "growth read from a lookahead", Src: "rule main = e $$\nrule e = &(e \"x\" t) e \"x\" / t\nrule t = [0-9]+",
			Inputs: []string{"1x2", "1x2x3", "1x23x4", "1x2x"}},
		{Name: "operators", Src: "rule main = e $$\n" + ops,
			Inputs: []string{"1", "1+2*3", "1 + 2 * 3 ** 4 ** 5", "-1+2", "2**-1", "1<2<3", "1<2", "1<2+3<4", "(1<2)<3", "(1+2)*3!", "1[2+3]!", "1+", "1 <", "(1", "1+-", ""}},
		{Name: "level-restricted calls", Src: "rule main = level(e, mul) (\",\" level(e, cmp))* $$\n" + ops,
			Inputs: []string{"1*2", "1+2", "1*2,3+4<5", "-1+2", "1,2<3<4", "1*2,"}},
		{Name: "operators with juxtaposition and cuts",
			Src:    "rule main = e $$\nrule e = #operators {\n operand [a-z]\n operand \"\"\n level { infix right \"\" }\n level { prefix \"-\" #cut \"z\" }\n level { postfix \"?\" #cut \"!\" }\n}",
			Inputs: []string{"", "a", "ab", "abc", "-za", "-a", "a?!", "a?b", "a?"}},
		{Name: "operators in left recursion", Src: "rule main = s $$\nrule s = s \";\" e / e\n" + ops,
			Inputs: []string{"1;2", "1+2;3*4", "1;", "1;2+"}},
		{Name: "recovery", Src: "rule main = stmt* $$\nrule stmt = (\"let\" \" \" [a-z]+ \";\") #recover(skip=[^;]* \";\")",
			Inputs: []string{"let a;", "let a;let b;", "lex a;let b;", "let ;x;", "let a", ";;"}},
		{Name: "recovery in left recursion", Src: "rule main = e $$\nrule e = e \"+\" (n #recover(skip=[a-z]+)) / n\nrule n = [0-9]+",
			Inputs: []string{"1+2", "1+x+2", "1+x", "1+", "x"}},
		{Name: "token insertion", Src: "rule main = \"(\" [a-z]* \")\" #missing \";\" #missing $$",
			Inputs: []string{"(a);", "(a;", "(a)", "(a", "(", "a"}},
		{Name: "token insertion in left recursion", Src: "rule main = e $$\nrule e = e \"+\" #missing n / n\nrule n = [0-9]+",
			Inputs: []string{"1+2", "12", "1+2 3", "1"}},
		{Name: "predicates and variables",
			Src:    "var depth int\nrule main = item* $$\nrule item = n:[0-9] { text($n) != \"0\" } { depth := depth + 1 } \"a\" / { depth > 1 } \"b\"",
			Inputs: []string{"1a", "1a2ab", "1ab", "0a", "b", "1a2a3ab"}},
		{Name: "bindings undone by backtracking",
			Src:    "var x int\nrule main = ({ x := 1 } \"a\" \"b\" / \"a\") { x == 0 } \"c\" $$",
			Inputs: []string{"ac", "abc", "ab"}},
		{Name: "ignorecase", Src: "rule main = (\"select\" kw)* $$ #ignorecase\nrule kw = \" \" \"from\"",
			Inputs: []string{"select from", "SELECT from", "SeLeCt FROM", "select"}},
		{Name: "inline actions", Src: "rule main = (e => 1) $$\nrule e = (e \"+\" [0-9] => 2) / ([0-9] => 3)",
			Inputs: []string{"1", "1+2", "1+2+", "x"}},
		{Name: "inline actions on cycles of left calls",
			Src:    "rule main = a $$\nrule a = (b \"1\" => 1) / \"a\"\nrule b = (a \"x\" => 2) / (b \"y\" => 3) / \"b\"",
			Inputs: []string{"b1", "a", "ax1", "by1", "byy1x1", "ax1yx1", "b", "x1"}},
		{Name: "values of hidden rules on a cycle of left calls",
			Src:    "rule main = t:_p { len($t) == 2 } \"!\" $$\nrule _p = _q\nrule _q = _p \"x\" / \"a\"",
			Inputs: []string{"ax!", "a!", "axx!", "x!"}},
		{Name: "an optional value of a recursive hidden rule",
			Src:    "rule main = (o:_r? { $o != nil } \";\")* $$\nrule _r = \"a\" _r / \"b\"",
			Inputs: []string{"ab;", ";", "b;aab;", "a;"}},
		{Name: "invalid UTF-8", Src: "rule main = (\"é\" / [a-z] / .)* $$",
			Inputs: []string{"é", "\xc3", "\xe2\x82A", "a\xffb", "\xf0\x9f\x98", "\xf0\x9f\x98\x80"}},
		{Name: "stream", Src: "rule main = item* #stream\nrule item = [a-z]+ \";\" / \"(\" [a-z]* \")\"",
			Inputs: []string{"a;bc;", "a;b", "(ab)c;", "(a", ""}},
		{Name: "stream with token insertion", Src: "rule main = item* #stream $$\nrule item = [a-z]+ \";\" #missing / \"(\" [a-z]* \")\" #missing",
			Inputs: []string{"a;bc;", "a;b", "ab(c", "(a)b(", "a(b;", ""}},
		{Name: "line anchors", Src: "rule main = (^ [a-z]+ $ \"\\n\"?)* $$",
			Inputs: []string{"ab\ncd", "ab\n", "ab cd", "\n\n"}},
		{Name: "a line end before a multibyte character", Src: "rule main = (\"na\" $ \"\\n\"?)* $$",
			Inputs: []string{"naé", "na\nnaé", "na\n"}},
		{Name: "a cut in a choice", Src: "rule main = x* $$\nrule x = \"a\" #cut \"b\" / \"ac\" / \"d\"",
			Inputs: []string{"ab", "ac", "abac", "d", "abd"}},
		{Name: "ignorecase over an inline action",
			Src:    "rule main = ((\"ab\" c => 1) #ignorecase \"d\")* $$\nrule c = \"c\"",
			Inputs: []string{"ABcd", "abcd", "ABCd", "AbcdaBcd", "ABcD"}},
		{Name: "an iteration that makes no capture",
			Src:    "rule main = xs:(n:\"a\" / -\"b\")* $$ => len(map($xs, (e) => e.$n))",
			Inputs: []string{"ab", "ba", "b", "aab", ""}},
		{Name: "the message of an inserted token",
			Src:    "rule main = [a\\né]* x:(\";\" #missing) { $x.(missing).message == \"2:2: syntax error\" } \"b\" $$",
			Inputs: []string{"\nab", "a\nab", "ab", "\néb", "\n;b"}},
		{Name: "lookaheads with token insertion", Src: "rule main = !(\"x\" #missing) \"y\" #missing &(\"z\" #missing) $$",
			Inputs: []string{"", "y", "z", "yz", "x"}},
		{Name: "decoding ends the examined range", Src: "rule main = [a-z]* $$",
			Inputs: []string{"\xe2\x82A", "a\xe2\x82", "\xf0\x9f\x98A", "\xc3A"}},
		{Name: "struct values and casts in predicates", Src: "type A = { N int; S string }\ntype B = { S string }\ntype U ~ A | B\nvar z int\n" +
			"rule main = item* $$\n" +
			"rule item = d:dyn { $d.(A) != nil } \"!\" / d:dyn { $d.(A).S == \"\" && $d.(A).N == 1 } \"&\" / d:dyn { $d.(U) != nil } \"?\" / \"/\" div\n" +
			"rule dyn _ = (\"x\" => A{N: 1}) / (\"y\" => B{S: \"s\"})\nrule div int = [0-9] => 1 / z",
			Inputs: []string{"x!", "y?", "x?", "y!", "x&", "y&", "x!y?x&", "/1", "x!/1"}},
		{Name: "named node conversions", Src: "type N = node[string]\nrule main = (x:n { $x.(N) != nil } \";\")* $$\nrule n = t:[a-z] => N{$t}",
			Inputs: []string{"a;", "a;b;", "a"}},
		{Name: "Operators Rule actions", Src: "rule main = (e:expr { $e == 3 } \"!\" / expr \"?\") $$\nrule expr int = #operators {\n" +
			" operand d:[0-9] => len(text($d))\n level { infix left \"+\" => $lhs + $rhs }\n level { prefix \"-\" => 0 - $rhs }\n}",
			Inputs: []string{"1+1+1!", "1+1!", "1+1?", "-1+1+1+1+1!", "-1!"}},
		{Name: "lambdas over a captured repetition", Src: "rule main = rs:(r:[a-z] \";\")* { len(filter(map($rs, (e) => e.$r), (c) => text(c) == \"a\")) == 1 } $$",
			Inputs: []string{"a;b;", "a;a;", "b;", ""}},
		{Name: "literal choices in bytes", Src: "rule main = atom* $$\nrule atom = \"-x\" / \"a\" / \"é\"",
			Inputs: []string{"¬", "\xc2", "a¬", "é", "-x"}},
		// The lookaheads walk ws from starts one and two characters after the start of a run, below the
		// end of the walks before them, so its scan site turns hot inside lookaheads, where a Document
		// parse keeps scan sites (outside them a Document resumes its runs instead), and a walk of
		// scanHotSpan characters or more measures what the rest of it examines for its record. The runs
		// are longer than the span, and their last characters are multibyte or cut by the end of the
		// input.
		{Name: "walks of a hot scan site in lookaheads",
			Src: "rule main = (!(ws \"a\") !(\" \" ws \"a\") !(\"  \" ws \"a\") ws [bc])* $$\nrule ws = [ é]*",
			Inputs: []string{
				strings.Repeat(" ", 20) + "c" + strings.Repeat(" ", 20) + "b",
				strings.Repeat(" ", 18) + "éé" + "c" + strings.Repeat(" ", 19) + "é" + "b",
				strings.Repeat(" ", 20) + "c" + strings.Repeat(" ", 20) + "\xc3",
				strings.Repeat(" ", 20) + "b" + strings.Repeat(" ", 17) + "\xe2\x82",
				strings.Repeat(" ", 20) + "c" + strings.Repeat(" ", 20) + "a",
			}},
	}
}
