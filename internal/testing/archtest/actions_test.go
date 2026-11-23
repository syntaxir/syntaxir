package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/action"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
)

// The action expressions of a grammar reach the backends only as the Action module of each table,
// which the Action segment of the pipeline lowers (internal/middleend/actionlower): no backend lowers
// an action expression itself, and no operation of Code IR carries one.

// actionLowering is the package of the action lowering, relative to the module.
const actionLowering = "internal/middleend/actionlower"

// actionLoweringImports returns the non-test files of the packages of pkgs under internal/backend
// that import the action lowering, as "<file>: imports <path>".
func actionLoweringImports(pkgs map[string]*sourcePackage, module string) []string {
	var out []string
	for path, sp := range pkgs {
		rel := relPath(path, module)
		if !under(rel, "internal/backend") {
			continue
		}
		for i, f := range sp.files {
			for _, s := range f.Imports {
				if p, err := strconv.Unquote(s.Path.Value); err == nil && p == module+"/"+actionLowering {
					out = append(out, fmt.Sprintf("%s/%s: imports %s", rel, sp.fileNames[i], actionLowering))
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// TestBackendsImportNoActionLowering checks that no package under internal/backend imports the
// action lowering: the printers, the bytecode lowering and the closures take the Action module of a
// table.
func TestBackendsImportNoActionLowering(t *testing.T) {
	pkgs, err := loadSources(moduleRoot, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range actionLoweringImports(pkgs, modulePath) {
		t.Errorf("%s; a backend takes the Action module of a table", v)
	}
}

// The negative control of TestBackendsImportNoActionLowering: a backend file that imports the action
// lowering, under another name, is reported, and a file outside internal/backend is not.
func TestBackendsImportNoActionLoweringNegativeControl(t *testing.T) {
	const module = "example.com/m"
	parse := func(src string) *ast.File {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	src := `package p; import al "` + module + "/" + actionLowering + `"`
	pkgs := map[string]*sourcePackage{
		module + "/internal/backend/vmlower": {name: "vmlower", files: []*ast.File{parse(src)}, fileNames: []string{"compile.go"}},
		module + "/internal/driver":          {name: "driver", files: []*ast.File{parse(src)}, fileNames: []string{"actions.go"}},
	}
	want := []string{"internal/backend/vmlower/compile.go: imports " + actionLowering}
	if got := actionLoweringImports(pkgs, module); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// codeOps returns the declared operations of the dialects of Code IR other than the primitives: those
// of the compositions of the Exec stage (compose.Build), the leaves and the constructors of the Action
// dialect (action.DeclareLeaves, action.NewOp) and the operations of the Flow dialect (flow.Ops), by
// name.
func codeOps() map[string]*code.Op {
	base := compose.Build()
	m := &code.Module{Components: slices.Clone(base.Components), Registers: base.Registers, Handles: slices.Clone(base.Handles),
		Ops: slices.Clone(base.Ops)}
	action.DeclareLeaves(m)
	ops := map[string]*code.Op{}
	for _, o := range slices.Concat(m.Ops, flow.Ops(), []*code.Op{action.NewOp(1)}) {
		ops[o.Name] = o
	}
	return ops
}

// coreValuesIn returns the paths in the type t to the values of Core IR that a value of t holds: a
// value of a type declared in Core IR (an action expression, a node, a shape, an expectation ID), or
// of another type that is a core.ActionExpr. The search follows the elements of pointers, slices,
// arrays and maps and the fields of structs, but not interfaces, whose values it cannot see.
func coreValuesIn(t reflect.Type) []string {
	actionExpr := reflect.TypeFor[core.ActionExpr]()
	corePkg := actionExpr.PkgPath()
	var out []string
	seen := map[reflect.Type]bool{}
	var walk func(t reflect.Type, path string)
	walk = func(t reflect.Type, path string) {
		if seen[t] {
			return
		}
		seen[t] = true
		if t.PkgPath() == corePkg || t == actionExpr ||
			(t.Kind() != reflect.Interface && (t.Implements(actionExpr) || reflect.PointerTo(t).Implements(actionExpr))) {
			out = append(out, path+" ("+t.String()+")")
			return
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(t.Elem(), path+"[]")
		case reflect.Map:
			walk(t.Key(), path+"[key]")
			walk(t.Elem(), path+"[value]")
		case reflect.Struct:
			for i := range t.NumField() {
				walk(t.Field(i).Type, path+"."+t.Field(i).Name)
			}
		}
	}
	walk(t, t.String())
	return out
}

// TestCodeOpsCarryNoCoreValues checks that no operation of Code IR, the primitives of the Exec stage
// (prim.OpNames) included, has an immediate operand that holds a value of Core IR. An operation
// names an action site by its number in the Action module of its table (prim.Predicate,
// prim.Binding, prim.Action), whose functions evaluate the action expressions; the terminals,
// classes, first sets, anchors, shapes and expectations of the Flow and Exec operations are values
// of Code IR (internal/ir/code/exec/prim), and an observed skip or an outlined call names what it
// reports or runs. The import rules keep Code IR from importing Core IR; this test also covers the
// immediates of the operations that compose.Build, action.DeclareLeaves and flow.Ops declare.
//
// The interfaces among the immediates are not searched: the checked type and the key of a cast
// (action.Cast.Check and Key), which the front end gives, and the constant of action.const
// (action.Const.Value), an int, a string, a bool or nil.
func TestCodeOpsCarryNoCoreValues(t *testing.T) {
	imms := map[string]reflect.Type{}
	for name, o := range codeOps() {
		imms[name] = o.ImmType
	}
	for _, name := range prim.OpNames() {
		imms[name], _ = prim.ImmType(name)
	}
	for _, name := range slices.Sorted(maps.Keys(imms)) {
		if imm := imms[name]; imm != nil {
			for _, p := range coreValuesIn(imm) {
				t.Errorf("the immediate of %s holds a value of Core IR: %s", name, p)
			}
		}
	}
}

// The negative control of TestCodeOpsCarryNoCoreValues: values of Core IR held directly, through a
// pointer, a slice, a map and a struct field are found, action expressions and other values alike; a
// type without one and an interface are not.
func TestCodeOpsCarryNoCoreValuesNegativeControl(t *testing.T) {
	type holder struct {
		Name string
		Pred *core.Pred
	}
	for _, c := range []struct {
		t    reflect.Type
		want int
	}{
		{reflect.TypeFor[core.ActionExpr](), 1},
		{reflect.TypeFor[*core.Binary](), 1},
		{reflect.TypeFor[[]core.ActionExpr](), 1},
		{reflect.TypeFor[map[string]*core.Const](), 1},
		{reflect.TypeFor[holder](), 1},
		{reflect.TypeFor[core.Node](), 1},
		{reflect.TypeFor[*core.Lit](), 1},
		{reflect.TypeFor[[]core.ClassRun](), 1},
		{reflect.TypeFor[core.Shape](), 1},
		{reflect.TypeFor[map[core.ExpID]bool](), 1},
		{reflect.TypeFor[prim.Predicate](), 0},
		{reflect.TypeFor[action.Cast](), 0},
		{reflect.TypeFor[any](), 0},
	} {
		if got := coreValuesIn(c.t); len(got) != c.want {
			t.Errorf("%s: found %q, want %d", c.t, got, c.want)
		}
	}
	if got := strings.Join(coreValuesIn(reflect.TypeFor[holder]()), ""); !strings.Contains(got, ".Pred[] (core.Pred)") {
		t.Errorf("the path of the predicate is %q", got)
	}
}
