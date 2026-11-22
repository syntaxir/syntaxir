package archtest

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The tests here check the rules of the pass pipeline that Go cannot enforce (record 074, "What
// Makes the Wrong Shape Impossible"): which packages execute a pipeline, what the driver exports,
// which functions the packages that declare passes export, and who sets the printers' test hooks.
// They read the non-test Go files of the module, under every build tag but ignore, with the parser
// alone: the rules are about names and calls that the syntax shows. A file built only under the
// ignore tag is a program of its own, such as a go generate program, and no part of the package of
// its directory.

// ignored reports whether the build constraint of the file f, parsed with its comments, is the tag
// ignore: whether it is a program of its own, which go run or go generate runs.
func ignored(f *ast.File) bool {
	for _, g := range f.Comments {
		if g.Pos() > f.Package {
			break
		}
		for _, c := range g.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}
			x, err := constraint.Parse(c.Text)
			tag, ok := x.(*constraint.TagExpr)
			return err == nil && ok && tag.Tag == "ignore"
		}
	}
	return false
}

// pipePath is the pass manager, whose Execute runs a pipeline.
const pipePath = modulePath + "/internal/pass/pipe"

// A sourcePackage is a package of the module: its import path and its non-test files, with their
// names (fileNames[i] is the base name of files[i]).
type sourcePackage struct {
	path      string
	name      string
	files     []*ast.File
	fileNames []string
}

