package complexity

import (
	"testing"

	"syntaxir.dev/sxir/internal/middleend/corepass"
)

// calculatorWith returns the calculator of the README with the skip ws and the rule ws given by
// body, whose repetition is a run site: the recursion of main through the operand "(" e:main ")"
// walks it again from many positions.
func calculatorWith(body string) string {
	return `type Num = node[string]
type Bin = { Left Expr; Op node[string]; Right Expr }
type Expr ~ Bin | Num

@start
rule main Expr = #operators {
    skip ws
    operand num
    operand "(" e:main ")" => $e
    level additive { infix left "+" / "-" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level multiplicative { infix left "*" / "/" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level power { infix right "^" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
}

rule num Num = ([0-9]+) #atomic
` + body + "\n"
}

// TestScanOnlyRunSites checks that a rule whose only repetition is a run site free of the
// environment, with an element that repeats nothing without a bound, is a scan-only rule: the
// records of the run site answer the evaluations of ws that each level of the Operators Rule makes
// again at one position, so the calculator is linear. A run site that is not free of the
// environment, one whose element holds a repetition without a bound, and the same grammar
// without run sites stay unknown.
func TestScanOnlyRunSites(t *testing.T) {
	for _, tc := range []struct {
		name, ws string
		linear   bool
	}{
		{"a literal of one character", `rule ws = " "*`, true},
		{"a choice of literals of one character", `rule ws = (" " / "\t")*`, true},
		{"a class, a scan site", `rule ws = [ ]*`, true},
		{"a run site that is not free of the environment", "var n int\nrule ws = (\" \" {n < 3})*", false},
		{"a run site whose element repeats without a bound", `rule ws = (" " "a"*)*`, false},
	} {
		c := classifySource(t, calculatorWith(tc.ws))
		if linear := c.Class == ClassLinear; linear != tc.linear {
			t.Errorf("%s: class %v, want linear %v: %v %v", tc.name, c.Class, tc.linear, c.Reasons, c.Causes)
		}
	}
}

// TestScanOnlyRunSitesMinimum checks that only a run site with minimum 0 makes a rule scan-only. The
// enclosing levels of the Operators Rule try the postfix part again where the inner operand ended,
// and each evaluates the call of sp there with its value: a walk of " "* takes the record of its
// start and copies nothing, while a walk of " "+ takes the record after its first iteration and
// copies the rest of the recorded list behind it, which costs the length of the run at each level.
func TestScanOnlyRunSitesMinimum(t *testing.T) {
	grammar := func(sp string) string {
		return `type Num = node[string]
type Neg = { X Expr }
type Bang = { X Expr; S node[[]node[string]] }
type Expr ~ Neg | Bang | Num

@start
rule main Expr = #operators {
    operand num
    operand "(" e:main ")" => $e
    level un { prefix "-" => Neg{X: $rhs} }
    level post { postfix s:sp "!" => Bang{X: $lhs, S: $s} }
}

rule num Num = ([0-9]+) #atomic
rule sp node[[]node[string]] = ` + sp + "\n"
	}
	for _, tc := range []struct {
		sp     string
		linear bool
	}{
		{`" "*`, true},
		{`" "+`, false},
	} {
		c := classifySource(t, grammar(tc.sp))
		if linear := c.Class == ClassLinear; linear != tc.linear {
			t.Errorf("sp = %s: class %v, want linear %v: %v %v", tc.sp, c.Class, tc.linear, c.Reasons, c.Causes)
		}
	}
}

// TestScanOnlyRunSitesNegativeControl checks that the run site is what makes the calculator with
// ws = " "* linear: compiled and classified without run sites, the same grammar is unknown.
func TestScanOnlyRunSitesNegativeControl(t *testing.T) {
	c := classifySourceWith(t, calculatorWith(`rule ws = " "*`), corepass.SiteSwitches{NoRunSites: true})
	if c.Class != ClassUnknown {
		t.Errorf("class %v without run sites, want unknown", c.Class)
	}
}
