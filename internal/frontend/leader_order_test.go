package frontend_test

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/middleend/corepass"
)

// TestLeaderDeclarationOrder pins that the frontend numbers the rules in declaration order and that
// the leaders of overlapping cycles follow that order. Rules a and c are declared first and last,
// and b is declared in the middle: the cycles a-b and b-c pass through b, so the leaders are a
// (first on the cycle a-b) and b (first on the cycle b-c, which a does not pass through).
func TestLeaderDeclarationOrder(t *testing.T) {
	src := "@start\n" +
		"rule main = a $$\n" +
		"rule a = b \"x\" / \"y\"\n" +
		"rule b = a \"z\" / c \"u\" / \"w\"\n" +
		"rule c = b \"v\" / \"t\"\n"
	g, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	_, st, err := frontend.CheckStructural(g, check.Options{}, nil, desugar.InlineNames{}, checkfacts.Source{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for i, r := range st.Module.Rules {
		if r.ID != i {
			t.Errorf("rule %s has ID %d at position %d, want the declaration position", r.Name, r.ID, i)
		}
		names = append(names, r.Name)
	}
	if want := []string{"main", "a", "b", "c"}; !slices.Equal(names, want) {
		t.Fatalf("rules %v, want declaration order %v", names, want)
	}
	var leaders []string
	for i, r := range corepass.FactsOf(st.Analyses, st.Module).Rules {
		if r.Leader {
			leaders = append(leaders, st.Module.Rules[i].Name)
		}
	}
	if want := []string{"a", "b"}; !slices.Equal(leaders, want) {
		t.Errorf("leaders %v, want %v", leaders, want)
	}
}
