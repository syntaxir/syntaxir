package print_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/backend/codegen/common"
	"zeroparse.dev/zp/internal/testing/codegentest"
)

// typedChargeGrammars are grammars whose typed results convert a struct with a list of structs, a
// node, a list of nodes and open values.
var typedChargeGrammars = []struct {
	src    string
	inputs []string
}{
	{`type Leaf = { Text string }
type Branch = { Leaves []Leaf }
rule main Branch = xs:(x:leaf)* => Branch{Leaves: map($xs, (x) => x.$x)}
rule leaf Leaf = t:"z" #atomic => Leaf{Text: text($t)}`, []string{"", "z", "zzzz", strings.Repeat("z", 512)}},
	{`rule main = "z"*`, []string{"", "z", "zzzz", strings.Repeat("z", 512)}},
	{`rule main = item*
rule item = k:[a-z]+ "=" v:[0-9]+ ";"`, []string{"", "a=1;", "ab=12;cd=3;", strings.Repeat("key=42;", 64)}},
}

// typedChargeGoConsumer compares, for each input and unit, the units of ParseValue with those of a
// parse that does not convert its result: the smallest compute limit under which each succeeds, and
// the Used/Requested of the LimitError at limits below it. Through the conversion of the Node
// result, that parse is Parse. Through the typed parser it is the typed parse with a conversion that
// returns the result as it is (the typed parser charges otherwise than Parse; the charge agreement
// harness compares Parse with the engines).
const typedChargeGoConsumer = `package PKG

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	zprt "zeroparse.dev/zp/pkg/runtime/libgo"
)

var _ = zprt.TParse[any]

func report(err error) string {
	var le *LimitError
	if errors.As(err, &le) && le.Kind == LimitCompute {
		return fmt.Sprintf("%d/%d", le.Used, le.Requested)
	}
	if err != nil {
		return "error"
	}
	return "ok"
}

func units(run func(uint64) string) (uint64, string) {
	hi := uint64(16)
	for run(hi) != "ok" {
		if hi >= 1<<40 {
			return 0, ""
		}
		hi *= 2
	}
	lo := uint64(0)
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if run(mid) == "ok" {
			hi = mid
		} else {
			lo = mid
		}
	}
	var at []string
	for l := uint64(1); l < hi; l += 1 + hi/40 {
		at = append(at, run(l))
	}
	return hi, strings.Join(at, ",")
}

func TestTypedCharges(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for i, in := range INPUTS {
			opts := func(l uint64) []ParseOption { return []ParseOption{WithUnit(unit), WithLimits(Limits{Compute: l})} }
			plain, plainAt := units(func(l uint64) string { PLAIN; return report(err) })
			typed, typedAt := units(func(l uint64) string { _, err := ParseValue(in, opts(l)...); return report(err) })
			if plain != typed || plainAt != typedAt {
				t.Errorf("%v input %d (%d bytes): ParseValue charges %d units, the parse without its conversion %d\nwithout conversion at limits below: %s\nParseValue at limits below:         %s", unit, i, len(in), typed, plain, plainAt, typedAt)
			}
		}
	}
}
`

// TestGeneratedGoTypedParseChargesWhatParseCharges checks that ParseValue charges the units of its
// parse and nothing for converting the result, through the conversion of the Node result and through
// the typed parser: the values a parse builds, in any representation, charge nothing.
func TestGeneratedGoTypedParseChargesWhatParseCharges(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated typed consumers")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoMod("codegentest")), 0644); err != nil {
		t.Fatal(err)
	}
	var pkgs []string
	for k, c := range typedChargeGrammars {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		var in []string
		for _, s := range c.inputs {
			in = append(in, fmt.Sprintf("%q", s))
		}
		inputs := "[]string{" + strings.Join(in, ",") + "}"
		for _, convert := range []bool{false, true} {
			pkg := fmt.Sprintf("typed%d_%v", k, convert)
			code := codegentest.GeneratePackage(t, dir, pkg, g, common.GenOptions{Start: "main", Types: true, ConvertTypes: convert})
			direct := strings.Contains(code, "TParse(")
			if direct == convert {
				t.Fatalf("%s: direct typed parser %v with ConvertTypes %v", pkg, direct, convert)
			}
			plain := "_, err := Parse(in, opts(l)...)"
			if direct {
				plain = "_, err := zprt.TParse(trules[0], in, opts(l), &tslabs{}, func(v any) any { return v })"
			}
			consumer := strings.NewReplacer("PKG", pkg, "INPUTS", inputs, "PLAIN", plain).Replace(typedChargeGoConsumer)
			if err := os.WriteFile(filepath.Join(dir, pkg, "charges_test.go"), []byte(consumer), 0644); err != nil {
				t.Fatal(err)
			}
			pkgs = append(pkgs, "./"+pkg)
		}
	}
	cmd := exec.Command("go", append([]string{"test", "-count=1"}, pkgs...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed charges: %v\n%s", err, codegentest.Clip(string(out), 0))
	}
}