// loadSources parses the non-test Go files of the packages under root, whose import paths start
// with module: every directory but testdata, hidden ones and those of another module, and every file
// but those built only under the ignore tag (ignored).
func loadSources(root, module string) (map[string]*sourcePackage, error) {
	pkgs := map[string]*sourcePackage{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if p != root && (n == "testdata" || n == "node_modules" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_")) {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		head, err := parser.ParseFile(fset, p, nil, parser.PackageClauseOnly|parser.ParseComments)
		if err != nil {
			return err
		}
		if ignored(head) {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		ip := module
		if dir != "." {
			ip = module + "/" + filepath.ToSlash(dir)
		}
		sp := pkgs[ip]
		if sp == nil {
			sp = &sourcePackage{path: ip, name: f.Name.Name}
			pkgs[ip] = sp
		}
		sp.files = append(sp.files, f)
		sp.fileNames = append(sp.fileNames, filepath.Base(p))
		return nil
	})
	return pkgs, err
}

// importNames returns the names under which the file f refers to the packages it imports, by
// import path, with the names of the packages of pkgs for an import without one.
func importNames(f *ast.File, pkgs map[string]*sourcePackage) map[string]string {
	names := map[string]string{}
	for _, s := range f.Imports {
		p, err := strconv.Unquote(s.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if sp, ok := pkgs[p]; ok {
			name = sp.name
		}
		if s.Name != nil {
			name = s.Name.Name
		}
		names[name] = p
	}
	return names
}

// relPath returns the import path p relative to the module.
func relPath(p, module string) string {
	if p == module {
		return "."
	}
	return strings.TrimPrefix(p, module+"/")
}

// executors returns the packages of pkgs that call the Execute of the manager (pipePath), other
// than the manager's own packages, the driver itself and test support: those may run pipelines.
func executors(pkgs map[string]*sourcePackage, module, pipe string) []string {
	var out []string
	for _, p := range slices.Sorted(maps.Keys(pkgs)) {
		r := relPath(p, module)
		if r == "internal/pass" || strings.HasPrefix(r, "internal/pass/") || r == "internal/driver" || strings.HasPrefix(r, "internal/testing/") {
			continue
		}
		found := false
		for _, f := range pkgs[p].files {
			names := importNames(f, pkgs)
			ast.Inspect(f, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok && isQualified(c.Fun, names, pipe, "Execute") {
					found = true
				}
				return !found
			})
		}
		if found {
			out = append(out, r)
		}
	}
	return out
}

// isQualified reports whether e names the identifier name of the package at path, under the names
// of a file's imports.
func isQualified(e ast.Expr, names map[string]string, path, name string) bool {
	if x, ok := e.(*ast.IndexExpr); ok {
		e = x.X
	}
	if x, ok := e.(*ast.IndexListExpr); ok {
		e = x.X
	}
	s, ok := e.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != name {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && names[id.Name] == path
}

// pipelineExecutors are the packages outside the driver that execute a pipeline today, each with
// the step of record 074 that removes it. The list must match exactly, so it only shrinks.
var pipelineExecutors = map[string]string{
	// corepass.Run runs the Core passes for the VM's and the closure backend's builds (vmlower,
	// closure) until migration step 12 compiles them through the driver, and for the tests.
	"internal/middleend/corepass": "step 12",
	// flowpass.Run runs the Flow passes on a module for the tests of the Flow passes and of the
	// lowering, which have no compilation to run them in. It stays in the package because the
	// package's own internal tests use it, and a shared helper in test support would import
	// flowpass, which those internal tests cannot import back without an import cycle.
	"internal/middleend/flowpass": "tests",
}

// TestOnlyTheDriverExecutesPipelines checks that outside internal/pass, the driver package itself
// (not its subpackages) and test support, no non-test package executes a pipeline, but the packages
// of pipelineExecutors: every other caller of a compilation goes through Compile.
func TestOnlyTheDriverExecutesPipelines(t *testing.T) {
	pkgs, err := loadSources(moduleRoot, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	got := executors(pkgs, modulePath, pipePath)
	for _, p := range got {
		if _, ok := pipelineExecutors[p]; !ok {
			t.Errorf("%s executes a pipeline (pipe.Execute); compile through the driver instead", p)
		}
	}
	for _, p := range slices.Sorted(maps.Keys(pipelineExecutors)) {
		if !slices.Contains(got, p) {
			t.Errorf("%s no longer executes a pipeline: remove it from pipelineExecutors", p)
		}
	}
}

// driverExports are the identifiers that the package internal/driver may export: the compilation
// API, the slot keys of its edits and the vocabulary of the settings. A new export fails the test.
var driverExports = []string{
	// The compilation API.
	"Backend", "Check", "CheckWith", "Compile", "CompileOptions", "Compiler", "Describe", "DescribeGoLibrary",
	"GoLibrary", "Options", "Output", "Program", "ProgramOf", "Target", "View",
	// The roles of the run-time programs of a grammar (ProgramOf).
	"Insertion", "Parser", "Recognizer", "Role",
	// The slot keys.
	"CoreSlot", "ExecSlot", "FlowSlot", "TargetSlot",
	// The settings: profiles, their documentation and values, the parsing of overrides and dump
	// selections, the names of the steps, and the check of the registry against them.
	"CheckDebug", "CheckOptimizable", "CheckPrintAfter", "CheckPrintBefore", "CheckRegistry", "Elaborate",
	"GenSettings", "ParsePrintAfter", "ParsePrintBefore", "ParseValues", "PrintAll", "PrintBeforeNames",
	"PrintNames", "Profile", "ProfileDoc", "ProfileValues", "Profiles",
}

// exports returns the package-level identifiers that the package exports, sorted.
func exports(p *sourcePackage) []string {
	var out []string
	add := func(id *ast.Ident) {
		if id.IsExported() && !slices.Contains(out, id.Name) {
			out = append(out, id.Name)
		}
	}
	for _, f := range p.files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					add(d.Name)
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						add(s.Name)
					case *ast.ValueSpec:
						for _, n := range s.Names {
							add(n)
						}
					}
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// exportDiff returns the identifiers of got that allowed lacks and those of allowed that got lacks.
func exportDiff(got, allowed []string) (extra, missing []string) {
	for _, n := range got {
		if !slices.Contains(allowed, n) {
			extra = append(extra, n)
		}
	}
	for _, n := range allowed {
		if !slices.Contains(got, n) {
			missing = append(missing, n)
		}
	}
	return extra, missing
}

// TestDriverExportsOnlyItsAPI checks that internal/driver exports driverExports exactly. Its
// subpackages keep their own exports, which the import rules cover.
func TestDriverExportsOnlyItsAPI(t *testing.T) {
	pkgs, err := loadSources(moduleRoot, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	p := pkgs[modulePath+"/internal/driver"]
	if p == nil {
		t.Fatal("no package internal/driver")
	}
	extra, missing := exportDiff(exports(p), driverExports)
	for _, n := range extra {
		t.Errorf("internal/driver exports %s, which is not in driverExports", n)
	}
	for _, n := range missing {
		t.Errorf("internal/driver no longer exports %s: remove it from driverExports", n)
	}
}

// runExports returns, for each package of pkgs, the exported functions that the run function of a
// pass declared in the package names (the function itself, or one it refers to), and the exported
// package-level variables that hold a pass definition (pipe.Def), as "path.Name". A pass is reached
// only through the manager, so the package that declares it exports the pass value and its data
// types, not the code that the pass runs.
func runExports(pkgs map[string]*sourcePackage, module, pipe string) []string {
	var out []string
	for _, p := range slices.Sorted(maps.Keys(pkgs)) {
		sp := pkgs[p]
		funcs := map[string]*ast.FuncDecl{}
		for _, f := range sp.files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
					funcs[fd.Name.Name] = fd
				}
			}
		}
		found := map[string]bool{}
		// names records the exported functions of the package that body refers to.
		names := func(body ast.Node) {
			ast.Inspect(body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.IsExported() && funcs[id.Name] != nil {
					found[id.Name] = true
				}
				if s, ok := n.(*ast.SelectorExpr); ok {
					// A selector names a field, a method or another package's function: only its
					// operand can name a function of this package.
					ast.Inspect(s.X, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok && id.IsExported() && funcs[id.Name] != nil {
							found[id.Name] = true
						}
						return true
					})
					return false
				}
				return true
			})
		}
		for _, f := range sp.files {
			imports := importNames(f, pkgs)
			isDef := func(e ast.Expr) bool { return isQualified(e, imports, pipe, "Def") }
			for _, d := range f.Decls {
				if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.VAR {
					for _, s := range g.Specs {
						vs := s.(*ast.ValueSpec)
						for i, n := range vs.Names {
							if !n.IsExported() {
								continue
							}
							if cl, ok := unparen(valueAt(vs, i)).(*ast.CompositeLit); ok && isDef(cl.Type) {
								found[n.Name] = true
							}
						}
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok || !isDef(cl.Type) {
					return true
				}
				for _, e := range cl.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); !ok || (k.Name != "Run" && k.Name != "Applies") {
						continue
					}
					switch v := unparen(kv.Value).(type) {
					case *ast.FuncLit:
						names(v.Body)
					case *ast.Ident:
						if v.IsExported() {
							found[v.Name] = true
						}
						if fd := funcs[v.Name]; fd != nil && fd.Body != nil {
							names(fd.Body)
						}
					}
				}
				return true
			})
		}
		for _, n := range slices.Sorted(maps.Keys(found)) {
			out = append(out, relPath(p, module)+"."+n)
		}
	}
	return out
}

