package print

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// tsMethods returns the printed methods of the TypeScript library for every feature, from its
// compositions specialized by the pass specialize.
func tsMethods() ([]tsMethod, error) {
	rs, err := LibraryRequests(common.FeatureSet(common.AllFeatures))
	if err != nil {
		return nil, err
	}
	lib, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(codepass.Specialization),
		&library.Library{Module: compose.Build(), Requests: rs})
	if err != nil {
		return nil, err
	}
	return tsMethodsFor(lib)
}

// TestSubstituteMethods checks that the printed compositions replace the hand-written methods of
// the library, with the comment lines before them, and nothing else.
func TestSubstituteMethods(t *testing.T) {
	ms, err := tsMethods()
	if err != nil {
		t.Fatal(err)
	}
	got := substituteMethods(readLib(t, tsLibFileName), ms)
	want := readLib(t, tsLibFileName)
	nested := 0
	for _, m := range ms {
		// A method of a class that a layer of the parser declares in its function is indented one
		// level more, with its printed text.
		indent, text := "  ", m.Text
		if !strings.Contains(want, "\n  "+m.Name+"(") && strings.Contains(want, "\n    "+m.Name+"(") {
			indent = "    "
			text = "  " + strings.ReplaceAll(strings.TrimSuffix(m.Text, "\n"), "\n", "\n  ") + "\n"
			text = strings.ReplaceAll(text, "\n  \n", "\n\n")
			nested++
		}
		if strings.Count(got, text) != 1 {
			t.Fatalf("the printed method %s is not in the library once", m.Name)
		}
		got = strings.Replace(got, text, "<"+m.Name+">", 1)
		// The hand-written method: its comment lines, its first line and its closing brace.
		i := strings.Index(want, "\n"+indent+m.Name+"(") + 1
		start := i
		for {
			j := strings.LastIndex(want[:start-1], "\n") + 1
			if !strings.HasPrefix(want[j:], indent+"// ") {
				break
			}
			start = j
		}
		end := i + strings.Index(want[i:], "\n"+indent+"}\n") + len("\n"+indent+"}\n")
		want = want[:start] + "<" + m.Name + ">" + want[end:]
	}
	if nested == 0 {
		t.Error("no printed method belongs to a class of a layer of the parser")
	}
	if got != want {
		t.Error("the substitution changed the library outside the replaced methods")
	}
	if strings.Contains(got, "  callBody(") || strings.Contains(got, "  grow(") {
		t.Error("a hand-written method is left")
	}
}

// TestSubstituteMethodsRefusesMalformedLib checks that a library without the method, with it
// twice, or without its closing brace at the class body's indentation stops the substitution.
func TestSubstituteMethodsRefusesMalformedLib(t *testing.T) {
	m := []tsMethod{{Name: "grow", Text: "  grow() {}\n"}}
	for name, src := range map[string]string{
		"missing":          "class P {\n  other() {\n  }\n}\n",
		"twice":            "class P {\n  grow() {\n  }\n  grow() {\n  }\n}\n",
		"no closing brace": "class P {\n  grow() {\n    return 1;\n}\n",
		"twice, nested":    "class P {\n  grow() {\n  }\n}\nfunction f() {\n  class Q {\n    grow() {\n    }\n  }\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			substituteMethods(src, m)
		})
	}
}

// TestPrintedMethodsHaveNoConstantTest checks that a condition the TypeScript library prints as a
// constant leaves no test in the printed methods.
func TestPrintedMethodsHaveNoConstantTest(t *testing.T) {
	ms, err := tsMethods()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		for _, c := range []string{"if (true)", "if (false)"} {
			if strings.Contains(m.Text, c) {
				t.Errorf("method %s has %q:\n%s", m.Name, c, m.Text)
			}
		}
	}
}

// TestPrintedMethodsVerify checks that the functions verify for every specialization the
// TypeScript library prints, and that each printed method defines its name.
func TestPrintedMethodsVerify(t *testing.T) {
	ms, err := tsMethods()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ms {
		if !strings.Contains(x.Text, x.Name+"(") {
			t.Errorf("method %s prints as\n%s", x.Name, x.Text)
		}
	}
}
