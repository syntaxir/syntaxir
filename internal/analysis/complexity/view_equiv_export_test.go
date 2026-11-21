package complexity

// A LawCase is a case of TestNormalView in which a law rewrites rule main, for the equivalence
// tests of package complexity_test: the grammar, the law and the options of the view.
type LawCase struct {
	Name string
	Law  Law
	Src  string
	Opts Options
}

// LawCases returns the cases of TestNormalView in which a law applies.
func LawCases() []LawCase {
	var out []LawCase
	for _, c := range normalViewCases {
		if c.rule != "" {
			out = append(out, LawCase{c.name, c.rule, "rule main = " + c.body + normalViewHelpers, c.opts})
		}
	}
	return out
}