// valueAt returns the value of the i-th name of the specification, or nil.
func valueAt(vs *ast.ValueSpec, i int) ast.Expr {
	if i < len(vs.Values) {
		return vs.Values[i]
	}
	return nil
}

// unparen strips the parentheses around e.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// TestPassesExportNoRunFunction checks that no package exports a pass definition or a function that
// the run function of a pass that it declares names; tests run a pass through pipe.Execute.
func TestPassesExportNoRunFunction(t *testing.T) {
	pkgs, err := loadSources(moduleRoot, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range runExports(pkgs, modulePath, pipePath) {
		t.Errorf("%s is exported and named by the run function of a pass of its package; keep it unexported and run the pass through pipe.Execute in tests", n)
	}
}

// hookSetters returns the places in pkgs, outside test support, that set the test hooks of a
// target of the driver (driver.Target.GoHooks and TSHooks) other than to nil, or that pass hooks
// other than nil to the generators of package generate (GoWith, TypeScriptWith), which set them: the
// hooks are for tests, and production leaves them nil. generate itself sets them from its parameter.
func hookSetters(pkgs map[string]*sourcePackage, module string) []string {
	generatePath := module + "/internal/driver/generate"
	var out []string
	for _, p := range slices.Sorted(maps.Keys(pkgs)) {
		r := relPath(p, module)
		if strings.HasPrefix(r, "internal/testing/") {
			continue
		}
		for _, f := range pkgs[p].files {
			imports := importNames(f, pkgs)
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.KeyValueExpr:
					if k, ok := n.Key.(*ast.Ident); ok && (k.Name == "GoHooks" || k.Name == "TSHooks") && !isNil(n.Value) && p != generatePath {
						out = append(out, fmt.Sprintf("%s: sets %s", r, k.Name))
					}
				case *ast.AssignStmt:
					for i, l := range n.Lhs {
						if s, ok := l.(*ast.SelectorExpr); ok && (s.Sel.Name == "GoHooks" || s.Sel.Name == "TSHooks") && (i >= len(n.Rhs) || !isNil(n.Rhs[i])) {
							out = append(out, fmt.Sprintf("%s: sets %s", r, s.Sel.Name))
						}
					}
				case *ast.CallExpr:
					for _, name := range []string{"GoWith", "TypeScriptWith"} {
						local := p == generatePath && isIdent(n.Fun, name)
						if (local || isQualified(n.Fun, imports, generatePath, name)) && len(n.Args) > 0 && !isNil(n.Args[len(n.Args)-1]) {
							out = append(out, fmt.Sprintf("%s: passes hooks to generate.%s", r, name))
						}
					}
				}
				return true
			})
		}
	}
	return out
}

