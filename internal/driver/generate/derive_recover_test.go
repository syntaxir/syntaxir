package generate_test

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestGeneratorDerivesFromRecoveringGrammar derives every program of a grammar with #recover and
// #ignorecase for the generators. The derived programs check the grammar as given: the normalized
// grammar holds attributes that only the checker writes, such as #recover with its skip
// expression, which grammar validation rejects.
func TestGeneratorDerivesFromRecoveringGrammar(t *testing.T) {
	g, err := compiletest.Parse(`@start
rule main = stmt* $$
rule stmt = s:("let" " " name ";") #recover(skip=[^;]* ";") => $s
rule name = [a-z]+ #atomic #ignorecase
`)
	if err != nil {
		t.Fatal(err)
	}
	_, derive, err := driver.CheckForGenerator(context.Background(), g, check.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []driver.Role{driver.Recognizer, driver.Insertion, driver.InsertionRecognizer} {
		if _, err := derive(role); err != nil {
			t.Errorf("%s: %v", role, err)
		}
	}
}
