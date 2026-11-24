package engine

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

func negativeMaxima() []int {
	values := []int{-1, -2, -1 << 31}
	if strconv.IntSize == 64 {
		large := int64(-1) << 32
		values = append(values, int(large), -int(^uint(0)>>1)-1)
	}
	return values
}

func TestNegativeRepetitionMaxima(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, src string }{
		{"ordinary", `rule main = "é"* $$`},
		// The call of e keeps the repetitions plain scans rather than scan sites.
		{"class scan", "rule main = [é]* #atomic e $$\nrule e = \"\""},
		{"any scan", "rule main = .* #atomic e $$\nrule e = \"\""},
		{"projection", `rule main = xs:(v:"é")* $$ => map($xs, (x) => x.$v)`},
	} {
		for _, maximum := range negativeMaxima() {
			t.Run(fmt.Sprintf("%s/%d", c.name, maximum), func(t *testing.T) {
				g := codegentest.GrammarWithNegativeMaximum(t, c.src, maximum)
				before, err := ast.MarshalJSON(g)
				if err != nil {
					t.Fatal(err)
				}
				for _, opts := range []checkpkg.Options{{}, {DisableMemo: true}, {NoProjections: true}} {
					p, err := Compile(g, opts)
					if maximum < -1 {
						// The syntax writes no other negative maximum than -1.
						codegentest.WantNegativeMaximumRejected(t, err)
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					// Saved ASTs use the compiled grammar, where source-only groups
					// are removed. The caller's AST is checked separately below.
					savedAST := executableASTJSON(t, p.Static().Grammar)
					// A negative maximum is no maximum: the body compares its count with no
					// maximum, so it holds no negative bound.
					for _, x := range flowConsts(p.Module()) {
						if n, ok := x.(int); ok && n < 0 {
							t.Errorf("a Flow body holds the bound %d", n)
						}
					}
					if err := program.ValidateModule(p.Module()); err != nil {
						t.Errorf("module: %v", err)
					}
					if tree := executableASTJSON(t, p.Static().Grammar); tree != savedAST {
						t.Fatal("compiled executable AST changed")
					}
					for _, mode := range allModes {
						data, err := p.Marshal("main", mode)
						if err != nil {
							t.Fatal(err)
						}
						if want := byte(bytecode.ISAVersion); data[isaOffset] != want {
							t.Fatalf("negative maxima alone must not require wide instructions: instruction set %d, want %d", data[isaOffset], want)
						}
						loaded, _, err := LoadProgram(data)
						if err != nil {
							t.Fatalf("load: %v", err)
						}
						for _, prog := range []*Program{p, loaded} {
							for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
								if prog == loaded && b == Closure {
									_, err := loaded.ParseWith("main", "", servedBy(mode, ParseOptions{Backend: b}))
									wantClosureRefused(t, err)
									continue
								}
								for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
									for _, recognize := range []bool{false, true} {
										if prog == loaded && recognize != (mode == CompiledRecognize) {
											continue // A compiled grammar serves the parses of its mode only.
										}
										for _, input := range []string{"", "é", "ééé", "x", "éx"} {
											ref := ParseOptions{Unit: unit, Recognize: recognize}
											want, we := p.ParseWith("main", input, ref)
											ref.Backend = b
											if prog == loaded {
												ref = servedBy(mode, ref)
											}
											got, ge := prog.ParseWith("main", input, ref)
											if codegentest.ResultJSON(got, ge) != codegentest.ResultJSON(want, we) {
												t.Errorf("%s/%s recognize=%v input=%q: got %s, want %s", b, unit, recognize, input, codegentest.ResultJSON(got, ge), codegentest.ResultJSON(want, we))
											}
										}
									}
								}
							}
						}
					}
				}
				after, err := ast.MarshalJSON(g)
				if err != nil || string(after) != string(before) {
					t.Fatalf("compiler mutated caller AST: %v", err)
				}
			})
		}
	}
}

