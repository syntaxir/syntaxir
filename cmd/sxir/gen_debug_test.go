package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
)

// timeFields matches the time and the share of a line of -time-passes, which differ between runs.
var timeFields = regexp.MustCompile(` +\d+\.\d{3}ms +\d+\.\d%$`)

// stable returns the output of the debugging flags without the times of -time-passes.
func stable(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = timeFields.ReplaceAllString(l, "")
	}
	return strings.Join(lines, "\n")
}

// TestGenDebug checks the debugging flags of sxir gen: each writes what the field of compiler.Debug
// that it maps to writes, leaves the generated files as they are, writes the same statistics in every
// run, and does not apply to the library alone.
func TestGenDebug(t *testing.T) {
	path := writeFile(t, "opt.sxir", optimizeGrammar)
	src := grammar.FromString("opt.sxir", optimizeGrammar)
	gen := func(lang string, args ...string) (map[string]string, string) {
		t.Helper()
		var diag strings.Builder
		old := genDiagnostics
		genDiagnostics = &diag
		defer func() { genDiagnostics = old }()
		dir := t.TempDir()
		base := []string{"gen", "-lang", lang, "-g", path, "-o", dir, "-types", "-lib", "import"}
		if lang == "go" {
			base = append(base, "-module", "example.com/gen", "-typed-only")
		} else {
			base = append(base, "-lib-module", "./lib.ts")
		}
		if _, err := runCLI(t, "", append(base, args...)...); err != nil {
			t.Fatalf("%s %v: %v", lang, args, err)
		}
		files := map[string]string{}
		err := filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			data, err := os.ReadFile(p)
			files[p[len(dir):]] = string(data)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return files, diag.String()
	}
	api := func(lang string, d compiler.Debug) string {
		t.Helper()
		var diag bytes.Buffer
		if d.PrintAfter != nil || d.PrintChanged || d.Stats || d.TimePasses {
			d.Output = &diag
		}
		var err error
		if lang == "go" {
			_, err = compiler.GenerateGo(src, compiler.GoConfig{Module: "example.com/gen", Lib: compiler.LibImport, Types: true, TypedOnly: true}, compiler.WithDebug(d))
		} else {
			_, err = compiler.GenerateTypeScript(src, compiler.TSConfig{Lib: compiler.LibImport, LibModule: "./lib.ts", Types: true}, compiler.WithDebug(d))
		}
		if err != nil {
			t.Fatalf("%s %+v: %v", lang, d, err)
		}
		return diag.String()
	}
	for _, lang := range []string{"go", "ts"} {
		t.Run(lang, func(t *testing.T) {
			plain, none := gen(lang)
			if none != "" {
				t.Errorf("a generation without the flags wrote:\n%.300s", none)
			}
			for _, c := range []struct {
				flags []string
				field compiler.Debug
				want  string // in the output
			}{
				{[]string{"-print-after", "guards"}, compiler.Debug{PrintAfter: []string{"guards"}}, "-- after guards ["},
				{[]string{"-print-changed"}, compiler.Debug{PrintChanged: true}, "-- after guards ["},
				{[]string{"-verify-each"}, compiler.Debug{VerifyEach: true}, ""},
				{[]string{"-stats"}, compiler.Debug{Stats: true}, "-- stats --\n"},
				{[]string{"-time-passes"}, compiler.Debug{TimePasses: true}, "-- time-passes --\n"},
			} {
				files, diag := gen(lang, c.flags...)
				if len(files) != len(plain) {
					t.Errorf("%v writes %d files, without it %d", c.flags, len(files), len(plain))
				}
				for name, text := range plain {
					if files[name] != text {
						t.Errorf("%v changes %s", c.flags, name)
					}
				}
				if c.want == "" && diag != "" || !strings.Contains(diag, c.want) {
					t.Errorf("%v wrote:\n%.500s", c.flags, diag)
				}
				if got := api(lang, c.field); stable(got) != stable(diag) {
					t.Errorf("%v and %+v differ:\n%.500s\n---\n%.500s", c.flags, c.field, diag, got)
				}
			}
			// The statistics are the same in every run.
			_, first := gen(lang, "-stats")
			if _, again := gen(lang, "-stats"); again != first || !strings.Contains(first, "  guards ") {
				t.Errorf("-stats differs between runs or lacks guards:\n%s\n---\n%s", first, again)
			}
		})
	}
	// The debugging flags do not apply to the library alone.
	for _, name := range []string{"-print-changed", "-verify-each", "-stats", "-time-passes"} {
		_, err := runCLI(t, "", "gen", "-lib", "only", "-o", t.TempDir(), name)
		if err == nil || !strings.Contains(err.Error(), "cannot be combined with -lib only") {
			t.Errorf("%s with -lib only: %v", name, err)
		}
		_, err = runCLI(t, "", "gen", "-lang", "ts", "-lib", "only", "-o", t.TempDir(), name)
		if err == nil || !strings.Contains(err.Error(), "cannot be combined with -lib only") {
			t.Errorf("%s with -lang ts -lib only: %v", name, err)
		}
	}
}
