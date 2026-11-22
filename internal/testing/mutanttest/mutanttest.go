// Package mutanttest lets a test that runs mutants, or another slow negative control, skip itself when what it
// depends on has not changed since a base revision.
//
// A mutant runner rebuilds and reruns tests for every mutant of one set of source files, so its verdict can
// change only when those files, the tests it runs against, or the runner itself change. The gate
// (hack/gate.sh) exports SXIR_MUTANTS_BASE=<its base revision>; a runner then calls SkipUnlessChanged with the
// repository paths it depends on, and skips with a message that names the base when none of them differs from
// it. With SXIR_MUTANTS_BASE unset, as in a plain `go test -tags sxir_long`, nothing skips and every mutant runs.
//
// The rule fails closed: whenever the answer is uncertain (git fails, the base is not a revision, or a path
// is not given) the runner runs.
package mutanttest

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// BaseEnv names the environment variable that holds the base revision.
const BaseEnv = "SXIR_MUTANTS_BASE"

// always are the paths that every runner depends on: the module files that select the toolchain and the
// dependencies, and this package.
var always = []string{"go.mod", "go.sum", "go.work", "internal/testing/mutanttest"}

// gitRepoVars point git at a repository, index or configuration from outside the directory it runs in. An
// inherited one would make a git command of this package, or of its tests, act on another repository.
var gitRepoVars = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_CONFIG_PARAMETERS"}

// gitEnv returns the environment of the git and go commands that this package and its tests run: the
// inherited one without gitRepoVars, with git reading none of the user's global or system configuration. A
// setting there such as core.fsmonitor starts a daemon per repository that outlives the test, and lets a git
// diff that follows a write at once report the file unchanged while the daemon has not yet seen the write.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(gitRepoVars, k) && k != "GIT_CONFIG_GLOBAL" && k != "GIT_CONFIG_NOSYSTEM" {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
}

// SkipUnlessChanged skips t when SXIR_MUTANTS_BASE is set and no file under any of paths, which are slash-separated
// paths from the repository root (a directory stands for everything under it), differs between that revision
// and the working tree, counting untracked files. Call it first in the test, before t.Parallel.
func SkipUnlessChanged(t testing.TB, paths ...string) {
	t.Helper()
	base := os.Getenv(BaseEnv)
	if base == "" {
		return
	}
	changed, err := Changed(base, paths...)
	if err != nil {
		t.Logf("%s=%s: cannot tell what changed (%v); running the mutants", BaseEnv, base, err)
		return
	}
	if len(changed) > 0 {
		t.Logf("%s=%s: running the mutants; changed: %s", BaseEnv, base, strings.Join(changed, " "))
		return
	}
	t.Skipf("%s=%s: none of %s changed since the base, so the mutants' verdict cannot differ", BaseEnv, base, strings.Join(paths, " "))
}

// Source returns the absolute path and the contents of the source file that a runner mutates; file is
// slash-separated from the module root (the nearest directory above the working directory with a go.mod).
// It fails t with a message that names the file and the package directory it was expected in, so a runner
// whose library file moved with its package stops there instead of reporting a misleading verdict.
func Source(t testing.TB, file string) (string, []byte) {
	t.Helper()
	abs, src, err := readSource(file)
	if err != nil {
		t.Fatal(err)
	}
	return abs, src
}

// readSource is Source without the test: it resolves file against the module root and reads it.
func readSource(file string) (string, []byte, error) {
	root, err := moduleRoot()
	if err != nil {
		return "", nil, fmt.Errorf("mutant source %s: %w", file, err)
	}
	pkg, name := path.Split(file)
	pkg = strings.TrimSuffix(pkg, "/")
	if pkg == "" || name == "" || !filepath.IsLocal(filepath.FromSlash(file)) {
		return "", nil, fmt.Errorf("mutant source %q: want a slash-separated path of a file from the module root", file)
	}
	dir := filepath.Join(root, filepath.FromSlash(pkg))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", nil, fmt.Errorf("mutant source %s: the package %s does not exist in %s; did it move?", file, pkg, root)
	}
	abs := filepath.Join(dir, name)
	src, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, fmt.Errorf("mutant source %s: the package %s has no file %s; did it move? (%w)", file, pkg, name, err)
	}
	return abs, src, nil
}

// moduleRoot returns the nearest directory above the working directory that has a go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod above the working directory")
		}
		dir = parent
	}
}

// Changed returns the files under paths (and under the paths every runner depends on) that differ between base
// and the working tree or that are untracked and not ignored. An error means the answer is unknown.
func Changed(base string, paths ...string) ([]string, error) {
	if len(paths) == 0 {
		return nil, os.ErrInvalid
	}
	rev := exec.Command("git", "rev-parse", "--show-toplevel")
	rev.Env = gitEnv()
	topOut, err := rev.Output()
	if err != nil {
		return nil, err
	}
	top := strings.TrimSpace(string(topOut))
	spec := append(append([]string{}, paths...), always...)
	var files []string
	for _, args := range [][]string{
		{"diff", "--name-only", base, "--"},
		{"ls-files", "--others", "--exclude-standard", "--"},
	} {
		cmd := exec.Command("git", append([]string{"-C", top}, append(args, spec...)...)...)
		cmd.Env = gitEnv()
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		files = append(files, strings.Fields(string(out))...)
	}
	return files, nil
}

// Pipeline are the paths that the code generators and the engine, and so the runners of their mutants, depend
// on: every module package that their tests import (go list -deps -test), and the grammars and other test
// data. TestPipelineCoversImports checks that no import leaves it. A change elsewhere (cmd, lint, the language
// server, the documentation) cannot change the verdict of such a runner.
var Pipeline = []string{
	"pkg/bytecode", "pkg/compiler", "pkg/grammar", "pkg/runtime", "pkg/sample", "testdata",
	"internal/analysis", "internal/backend", "internal/bench", "internal/cancel",
	"internal/diagnostic", "internal/driver", "internal/frontend", "internal/ir",
	"internal/middleend", "internal/pass", "internal/runtime", "internal/syntax", "internal/testing",
}
