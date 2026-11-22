package archtest

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The stage of the printers (record 074, Printers Take Exec-Stage Units): the import rules keep the
// Flow dialect out of the printers, but the marker of the Flow stage (code.Flow) is declared in Code
// IR, which a printer imports, and a permitted package could pass a unit at the Flow stage along under
// another name. So the printers are type-checked, and no expression or declared name of their non-test
// files may have a type that mentions code.Flow: a unit at the Flow stage (code.At[code.Flow]), its
// kind (code.FlowStage), its entry (code.EnterFlow) or a type that carries one as a type argument,
// element, field of an unnamed struct, parameter or result. A named type is not opened: its fields are
// the business of its own package, whose rules are its own.

// flowMarkerPkg and flowMarkerName are the package and the name of the marker type of the Flow stage.
const flowMarkerPkg, flowMarkerName = modulePath + "/internal/ir/code", "Flow"

// mentionsFlowStage reports whether t mentions the marker type of the Flow stage, without opening
// named types other than through their type arguments.
func mentionsFlowStage(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.Alias:
		return mentionsFlowStage(types.Unalias(t), seen)
	case *types.Named:
		if o := t.Obj(); o.Pkg() != nil && o.Pkg().Path() == flowMarkerPkg && o.Name() == flowMarkerName {
			return true
		}
		for a := range t.TypeArgs().Types() {
			if mentionsFlowStage(a, seen) {
				return true
			}
		}
	case *types.Pointer:
		return mentionsFlowStage(t.Elem(), seen)
	case *types.Slice:
		return mentionsFlowStage(t.Elem(), seen)
	case *types.Array:
		return mentionsFlowStage(t.Elem(), seen)
	case *types.Chan:
		return mentionsFlowStage(t.Elem(), seen)
	case *types.Map:
		return mentionsFlowStage(t.Key(), seen) || mentionsFlowStage(t.Elem(), seen)
	case *types.Tuple:
		for v := range t.Variables() {
			if mentionsFlowStage(v.Type(), seen) {
				return true
			}
		}
	case *types.Signature:
		return mentionsFlowStage(t.Params(), seen) || mentionsFlowStage(t.Results(), seen)
	case *types.Struct:
		for f := range t.Fields() {
			if mentionsFlowStage(f.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// listedPackage is a package as go list reports it: its export data file, its directory and the Go
// files that the build compiles, after build constraints.
type listedPackage struct {
	export, dir string
	goFiles     []string
}

// listPackages returns the packages pkgs and their dependencies, by import path, from go list in the
// module root.
func listPackages(pkgs ...string) (map[string]listedPackage, error) {
	cmd := exec.Command("go", append([]string{"list", "-export", "-deps", "-f", "{{.ImportPath}}\t{{.Export}}\t{{.Dir}}\t{{join .GoFiles \",\"}}"}, pkgs...)...)
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -export: %v\n%s", err, stderr.String())
	}
	listed := map[string]listedPackage{}
	for line := range strings.Lines(string(out)) {
		f := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if len(f) != 4 {
			return nil, fmt.Errorf("go list -export: unexpected line %q", line)
		}
		lp := listedPackage{export: f[1], dir: f[2]}
		if f[3] != "" {
			lp.goFiles = strings.Split(f[3], ",")
		}
		listed[f[0]] = lp
	}
	return listed, nil
}

// flowStageUses type-checks the package at the import path pkg, from the Go files that its build
// compiles and the extra files (name to source), against the export data of its dependencies, and
// returns the lines of its files that hold expressions or declared names whose types mention the
// marker of the Flow stage, as "<file>:<line>: <source text>" with the shortest such text of the line.
func flowStageUses(pkg string, listed map[string]listedPackage, extra map[string]string) ([]string, error) {
	lp, ok := listed[pkg]
	if !ok {
		return nil, fmt.Errorf("go list does not list %s", pkg)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, n := range lp.goFiles {
		f, err := parser.ParseFile(fset, filepath.Join(lp.dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	for _, n := range slices.Sorted(maps.Keys(extra)) {
		f, err := parser.ParseFile(fset, filepath.Join(lp.dir, n), extra[n], parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file := listed[path].export
		if file == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	})
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: imp, FakeImportC: true}
	if _, err := conf.Check(pkg, fset, files, info); err != nil {
		return nil, err
	}
	// The shortest source text by line: a use reports the name, not each expression around it.
	byLine := map[string]string{}
	report := func(n ast.Node, t types.Type) {
		if !mentionsFlowStage(t, map[types.Type]bool{}) {
			return
		}
		p := fset.Position(n.Pos())
		var text bytes.Buffer
		if err := printer.Fprint(&text, fset, n); err != nil {
			return
		}
		key := fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line)
		if old, ok := byLine[key]; !ok || text.Len() < len(old) || text.Len() == len(old) && text.String() < old {
			byLine[key] = text.String()
		}
	}
	for e, tv := range info.Types {
		report(e, tv.Type)
	}
	for id, o := range info.Defs {
		if o != nil {
			report(id, o.Type())
		}
	}
	var out []string
	for _, k := range slices.Sorted(maps.Keys(byLine)) {
		out = append(out, k+": "+byLine[k])
	}
	return out, nil
}

// TestPrintersHoldNoFlowStageUnit checks that the printers hold no unit at the Flow stage.
func TestPrintersHoldNoFlowStageUnit(t *testing.T) {
	var pkgs []string
	for _, p := range printPackages {
		pkgs = append(pkgs, modulePath+"/"+p)
	}
	listed, err := listPackages(pkgs...)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		uses, err := flowStageUses(p, listed, nil)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		for _, u := range uses {
			t.Errorf("%s/%s has a type at the Flow stage; a printer takes Exec-stage units", strings.TrimPrefix(p, modulePath+"/"), u)
		}
	}
}

// The negative control of the rule: a file planted in the Go printer holds a unit at the Flow stage
// under an alias, through the entry of the stage, its kind, a slice of units, a function that returns
// one and an unnamed struct; the Exec stage and Code IR itself pass.
func TestPrinterStageRuleNegativeControl(t *testing.T) {
	p := modulePath + "/internal/backend/codegen/golang/print"
	listed, err := listPackages(p)
	if err != nil {
		t.Fatal(err)
	}
	const planted = `package print

import "syntaxir.dev/sxir/internal/ir/code"

type laundered = code.At[code.Flow]

var plantedKind = code.FlowStage

func plantedEnter(m *code.Module) []laundered {
	return []laundered{code.EnterFlow(m)}
}

var plantedFunc func() struct{ u laundered }

var plantedExec code.At[code.Exec]

var plantedModule *code.Module
`
	got, err := flowStageUses(p, listed, map[string]string{"zz_planted.go": planted})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		// Sorted as text; each line with the shortest text that has the type.
		"zz_planted.go:10: laundered",
		"zz_planted.go:13: u",
		"zz_planted.go:5: code.Flow",
		"zz_planted.go:7: plantedKind",
		"zz_planted.go:9: laundered",
	}
	if !slices.Equal(got, want) {
		t.Errorf("uses:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
