package desugar_test

import (
	"context"
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestNormalizationContextParity(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`rule main = ("a" #repeat(1, 3) #atomic @error("as")) "b" $$`,
		`rule main = ("a" #atomic ";") #recover(skip=([^;]+ ";")) $$`,
		`var x int
rule main = {x := (1); (x == 1)} "a"`,
		`type T = { Text (string) }
type Alias ~ (T)
rule main (Alias) = n:([0-9]+) => (T{Text: (text($n))})`,
		`rule main = #operators { skip " "*; operand [0-9]+; operand "(" main ")"; level { infix left "+" / "-" } }`,
		`rule main = "a" @error(nil, "bad")`,
		`rule main = "a" @error("a", message="b")`,
		`rule main = "a" @atomic`,
		`rule main = "a" #repeat(3, 1)`,
	} {
		t.Run(source, func(t *testing.T) {
			g, err := enginetest.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			before, err := ast.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			for _, eager := range []bool{false, true} {
				ctx, stop := context.WithCancel(context.Background())
				defer stop()
				got, gotErr := desugar.NormalizeAttributesControlled(g, eager, &cancel.Control{Ctx: ctx})
				want, wantErr := desugar.NormalizeAttributesWithCopies(g, eager)
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
					t.Fatalf("context changed normalized result: %v / %v", gotErr, wantErr)
				}
				after, err := ast.MarshalJSON(g)
				if err != nil || string(after) != string(before) {
					t.Fatalf("normalization changed source: %v", err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, gotErr := entry.CompileContext(ctx, g, entry.Options{})
			want, wantErr := entry.Compile(g, entry.Options{})
			if !reflect.DeepEqual(gotErr, wantErr) {
				t.Fatalf("compile diagnostics differ: %v / %v", gotErr, wantErr)
			}
			if gotErr == nil && codegentest.ResultJSON(got.Parse("main", "a")) != codegentest.ResultJSON(want.Parse("main", "a")) {
				t.Fatal("context changed compiled behavior")
			}
		})
	}
}