// isNil reports whether e is the identifier nil.
func isNil(e ast.Expr) bool { return isIdent(unparen(e), "nil") }

// isIdent reports whether e is the identifier name.
func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// TestOnlyTestsSetPrinterHooks checks that only tests and test support set the hooks of the Go and
// TypeScript printers (driver.Target.GoHooks, TSHooks), directly or through package generate.
func TestOnlyTestsSetPrinterHooks(t *testing.T) {
	pkgs, err := loadSources(moduleRoot, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	if pkgs[modulePath+"/internal/driver/generate"] == nil {
		t.Fatal("no package internal/driver/generate")
	}
	for _, s := range hookSetters(pkgs, modulePath) {
		t.Errorf("%s; the printer hooks are for tests only", s)
	}
}

// TestPipelineRulesNegativeControls runs the checks of this file on the packages of
// testdata/pipeline, a module example.com/pipe of fakes that breaks each rule once: the manager
// (pipe), a package that executes a pipeline and exports a run function and a pass definition, a
// driver with an export of its own, and a command that sets the printer hooks and passes them to
// generate.
func TestPipelineRulesNegativeControls(t *testing.T) {
	const module = "example.com/pipe"
	pkgs, err := loadSources(filepath.Join("testdata", "pipeline"), module)
	if err != nil {
		t.Fatal(err)
	}
	// lower is the package of the fakes that execute a pipeline and export run functions.
	const lower = "internal/lower"
	if got, want := executors(pkgs, module, module+"/internal/pass/pipe"), []string{lower}; !slices.Equal(got, want) {
		t.Errorf("executors = %q, want %q", got, want)
	}
	extra, missing := exportDiff(exports(pkgs[module+"/internal/driver"]), []string{"Compile", "Gone"})
	if !slices.Equal(extra, []string{"RunStage"}) || !slices.Equal(missing, []string{"Gone"}) {
		t.Errorf("driver exports: extra %q, missing %q; want [RunStage] and [Gone]", extra, missing)
	}
	if got, want := runExports(pkgs, module, module+"/internal/pass/pipe"), []string{lower + ".Def", lower + ".Exported", lower + ".Lower", lower + ".RunNamed"}; !slices.Equal(got, want) {
		t.Errorf("run exports = %q, want %q", got, want)
	}
	want := []string{
		"cmd/tool: passes hooks to generate.GoWith",
		"cmd/tool: sets GoHooks",
		"cmd/tool: sets TSHooks",
	}
	got := hookSetters(pkgs, module)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("hook setters = %q, want %q", got, want)
	}
}
