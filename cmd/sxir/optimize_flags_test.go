package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
)

const engineOptimizeGrammar = "@start\nrule main = (a / b)* $$\nrule a = \"x\" \"y\"\nrule b = \"x\" \"z\" / \"q\"\n"

// TestOptimizeFlagsReachTheBuild checks that the commands that compile a grammar build it under
// the optimization setting of -optimize, -opt and -no-opt, as sxir gen does.
func TestOptimizeFlagsReachTheBuild(t *testing.T) {
	g := writeFile(t, "g.sxir", engineOptimizeGrammar)
	for _, tc := range []struct {
		args   []string
		guards bool
	}{
		{nil, true},
		{[]string{"-optimize", "size"}, true},
		{[]string{"-no-opt", "guards"}, false},
		{[]string{"-opt", "guards=0"}, false},
		{[]string{"-opt", "guards", "-no-opt", "guards"}, false},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		o := registerOptimizeFlags(fs)
		if err := fs.Parse(tc.args); err != nil {
			t.Fatal(err)
		}
		flags, err := compileFlags{}.withOptimize(o)
		if err != nil {
			t.Fatal(tc.args, err)
		}
		if flags.optimizeGiven != (len(tc.args) > 0) {
			t.Errorf("%v: given %v", tc.args, flags.optimizeGiven)
		}
		p, err := loadParser(g, "", flags)
		if err != nil {
			t.Fatal(tc.args, err)
		}
		if got := entry.Source(p.Program()).Settings().On("guards"); got != tc.guards {
			t.Errorf("%v: guards on %v, want %v", tc.args, got, tc.guards)
		}
	}
}

// TestOptimizeFlagsOfEngineCommands runs every command that compiles a grammar for the engine with
// the optimization flags: a setting changes what sxir compile saves and not what a parse returns, a
// flag that names no optimization is an error that names the flag, and a compiled grammar, which
// holds the bytecode its setting built, takes no flag.
func TestOptimizeFlagsOfEngineCommands(t *testing.T) {
	g := writeFile(t, "g.sxir", engineOptimizeGrammar)
	dir := t.TempDir()
	compile := func(name string, args ...string) []byte {
		t.Helper()
		out := filepath.Join(dir, name)
		if _, err := runCLI(t, "", append([]string{"compile", "-g", g, "-o", out}, args...)...); err != nil {
			t.Fatal(args, err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	def, speed, noGuards := compile("d.sxirc"), compile("s.sxirc", "-optimize", "speed"), compile("n.sxirc", "-no-opt", "guards")
	if !bytes.Equal(def, speed) {
		t.Error("-optimize speed, the default profile, saves other bytecode than no flag")
	}
	if bytes.Equal(def, noGuards) {
		t.Error("-no-opt guards saves the bytecode of the default setting")
	}

	want, err := runCLI(t, "", "parse", "-g", g, "-i", "xyq", "-f", "sexpr")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-no-opt", "guards,scans"}, {"-optimize", "memory"}, {"-opt", "class-tables=3"}} {
		if got, err := runCLI(t, "", append([]string{"parse", "-g", g, "-i", "xyq", "-f", "sexpr"}, args...)...); err != nil || got != want {
			t.Errorf("parse %v: got %q, %v, want %q", args, got, err, want)
		}
	}

	suite := filepath.Join(filepath.Dir(g), "suite.json")
	if err := os.WriteFile(suite, []byte(`{"version":1,"grammar":"g.sxir","cases":[{"name":"one","input":"xyq","expect":{"outcome":"accepted"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, "", "test", "-f", suite, "-no-opt", "guards"); err != nil || !strings.Contains(out, "all fixtures passed") {
		t.Errorf("test -no-opt guards: got %q, %v", out, err)
	}

	commands := [][]string{
		{"parse", "-g", g, "-i", "xy"},
		{"trace", "-g", g, "-i", "xy"},
		{"profile", "-g", g, "-i", "xy"},
		{"explain", "-g", g, "-i", "xy"},
		{"compile", "-g", g, "-o", filepath.Join(dir, "bad.sxirc")},
		{"test", "-f", suite},
	}
	for _, cmd := range commands {
		if _, err := runCLI(t, "", append(cmd, "-no-opt", "guards")...); err != nil {
			t.Errorf("%s -no-opt guards: %v", cmd[0], err)
		}
		for _, bad := range [][]string{{"-no-opt", "bogus"}, {"-opt", "bogus"}, {"-optimize", "bogus"}} {
			_, err := runCLI(t, "", append(cmd, bad...)...)
			if err == nil || !strings.HasPrefix(err.Error(), bad[0]+":") {
				t.Errorf("%s %v: got %v, want an error of %s", cmd[0], bad, err, bad[0])
			}
		}
	}

	compiled := filepath.Join(dir, "d.sxirc")
	for _, cmd := range [][]string{
		{"parse", "-g", compiled, "-i", "xy"},
		{"compile", "-g", compiled, "-o", filepath.Join(dir, "again.sxirc")},
	} {
		if _, err := runCLI(t, "", append(cmd, "-optimize", "speed")...); err == nil || !strings.Contains(err.Error(), "holds the bytecode that its optimization settings built") {
			t.Errorf("%s of a compiled grammar with -optimize: got %v", cmd[0], err)
		}
	}
	if _, err := runCLI(t, "", "parse", "-g", compiled, "-i", "xy"); err != nil {
		t.Errorf("parse of a compiled grammar without a flag: %v", err)
	}
}
