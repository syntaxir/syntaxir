package main

import (
	"bytes"
	"io"
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
		if d.PrintAfter != nil || d.PrintBefore != nil || d.PrintChanged || d.Stats || d.TimePasses || d.OptBisectLimit != nil {
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
				{[]string{"-print-before", "guards"}, compiler.Debug{PrintBefore: []string{"guards"}}, "-- before guards ["},
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
			// Under a bisection limit the generator generates once: limit 0 skips every optional run
			// and a limit past the last runs them all, which the flag and the field report alike. A
			// skipped pass decides none of its patterns, so the two report different numbers of runs.
			zero, n := 0, 1<<20
			_, none0 := gen(lang, "-opt-bisect-limit", "0")
			_, all := gen(lang, "-opt-bisect-limit", "1048576")
			if !strings.HasPrefix(none0, "opt-bisect: 1 skip ") || strings.Contains(none0, " run ") {
				t.Errorf("-opt-bisect-limit 0 wrote:\n%.500s", none0)
			}
			if !strings.HasPrefix(all, "opt-bisect: 1 run ") || strings.Contains(all, " skip ") {
				t.Errorf("-opt-bisect-limit %d wrote:\n%.500s", n, all)
			}
			if got := api(lang, compiler.Debug{OptBisectLimit: &zero}); got != none0 {
				t.Errorf("-opt-bisect-limit 0 and OptBisectLimit 0 differ:\n%.500s\n---\n%.500s", none0, got)
			}
			// The statistics are the same in every run.
			_, first := gen(lang, "-stats")
			if _, again := gen(lang, "-stats"); again != first || !strings.Contains(first, "  guards ") {
				t.Errorf("-stats differs between runs or lacks guards:\n%s\n---\n%s", first, again)
			}
		})
	}
	// A required step is not a pass to print before, and a negative limit is an error.
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-print-before", "flowlower"}, "-print-before: flowlower is a required step"},
		{[]string{"-opt-bisect-limit", "-1"}, "-opt-bisect-limit: the limit -1 is negative"},
	} {
		_, err := runCLI(t, "", append([]string{"gen", "-g", path, "-o", t.TempDir(), "-module", "example.com/gen"}, c.args...)...)
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.args, err, c.want)
		}
	}
	neg := -1
	for _, d := range []compiler.Debug{{PrintBefore: []string{"flowlower"}, Output: io.Discard}, {OptBisectLimit: &neg, Output: io.Discard}, {PrintBefore: []string{"guards"}}} {
		if _, err := compiler.GenerateGo(src, compiler.GoConfig{Module: "example.com/gen"}, compiler.WithDebug(d)); err == nil {
			t.Errorf("WithDebug(%+v) is no error", d)
		}
	}
	// A required step is not an optimization to turn on or off.
	for _, c := range [][]string{{"-no-opt", "flowlower"}, {"-opt", "execlower=1"}} {
		_, err := runCLI(t, "", append([]string{"gen", "-g", path, "-o", t.TempDir(), "-module", "example.com/gen"}, c...)...)
		if want := c[0] + ": " + strings.SplitN(c[1], "=", 2)[0] + " is a required step and always runs"; err == nil || err.Error() != want {
			t.Errorf("%v: %v, want %q", c, err, want)
		}
	}
	// The debugging flags do not apply to the library alone.
	for _, args := range [][]string{{"-print-changed"}, {"-verify-each"}, {"-stats"}, {"-time-passes"}, {"-print-before", "guards"}, {"-opt-bisect-limit", "3"}} {
		name := strings.Join(args, " ")
		_, err := runCLI(t, "", append([]string{"gen", "-lib", "only", "-o", t.TempDir()}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "cannot be combined with -lib only") {
			t.Errorf("%s with -lib only: %v", name, err)
		}
		_, err = runCLI(t, "", append([]string{"gen", "-lang", "ts", "-lib", "only", "-o", t.TempDir()}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "cannot be combined with -lib only") {
			t.Errorf("%s with -lang ts -lib only: %v", name, err)
		}
	}
}
