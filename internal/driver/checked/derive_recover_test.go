package checked

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/parser"
)

// A program derives its recognizer and insertion program from the grammar as given: the normalized
// grammar holds attributes that only the checker writes, such as #recover with its skip
// expression, which grammar validation rejects.
func TestDerivedProgramsOfARecoveringGrammar(t *testing.T) {
	t.Parallel()
	g, err := parser.Parse(`@start
rule main = stmt* $$
rule stmt = s:("let" " " name ";") #recover(skip=[^;]* ";") => $s
rule name = [a-z]+ #atomic #ignorecase
`)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Compile(g, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recognizer(); err != nil {
		t.Errorf("recognizer: %v", err)
	}
	ins, err := s.Insertion()
	if err != nil {
		t.Fatalf("insertion program: %v", err)
	}
	if _, err := ins.(*Source).Recognizer(); err != nil {
		t.Errorf("recognizer of the insertion program: %v", err)
	}
}
