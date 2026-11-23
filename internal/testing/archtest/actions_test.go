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

// actionValuesIn returns the paths in the type t to the action values of Core IR that a value of t
// holds: a core.ActionExpr, or a value of a type of Core IR that is one. The search follows the
// elements of pointers, slices, arrays and maps and the fields of structs, but not interfaces, whose
// values it cannot see.
func actionValuesIn(t reflect.Type) []string {
	actionExpr := reflect.TypeFor[core.ActionExpr]()
	var out []string
	seen := map[reflect.Type]bool{}
	var walk func(t reflect.Type, path string)
	walk = func(t reflect.Type, path string) {
		if seen[t] {
			return
		}
		seen[t] = true
		if t == actionExpr || (t.Kind() != reflect.Interface && (t.Implements(actionExpr) || reflect.PointerTo(t).Implements(actionExpr))) {
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

// TestCodeOpsCarryNoActionValues checks that no operation of Code IR, the primitives of the Exec stage
// (prim.OpNames) included, has an immediate operand that holds an action value of Core IR: an
// operation names an action site by its number in the Action module of its table (prim.Predicate,
// prim.Binding, prim.Action), whose functions evaluate the action expressions.
//
// The check covers the action values alone. The immediates of the Flow and Exec operations still
// hold other values of Core IR, which the backends read in place: the matched node of observeSkip
// (core.Node), the literal of hasLit and matchLit (*core.Lit), the class of matchClass and inClass
// (*core.Class), the first sets of the guards (*core.First), the anchor kind of atAnchor, the
// terminal of skipTerminal (*core.Terminal), the class runs of skipClasses (*core.ClassRun), the
// shapes of the value operations (core.Shape) and the expectation of mergeRenamed (core.ExpID).
// The interfaces among them are not searched.
func TestCodeOpsCarryNoActionValues(t *testing.T) {
	imms := map[string]reflect.Type{}
	for name, o := range codeOps() {
		imms[name] = o.ImmType
	}
	for _, name := range prim.OpNames() {
		imms[name], _ = prim.ImmType(name)
	}
	for _, name := range slices.Sorted(maps.Keys(imms)) {
		if imm := imms[name]; imm != nil {
			for _, p := range actionValuesIn(imm) {
				t.Errorf("the immediate of %s holds an action value of Core IR: %s", name, p)
			}
		}
	}
}

// The negative control of TestCodeOpsCarryNoActionValues: action values held directly, through a
// pointer, a slice, a map and a struct field are found; a type without one and an interface are not.
func TestCodeOpsCarryNoActionValuesNegativeControl(t *testing.T) {
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
		{reflect.TypeFor[prim.Predicate](), 0},
		{reflect.TypeFor[core.Node](), 0},
		{reflect.TypeFor[any](), 0},
	} {
		if got := actionValuesIn(c.t); len(got) != c.want {
			t.Errorf("%s: found %q, want %d", c.t, got, c.want)
		}
	}
	if got := strings.Join(actionValuesIn(reflect.TypeFor[holder]()), ""); !strings.Contains(got, ".Pred[].Action") {
		t.Errorf("the path of the action of a predicate is %q", got)
	}
}
