package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Go and TypeScript programs of the pages, which call generated parsers, are built and run by
// TestDocumentationExamples. A page shows a program in one of two ways:
//
//   - A file block (`// main.go` or `// main.ts` on its first line, or a `file` marker) and a bash
//     block of program commands after it: lines that start with `go mod init`, `go get`,
//     `go generate`, `go run`, `go build`, `go test`, `go vet`, `go tool`, `node` or `cat` (of a
//     file that the commands wrote), after an optional `printf '...' | ` that gives the input. The
//     text block after the commands is their output.
//   - A block marked `<!-- example: program <name> [module <path>] -->`, saved under name and run
//     (`go run` or `node`). A Go block without a package clause is a fragment: its import lines
//     stay imports, its other lines become the body of main, and "fmt" is imported if it is used.
//     A Go program without a go.mod of the page gets one for module path (example.com/app if the
//     marker names none). The output is the text block after the program, or else the trailing
//     `// ` comments of its lines, one line of output each, where "..." inside a line stands for
//     any text.
//
// Programs run against the worktree: after `go mod init`, and in the go.mod of a fragment, the
// module requires syntaxir.dev/sxir replaced by the repository, so `go get -tool` and `go tool sxir`
// work offline (GOPROXY=off) with the code under test. `go run PKG ARGS` is run as a build of PKG
// and a run of the binary, so that the exit status line of go run is not part of the output. The
// TypeScript runs on node with its type stripping; pages write `node main.ts`.

var (
	exampleProgramLineRe = regexp.MustCompile(`^(printf '[^']*' \| )?(go (mod init|get|generate|run|build|test|vet|tool) |node |cat [^<>|&;]*$)`)
	exampleGoModInitRe   = regexp.MustCompile(`^go mod init\s`)
	exampleGoRunRe       = regexp.MustCompile(`^(printf '[^']*' \| )?go run\s+(\S+)(.*)$`)
	examplePackageRe     = regexp.MustCompile(`^package\s+\w+`)
	exampleImportRe      = regexp.MustCompile(`^import\s+(\(|"[^"]*"|\w+\s+"[^"]*")`)
)

// isProgramBlock reports whether every line of a bash block is a program command.
func isProgramBlock(body []string) bool {
	for _, l := range body {
		if !exampleProgramLineRe.MatchString(l) {
			return false
		}
	}
	return len(body) > 0
}

// programMarker is the content of a program marker.
type programMarker struct{ Name, Module string }

// parseProgramMarker parses "program <name> [module <path>]".
func parseProgramMarker(marker string) programMarker {
	f := strings.Fields(marker)
	p := programMarker{Name: f[1], Module: "example.com/app"}
	if len(f) == 4 && f[2] == "module" {
		p.Module = f[3]
	}
	return p
}

// programText returns the file that a program block is saved as: a Go fragment wrapped in a main
// package, or the block as it is.
func programText(b exampleBlock, p programMarker) string {
	text := strings.Join(b.Body, "\n") + "\n"
	if b.Lang != "go" || slices.ContainsFunc(b.Body, examplePackageRe.MatchString) {
		return text
	}
	var imports, body []string
	inImports := false
	for _, l := range b.Body {
		switch {
		case inImports:
			if strings.TrimSpace(l) == ")" {
				inImports = false
			} else if s := strings.TrimSpace(l); s != "" {
				imports = append(imports, s)
			}
		case exampleImportRe.MatchString(l):
			m := exampleImportRe.FindStringSubmatch(l)
			if m[1] == "(" {
				inImports = true
			} else {
				imports = append(imports, m[1])
			}
		default:
			body = append(body, l)
		}
	}
	if strings.Contains(strings.Join(body, "\n"), "fmt.") && !slices.Contains(imports, `"fmt"`) {
		imports = append([]string{`"fmt"`}, imports...)
	}
	var w strings.Builder
	w.WriteString("package main\n\nimport (\n")
	for _, imp := range imports {
		w.WriteString("\t" + imp + "\n")
	}
	w.WriteString(")\n\nfunc main() {\n")
	for _, l := range body {
		if l != "" {
			l = "\t" + l
		}
		w.WriteString(l + "\n")
	}
	w.WriteString("}\n")
	return w.String()
}

