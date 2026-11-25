package flowlower

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
)

// The tests of this file hold the chapter Lowering From Core IR of the Flow IR specification
// (spec/flow-ir/lowering.md) current with the lowering: every node of Core IR, every use of a
// planned function and every option of the lowering has an entry in the chapter, every entry names
// one that the implementation has, and every operation that the chapter names is an operation of the
// Flow dialect. They live beside the lowering, so that a change of it runs them.

const (
	// loweringChapter is the chapter, relative to this package.
	loweringChapter = "../../../spec/flow-ir/lowering.md"
	// coreNodes is the file of Core IR that declares its nodes, and plandataFile the file that
	// declares the uses of planned functions, relative to this package.
	coreNodes    = "../../ir/core/node.go"
	plandataFile = "../flowplan/plandata/plandata.go"
)

// TestLoweringChapterEntries checks that the tables of nodes, uses and options of the chapter list
// exactly the nodes of Core IR, the uses of the plan and the options of the lowering.
func TestLoweringChapterEntries(t *testing.T) {
	src, err := os.ReadFile(loweringChapter)
	if err != nil {
		t.Fatal(err)
	}
	var options []string
	for _, f := range reflect.VisibleFields(reflect.TypeFor[lowerdata.Options]()) {
		options = append(options, f.Name)
	}
	for _, c := range []struct {
		kind, head string
		want       []string
	}{
		{"node", "Node", nodeTypes(t)},
		{"use", "Use", constsOf(t, plandataFile, "Use")},
		{"option", "Option", options},
	} {
		if len(c.want) == 0 {
			t.Fatalf("no %s found in the code", c.kind)
		}
		for _, e := range entryErrors(c.kind, c.want, tableColumn(string(src), c.head)) {
			t.Error(e)
		}
	}
}

// TestLoweringChapterOperations checks that every operation of the Flow dialect that the chapter
// names is one that the dialect has.
func TestLoweringChapterOperations(t *testing.T) {
	src, err := os.ReadFile(loweringChapter)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, o := range flow.Ops() {
		have[o.Name] = true
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile("`(flow\\.[A-Za-z]+)`").FindAllStringSubmatch(string(src), -1) {
		if name := m[1]; !have[name] && !seen[name] {
			seen[name] = true
			t.Errorf("the chapter names %s, which the Flow dialect does not have", name)
		}
	}
}

// tableColumn returns the first cells of the rows of the tables of the Markdown src whose first
// header cell is head, with a cell that is one code span replaced by its content.
func tableColumn(src, head string) []string {
	var out []string
	table, in, fenced := false, false, false
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
		}
		if fenced || !strings.HasPrefix(line, "|") {
			table, in = false, false
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		first := strings.Trim(strings.TrimSpace(cells[0]), "`")
		switch {
		case strings.HasPrefix(line, "|:") || strings.HasPrefix(line, "|-"):
		case !table:
			table, in = true, first == head
		case in:
			out = append(out, first)
		}
	}
	return out
}

// entryErrors returns the differences between the names that the code declares, want, and those
// that the chapter lists, got.
func entryErrors(kind string, want, got []string) []string {
	var errs []string
	seen := map[string]bool{}
	for _, g := range got {
		if seen[g] {
			errs = append(errs, fmt.Sprintf("%s %s: listed twice in the chapter", kind, g))
		}
		seen[g] = true
		if !slices.Contains(want, g) {
			errs = append(errs, fmt.Sprintf("%s %s: in the chapter but not in the code", kind, g))
		}
	}
	for _, w := range want {
		if !seen[w] {
			errs = append(errs, fmt.Sprintf("%s %s: in the code but not in the chapter", kind, w))
		}
	}
	return errs
}

// nodeTypes returns the names of the nodes of Core IR: the struct types of its node file that embed
// the common part of a node, base.
func nodeTypes(t *testing.T) []string {
	t.Helper()
	f := parseFile(t, coreNodes)
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, fd := range st.Fields.List {
			if id, ok := fd.Type.(*ast.Ident); ok && len(fd.Names) == 0 && id.Name == "base" {
				out = append(out, ts.Name.Name)
			}
		}
		return false
	})
	return out
}

// constsOf returns the names of the constants of the type typ that the file declares, those of an
// iota block that takes its type from its first constant included.
func constsOf(t *testing.T, file, typ string) []string {
	t.Helper()
	f := parseFile(t, file)
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		in := false
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			switch {
			case vs.Type != nil:
				id, ok := vs.Type.(*ast.Ident)
				in = ok && id.Name == typ
			case len(vs.Values) > 0:
				in = false
			}
			if in {
				for _, n := range vs.Names {
					out = append(out, n.Name)
				}
			}
		}
	}
	return out
}

func parseFile(t *testing.T, file string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.FromSlash(file), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
