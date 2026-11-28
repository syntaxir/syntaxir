package mutanttest

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// isolateGit keeps git from reading the user's global and system configuration for the rest of the test. A
// setting there such as core.fsmonitor applies to the scratch repository, starts a daemon per repository that
// outlives the test, and lets a git diff that follows a write at once report the file unchanged while the daemon
// has not yet seen the write. The scratch repositories of archtest's script tests use the same two variables.
// It also unsets the variables that steer git at a repository, index or configuration from outside it
// (gitRepoVars), which an inherited environment may set; each is restored when the test ends. The commands
// that this package runs itself take the same environment from gitEnv.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range gitRepoVars {
		unsetEnv(t, k)
	}
}

// unsetEnv unsets k for the test and restores its value, if it had one, when the test ends.
func unsetEnv(t *testing.T, k string) {
	t.Helper()
	t.Setenv(k, "")
	if err := os.Unsetenv(k); err != nil {
		t.Fatal(err)
	}
}

// repo makes a git repository with one commit holding src/a.go and other/b.go and makes it the working directory.
// Git reads none of the user's configuration in it or in the rest of the test. It fails the test, rather than
// skipping it, when git cannot make the repository: a skipped guard would not guard anything.
func repo(t *testing.T) {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	for _, f := range []string{"src/a.go", "other/b.go"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(dir)
}

// TestRepoIgnoresUserConfig plants a global configuration that enables core.fsmonitor where git looks for the
// user's ($XDG_CONFIG_HOME and $HOME) and checks that the scratch repository does not see it. The check
// means something only if git would read the planted file without the isolation, so the test first unsets
// GIT_CONFIG_GLOBAL and GIT_CONFIG_NOSYSTEM (a developer's environment may set them, and then git reads no
// HOME file at all) and checks that the planted setting is visible.
func TestRepoIgnoresUserConfig(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	user := []byte("[core]\n\tfsmonitor = true\n")
	for _, p := range []string{filepath.Join(xdg, "git", "config"), filepath.Join(home, ".gitconfig")} {
		if err := os.WriteFile(p, user, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	unsetEnv(t, "GIT_CONFIG_GLOBAL")
	unsetEnv(t, "GIT_CONFIG_NOSYSTEM")
	if out, err := exec.Command("git", "config", "--global", "--get", "core.fsmonitor").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("without the isolation git does not read the planted setting (%v, %q): the guard would pass vacuously", err, out)
	}
	repo(t)
	out, err := exec.Command("git", "config", "--show-origin", "--get", "core.fsmonitor").CombinedOutput()
	if err == nil {
		t.Fatalf("the scratch repository sees the user's core.fsmonitor: %s", out)
	}
}

func TestChanged(t *testing.T) {
	repo(t)
	got, err := Changed("HEAD", "src")
	if err != nil || len(got) != 0 {
		t.Fatalf("a clean tree: got %v, %v", got, err)
	}
	if err := os.WriteFile("other/b.go", []byte("package y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Changed("HEAD", "src"); err != nil || len(got) != 0 {
		t.Fatalf("a change outside the paths: got %v, %v", got, err)
	}
	if got, err := Changed("HEAD", "other"); err != nil || !slices.Equal(got, []string{"other/b.go"}) {
		t.Fatalf("a modified file under a directory: got %v, %v", got, err)
	}
	if err := os.WriteFile("src/new.go", []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Changed("HEAD", "src"); err != nil || !slices.Equal(got, []string{"src/new.go"}) {
		t.Fatalf("an untracked file: got %v, %v", got, err)
	}
	if err := os.WriteFile("go.mod", []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Changed("HEAD", "src"); err != nil || !slices.Contains(got, "go.mod") {
		t.Fatalf("go.mod is a dependency of every runner: got %v, %v", got, err)
	}
	if _, err := Changed("no-such-revision", "src"); err == nil {
		t.Fatal("an unknown base must be an error, so that the runner runs")
	}
	if _, err := Changed("HEAD"); err == nil {
		t.Fatal("no paths must be an error")
	}
}

func TestSkipUnlessChanged(t *testing.T) {
	repo(t)
	t.Run("no base runs", func(t *testing.T) {
		t.Setenv(BaseEnv, "")
		SkipUnlessChanged(t, "src")
		if t.Skipped() {
			t.Fatal("skipped without a base")
		}
	})
	t.Run("unknown base runs", func(t *testing.T) {
		t.Setenv(BaseEnv, "no-such-revision")
		SkipUnlessChanged(t, "src")
		if t.Skipped() {
			t.Fatal("skipped with an unknown base")
		}
	})
	t.Run("unchanged skips", func(t *testing.T) {
		t.Setenv(BaseEnv, "HEAD")
		SkipUnlessChanged(t, "src")
		t.Fatal("did not skip")
	})
}

// runnerPackages are the package patterns whose tests run the mutants that use Pipeline.
var runnerPackages = []string{"./internal/backend/...", "./pkg/runtime/libgo/...", "./pkg/sample", "./internal/runtime/engine/...", "./internal/testing/examinedref"}

// fixedTagSets are the build tag sets under which testImports lists the runners' dependencies, but for
// the reference builds (runnerTagSets): the default (files under !sxir_long, !sxir_feature_witness and
// !sxir_budget_controls_reference), the long tier with the charge log and the feature witness, and the
// long tier with the budget-control reference.
var fixedTagSets = [][]string{
	nil,
	{"sxir_long", "sxir_charge_log", "sxir_feature_witness"},
	{"sxir_long", "sxir_budget_controls_reference"},
}

// referenceTag matches a reference tag of the module, which selects the simpler or the former form of
// a mechanism (hack/test-reference-tags.sh): sxir_reference_* and sxir_snapshot_*_reference.
var referenceTag = regexp.MustCompile(`\bsxir_(?:reference_[a-z0-9_]+|snapshot_[a-z0-9_]+_reference)\b`)

// runnerTagSets returns the build tag sets under which testImports lists the runners' dependencies. A
// file whose build constraint names a tag of the module (sxir_*) takes part in some sets and not in
// others, and the mutant runners live mostly in the long tier, so the listing is the union over sets
// that between them build every such file: fixedTagSets, and the long tier with every reference tag
// that a build constraint of the runner packages names, read from the files that the default build
// leaves out, so that a new reference form is listed without a list to update.
// TestRunnerTagSetsBuildEveryFile checks that they build every such file.
func runnerTagSets() ([][]string, error) {
	out, err := goList(nil, append([]string{"-f", `{{$d := .Dir}}{{range .IgnoredGoFiles}}{{$d}}/{{.}}
{{end}}`}, runnerPackages...)...)
	if err != nil {
		return nil, err
	}
	refs := map[string]bool{}
	for _, f := range strings.Fields(out) {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, l := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(l, "package ") {
				break
			}
			if strings.HasPrefix(l, "//go:build ") {
				for _, tag := range referenceTag.FindAllString(l, -1) {
					refs[tag] = true
				}
			}
		}
	}
	sets := slices.Clone(fixedTagSets)
	if len(refs) > 0 {
		sets = append(sets, append([]string{"sxir_long"}, slices.Sorted(maps.Keys(refs))...))
	}
	return sets, nil
}

// goList runs go list with the build tags tags and the arguments args, from the module root. A pattern that
// matches no package is an error that names it.
func goList(tags []string, args ...string) (string, error) {
	cmd := exec.Command("go", append([]string{"list", "-tags", strings.Join(tags, ",")}, args...)...)
	cmd.Env = gitEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -tags %q %s: %v: %s", strings.Join(tags, ","), strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// testImports returns the import paths of the packages that the tests of the packages pkgs depend on under
// any of the tag sets sets, from the module root. A pattern that matches no package is an error that names
// it: a runner list that names a moved or deleted package must not leave the check with nothing to check.
func testImports(sets [][]string, pkgs ...string) ([]string, error) {
	var all []string
	for _, tags := range sets {
		out, err := goList(tags, append([]string{"-deps", "-test", "-f", "{{.ImportPath}}"}, pkgs...)...)
		if err != nil {
			return nil, err
		}
		all = append(all, strings.Split(strings.TrimSpace(out), "\n")...)
	}
	slices.Sort(all)
	return slices.Compact(all), nil
}

// unbuiltTaggedFiles returns the Go files of the packages pkgs that none of the tag sets sets builds although
// their build constraint names a tag of the module (sxir_*): files whose imports testImports cannot see. Files
// left out for other reasons (//go:build ignore, another platform) are not its concern.
func unbuiltTaggedFiles(sets [][]string, pkgs ...string) ([]string, error) {
	const files = `{{$d := .Dir}}{{range .GoFiles}}+{{$d}}/{{.}}
{{end}}{{range .CgoFiles}}+{{$d}}/{{.}}
{{end}}{{range .TestGoFiles}}+{{$d}}/{{.}}
{{end}}{{range .XTestGoFiles}}+{{$d}}/{{.}}
{{end}}{{range .IgnoredGoFiles}}-{{$d}}/{{.}}
{{end}}`
	built, ignored := map[string]bool{}, map[string]bool{}
	for _, tags := range sets {
		out, err := goList(tags, append([]string{"-f", files}, pkgs...)...)
		if err != nil {
			return nil, err
		}
		for _, l := range strings.Fields(out) {
			if f, ok := strings.CutPrefix(l, "+"); ok {
				built[f] = true
			} else {
				ignored[strings.TrimPrefix(l, "-")] = true
			}
		}
	}
	var out []string
	for f := range ignored {
		if built[f] {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, l := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(l, "package ") {
				break
			}
			if strings.HasPrefix(l, "//go:build ") && strings.Contains(l, "sxir_") {
				out = append(out, f)
				break
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

// TestRunnerTagSetsBuildEveryFile checks that the tag sets of testImports build every file of the mutant
// runners that a tag of the module selects, so that no import of such a file escapes
// TestPipelineCoversImports. Its negative control is that the default build alone leaves the long tier out:
// the check sees build tags, and the listing with runnerTagSets is what includes those files.
func TestRunnerTagSetsBuildEveryFile(t *testing.T) {
	t.Chdir("../../..")
	sets, err := runnerTagSets()
	if err != nil {
		t.Fatal(err)
	}
	missed, err := unbuiltTaggedFiles(sets, runnerPackages...)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range missed {
		t.Errorf("%s: no tag set of runnerTagSets builds it, so its imports are not checked", f)
	}
	missed, err = unbuiltTaggedFiles([][]string{nil}, runnerPackages...)
	if err != nil {
		t.Fatal(err)
	}
	long := 0
	for _, f := range missed {
		if strings.HasSuffix(f, "_long_test.go") {
			long++
		}
	}
	if long == 0 {
		t.Errorf("the default build leaves no long-tier test file out (%d files): the check does not see build tags", len(missed))
	}
}

// TestPipelineCoversImports checks that every module package that the tests of the packages whose mutants use
// Pipeline import lies under it, so that a new import cannot leave a runner unaware of a dependency. It fails,
// rather than skips, when the packages cannot be listed.
func TestPipelineCoversImports(t *testing.T) {
	t.Chdir("../../..")
	sets, err := runnerTagSets()
	if err != nil {
		t.Fatal(err)
	}
	imports, err := testImports(sets, runnerPackages...)
	if err != nil {
		t.Fatal(err)
	}
	const mod = "syntaxir.dev/sxir/"
	for _, p := range imports {
		rel, ok := strings.CutPrefix(p, mod)
		if !ok {
			continue
		}
		rel, _, _ = strings.Cut(rel, " ") // "pkg [pkg.test]"
		rel = strings.TrimSuffix(strings.TrimSuffix(rel, ".test"), "_test")
		covered := false
		for _, d := range Pipeline {
			covered = covered || rel == d || strings.HasPrefix(rel, d+"/")
		}
		if !covered {
			t.Errorf("%s is imported by the tests of the mutant runners but is not under Pipeline", rel)
		}
	}
}

// TestPipelineImportsNameMissingPackages is the negative control of TestPipelineCoversImports: a runner
// package that does not exist makes the listing fail with an error that names it.
func TestPipelineImportsNameMissingPackages(t *testing.T) {
	t.Chdir("../../..")
	const missing = "./no/such/runner"
	_, err := testImports(fixedTagSets, append(slices.Clone(runnerPackages), missing)...)
	if err == nil || !strings.Contains(err.Error(), "no/such/runner") {
		t.Fatalf("listing with %s: error %v, want one that names it", missing, err)
	}
}

// TestSourceNamesMissingFiles is the negative control of Source: a runner's source file that is not where
// the runner looks for it, because the file or its package moved, is an error that names the file and the
// package, never an empty source.
func TestSourceNamesMissingFiles(t *testing.T) {
	abs, src, err := readSource("internal/testing/mutanttest/mutanttest.go")
	if err != nil || !strings.Contains(string(src), "package mutanttest") || !filepath.IsAbs(abs) {
		t.Fatalf("an existing file: %s, %d bytes, error %v", abs, len(src), err)
	}
	for _, c := range []struct{ file, want string }{
		{"internal/testing/mutanttest/no_such_file.go", "the package internal/testing/mutanttest has no file no_such_file.go"},
		{"internal/no/such/package/call_exec.go", "the package internal/no/such/package does not exist"},
		{"state/call_exec.go", "the package state does not exist"},
		{"call_exec.go", "want a slash-separated path"},
		{"../outside/call_exec.go", "want a slash-separated path"},
	} {
		_, src, err := readSource(c.file)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), c.file) || src != nil {
			t.Errorf("readSource(%q): %d bytes, error %v; want one that names %q and the file", c.file, len(src), err, c.want)
		}
	}
}

// TestRepoDropsInheritedGitVariables checks that a variable that steers git at another repository, index or
// configuration does not reach the scratch repository.
func TestRepoDropsInheritedGitVariables(t *testing.T) {
	for _, k := range gitRepoVars {
		t.Setenv(k, "/outside")
	}
	repo(t)
	for _, k := range gitRepoVars {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s is still set to %q in the scratch repository", k, v)
		}
	}
}

// TestChangedDropsInheritedGitVariables checks that Changed asks the repository it runs in, whatever the
// inherited environment points git at: with GIT_DIR and the other variables of gitRepoVars set to another
// place, it still reports the file changed in the scratch repository.
func TestChangedDropsInheritedGitVariables(t *testing.T) {
	repo(t)
	if err := os.WriteFile("src/a.go", []byte("package x // changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, k := range gitRepoVars {
		t.Setenv(k, filepath.Join(t.TempDir(), "outside"))
	}
	files, err := Changed("HEAD", "src")
	if err != nil || !slices.Equal(files, []string{"src/a.go"}) {
		t.Fatalf("Changed: %q, %v; want src/a.go", files, err)
	}
}

// TestGitEnvIsolates checks the environment of the commands this package runs: no variable of gitRepoVars,
// and git reads no global or system configuration, whatever the inherited environment sets.
func TestGitEnvIsolates(t *testing.T) {
	for _, k := range gitRepoVars {
		t.Setenv(k, "/outside")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/outside/config")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
	env := gitEnv()
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if slices.Contains(gitRepoVars, k) {
			t.Errorf("%s is inherited", kv)
		}
	}
	for _, want := range []string{"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1"} {
		k, _, _ := strings.Cut(want, "=")
		var got []string
		for _, kv := range env {
			if strings.HasPrefix(kv, k+"=") {
				got = append(got, kv)
			}
		}
		if !slices.Equal(got, []string{want}) {
			t.Errorf("%s: %q, want only %s", k, got, want)
		}
	}
}
