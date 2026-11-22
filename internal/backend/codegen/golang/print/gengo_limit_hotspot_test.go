package print_test

import (
	"os"
	"path/filepath"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedLimitHotspot checks that a compute limit error of a generated parser names the rule that
// the profile of the parse ranks first, and has no hotspot for a parse without a profile.
func TestGeneratedLimitHotspot(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := parseTest(`rule main = y / x "a" / x "b" / x "c"
rule y = "x"+ "!"
rule x = "x"+`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(codegentest.GoMod("hotspot")))
	files, err := codegentest.Generate(t, g, common.GenOptions{Module: "hotspot/p", Package: "p", Start: "main", Lib: common.LibImport, LimitControls: true, Observability: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		write(filepath.Join("p", f.Path), f.Content)
	}
	write(filepath.Join("p", "hotspot_test.go"), []byte(`package p

import (
	"errors"
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"
	sxirrt "syntaxir.dev/sxir/pkg/runtime/libgo"
)

func TestHotspot(t *testing.T) {
	in := strings.Repeat("x", 40) + "c"
	// One unit less than the parse needs, so the check that fails comes after every evaluation.
	need := uint64(1)
	for {
		if _, err := Parse(in, WithLimits(Limits{Compute: need})); err == nil {
			break
		}
		if need++; need > 5000 {
			t.Fatal("the parse needs more than 5000 units")
		}
	}
	limits := WithLimits(Limits{Compute: need - 1})
	var profile sxirinstrument.Profile
	_, err := Parse(in, limits, sxirrt.WithProfile(&profile))
	var le *sxirruntime.LimitError
	if !errors.As(err, &le) || le.Kind != sxirruntime.LimitCompute {
		t.Fatalf("got %v, want a compute limit error", err)
	}
	if le.Hotspot != "x" || !strings.Contains(le.Error(), "hotspot rule x") {
		t.Errorf("hotspot %q, error %q", le.Hotspot, le.Error())
	}
	_, err = Parse(in, limits)
	if !errors.As(err, &le) || le.Hotspot != "" {
		t.Errorf("without a profile: %v", err)
	}
}
`))
	codegentest.GoVetAndTest(t, dir, true)
}
