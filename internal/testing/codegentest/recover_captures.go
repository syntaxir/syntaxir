package codegentest

// RecoveredCaptureCases are grammars with a capture inside an expression that #recover recovers:
// the capture is not made when the expression fails and is recovered, also when part of the
// expression matched before it failed, so it is nil, and its type is optional, as in an optional
// expression (spec/language/type-checking.md, Types of Captures). A capture of the recovery
// itself is always made, and so is a name captured again after the recovery. The skip matches
// "z" and "az", so that the recovery of "az" follows a capture of "a" that the failure undoes.
var RecoveredCaptureCases = []RecoverCase{
	{Case{"recovered capture/unset after a recovery", `rule main = (x:"a" "b") #recover(skip="a"? "z") {$x == nil} $$`, []string{"ab", "z", "az"}}, []bool{false, true, true}},
	{Case{"recovered capture/set without a recovery", `rule main = (x:"a" "b") #recover(skip="a"? "z") {$x != nil} $$`, []string{"ab", "az"}}, []bool{true, false}},
	{Case{"recovered capture/capture of the recovery", `rule main = x:("a" "b") #recover(skip="a"? "z") {$x.Start == 0} $$`, []string{"ab", "az", "z"}}, []bool{true, true, true}},
	{Case{"recovered capture/captured again after the recovery", `rule main = (x:"a" "b") #recover(skip="a"? "z") x:"c" {text($x) == "c"} $$`, []string{"abc", "azc", "zc"}}, []bool{true, true, true}},
	{Case{"recovered capture/captured again in an optional", `rule main = (x:"a" "b") #recover(skip="a"? "z") (x:"c")? {$x == nil} $$`, []string{"ab", "az", "abc", "azc"}}, []bool{false, true, false, false}},
}