// Compiled ASTs preserve executable structure, not grammar source metadata.
// Ignore only SourceInfo; all other serialized fields must still round-trip.
func executableASTJSON(t testing.TB, g *ast.Grammar) string {
	t.Helper()
	copy := g.Clone()
	var strip func(reflect.Value)
	strip = func(v reflect.Value) {
		if v.Type() == reflect.TypeFor[ast.SourceInfo]() {
			v.SetZero()
			return
		}
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				strip(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				strip(v.Field(i))
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				strip(v.Index(i))
			}
		case reflect.Map:
			for _, key := range v.MapKeys() {
				strip(v.MapIndex(key))
			}
		}
	}
	strip(reflect.ValueOf(copy))
	data, err := ast.MarshalJSON(copy)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCompiledSourceMetadataOmitted(t *testing.T) {
	t.Parallel()
	g, err := parseTest("// documentation\npackage p\n// rule documentation\nrule main = \"x\" #atomic")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, checkpkg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range allModes {
		data, err := p.Marshal("main", mode)
		if err != nil {
			t.Fatal(err)
		}
		// A compiled grammar holds no grammar, so it archives no source metadata.
		if strings.Contains(string(data), "documentation") {
			t.Fatalf("%s mode file archives source metadata", mode)
		}
		loaded, _, err := LoadProgram(data)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.SourceGrammar() != nil {
			t.Fatalf("%s mode file holds a grammar", mode)
		}
	}
}

func TestNegativeMaximumWithWideMinimum(t *testing.T) {
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("wide minimum needs 64-bit implementation")
	}
	for _, body := range []string{`"é" #repeat(2147483648, ) $$`, `[é] #repeat(2147483648, ) #atomic $$`} {
		for _, maximum := range negativeMaxima() {
			g := codegentest.GrammarWithNegativeMaximum(t, "rule main = "+body, maximum)
			p, err := Compile(g, checkpkg.Options{})
			if maximum < -1 {
				codegentest.WantNegativeMaximumRejected(t, err)
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, x := range flowConsts(p.Module()) {
				if n, ok := x.(int); ok && n < 0 {
					t.Fatalf("a Flow body holds the bound %d", n)
				}
				found = found || x == 2147483648
			}
			if !found {
				t.Fatal("the wide minimum is in no Flow body")
			}
			for _, mode := range valueModes {
				loaded := saveLoad(t, p, "main", mode)
				for _, b := range []Backend{Bytecode, BytecodeIterative} {
					for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
						ref := ParseOptions{Unit: unit}
						want, we := p.ParseWith("main", "éé", ref)
						ref.Backend = b
						got, ge := loaded.ParseWith("main", "éé", servedBy(mode, ref))
						if codegentest.ResultJSON(got, ge) != codegentest.ResultJSON(want, we) {
							t.Fatalf("wide %s/%s got %s, want %s", b, unit, codegentest.ResultJSON(got, ge), codegentest.ResultJSON(want, we))
						}
					}
				}
			}
		}
	}
}

// flowConsts returns the immediates of the CONST instructions of the Flow bodies of m: those of its
// rules, stream bodies, and Operators Rule skips and lines. The CONST that sets the register of a
// STEP to -1 before a loop's first step marks no position and bounds nothing, so it is left out.
func flowConsts(m *bytecode.Module) []any {
	var out []any
	add := func(f *bytecode.Flow) {
		if f == nil {
			return
		}
		steps := map[int]bool{}
		for _, in := range f.Code {
			if in.Op == bytecode.FlowStep && len(in.Dst) == 1 {
				steps[in.Dst[0]] = true
			}
		}
		for _, in := range f.Code {
			if in.Op == bytecode.FlowConst && !(len(in.Dst) == 1 && steps[in.Dst[0]] && in.Imm == -1) {
				out = append(out, in.Imm)
			}
		}
	}
	for _, f := range m.Flows {
		add(f)
	}
	for _, f := range m.Streams {
		add(f)
	}
	for _, p := range m.OperatorsRules {
		add(p.SkipFlow)
		for _, l := range p.Operands {
			add(l.Flow)
		}
		for _, o := range slices.Concat(p.Prefix, p.InfixPostfix) {
			add(o.Line.Flow)
		}
	}
	return out
}
