package print_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// windowLib returns a copy of the library whose streams call CommitHook after each commit with the
// commit point, the floor of the memo and the memory count, and with the commits of mutate applied
// to its source if not nil.
func windowLib(t *testing.T, mutate func(name, src string) string) string {
	t.Helper()
	patch, check := codegentest.GoLibPatch(t, func(name, src string) string {
		if mutate != nil {
			src = mutate(name, src)
		}
		if name != "stream_exec.go" {
			return src
		}
		const end = "\tif p.Stream.tail.limit > 0 {\n\t\tp.streamSettleErrors()\n\t}\n}\n"
		if strings.Count(src, end) != 1 {
			t.Fatalf("runtime anchor changed: %q", end)
		}
		return strings.Replace(src, end, strings.TrimSuffix(end, "}\n")+"\tif CommitHook != nil {\n\t\tCommitHook(pos, p.Stream.memoBase, p.Control.memoryUsed())\n\t}\n}\n", 1)
	})
	lib := codegentest.LibCopyWith(t, patch, map[string]string{"zz_hooks.go": "package libgo\n\nvar CommitHook func(commit, floor int, used uint64)\n"})
	check()
	return lib
}

// streamWindowConsumer parses the streams of the cases, each generated as package g<i>, at both
// lengths, and prints for each its name, the largest memory counts after a commit of the shorter and
// the longer stream, and the first commit after which the floor of the memo breaks the window, if any.
const streamWindowConsumer = `package main

import (
	"fmt"
	"io"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirrt "syntaxir.dev/sxir/pkg/runtime/libgo"
IMPORTS)

type lineReader struct {
	line string
	n    int
}

func (r *lineReader) Read(b []byte) (int, error) {
	if r.n == 0 {
		return 0, io.EOF
	}
	r.n--
	return copy(b, r.line), nil
}

// most runs parse and returns the largest memory count after a commit and the first problem of the
// window: the floor is a commit point fewer than 1,024 units before the commit point, and it moves only
// to a commit point at least 1,024 units past it.
func most(parse func(r io.Reader, opts ...sxirruntime.ParseOption) error, line string, n, limit int) (uint64, string) {
	var m uint64
	floor, problem := 0, ""
	commits := map[int]bool{0: true}
	sxirrt.CommitHook = func(commit, f int, used uint64) {
		m = max(m, used)
		commits[commit] = true
		switch {
		case problem != "":
		case !commits[f] || f > commit || commit-f >= 1024 || f < floor || f > floor && f-floor < 1024:
			problem = fmt.Sprintf("after the commit at %d the floor moved from %d to %d", commit, floor, f)
		}
		floor = f
	}
	opts := []sxirruntime.ParseOption{sxirruntime.WithLimits(sxirruntime.Limits{Memory: 1 << 40})}
	if limit > 0 {
		opts = append(opts, sxirruntime.WithDiagnosticLimit(limit))
	}
	if err := parse(&lineReader{line, n}, opts...); err != nil && limit == 0 {
		problem = err.Error()
	}
	return m, problem
}

func main() {
CASES}
`

// TestGeneratedStreamMemoryWindow checks the window of the memory theorem of the streaming proof on
// generated Go streams, as TestStreamMemoryWindow does on the engine's: after each commit the floor of
// the memo is a commit point fewer than 1,024 units back that moved there from at least 1,024 units
// back, and the largest memory count after a commit rises by at most StreamCountSlack for a stream
// four times as long.
func TestGeneratedStreamMemoryWindow(t *testing.T) {
	t.Parallel()
	for _, bad := range streamWindowGo(t, nil) {
		t.Error(bad)
	}
}

// streamWindowGo generates the streams of StreamWindowCases against the library with mutate applied,
// runs them, and returns how each breaks the window or the count.
func streamWindowGo(t *testing.T, mutate func(name, src string) string) []string {
	t.Helper()
	goBin := skipRunsGoBin(t)
	codegentest.NeedsBudgetControls(t)
	dir := t.TempDir()
	var imports, cases strings.Builder
	for i, c := range codegentest.StreamWindowCases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatal(err)
		}
		pkg := "g" + strconv.Itoa(i)
		codegentest.GeneratePackageIn(t, dir, "window", pkg, g, common.GenOptions{Start: "main", Stream: true})
		fmt.Fprintf(&imports, "\t%s \"window/%s\"\n", pkg, pkg)
		parse := fmt.Sprintf("func(r io.Reader, opts ...sxirruntime.ParseOption) error { return %s.ParseStream(r, func(*%s.Node) error { return nil }, opts...) }", pkg, pkg)
		fmt.Fprintf(&cases, "\t{\n\t\tparse := %s\n\t\tshort, p1 := most(parse, %q, %d, %d)\n\t\tlong, p2 := most(parse, %q, %d, %d)\n\t\tfmt.Printf(\"%%s\\t%%d\\t%%d\\t%%s\\n\", %q, short, long, p1+p2)\n\t}\n",
			parse, c.Line, c.Lines, c.Limit, c.Line, 4*c.Lines, c.Limit, c.Name)
	}
	main := strings.NewReplacer("IMPORTS", imports.String(), "CASES", cases.String()).Replace(streamWindowConsumer)
	for name, content := range map[string]string{
		"go.mod":  codegentest.GoModAt("window", windowLib(t, mutate)),
		"main.go": main,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	return streamWindowProblems(t, string(out))
}

// streamWindowProblems reads the lines of the cases, name, the two largest counts and a problem, and
// returns the problems and the counts that rise by more than the slack or fall.
func streamWindowProblems(t *testing.T, out string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(codegentest.StreamWindowCases) {
		t.Fatalf("got %d results, want %d\n%s", len(lines), len(codegentest.StreamWindowCases), out)
	}
	var bad []string
	for _, line := range lines {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("result %q", line)
		}
		short, err1 := strconv.ParseUint(f[1], 10, 64)
		long, err2 := strconv.ParseUint(f[2], 10, 64)
		switch {
		case err1 != nil || err2 != nil || short == 0:
			t.Fatalf("result %q", line)
		case f[3] != "":
			bad = append(bad, f[0]+": "+f[3])
		case long < short || long > short+codegentest.StreamCountSlack:
			bad = append(bad, fmt.Sprintf("%s: the largest count is %d bytes for the shorter stream and %d for the longer", f[0], short, long))
		}
	}
	return bad
}