// commentOutput returns the output that a program shows in the trailing `// ` comments of its
// lines.
func commentOutput(body []string) []string {
	var out []string
	for _, l := range body {
		if strings.HasPrefix(strings.TrimSpace(l), "//") {
			continue
		}
		if _, c, ok := strings.Cut(l, " // "); ok {
			out = append(out, strings.TrimSpace(c))
		}
	}
	return out
}

// lineMatches reports whether got is the line want, where "..." in want stands for any text.
func lineMatches(got, want string) bool {
	parts := strings.Split(want, "...")
	if len(parts) == 1 {
		return got == want
	}
	if !strings.HasPrefix(got, parts[0]) {
		return false
	}
	got = got[len(parts[0]):]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(got, p)
		if i < 0 {
			return false
		}
		got = got[i+len(p):]
	}
	return strings.HasSuffix(got, parts[len(parts)-1])
}

// exampleGoEnv returns the Go environment of the repository's toolchain: GOROOT, GOCACHE, GOMODCACHE
// and GOPATH, so that programs build with the toolchain and the caches of the tests.
var exampleGoEnv = sync.OnceValues(func() ([]string, error) {
	cmd := exec.Command("go", "env", "GOROOT", "GOCACHE", "GOMODCACHE", "GOPATH")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go env: %v", err)
	}
	v := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(v) != 4 {
		return nil, fmt.Errorf("go env: %q", out)
	}
	root, err := filepath.Abs("..")
	if err != nil {
		return nil, err
	}
	return []string{
		"PATH=" + filepath.Join(v[0], "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GOROOT=" + v[0], "GOCACHE=" + v[1], "GOMODCACHE=" + v[2], "GOPATH=" + v[3],
		"GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOFLAGS=", "SXIR_DOC_ROOT=" + root,
	}, nil
})

// programEnv returns the environment of the program commands of a page in dir.
func programEnv(bin, dir string) ([]string, error) {
	goEnv, err := exampleGoEnv()
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(), goEnv...)
	// The built sxir comes first on the path, before the toolchain.
	for i, e := range env {
		if p, ok := strings.CutPrefix(e, "PATH="); ok {
			env[i] = "PATH=" + filepath.Dir(bin) + string(os.PathListSeparator) + p
		}
	}
	return append(env, "HOME="+dir), nil
}

// runProgramLine runs one shell line in dir and returns its standard output and standard error
// together. A nonzero exit status is part of what a page shows, so it is not an error.
func runProgramLine(env []string, dir, line string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", line)
	cmd.Dir, cmd.Env = dir, env
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if ctx.Err() != nil {
		return buf.String(), ctx.Err()
	}
	if _, ok := err.(*exec.ExitError); ok {
		err = nil
	}
	return buf.String(), err
}

// requireWorktree makes the module in dir require syntaxir.dev/sxir from the repository.
func requireWorktree(env []string, dir string) error {
	out, err := runProgramLine(env, dir, `go mod edit -require=syntaxir.dev/sxir@v0.0.0-00010101000000-000000000000 -replace="syntaxir.dev/sxir=$SXIR_DOC_ROOT"`)
	if err != nil || strings.TrimSpace(out) != "" {
		return fmt.Errorf("go mod edit: %v %s", err, out)
	}
	return nil
}

// runProgramCommands runs the program commands of a bash block in dir, in order, and returns their
// output.
func runProgramCommands(t *testing.T, bin, dir string, lines []string) (string, error) {
	env, err := programEnv(bin, dir)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, line := range lines {
		if m := exampleGoRunRe.FindStringSubmatch(line); m != nil {
			prog := filepath.Join(t.TempDir(), "program")
			build, err := runProgramLine(env, dir, "go build -o '"+prog+"' "+m[2])
			if err != nil {
				return out.String(), err
			}
			if _, err := os.Stat(prog); err != nil {
				return out.String() + build, nil // the build failed; its errors are the output
			}
			line = m[1] + "'" + prog + "'" + m[3]
		}
		got, err := runProgramLine(env, dir, line)
		out.WriteString(got)
		if err != nil {
			return out.String(), err
		}
		if exampleGoModInitRe.MatchString(line) {
			if err := requireWorktree(env, dir); err != nil {
				return out.String(), err
			}
		}
	}
	return out.String(), nil
}

