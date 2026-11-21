package mutanttest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// A Mutant is one broken variant of source files and the tests that must fail with it.
type Mutant struct {
	// Replace maps the absolute path of each mutated file to its mutated contents.
	Replace map[string]string
	// Pkgs are the go test package arguments, resolved in the directory of the Batch, and Run is the -run pattern.
	Pkgs []string
	Run  string
}

// SchemaEnv names the environment variable that selects the mutant of a schema binary; unset or 0 is the code as
// it is.
const SchemaEnv = "SXIR_MUTANT_SCHEMA_ID"

// schemaVar is the package variable that holds the selected mutant in a schema build.
const schemaVar = "sxirMutantSchemaID"

// A Batch runs the tests of a set of mutants with one build per package instead of one per mutant, through
// mutant schemata: every function that a mutant changes is compiled in twice, as it is and as the mutant has it
// (a copy named <name>__sxirm<k>), and a switch on the first line of the original body calls the copy of the mutant
// that SchemaEnv selects. The switch is on the line of the opening brace, so the code as it is keeps its line
// numbers. Each package is built once with go test -c over an overlay that holds the schemata, and each mutant runs
// the test binaries with SchemaEnv set, as go test would run them.
//
// A mutant whose change the schema cannot express (a change outside a function body, in a signature, or in a
// function whose parameters cannot be forwarded) runs as before, with go test over an overlay of its own files, and
// so does every mutant of a package whose schema build fails. Both ways run the same tests on the same code, so a
// mutant that changes nothing still passes them and is still reported as surviving.
type Batch struct {
	dir     string
	env     []string
	base    map[string]string
	mutants []Mutant
	tmp     string
	schema  []bool            // schema[i]: mutant i runs from the schema binaries
	bins    map[string]string // package argument -> schema test binary
	pkgDirs map[string]string // package argument -> its directory, where go test runs its binary
	mu      sync.Mutex
}

