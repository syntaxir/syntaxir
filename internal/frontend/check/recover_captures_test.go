package check

import (
	"strings"
	"testing"
)

// TestRecoveredCaptureTypes checks the types of captures inside an expression that #recover recovers
// (spec/language/type-checking.md, Types of Captures): such a capture is not made when the expression
// fails and is recovered, so it is optional, *T, as inside an optional expression, and reading it as
// always set is a compile-time error. A capture of the recovery itself is always made, and a name
// captured again after the recovery is set when that capture is always made.
func TestRecoveredCaptureTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule a = (x:"a" "b") #recover(skip="z") => $x`, `*node[string]`},
		{`rule a = (x:"a" "b") #recover(skip="z") => $x == nil`, `bool`},
		// The pragma binds tighter than the capture: x captures the recovery.
		{`rule a = x:("a" "b") #recover(skip="z") => $x.Start`, `int`},
		{`rule a = x:("a" "b") #recover(skip="z") => $x`, `node[(node[string], node[string])] | error`},
		// The join is without nil when one capture of the name is always made.
		{`rule a = (x:"a" "b") #recover(skip="z") x:"c" => $x`, `node[string]`},
		{`rule a = (x:"a" "b") #recover(skip="z") (x:"c")? => $x`, `*node[string]`},
		// Nested recoveries and one inside a capture.
		{`rule a = ((x:"a" "b") #recover(skip="y") "c") #recover(skip="z") => $x`, `*node[string]`},
		{`rule a = y:((x:"a" "b") #recover(skip="z") "c") => $y.$x`, `*node[string]`},
		// A positive lookahead keeps its captures beside the tuple; recovered, they may be unset too.
		{`rule a = (&(x:"a") "a") #recover(skip="z") => $x`, `*node[string]`},
		// #missing inserts a literal, which makes no capture inside it: a capture of it stays set.
		{`rule a = x:"a" #missing => $x`, `node[string] | missing`},
	} {
		types, errs := valueTypesOf(t, tc.src)
		if len(errs) > 0 {
			t.Errorf("%q: %v", tc.src, errs)
		}
		if got := types["a"].String(); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.src, got, tc.want)
		}
	}
	for _, tc := range []struct{ src, want string }{
		{"@start\nrule r = (x:\"a\" \"b\") #recover(skip=\"z\") => $x.Start", "cannot access .Start of *node[string]: the value may be nil"},
		{"@start\nrule r int = (x:\"a\" \"b\") #recover(skip=\"z\") => $x.Start", "cannot access .Start of *node[string]: the value may be nil"},
		{"@start\nrule r node[string] = (x:\"a\" \"b\") #recover(skip=\"z\") => $x", "rule r produces *node[string], which is not assignable to node[string]"},
		{"@start\nrule r = (x:\"a\" \"b\") #recover(skip=\"z\") (x:\"c\")? => $x.Start", "cannot access .Start of *node[string]: the value may be nil"},
	} {
		if got := compileMessage(t, tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.src, got, tc.want)
		}
	}
	for _, src := range []string{
		"@start\nrule r = x:(\"a\" \"b\") #recover(skip=\"z\") => $x.Start",
		"@start\nrule r int = (x:\"a\" \"b\") #recover(skip=\"z\") x:\"c\" => $x.Start",
	} {
		if got := compileMessage(t, src); got != "" {
			t.Errorf("%q: %s", src, got)
		}
	}
}