// checkProgram runs the program of block i, which a program marker names, and compares its output
// with the output that the page shows.
func checkProgram(t *testing.T, bin, dir, at string, b exampleBlock, blocks []exampleBlock, i int) {
	t.Helper()
	p := parseProgramMarker(b.Marker)
	env, err := programEnv(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	var command string
	switch b.Lang {
	case "go":
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			if out, err := runProgramLine(env, dir, "go mod init "+p.Module); err != nil {
				t.Fatalf("%s: go mod init: %v %s", at, err, out)
			}
			if err := requireWorktree(env, dir); err != nil {
				t.Fatalf("%s: %v", at, err)
			}
		}
		command = "go run " + p.Name
	case "ts":
		command = "node " + p.Name
	default:
		t.Fatalf("%s: a program is a go or ts block, not %q", at, b.Lang)
	}
	got, err := runProgramCommands(t, bin, dir, []string{command})
	if err != nil {
		t.Errorf("%s: %s: %v", at, command, err)
		return
	}
	if i+1 < len(blocks) && blocks[i+1].Lang == "text" {
		checkShownOutput(t, at, command, got, blocks, i, nil)
		return
	}
	want := commentOutput(b.Body)
	if len(want) == 0 {
		t.Errorf("%s: the program shows no output: no text block after it and no trailing comments", at)
		return
	}
	gotLines := strings.Split(trimOutput(got), "\n")
	ok := len(gotLines) == len(want)
	for j := 0; ok && j < len(want); j++ {
		ok = lineMatches(gotLines[j], want[j])
	}
	if !ok {
		t.Errorf("%s: %s\ngot:\n%s\nwant (from the comments):\n%s", at, command, trimOutput(got), strings.Join(want, "\n"))
	}
}

func TestIsProgramBlock(t *testing.T) {
	for _, c := range []struct {
		body []string
		ok   bool
	}{
		{[]string{"go mod init example.com/ship", "go get -tool syntaxir.dev/sxir/cmd/sxir"}, true},
		{[]string{"go generate ./...", "go run . 1.2.3"}, true},
		{[]string{"node main.ts   # Node.js 22.18+"}, true},
		{[]string{"go install syntaxir.dev/sxir/cmd/sxir@latest"}, false},
		{[]string{"go run . 1", "sxir parse -g a.sxir"}, false},
		{[]string{"nodemon main.ts"}, false},
		{[]string{`printf 'a=1\n' | go run .`}, true},
		{[]string{`printf 'a=1\n' | sxir parse -g a.sxir`}, false},
		{[]string{"cat testdata/basic.golden"}, true},
		{[]string{"go test -update .", "cat testdata/basic.golden"}, true},
		{[]string{"catalog x"}, false},
		{[]string{"cat > a.txt <<'EOF'", "a", "EOF"}, false},
		{nil, false},
	} {
		if got := isProgramBlock(c.body); got != c.ok {
			t.Errorf("isProgramBlock(%q) = %v, want %v", c.body, got, c.ok)
		}
	}
}

func TestProgramMarker(t *testing.T) {
	blocks := exampleBlocks([]string{"<!-- example: program main.go module example.com/app -->", "```go", "x := 1", "```",
		"<!-- example: program check.ts -->", "```ts", "```"})
	if len(blocks) != 2 {
		t.Fatalf("exampleBlocks found %d blocks, want 2", len(blocks))
	}
	if got, want := parseProgramMarker(blocks[0].Marker), (programMarker{"main.go", "example.com/app"}); got != want {
		t.Errorf("first marker = %+v, want %+v", got, want)
	}
	if got, want := parseProgramMarker(blocks[1].Marker), (programMarker{"check.ts", "example.com/app"}); got != want {
		t.Errorf("second marker = %+v, want %+v", got, want)
	}
	name, _, ok := fileOfBlock(blocks, 0, map[string]string{})
	if !ok || name != "main.go" {
		t.Errorf("a program block saves %q, %v; want main.go", name, ok)
	}
	sources := exampleBlocks([]string{"```go", "// main.go", "package main", "```", "```ts", "// check.ts", "```", "```go", "// Code generated by sxir. DO NOT EDIT.", "```"})
	for i, want := range []string{"main.go", "check.ts", ""} {
		if name, _, _ := fileOfBlock(sources, i, map[string]string{}); name != want {
			t.Errorf("block %d saves %q, want %q", i, name, want)
		}
	}
}