// NewBatch prepares the mutants: dir is the working directory of the go commands (empty for the test's own), env
// their environment, and base the overlay replacements (absolute path to replacement file) that every mutant
// builds with. It builds the schema binaries, concurrently per package, before it returns.
func NewBatch(t testing.TB, dir string, env []string, base map[string]string, mutants []Mutant) *Batch {
	t.Helper()
	b := &Batch{dir: dir, env: env, base: base, mutants: mutants, tmp: t.TempDir(),
		schema: make([]bool, len(mutants)), bins: map[string]string{}, pkgDirs: map[string]string{}}
	files := map[string]*schemaFile{}
	for i, m := range mutants {
		ok := true
		var changed []*schemaFile
		for path, text := range m.Replace {
			f := files[path]
			if f == nil {
				orig, err := b.original(path)
				if err != nil {
					t.Fatal(err)
				}
				f = newSchemaFile(orig)
				files[path] = f
			}
			if f.err != nil || !f.add(i+1, text) {
				ok = false
				break
			}
			changed = append(changed, f)
		}
		b.schema[i] = ok
		if !ok {
			for _, f := range changed {
				f.drop(i + 1)
			}
		}
	}
	overlay := map[string]string{}
	for k, v := range base {
		overlay[k] = v
	}
	pkgNames := map[string]string{} // directory -> package name of its schema files
	n := 0
	for path, f := range files {
		if len(f.mutants) == 0 {
			continue
		}
		text, err := f.text()
		if err != nil {
			t.Logf("mutant schemata: %s: %v; its mutants build one by one", path, err)
			for k := range f.mutants {
				b.schema[k-1] = false
			}
			continue
		}
		n++
		out := filepath.Join(b.tmp, fmt.Sprintf("schema_%d_%s", n, filepath.Base(path)))
		if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		overlay[path] = out
		pkgNames[filepath.Dir(path)] = f.pkg
	}
	for d, name := range pkgNames {
		out := filepath.Join(b.tmp, fmt.Sprintf("schema_id_%s.go", strings.ReplaceAll(strings.Trim(d, string(filepath.Separator)), string(filepath.Separator), "_")))
		src := fmt.Sprintf("package %s\n\nimport (\n\t\"os\"\n\t\"strconv\"\n)\n\n// %s selects the mutant of the schema (internal/testing/mutanttest).\nvar %s, _ = strconv.Atoi(os.Getenv(%q))\n", name, schemaVar, schemaVar, SchemaEnv)
		if err := os.WriteFile(out, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		overlay[filepath.Join(d, "zz_mutant_schema_id.go")] = out
	}
	ov := filepath.Join(b.tmp, "schema.json")
	if err := writeOverlay(ov, overlay); err != nil {
		t.Fatal(err)
	}
	var pkgs []string
	for i, m := range mutants {
		if b.schema[i] {
			for _, p := range m.Pkgs {
				if !slices.Contains(pkgs, p) {
					pkgs = append(pkgs, p)
				}
			}
		}
	}
	var wg sync.WaitGroup
	for j, p := range pkgs {
		wg.Go(func() {
			bin := filepath.Join(b.tmp, fmt.Sprintf("schema_%d.test", j))
			out, err := b.goCmd("test", "-c", "-vet=off", "-overlay", ov, "-o", bin, p).CombinedOutput()
			if err == nil {
				if _, err = os.Stat(bin); err != nil {
					err = fmt.Errorf("go test -c wrote no binary (no test files?): %w", err)
				}
			}
			var dir []byte
			if err == nil {
				dir, err = b.goCmd("list", "-f", "{{.Dir}}", p).Output()
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			if err != nil {
				t.Logf("mutant schemata: the schema build of %s failed (%v); its mutants build one by one:\n%s", p, err, out)
				return
			}
			b.bins[p] = bin
			b.pkgDirs[p] = strings.TrimSpace(string(dir))
		})
	}
	wg.Wait()
	for i, m := range mutants {
		for _, p := range m.Pkgs {
			if b.bins[p] == "" {
				b.schema[i] = false
			}
		}
	}
	return b
}

// Schema reports whether mutant i runs from the schema binaries.
func (b *Batch) Schema(i int) bool { return b.schema[i] }

// Test runs the tests of mutant i and returns their combined output and the error of the run, as go test -count=1
// -vet=off -overlay <the mutant> -run <Run> <Pkgs> would: an error when a test failed or the tests did not build.
func (b *Batch) Test(i int) ([]byte, error) {
	m := b.mutants[i]
	if !b.schema[i] {
		overlay := map[string]string{}
		for k, v := range b.base {
			overlay[k] = v
		}
		for path, text := range m.Replace {
			f := filepath.Join(b.tmp, fmt.Sprintf("mutant_%d_%s", i, filepath.Base(path)))
			if err := os.WriteFile(f, []byte(text), 0o644); err != nil {
				return nil, err
			}
			overlay[path] = f
		}
		ov := filepath.Join(b.tmp, fmt.Sprintf("mutant_%d.json", i))
		if err := writeOverlay(ov, overlay); err != nil {
			return nil, err
		}
		return b.goCmd(append([]string{"test", "-count=1", "-vet=off", "-overlay", ov, "-run", m.Run}, m.Pkgs...)...).CombinedOutput()
	}
	var all bytes.Buffer
	var errs []error
	for _, p := range m.Pkgs {
		// The flags that go test passes to a test binary, with its default timeout.
		cmd := exec.Command(b.bins[p], "-test.paniconexit0", "-test.timeout=10m0s", "-test.count=1", "-test.run="+m.Run)
		cmd.Dir = b.pkgDirs[p]
		cmd.Env = append(slices.Clip(b.environ()), SchemaEnv+"="+strconv.Itoa(i+1))
		out, err := cmd.CombinedOutput()
		all.Write(out)
		if err != nil {
			fmt.Fprintf(&all, "FAIL\t%s\n", p)
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		} else {
			fmt.Fprintf(&all, "ok  \t%s\n", p)
		}
	}
	return all.Bytes(), errors.Join(errs...)
}

func (b *Batch) environ() []string {
	if b.env != nil {
		return b.env
	}
	return os.Environ()
}

func (b *Batch) goCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = b.dir
	cmd.Env = b.environ()
	return cmd
}

// original returns the contents that path has in every build of the batch: its base replacement, or the file.
func (b *Batch) original(path string) (string, error) {
	if r, ok := b.base[path]; ok {
		path = r
	}
	src, err := os.ReadFile(path)
	return string(src), err
}

func writeOverlay(path string, replace map[string]string) error {
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": replace})
	if err != nil {
		return err
	}
	return os.WriteFile(path, overlay, 0o644)
}

// A schemaFile is one source file and the mutants of its functions.
type schemaFile struct {
	src   string
	pkg   string
	funcs []*ast.FuncDecl
	fset  *token.FileSet
	err   error
	// mutants maps a mutant number to the mutated texts of the functions it changes, by index in funcs.
	mutants map[int]map[int]string
}

func newSchemaFile(src string) *schemaFile {
	f := &schemaFile{src: src, fset: token.NewFileSet(), mutants: map[int]map[int]string{}}
	file, err := parser.ParseFile(f.fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		f.err = err
		return f
	}
	f.pkg = file.Name.Name
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			f.funcs = append(f.funcs, fd)
		}
	}
	return f
}

