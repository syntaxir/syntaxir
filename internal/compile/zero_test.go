package compile

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestZeroValues holds the rule of definite assignment: a constructor may leave out a field only if
// the declared type of the field has a zero value (spec/language/types.md#zero-values).
func TestZeroValues(t *testing.T) {
	t.Parallel()
	const prelude = `type Inner = { N int; S string }
type Deep = { In Inner; Xs []Inner; Pair (int, string); Opt *node[string] }
type Cycle = { Next Cycle }
type Loop = { Back *Loop; Tag int }
type Choice ~ Inner | Loop
type Kid = node[string]
`
	for _, tc := range []struct {
		name, ftype string
		zero        bool
	}{
		{"int", "int", true},
		{"string", "string", true},
		{"bool", "bool", true},
		{"list", "[]int", true},
		{"list of nodes", "[]node[string]", true},
		{"optional node", "*node[string]", true},
		{"empty list type", "[]_|_", true},
		{"nil type", "*_|_", true},
		{"optional join", "*(Inner | Loop)", true},
		{"top", "_", true},
		{"tuple", "(int, string)", true},
		{"unit", "()", true},
		{"struct with zero fields", "Inner", true},
		{"nested struct", "Deep", true},
		{"struct through an optional cycle", "Loop", true},
		{"node", "node[string]", false},
		{"node of a tuple", "node[(int, int)]", false},
		{"named node type", "Kid", false},
		{"tuple with a node", "(int, node[string])", false},
		{"struct with a node field", "Holder", false},
		{"struct cycle", "Cycle", false},
		{"join", "Inner | Loop", false},
		{"named join", "Choice", false},
		{"error", "error", false},
		{"join with error", "Inner | error", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, err := parseTest(prelude + "type Holder = { K node[string] }\ntype Box = { F " + tc.ftype + "; Pad int }\n@start\nrule main Box = \"x\" => Box{Pad: 1}\n")
			if err != nil {
				t.Fatal(err)
			}
			_, err = compileStatic(g, Options{}, nil)
			switch {
			case tc.zero && err != nil:
				t.Fatalf("a field of type %s has a zero value, got %v", tc.ftype, err)
			case !tc.zero && err == nil:
				t.Fatalf("a field of type %s has no zero value, but leaving it out compiles", tc.ftype)
			case !tc.zero && !strings.Contains(err.Error(), "field F of Box has type "+tc.ftype+", which has no zero value: set it in the constructor, or declare it as *"):
				t.Fatalf("message: %v", err)
			}
		})
	}
	// The error names the optional type to declare, and a constructor that sets the field compiles.
	g, err := parseTest("type Box = { F node[string] | error }\n@start\nrule main Box = f:\"x\" => Box{F: $f}\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileStatic(g, Options{}, nil); err != nil {
		t.Fatalf("a field that is set: %v", err)
	}
}

// TestStructZeroDescriptors pins the descriptors that the runtimes build the zero value of an unset
// field from, in declaration order.
func TestStructZeroDescriptors(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`type Inner = { N int; S string }
type Loop = { Back *Loop; Tag int }
type Alias ~ Inner
type Z = { I int; S string; B bool; L []Inner; O *int; T (int, (string, bool)); U (); In Inner; A _; Lp Loop; Al Alias; K node[string] | error; E []_|_; N *_|_ }
@start
rule main Z = k:"x" => Z{K: $k}
`)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := compileStatic(g, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	z := prog.StructZeroOf("Z")
	for _, f := range z.Names {
		got = append(got, f+"="+z.Zeros[f])
	}
	want := "I=i S=s B=b L=l O=n T=(i(sb)) U=() In={Inner} A=n Lp={Loop} Al={Inner} K=- E=l N=n"
	if s := strings.Join(got, " "); s != want {
		t.Errorf("descriptors\n got %s\nwant %s", s, want)
	}
	if prog.StructZeroOf("Nope") != nil {
		t.Error("a name that is not a struct type has descriptors")
	}
}

// declCountingProgram counts the reads of the type declarations of a program. Whether a struct type
// has a zero value reads them once for each struct type that it expands, so the count is the work of
// that decision in steps, which the compiler's cancellation control does not poll after the build.
type declCountingProgram struct {
	check.Program
	reads *int
}

func (p declCountingProgram) TypeDecls() map[string]ast.TypeDecl {
	*p.reads++
	return p.Program.TypeDecls()
}

// deepStructReads returns the reads of type declarations that the zero-value decision of a chain of
// depth struct types, each holding the next twice, takes: the same type is reached by 2^depth paths.
func deepStructReads(t *testing.T, depth int) uint64 {
	t.Helper()
	prog := deepStructProgram(t, depth)
	var reads int
	checker := check.NewChecker(declCountingProgram{Program: prog.Checker().Program, reads: &reads})
	if !checker.HasZero(types.NamedTy{Name: "S0", Kind: 's'}) {
		t.Fatal("the chain has no zero value")
	}
	return uint64(reads)
}

// deepStructProgram compiles a chain of depth struct types, each holding the next twice, below Top.
func deepStructProgram(t *testing.T, depth int) *Program {
	t.Helper()
	var src strings.Builder
	for i := range depth {
		fmt.Fprintf(&src, "type S%d = { X S%d; Y S%d }\n", i, i+1, i+1)
	}
	fmt.Fprintf(&src, "type S%d = { V int }\ntype Top = { K node[string]; In S0 }\n", depth)
	src.WriteString("@start\nrule main Top = k:\"x\" => Top{K: $k}\n")
	g, err := parseTest(src.String())
	if err != nil {
		t.Fatal(err)
	}
	prog, err := compileStatic(g, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return prog
}

// growsLinearly reports whether a count for twice the depth is at most three times the count for the
// depth (a linear count doubles; an exponential one at the depths of the test grows by 2^10).
func growsLinearly(small, large uint64) bool { return large <= 3*small }

// TestZeroValueOfDeepStructTypesIsLinear pins that the zero descriptors of a chain of struct types, each
// holding the next twice, are found in a number of steps linear in the number of types: the same type is
// reached by 2^depth paths, and visiting every path takes minutes at depth 40. The steps are the reads of
// the type declarations (declCountingProgram), compared at depths 10 and 20, where a visit of every path
// still ends at once (2^20 reads) so that a regression fails the comparison instead of hanging the test.
func TestZeroValueOfDeepStructTypesIsLinear(t *testing.T) {
	t.Parallel()
	small, large := deepStructReads(t, 10), deepStructReads(t, 20)
	t.Logf("%d reads at depth 10, %d at depth 20", small, large)
	if !growsLinearly(small, large) {
		t.Errorf("%d reads at depth 10 and %d at depth 20: the zero-value decision grows faster than the depth", small, large)
	}
	// The descriptors themselves are those of the chain at the depth of the original check.
	prog := deepStructProgram(t, 40)
	if z := prog.StructZeroOf("Top"); z == nil || z.Zeros["In"] != "{S0}" || z.Zeros["K"] != NoZeroDesc {
		t.Errorf("descriptors of Top: %+v", z)
	}
}

// TestZeroGrowthDetectorSeesExponentialReads is the negative control of
// TestZeroValueOfDeepStructTypesIsLinear: the growth measure rejects counts that double with every
// level, the counts of a visit of every path, and accepts linear ones.
func TestZeroGrowthDetectorSeesExponentialReads(t *testing.T) {
	if growsLinearly(1<<10, 1<<20) {
		t.Error("exponential reads passed as linear")
	}
	if !growsLinearly(30, 60) {
		t.Error("linear reads were reported as growing faster")
	}
}
