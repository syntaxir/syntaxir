package driver

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	tslib "syntaxir.dev/sxir/internal/backend/codegen/typescript/lib"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/pass"
)

// bisectTargets are the targets whose decisions the bisection tests cover: a typed Go parser and a
// TypeScript parser, both with their library embedded, so that its pruning is decided too.
func bisectTargets() []Target {
	return []Target{
		{Backend: result.Go, Gen: common.GenOptions{Module: "example.com/p", Lib: common.LibEmbed, LibGo: libsrc.FS, Types: true}},
		{Backend: result.TypeScript, Gen: common.GenOptions{Lib: common.LibEmbed, TSLib: tslib.FS, Types: true}},
	}
}

// bisectCompile generates the files of the target from p with the settings s and the bisection limit
// limit (none if nil) and returns them as one text, with the decision log.
func bisectCompile(t *testing.T, p *Program, s pass.Settings, limit *int, tg Target) (files, log string) {
	t.Helper()
	var w strings.Builder
	out, err := Compile(context.Background(), p, Options{Settings: s, Debug: common.Debug{OptBisectLimit: limit, Output: &w}}, tg)
	if err != nil {
		t.Fatalf("%s at the limit %v: %v", tg.Backend, limitText(limit), err)
	}
	return filesText(t, out[0]), w.String()
}

func limitText(limit *int) any {
	if limit == nil {
		return "none"
	}
	return *limit
}

// everySettingOff returns the settings with every optimization off.
func everySettingOff(t *testing.T) pass.Settings {
	t.Helper()
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range pass.Flags() {
		names = append(names, f.Name)
	}
	off, err := speed.Without(names...)
	if err != nil {
		t.Fatal(err)
	}
	return off
}

// Canonicalization changes no parser (optimization record 142). With every setting off, the only
// decisions are the applications of the canonicalize patterns, so the limit 0 skips each of them and
// a limit beyond the last runs each. The first alternative of the grammar's start rule cannot match
// (!""* fails), so the code after its predicate is unreachable, which drop-after-exit removes: the
// printers must form nothing for such code, or the names of the tables after it would shift.
func TestCanonicalizationChangesNoParser(t *testing.T) {
	t.Parallel()
	off := everySettingOff(t)
	zero, beyond := 0, 1<<30
	p, err := checkSource(t, "@start\nrule main = !\"\"* B / \"x\"\nrule B = \"b\"\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range bisectTargets() {
		with, log := bisectCompile(t, p, off, &beyond, tg)
		for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
			if f := strings.Fields(line); len(f) < 4 || f[2] != "run" || !strings.HasPrefix(f[3], "canonicalize#") {
				t.Fatalf("%s: with every setting off, a decision that is not a canonicalize pattern was asked: %q", tg.Backend, line)
			}
		}
		if !strings.Contains(log, "canonicalize#2/drop-after-exit") {
			t.Fatalf("%s: drop-after-exit applied nowhere at the Exec stage, so the test is empty:\n%s", tg.Backend, log)
		}
		if without, _ := bisectCompile(t, p, off, &zero, tg); without != with {
			t.Errorf("%s: the parser without the canonicalize patterns differs from the one with them:\n%s", tg.Backend, firstDiff(without, with))
		}
	}
}

// firstDiff returns the lines around the first line where a and b differ.
func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return "line " + strconv.Itoa(i+1) + ":\n- " + x + "\n+ " + y
		}
	}
	return "(equal)"
}