func TestGoFragmentProgram(t *testing.T) {
	b := exampleBlock{Lang: "go", Body: []string{
		`import "example.com/app/calc"`,
		``,
		`node, err := calc.Parse("1")`,
		`fmt.Println(node, err) // Num("1") <nil>`,
	}}
	want := "package main\n\nimport (\n\t\"fmt\"\n\t\"example.com/app/calc\"\n)\n\nfunc main() {\n\n\tnode, err := calc.Parse(\"1\")\n\tfmt.Println(node, err) // Num(\"1\") <nil>\n}\n"
	if got := programText(b, programMarker{"main.go", "example.com/app"}); got != want {
		t.Errorf("programText:\n%s\nwant\n%s", got, want)
	}
	grouped := exampleBlock{Lang: "go", Body: []string{"import (", `	"errors"`, `	"fmt"`, ")", "fmt.Println(errors.New(\"x\"))"}}
	if got := programText(grouped, programMarker{"main.go", ""}); !strings.Contains(got, "import (\n\t\"errors\"\n\t\"fmt\"\n)") || strings.Count(got, `"fmt"`) != 1 {
		t.Errorf("programText of a grouped import:\n%s", got)
	}
	whole := exampleBlock{Lang: "go", Body: []string{"package main", "func main() {}"}}
	if got := programText(whole, programMarker{"main.go", ""}); got != "package main\nfunc main() {}\n" {
		t.Errorf("a complete program is changed:\n%s", got)
	}
	ts := exampleBlock{Lang: "ts", Body: []string{`console.log(1); // 1`}}
	if got := programText(ts, programMarker{"main.ts", ""}); got != "console.log(1); // 1\n" {
		t.Errorf("a TypeScript program is changed:\n%s", got)
	}
}

func TestCommentOutput(t *testing.T) {
	got := commentOutput([]string{"// a comment line", `fmt.Println(node, err) // Bin{Left: Num("1"), Right: Bin{...}} <nil>`, "x := 1", `console.log(e); // null`})
	want := []string{`Bin{Left: Num("1"), Right: Bin{...}} <nil>`, "null"}
	if !slices.Equal(got, want) {
		t.Errorf("commentOutput = %q, want %q", got, want)
	}
	for _, c := range []struct {
		got, want string
		ok        bool
	}{
		{`Bin{Left: Num("1"), Right: Bin{Op: "*"}} <nil>`, `Bin{Left: Num("1"), Right: Bin{...}} <nil>`, true},
		{`Bin{Left: Num("2"), Right: Bin{Op: "*"}} <nil>`, `Bin{Left: Num("1"), Right: Bin{...}} <nil>`, false},
		{`Bin{Left: Num("1"), Right: Bin{Op: "*"}} err`, `Bin{Left: Num("1"), Right: Bin{...}} <nil>`, false},
		{"null", "null", true},
		{"null x", "null", false},
		{"a b c", "a...c", true},
		{"ac", "a...b...c", false},
	} {
		if got := lineMatches(c.got, c.want); got != c.ok {
			t.Errorf("lineMatches(%q, %q) = %v, want %v", c.got, c.want, got, c.ok)
		}
	}
}

// pageInRepository reports whether the page marks its programs as run in the repository, with
// `<!-- example: repository -->` anywhere on it.
func pageInRepository(t *testing.T, rel string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return slices.ContainsFunc(strings.Split(string(data), "\n"), func(l string) bool {
		return exampleRepoRe.MatchString(strings.TrimSpace(l))
	})
}

// repositoryModule stands in for the repository in dir, for a page whose programs run in the
// repository: a module that requires syntaxir.dev/sxir from the checkout. The paths of the
// repository that a command names are linked by exampleCommandPaths.
func repositoryModule(t *testing.T, bin, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return
	}
	env, err := programEnv(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runProgramLine(env, dir, "go mod init example.com/repository"); err != nil {
		t.Fatalf("go mod init: %v %s", err, out)
	}
	if err := requireWorktree(env, dir); err != nil {
		t.Fatal(err)
	}
}
