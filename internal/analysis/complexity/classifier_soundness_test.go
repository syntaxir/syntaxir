package complexity

import "testing"

// classifierSoundnessCases pins the class of the grammars that the soundness review of the cost
// classifier found: reads of variables that hold values of calls, growing units led by predicates
// and calls that match no input, growths that a unit throws away, and units that a lookahead
// decides. Degree -1 leaves the degree open; growing -1 leaves the growing-variable bindings open.
var classifierSoundnessCases = []struct {
	group, name, src string
	class            ComplexityClass
	degree, growing  int
}{
	{"variables", "copy of a read of a call value, doubling", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = ("(" c:r ")" {x := $c} => x + x) / ("a" => "a")
`, ClassExp, -1, -1}, // p1
	{"variables", "x + \"b\" over a call value", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = ("(" c:r ")" {x := $c} => x + "b") / ("a" => "a")
`, ClassPoly, 2, -1}, // p2
	{"variables", "$c + \"b\" control", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = ("(" c:r ")" {x := $c} => $c + "b") / ("a" => "a")
`, ClassPoly, 2, -1}, // p3
	{"variables", "copy in a rule called after the binding", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = ("(" c:r ")" {x := $c} v:t => $v) / ("a" => "a")
rule t string = "" => x + x
`, ClassExp, -1, -1}, // p4
	{"variables", "x + \"b\" in a rule called after the binding", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = ("(" c:r ")" {x := $c} v:t => $v) / ("a" => "a")
rule t string = "" => x + "b"
`, ClassPoly, 2, -1}, // p5
	{"variables", "read without a copy", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = ("(" c:r ")" {x := $c} => x) / ("a" => "a")
`, ClassLinearEnv, -1, -1}, // p6
	{"variables", "doubling in a positive lookahead", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = (&({x := x + x}) "y" v:r => $v) / ("z" => x)
`, ClassExp, -1, 1}, // p7
	{"variables", "extension in a positive lookahead", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = (&({x := x + "a"}) "y" v:r => $v) / ("z" => x)
`, ClassExp, -1, -1}, // p8
	{"variables", "extension before the call control", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = ({x := x + "a"} "y" v:r => $v) / ("z" => x)
`, ClassExp, -1, -1}, // p9
	{"variables", "doubling in a discard control", `var x string = "q"
@start
rule s string = v:r $$ => $v
rule r string = (-({x := x + x} "y") v:r => $v) / ("z" => x)
`, ClassExp, -1, -1}, // p10
	{"variables", "bounded chain without recursion", `var x string = "q"
@start
rule s = a:A {x := $a} A $$
rule A string = {x := x + x} => x
`, ClassLinearEnv, -1, -1}, // p11
	// The call r is under a negative lookahead, but it sees the binding before the lookahead and
	// evaluates the binding again: on "(" n times and "a" the value of x doubles n times, and the
	// parse of 24 "(" exceeds a compute limit of 2^20 that 16 stay within.
	{"variables", "doubling before a call in a negative lookahead", `var x string = "q"
@start
rule s = r $$
rule r = {x := x + x} "(" !(r "]") ")" / "a"
`, ClassExp, -1, -1}, // neg1
	{"growing", "predicate before the left call (off-cycle empty call)", `@start
rule s = E $$
rule E = Z E R "!" / E "+" "x" / "x"
rule Z = ""
rule R = [ ]*
`, ClassLinear, 0, -1}, // p12
	{"growing", "predicate before the left call", `@start
rule s = E $$
rule E = {true} E R "!" / E "+" "x" / "x"
rule Z = ""
rule R = [ ]*
`, ClassLinear, 0, -1}, // p13
	{"growing", "growing unit without predicate control", `@start
rule s = E $$
rule E = E R "!" / E "+" "x" / "x"
rule Z = ""
rule R = [ ]*
`, ClassLinear, 0, -1}, // p14
	{"growing", "alternative dropped before a growing one, dropped result regrown", `@start
rule main = S $$
rule S = R "z" / S "q" / "y"
rule R = R "q" / S
`, ClassUnknown, 2, -1}, // p15
	{"growing", "dropped result behind a predicate", `@start
rule main = S $$
rule S = R "z" / {true} S "q" / "y"
rule R = R "q" / S
`, ClassUnknown, 2, -1}, // p16
	{"growing", "left call that fails after a path through a call", `@start
rule s = E $$
rule E = F "+" "x" / E [a-z] / "a"
rule F = E "z" / "a" [a-z]* "?"
`, ClassUnknown, -1, -1}, // p17
	{"growing", "left call that fails after a path through a group", `@start
rule s = E $$
rule E = (E "z" / "a" [a-z]* "?") "+" "x" / E [a-z] / "a"
`, ClassUnknown, -1, -1}, // p18
	{"growing", "dropped result, last alternative", `@start
rule main = S $$
rule S = R "z" / S "q"
rule R = R "q" / S / "y"
`, ClassUnknown, 2, -1}, // c5last
	{"growing", "dropped result behind an option", `@start
rule main = S $$
rule S = R "z" / "w"? S "q" / "y"
rule R = R "q" / S
`, ClassUnknown, 2, -1}, // c5w
	{"growing", "lookahead of the cycle itself", `@start
rule s = E $$
rule E = &E "A" [a-z]* "?" / E [a-z] / "A"
`, ClassUnknown, -1, -1}, // pla
	{"growing", "lookahead of the cycle itself in a group", `@start
rule s = E $$
rule E = &(E) "A" [a-z]* "?" / E [a-z] / "A"
`, ClassUnknown, -1, -1}, // pla2
	{"growing", "lookahead scan over the left call", `@start
rule s = E $$
rule E = &("A" [a-z]*) E [a-z] / "A"
`, ClassUnknown, 2, -1}, // la1
	{"growing", "empty call before a scan", `@start
rule s = E $$
rule E = Z "a" [a-z]* "?" / Z E [a-z] / "a"
rule Z = ""
`, ClassUnknown, 2, -1}, // zs1
	{"growing", "shared scan after the growing alternative", `@start
rule s = E $$
rule E = "A" W "!" [a-z]* "?" / E "y" / "A" W "x"
rule W = [ ]*
`, ClassPoly, 2, -1}, // dw1
	{"thrown", "growth thrown away in the step of another leader (main repeats S)", `@start
rule main = S* $$
rule S = R "z" / S "q" / "a"
rule R = R [ac] / S
`, ClassUnknown, 2, -1}, // n1
	{"thrown", "growth thrown away in the step of another leader", `@start
rule main = (S "b")* $$
rule S = R "z" / S "q" / "a"
rule R = R [ab] / S "b"
`, ClassUnknown, -1, -1}, // n2
	{"thrown", "growth thrown away by a rule that is not a leader", `@start
rule main = (S "b")* $$
rule S = R "z" / "a"
rule R = R [ab] / S "b"
`, ClassUnknown, -1, -1}, // n3
	{"thrown", "growth of a leader that the other leader reads", `@start
rule main = S $$
rule S = R "z" / S "q" / R
rule R = R "b" / S "y" / "c"
`, ClassUnknown, -1, -1}, // s4b
	{"thrown", "cut after a left call makes a later step take an alternative the first did not reach", `@start
rule main = S $$
rule S = S "w" / R "z" / "c" Bs
rule Bs = "b" Bs / ""
rule R = (R #cut "b" / S "y" / "c") / S "x"
`, ClassUnknown, -1, -1}, // s4d
	// Declaration-order leaders make x a leader of the cycle with r, which the classifier does not
	// analyze yet. The class falls back to Unknown (sound) instead of Polynomial 2. Restore ClassPoly
	// and the early restart detection with #314 (Classifier: keep ClassPoly and early restarts when
	// declaration-order leaders add a leader to a cycle).
	{"shared calls", "a shared call of a cycle rule lies in the growths of the unit, not of the continuation", `@start
rule top = item* $$
rule item = x "!" / r "?" / "."
rule x = r "a"
rule r = r "c" / x "d" / "e"
`, ClassUnknown, -1, -1}, // w2: the spec does not share r (it is reached through x); no super-linear input is known
	// The call S after "c" lies after leaving the body of S, outside the growth of S that the
	// unit sees, so it is not shared and the alternative is not committing. Without the record of
	// the bodies a path has left, the grammar is classified linear.
	{"shared calls", "a call of a cycle rule after leaving the body of a rule of the cycle is not shared", `@start
rule main = S $$
rule S = S "c" S / "x"
`, ClassUnknown, -1, -1}, // reduced from seed 1655
	{"thrown", "nested reads", `@start
rule main = S $$
rule S = S "q" / R "s" / "y"
rule R = R "r" / S "t"
`, ClassUnknown, -1, -1}, // t1
	{"thrown", "lookahead regrowth", `@start
rule main = S $$
rule S = &R S "q" / "y"
rule R = R ("q" / "r") / S "t" / "y"
`, ClassUnknown, -1, -1}, // t2
	{"thrown", "three leaders", `@start
rule main = S $$
rule S = &R S "q" / "y"
rule R = &T R "q" / "y"
rule T = T "q" / S "z" / "y"
`, ClassUnknown, -1, -1}, // t3
	{"thrown", "tree carried after the growing alternative", `@start
rule main = S $$
rule S = S "q" / R "!" / "y"
rule R = R "q" / S
`, ClassUnknown, -1, -1}, // t4
	{"decided", "deciding negative lookahead with a scan", `@start
rule main = ( !(a ([a-z] #cut)* "!" / a) a ";" / a )* $$
rule a = "a"
`, ClassPoly, 2, -1}, // k8
	{"decided", "deciding negative lookahead of a rule with a scan", `@start
rule main = ( !k a ";" / a )* $$
rule k = a ([a-z] #cut)* "!" / a
rule a = "a"
`, ClassPoly, 2, -1}, // m1
	{"decided", "deciding negative lookahead of a rule that calls a memoized scan", `@start
rule main = ( !k a ";" / a )* $$
rule k = a s "!" / a
rule s = ([a-z] #cut)*
rule a = "a"
`, ClassPoly, 2, -1}, // m2
	{"decided", "decisive call that expands to a deciding negative lookahead", `@start
rule main = ( c "x" / "q" )* $$
rule c = !("q" s) "b"
rule s = ([q] #cut)*
`, ClassPoly, 2, -1}, // c5
	{"decided", "passed positive lookahead starting with a scanning negative lookahead", `@start
rule main = ( &k "x" / "q" )* $$
rule k = !("q" s) "b"
rule s = ([q] #cut)*
`, ClassPoly, 2, -1}, // c6
	{"decided", "passed positive lookahead of a rule with a scan", `@start
rule main = ( &k "x" / "q" )* $$
rule k = "q" s "!" / "q"
rule s = ([q] #cut)*
`, ClassPoly, 2, -1}, // c10
	{"decided", "passed positive lookahead of a memoized scanning call", `@start
rule main = ( &(id "=") "Q" id "=" / [a-z] )* $$
rule id = [a-z] ([a-z] #cut)*
`, ClassPoly, 2, -1}, // c13
	{"decided", "deciding negative lookahead without a scan", `@start
rule main = ( !(a "!" / a) a ";" / a )* $$
rule a = "a"
`, ClassLinear, 0, -1}, // m3
	{"decided", "passed positive lookahead, continuation repeats the call", `@start
rule main = ( &(id "=") id "=" / id ":" )* $$
rule id = [a-z] ([a-z] #cut)*
`, ClassLinear, 0, -1}, // c12
	{"decided", "restart through another alternative", `@start
rule main = ( !(a ([a-z] #cut)* "!" / a) a ";" / a / "q" a )* $$
rule a = "a"
`, ClassPoly, 2, -1}, // k9
	{"decided", "restart through a choice", `@start
rule main = ( (k / a) ";" / a )* $$
rule k = a s "!"
rule s = ([a-z] #cut)*
rule a = "a"
`, ClassPoly, 2, -1}, // c8
	{"decided", "undecided restart", `@start
rule main = ( &(id "=") id "=" / [a-z] )* $$
rule id = [a-z] ([a-z] #cut)*
`, ClassPoly, 2, -1}, // c11
	{"decided", "pair of constant lookaheads", `@start
rule main = ( "x" &"a" [a-z] ([a-z] #cut)* "!" / "x" &"b" [a-z] )* $$
rule a = "a"
`, ClassLinear, 0, -1}, // p1
	{"decided", "pair of lookaheads with a class", `@start
rule main = ( "x" &"a" [a-z] ([a-z] #cut)* "!" / "x" &[ab] [a-z] )* $$
rule a = "a"
`, ClassPoly, 2, -1}, // p2
	{"decided", "continuation lookahead that scans, deciding against a unit lookahead", `@start
rule main = ( &"a" [a-z] ";" / &k [a-z] )* $$
rule k = "b" s "!" / "b"
rule s = ([b] #cut)*
`, ClassPoly, 2, -1}, // f2a
	{"decided", "unit lookahead against a continuation negative lookahead", `@start
rule main = ( &"a" [a-z] ";" / !k "b" )* $$
rule k = "b" s "!"
rule s = ([b] #cut)*
`, ClassPoly, 2, -1}, // f2b
	{"decided", "case-insensitive literal against a case-sensitive one", `@start
rule main = ( "ab"i s "!" / "AC" )* $$
rule s = ([a-zA-Z] #cut)*
`, ClassPoly, 2, -1}, // f3a
	{"decided", "case-insensitive literals that differ", `@start
rule main = ( "ab"i s "!" / "AX"i )* $$
rule s = ([a-zA-Z] #cut)*
`, ClassLinear, 0, -1}, // f3b
	{"decided", "alike bounded repetitions", `@start
rule main = ( "x" [a-z] #repeat(,3) "1" (!";" .)* "!" / "x" [a-z] #repeat(,3) "2" (!";" .)* ";" )* $$
`, ClassLinear, 0, -1}, // r1
	{"decided", "alike bounded repetitions with a predicate", `@start
rule main = ( "x" ([a-z] {1 == 1}) #repeat(,3) "1" (!";" .)* "!" / "x" ([a-z] {1 == 1}) #repeat(,3) "2" (!";" .)* ";" )* $$
`, ClassPoly, 2, -1}, // r2
	// The first alternative of E never succeeds, since [a-z]* consumes the c, yet the normal view
	// keeps it: its path is still its symbols. Each step of the growth of E scans W again before
	// the alternative fails at &"b": on a (ab)^k y x^k the minimum compute is 321795,
	// 1268545 and 5037045 for k = 250, 500 and 1000.
	{"decided", "unit that never succeeds without a dead symbol", `@start
rule s = E $$
rule E = "a" W &"b" [a-z]* "c" / E "x" / "a" W "y"
rule W = ("a" #cut "b")*
`, ClassPoly, 2, -1}, // g1
	// An operator part reads the value of an earlier application through $lhs, as a call on a
	// recursion would be read: joining it twice doubles the value at each operator. On "a" and k
	// times "+a" the minimum compute is 634, 8370 and 131306 for k = 8, 12 and 16.
	{"operators", "operand value joined twice", `@start
rule main = e:expr $$ => $e
rule expr string = #operators { operand a:"a" => text($a); level additive { infix left "+" => $lhs + $lhs } }
`, ClassExp, -1, -1},
	// The same value bound to a variable: the output is 259, 4099 and 65539 bytes for k = 8, 12
	// and 16.
	{"operators", "operand value joined twice into a variable", `var x string
@start
rule main = e:expr {x := $e} $$ => x
rule expr string = #operators { operand a:"a" => text($a); level additive { infix left "+" => $lhs + $lhs } }
`, ClassExp, -1, -1},
	// Joining both operands copies the left operand, which grows by one operand at each
	// operator: the minimum compute is 1987, 6462, 22912 and 85812 for 50, 100, 200 and 400
	// operators.
	{"operators", "operand values joined once", `@start
rule main = e:expr $$ => $e
rule expr string = #operators { operand a:"a" => text($a); level additive { infix left "+" => $lhs + $rhs } }
`, ClassPoly, 2, -1},
	// The same doubling of a list: on "a" and k times "+a" the minimum compute is 2216, 33016 and
	// 524616 for k = 8, 12 and 16.
	{"operators", "operand list joined twice", `@start
rule main = e:expr $$ => len($e)
rule expr []string = #operators { operand a:"a" => list(text($a)); level additive { infix left "+" => concat($lhs, $lhs) } }
`, ClassExp, -1, -1},
	// Joining both operand lists copies the left one at each operator: the minimum compute is
	// 6266, 22516, 85016 and 330016 for 50, 100, 200 and 400 operators.
	{"operators", "operand lists joined once", `@start
rule main = e:expr $$ => len($e)
rule expr []string = #operators { operand a:"a" => list(text($a)); level additive { infix left "+" => concat($lhs, $rhs) } }
`, ClassPoly, 2, -1},
	// The whole-rule action reads the folded value through $value, which holds the value of the
	// call of the parenthesized operand: joining it twice doubles the value at each level of
	// parentheses. On k parentheses around "a" the minimum compute is 141, 1165 and 16589 for
	// k = 4, 8 and 12.
	{"operators", "folded value joined twice", `@start
rule main = v:e $$ => $v
rule e string = #operators { operand a:"a" => text($a); operand "(" x:e ")" => $x; level additive { infix left "+" => "p" } } => $value + $value
`, ClassExp, -1, -1},
	// Joining it once copies it at each level: the minimum compute is 2141, 6766, 23516 and
	// 87016 for k = 50, 100, 200 and 400.
	{"operators", "folded value joined once", `@start
rule main = v:e $$ => $v
rule e string = #operators { operand a:"a" => text($a); operand "(" x:e ")" => $x; level additive { infix left "+" => "p" } } => $value + "x"
`, ClassPoly, 2, -1},
	// Passing it on copies nothing.
	{"operators", "folded value passed on", `@start
rule main = v:e $$ => $v
rule e string = #operators { operand a:"a" => text($a); operand "(" x:e ")" => $x; level additive { infix left "+" => "p" } } => $value
`, ClassLinear, -1, -1},
	// $lhs in the action of a part holds an earlier application in the same call, not a call on
	// a recursion, so the whole-rule action copies the folded value once per call: the minimum
	// compute is 1215, 2415 and 4815 for 100, 200 and 400 operators.
	{"operators", "folded value of earlier applications joined once", `@start
rule main = v:e $$ => $v
rule e string = #operators { operand a:"a" => text($a); level additive { infix left "+" => $lhs } } => $value + "x"
`, ClassLinear, -1, -1},
	// A parenthesized operand that drops the value of its call holds no call on a recursion:
	// the minimum compute is 1817, 3617 and 7217 for k = 100, 200 and 400.
	{"operators", "folded value without the call joined twice", `@start
rule main = v:e $$ => $v
rule e string = #operators { operand a:"a" => text($a); operand "(" x:e ")" => "b"; level additive { infix left "+" => $lhs } } => $value + $value
`, ClassLinear, -1, -1},
	// An operand that can match nothing lets the operator parts after an operand consume the first
	// character of the expression: `E ";"` fails after its postfix operators scanned the rest of
	// the input, and X starts again inside it. On n times "b" the minimum compute is 5353, 20703
	// and 81403 for n = 100, 200 and 400.
	{"operators", "postfix after an empty operand", `@start
rule main = (E ";" / X)* $$
rule X = [ab]
rule E = #operators { operand ""; level { postfix [ab] } }
`, ClassPoly, 2, -1},
	// The same with an infix operator part: 10303, 40603 and 161203.
	{"operators", "infix after an empty operand", `@start
rule main = (E ";" / X)* $$
rule X = [ab]
rule E = #operators { operand ""; level { infix left [ab] } }
`, ClassPoly, 2, -1},
	// And with an operand that can match nothing but does not here: 5353, 20703 and 81403.
	{"operators", "postfix after an optional operand", `@start
rule main = (E ";" / X)* $$
rule X = [ab]
rule E = #operators { operand "("?; level { postfix [ab] } }
`, ClassPoly, 2, -1},
	// Random search cases (the Operators Rule grammars that were classified Linear and grow as n^2).
	// Measured compute on the input "b" repeated n times and then "b": 3464, 50696 and 792584 for
	// n = 64, 256 and 1024.
	{"operators", "random search: infix after an optional operand, cut", `@start
rule main = (X E ";" / X)* $$
rule D = [ab]
rule X = [ab]
rule E = #operators {
    operand "-"?
    level {
        infix left [ab] ("-" / "a") #cut
        postfix [ab] ("b" / "-")
    }
}
`, ClassPoly, 2, -1},
	// Measured compute on "a" repeated n times and then "!" for n = 64, 256 and 1024: 4293, 66309
	// and 1049601.
	{"operators", "random search: infix after a repeated operand, two levels", `@start
rule main = (X E ";" / X)* $$
rule D = "+"
rule X = [ab]
rule E = #operators {
    operand ")"
    operand ")"*
    level {
        infix none ")"
    }
    level {
        infix left [ab]
    }
}
`, ClassPoly, 2, -1},
	// Measured compute on "a" repeated n times with no tail, for n = 64, 256 and 1024: 1219,
	// 17155 and 265219.
	{"operators", "random search: right-associative infix after a repeated dash operand", `@start
rule main = (D E ";" / D)* $$
rule D = [ab] ("-" / "a")
rule X = "+" "("*
rule E = #operators {
    operand "-"*
    operand "("? ("+" / "(") "+"
    level {
        infix right [ab]
        infix right "-"
    }
}
`, ClassPoly, 2, -1},
	// Measured compute on "aa" repeated n times and then "?" for n = 64, 256 and 1024: 1220, 17156
	// and 265220.
	{"operators", "random search: postfix after a repeated operand, nested", `@start
rule main = (D E ";" / D)* $$
rule D = [ab] [ab]
rule X = "b"*
rule E = #operators {
    operand "("*
    level {
        postfix [ab]
    }
}
`, ClassPoly, 2, -1},
	// Operator parts that do not overlap what follows the empty operand keep the grammar Linear.
	{"operators", "postfix after an empty operand, disjoint from the continuation", `@start
rule main = (E ";" / X)* $$
rule X = [ab]
rule E = #operators { operand ""; level { postfix [cd] } }
`, ClassLinear, -1, -1},
	{"operators", "infix after an empty operand, disjoint from the continuation", `@start
rule main = (E ";" / X)* $$
rule X = [ab]
rule E = #operators { operand ""; level { infix left [cd] } }
`, ClassLinear, -1, -1},
}

// TestClassifierSoundness checks the class of each grammar in classifierSoundnessCases.
func TestClassifierSoundness(t *testing.T) {
	t.Parallel()
	for _, tc := range classifierSoundnessCases {
		t.Run(tc.group+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			c := classifySource(t, tc.src)
			if c.Class != tc.class {
				t.Fatalf("class %v, want %v\ncauses %v\nreasons %v", c.Class, tc.class, c.Causes, c.Reasons)
			}
			if tc.degree >= 0 && classDegree(c) != tc.degree {
				t.Errorf("degree %d, want %d\ncauses %v\nreasons %v", classDegree(c), tc.degree, c.Causes, c.Reasons)
			}
			if tc.growing >= 0 && len(c.Growing) != tc.growing {
				t.Errorf("growing %v, want %d", c.Growing, tc.growing)
			}
		})
	}
}

// classifierSoundnessMutants are the mutation controls of classifierSoundnessCases: each turns off
// one rule of the classifier through its test hook and gives the class that a case then receives,
// which differs from its class with the rule. The cases are named as in classifierSoundnessCases.
var classifierSoundnessMutants = []struct {
	hook  string
	names []string
	class ComplexityClass
}{
	// A read of a variable that holds only the variable's own value hides the call value that the
	// binding binds.
	{"varReads", []string{
		"variables/copy of a read of a call value, doubling",
		"variables/x + \"b\" over a call value",
		"variables/copy in a rule called after the binding",
		"variables/x + \"b\" in a rule called after the binding",
	}, ClassLinearEnv},
	// A positive lookahead that hides its bindings from the calls after it.
	{"lookaheadBinds", []string{
		"variables/doubling in a positive lookahead",
		"variables/extension in a positive lookahead",
	}, ClassLinearEnv},
	// A growing unit that stops at a predicate or at a call that examines no input is checked as
	// an alternative that is not growing, which rejects these linear grammars.
	{"leadingPass", []string{
		"growing/predicate before the left call (off-cycle empty call)",
		"growing/predicate before the left call",
	}, ClassUnknown},
	// A growth that starts at a left call of a leader from another rule is no scanning source, so
	// the unit that throws it away is not checked.
	{"leaderSource", []string{
		"thrown/growth thrown away in the step of another leader",
		"thrown/growth thrown away by a rule that is not a leader",
		"thrown/growth of a leader that the other leader reads",
	}, ClassLinear},
	// A negative lookahead of the unit that reaches a scanning source, directly or in a call, is
	// taken as cheap, so the unit that it decides is accepted as committing.
	{"pnotSource", []string{
		"decided/deciding negative lookahead with a scan",
		"decided/deciding negative lookahead of a rule with a scan",
		"decided/deciding negative lookahead of a rule that calls a memoized scan",
	}, ClassLinear},
	// A positive lookahead that the unit only passes and that reaches a scanning source, directly,
	// in a call, or in a negative lookahead at its start, is taken as cheap.
	{"pandSource", []string{
		"decided/passed positive lookahead starting with a scanning negative lookahead",
		"decided/passed positive lookahead of a rule with a scan",
		"decided/passed positive lookahead of a memoized scanning call",
	}, ClassLinear},
	// $lhs and $rhs of an operator part, and $value of the whole-rule action, counted as values of
	// unknown size only.
	{"operandValues", []string{
		"operators/operand value joined twice",
		"operators/operand values joined once",
		"operators/folded value joined twice",
		"operators/folded value joined once",
		"operators/operand list joined twice",
		"operators/operand lists joined once",
	}, ClassLinear},
	{"operandValues", []string{"operators/operand value joined twice into a variable"}, ClassLinearEnv},
	// The requirements that a lookahead only the continuation evaluates costs a constant are controlled
	// in TestStrictLookaheadMutants: a class does not show them, as the scan is a lookahead-scan cause.
	// Either rule alone rejects the dropped result behind a predicate: without both, condition 5
	// misses the alternative that drops the result and no source is around the call of R.
	{"leadingPass+leaderSource", []string{"growing/dropped result behind a predicate"}, ClassLinear},
}

// setSoundnessHooks sets the test hooks that classifierSoundnessMutants name; the hooks are
// package variables, so the test that sets them does not run in parallel.
func setSoundnessHooks(hook string, on bool) {
	switch hook {
	case "varReads":
		varReadsOn = on
	case "lookaheadBinds":
		lookaheadBindsOn = on
	case "leadingPass":
		leadingPassOn = on
	case "leaderSource":
		leaderSourceOn = on
	case "pnotSource":
		pnotSourceOn = on
	case "pandSource":
		pandSourceOn = on
	case "operandValues":
		operandValuesOn = on
	case "leadingPass+leaderSource":
		leadingPassOn, leaderSourceOn = on, on
	default:
		panic("unknown hook " + hook)
	}
}

// classifyWithout classifies src with the rule of hook turned off, and turns it on again even if
// the classification stops the test, so that the parallel tests that follow see every rule on.
func classifyWithout(t *testing.T, hook, src string) *Complexity {
	t.Helper()
	setSoundnessHooks(hook, false)
	defer setSoundnessHooks(hook, true)
	return classifySource(t, src)
}

// TestClassifierSoundnessMutants checks that each case of classifierSoundnessMutants depends on
// the rule that its hook turns off: with the rule off, the case receives the class of the
// mutant instead of its pinned class.
func TestClassifierSoundnessMutants(t *testing.T) {
	src := map[string]string{}
	pinned := map[string]ComplexityClass{}
	for _, tc := range classifierSoundnessCases {
		src[tc.group+"/"+tc.name] = tc.src
		pinned[tc.group+"/"+tc.name] = tc.class
	}
	for _, m := range classifierSoundnessMutants {
		for _, name := range m.names {
			s, ok := src[name]
			if !ok {
				t.Fatalf("%s: no case %q", m.hook, name)
			}
			if pinned[name] == m.class {
				t.Fatalf("%s: case %q is pinned to the class of the mutant %v", m.hook, name, m.class)
			}
			c := classifyWithout(t, m.hook, s)
			if c.Class != m.class {
				t.Errorf("%s without %s: class %v, want %v\ncauses %v", name, m.hook, c.Class, m.class, c.Causes)
			}
		}
	}
}