// add records mutant k, whose text of the whole file is mutated, if the schema can express it: the two texts must
// be equal outside the bodies of the same functions, and every function whose body differs must be one whose call
// the switch can forward. It reports whether it recorded the mutant.
func (f *schemaFile) add(k int, mutated string) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", mutated, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	var funcs []*ast.FuncDecl
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			funcs = append(funcs, fd)
		}
	}
	if len(funcs) != len(f.funcs) || stripBodies(f.src, f.fset, f.funcs) != stripBodies(mutated, fset, funcs) {
		return false
	}
	changed := map[int]string{}
	for j, fd := range f.funcs {
		ob := f.src[f.fset.Position(fd.Body.Lbrace).Offset : f.fset.Position(fd.Body.Rbrace).Offset+1]
		md := funcs[j]
		mb := mutated[fset.Position(md.Body.Lbrace).Offset : fset.Position(md.Body.Rbrace).Offset+1]
		if ob == mb {
			continue
		}
		if _, ok := forwardCall(fd, ""); !ok {
			return false
		}
		start := fset.Position(md.Type.Func).Offset
		if md.Type.Func == token.NoPos {
			return false
		}
		name := fset.Position(md.Name.Pos()).Offset
		end := fset.Position(md.Body.Rbrace).Offset + 1
		changed[j] = mutated[start:name] + copyName(md.Name.Name, k) + mutated[name+len(md.Name.Name):end]
	}
	f.mutants[k] = changed
	return true
}

// drop removes mutant k, which runs one by one after all.
func (f *schemaFile) drop(k int) { delete(f.mutants, k) }

// text returns the schema: the file with a switch at the start of every function that a mutant changes, and the
// mutated copies after it.
func (f *schemaFile) text() (string, error) {
	type insert struct {
		at   int
		text string
	}
	var ins []insert
	var copies strings.Builder
	ks := make([]int, 0, len(f.mutants))
	for k := range f.mutants {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	for j, fd := range f.funcs {
		var sw strings.Builder
		for _, k := range ks {
			text, ok := f.mutants[k][j]
			if !ok {
				continue
			}
			call, _ := forwardCall(fd, copyName(fd.Name.Name, k))
			fmt.Fprintf(&sw, " case %d: %s;", k, call)
			copies.WriteString("\n\n" + text)
		}
		if sw.Len() > 0 {
			ins = append(ins, insert{f.fset.Position(fd.Body.Lbrace).Offset + 1, " switch " + schemaVar + " {" + sw.String() + " };"})
		}
	}
	var out strings.Builder
	last := 0
	for _, in := range ins {
		out.WriteString(f.src[last:in.at])
		out.WriteString(in.text)
		last = in.at
	}
	out.WriteString(f.src[last:])
	out.WriteString(copies.String())
	out.WriteString("\n")
	return out.String(), nil
}

func copyName(name string, k int) string { return fmt.Sprintf("%s__sxirm%d", name, k) }

// forwardCall returns the statement that calls name with the receiver, type parameters and parameters of fd and
// returns its results; ok is false when fd cannot forward them (init, or a receiver or parameter without a name).
func forwardCall(fd *ast.FuncDecl, name string) (string, bool) {
	if fd.Name.Name == "init" && fd.Recv == nil {
		return "", false
	}
	call := name
	if fd.Recv != nil {
		if len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 || fd.Recv.List[0].Names[0].Name == "_" {
			return "", false
		}
		call = fd.Recv.List[0].Names[0].Name + "." + name
	}
	if tp := fd.Type.TypeParams; tp != nil && len(tp.List) > 0 {
		var names []string
		for _, f := range tp.List {
			for _, n := range f.Names {
				names = append(names, n.Name)
			}
		}
		call += "[" + strings.Join(names, ", ") + "]"
	}
	var args []string
	for _, p := range fd.Type.Params.List {
		if len(p.Names) == 0 {
			return "", false
		}
		for _, n := range p.Names {
			if n.Name == "_" {
				return "", false
			}
			a := n.Name
			if _, ok := p.Type.(*ast.Ellipsis); ok {
				a += "..."
			}
			args = append(args, a)
		}
	}
	call += "(" + strings.Join(args, ", ") + ")"
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		return "return " + call, true
	}
	return call + "; return", true
}

// stripBodies returns src without the bodies of funcs, to compare the rest of two texts.
func stripBodies(src string, fset *token.FileSet, funcs []*ast.FuncDecl) string {
	var out strings.Builder
	last := 0
	for _, fd := range funcs {
		lb := fset.Position(fd.Body.Lbrace).Offset
		out.WriteString(src[last:lb])
		out.WriteString("{}")
		last = fset.Position(fd.Body.Rbrace).Offset + 1
	}
	out.WriteString(src[last:])
	return out.String()
}
