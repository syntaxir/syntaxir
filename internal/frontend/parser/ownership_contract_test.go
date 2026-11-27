package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/parser"
	format "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The tests in this file check the contract of "Comment Grouping and
// Ownership" in spec/language/grammar-files.md on each frontend by itself:
// the generated frontend (Parse) and the hand-written parser
// (ParseReference). The differential tests in spec/language compare the two
// with each other, which cannot see a defect that both share.

// contractFrontends are the implementations of the contract.
var contractFrontends = []struct {
	name  string
	parse func(string) (*ast.Grammar, error)
}{
	{"generated", parser.Parse},
	{"reference", parser.ParseReference},
}

// ownedComment is one normalized comment string that a node owns: Path is the
// node's path in the AST and Kind the field that holds the string.
type ownedComment struct {
	Path, Kind, Text string
	// Pos and End are the span of the owner, and Node its type.
	Pos, End ast.Pos
	Node     string
}

func (o ownedComment) String() string { return o.Path + " " + o.Kind + " " + strconvQuote(o.Text) }

func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }

var sourceInfoType = reflect.TypeOf(ast.SourceInfo{})

// ownedComments returns every normalized comment string of g with its owner:
// the comments of each SourceInfo, and the header and keyword comments of
// declarations and fields.
func ownedComments(g *ast.Grammar) []ownedComment {
	var out []ownedComment
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value, path string, owner *ast.SourceInfo)
	var node string // the type of the node whose fields are walked
	add := func(path, kind string, owner *ast.SourceInfo, texts ...string) {
		for _, text := range texts {
			if text == "" {
				continue
			}
			o := ownedComment{Path: path, Kind: kind, Text: text, Node: node}
			if owner != nil {
				o.Pos, o.End = owner.Pos, owner.End
			}
			out = append(out, o)
		}
	}
	walk = func(v reflect.Value, path string, owner *ast.SourceInfo) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			walk(v.Elem(), path, owner)
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), path, owner)
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i), owner)
			}
		case reflect.Struct:
			if v.Type() == sourceInfoType {
				s := v.Addr().Interface().(*ast.SourceInfo)
				add(path, "leading", s, s.LeadingComments)
				add(path, "trailing", s, s.TrailingComments...)
				add(path, "detached", s, s.LeadingDetachedComments...)
				return
			}
			// The SourceInfo of a struct is the owner of its header comments.
			var self *ast.SourceInfo
			if f, ok := v.Type().FieldByName("SourceInfo"); ok && f.Anonymous && v.CanAddr() {
				self = v.FieldByIndex(f.Index).Addr().Interface().(*ast.SourceInfo)
			}
			for i := 0; i < v.NumField(); i++ {
				node = v.Type().Name()
				f := v.Type().Field(i)
				if !f.IsExported() || f.Tag.Get("json") == "-" {
					continue
				}
				fv := v.Field(i)
				name := path + "." + f.Name
				if f.Anonymous && f.Type == sourceInfoType {
					name = path
				}
				switch {
				case f.Type.Kind() == reflect.String && strings.HasSuffix(f.Name, "Comments"):
					add(path, f.Name, self, fv.String())
				case f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.String && strings.HasSuffix(f.Name, "Comments"):
					add(path, f.Name, self, fv.Interface().([]string)...)
				default:
					walk(fv, name, self)
				}
			}
		}
	}
	walk(reflect.ValueOf(g), "", nil)
	return out
}

// sourceSpans returns the path and SourceInfo of every source-bearing node of g
// with a span, with the path of the closest enclosing one.
type spanNode struct {
	Path, Parent string
	// Type is the name of the node's type.
	Type       string
	Info       *ast.SourceInfo
	ParentInfo *ast.SourceInfo
}

func sourceSpans(g *ast.Grammar) []spanNode {
	var out []spanNode
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value, path string, parent spanNode)
	walk = func(v reflect.Value, path string, parent spanNode) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			walk(v.Elem(), path, parent)
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), path, parent)
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i), parent)
			}
		case reflect.Struct:
			if v.Type() == sourceInfoType {
				return
			}
			if f, ok := v.Type().FieldByName("SourceInfo"); ok && f.Anonymous && v.CanAddr() {
				s := v.FieldByIndex(f.Index).Addr().Interface().(*ast.SourceInfo)
				if s.Pos.IsValid() {
					n := spanNode{Path: path, Parent: parent.Path, Type: v.Type().Name(), Info: s, ParentInfo: parent.Info}
					out = append(out, n)
					parent = spanNode{Path: path, Info: s}
				}
			}
			for i := 0; i < v.NumField(); i++ {
				f := v.Type().Field(i)
				if !f.IsExported() || f.Tag.Get("json") == "-" || f.Anonymous && f.Type == sourceInfoType {
					continue
				}
				if f.Type == sourceInfoType {
					// SkipSource: a span of its own inside the node.
					s := v.Field(i).Addr().Interface().(*ast.SourceInfo)
					if s.Pos.IsValid() {
						out = append(out, spanNode{Path: path + "." + f.Name, Parent: parent.Path, Info: s, ParentInfo: parent.Info})
					}
					continue
				}
				walk(v.Field(i), path+"."+f.Name, parent)
			}
		}
	}
	walk(reflect.ValueOf(g), "", spanNode{})
	return out
}

func posLess(a, b ast.Pos) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col
}

// runeIndex maps the positions of src to offsets of its runes.
type runeIndex struct {
	src   []rune
	lines []int // the offset of the first rune of each line
}

func newRuneIndex(src string) *runeIndex {
	r := &runeIndex{src: []rune(src), lines: []int{0}}
	for i, c := range r.src {
		// A line ends at "\n", at "\r" not followed by "\n", and at "\r\n".
		if c == '\n' || c == '\r' && (i+1 >= len(r.src) || r.src[i+1] != '\n') {
			r.lines = append(r.lines, i+1)
		}
	}
	return r
}

func (r *runeIndex) offset(p ast.Pos) int { return r.lines[p.Line-1] + p.Col - 1 }

func (r *runeIndex) pos(off int) ast.Pos {
	line := 0
	for line+1 < len(r.lines) && r.lines[line+1] <= off {
		line++
	}
	return ast.Pos{Line: line + 1, Col: off - r.lines[line] + 1}
}

// checkSpans checks the positions of every node of g, parsed from src: the
// span is ordered, starts and ends at a token rather than at white space or a
// comment, and lies inside the span of the closest enclosing node.
func checkSpans(src string, g *ast.Grammar) error {
	r := newRuneIndex(src)
	for _, n := range sourceSpans(g) {
		s := n.Info
		if posLess(s.End, s.Pos) {
			return fmt.Errorf("%s: span %v-%v ends before it starts", n.Path, s.Pos, s.End)
		}
		if n.Path == ".File" {
			continue
		}
		start, end := r.offset(s.Pos), r.offset(s.End)
		// at reports whether the runes at i are text; it looks at no more than
		// text, so that the check is linear in the source.
		at := func(i int, text string) bool {
			return i >= 0 && i+len(text) <= len(r.src) && string(r.src[i:i+len(text)]) == text
		}
		if start < len(r.src) && (isSpace(r.src[start]) || at(start, "//") || at(start, "/*")) {
			return fmt.Errorf("%s: span %v-%v starts at %q", n.Path, s.Pos, s.End, string(r.src[start:min(len(r.src), start+10)]))
		}
		if end > start && (isSpace(r.src[end-1]) || at(end-2, "*/") && !(end-start >= 4 && at(end-4, "\"*/\""))) {
			return fmt.Errorf("%s: span %v-%v ends at %q", n.Path, s.Pos, s.End, string(r.src[max(0, end-10):end]))
		}
		if p := n.ParentInfo; p != nil && (posLess(s.Pos, p.Pos) || posLess(p.End, s.End)) {
			return fmt.Errorf("%s: span %v-%v is not inside %s %v-%v", n.Path, s.Pos, s.End, n.Parent, p.Pos, p.End)
		}
	}
	return nil
}

func isSpace(c rune) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// ownershipCase is a source with the owners of its comments.
type ownershipCase struct {
	name, src string
	want      []string // ownedComment.String() of every owned comment, sorted
}

// ownershipCases cover the rows of the grouping table and the ownership rules.
var ownershipCases = []ownershipCase{
	{"start of file leads", "// a\npackage p\n", []string{".File.Pkg leading \" a\\n\""}},
	{"trailing on the line", "package p // a\n", []string{".File.Pkg trailing \" a\\n\""}},
	{"group after a line break trails before EOF", "package p\n// a\n", []string{".File.Pkg trailing \" a\\n\""}},
	{"blank line before EOF has no owner", "package p\n\n// a\n", nil},
	{"consecutive line comments are one group", "package p\n\n// a\n// b\nrule r = \"x\"\n", []string{".File.Decls[0] leading \" a\\n b\\n\""}},
	{"blank line detaches", "package p\n\n// a\n\n// b\nrule r = \"x\"\n", []string{".File.Decls[0] detached \" a\\n\"", ".File.Decls[0] leading \" b\\n\""}},
	{"block then line comment are two groups", "package p\n\n/* a */\n// b\nrule r = \"x\"\n", []string{".File.Decls[0] detached \" a \"", ".File.Decls[0] leading \" b\\n\""}},
	{"inline first group trails, rest detached, last leads", "package p\n\nrule r = \"x\" /* a */ /* b */ /* c */ \"y\"\n", []string{".File.Decls[0].Expr.Items[0] trailing \" a \"", ".File.Decls[0].Expr.Items[1] detached \" b \"", ".File.Decls[0].Expr.Items[1] leading \" c \""}},
	{"single same-line group is detached", "package p\n\nrule r = \"x\" /* a */ \"y\"\n", []string{".File.Decls[0].Expr.Items[1] detached \" a \""}},
	{"after an opener the group leads", "package p\n\nrule r = ( // a\n  \"x\" )\n", []string{".File.Decls[0].Expr.Expr leading \" a\\n\""}},
	{"single group after a comma leads", "package p\n\nrule r = \"x\" => f(1, /* a */ 2)\n", []string{".File.Decls[0].Action.Args[1] leading \" a \""}},
	{"comment after a comma trails the element before it", "package p\n\nrule r = \"x\" => f(1, // a\n  2)\n", []string{".File.Decls[0].Action.Args[0] trailing \" a\\n\""}},
	{"semicolon trailing follows the one before", "package p\n\nrule r = \"x\" /* a */; // b\n", []string{".File.Decls[0] trailing \" a \"", ".File.Decls[0] trailing \" b\\n\""}},
	{"delimiters enclosing nothing", "package p\n\ntype T = { /* a */ }\n", nil},
	{"declaration header", "package p\n\nrule /* a */ r /* b */ = \"x\"\n", []string{".File.Decls[0] HeaderComments \" a \"", ".File.Decls[0] HeaderComments \" b \""}},
	{"rule result type in the header", "package p\n\nrule r /* a */ T /* b */ = \"x\"\n", []string{".File.Decls[0] HeaderComments \" a \"", ".File.Decls[0] HeaderComments \" b \""}},
	{"rule body owns the gap after =", "package p\n\nrule r = /* a */ \"x\"\n", []string{".File.Decls[0].Expr detached \" a \""}},
	{"struct type header", "package p\n\ntype T /* a */ = { A int }\n", []string{".File.Decls[0] HeaderComments \" a \""}},
	{"struct type owns the gap after =", "package p\n\ntype T = /* a */ { A int }\n", []string{".File.Decls[0].Type detached \" a \""}},
	{"node type owns the gap after =", "package p\n\ntype T = /* a */ node[string]\n", []string{".File.Decls[0].Type detached \" a \""}},
	{"alias type owns the gap after ~", "package p\n\ntype T /* a */ ~ /* b */ int\n", []string{".File.Decls[0] HeaderComments \" a \"", ".File.Decls[0].Type detached \" b \""}},
	{"var header and value", "package p\n\nvar /* a */ v /* b */ int = /* c */ 1\n", []string{".File.Decls[0] HeaderComments \" a \"", ".File.Decls[0] HeaderComments \" b \"", ".File.Decls[0].Value detached \" c \""}},
	{"var header ends at =", "package p\n\nvar v /* a */ = /* b */ 1\n", []string{".File.Decls[0] HeaderComments \" a \"", ".File.Decls[0].Value detached \" b \""}},
	{"var type and = in the header", "package p\n\nvar v int /* a */ = 1\n", []string{".File.Decls[0] HeaderComments \" a \""}},
	{"field header", "package p\n\ntype T = { A /* a */ []int }\n", []string{".File.Decls[0].Type.Fields[0] HeaderComments \" a \""}},
	{"field leads and trails", "package p\n\ntype T = {\n    // a\n    A int // b\n}\n", []string{".File.Decls[0].Type.Fields[0] leading \" a\\n\"", ".File.Decls[0].Type.Fields[0] trailing \" b\\n\""}},
	{"keyword comments after attributes", "package p\n\n@inline\n// a\nrule r = \"x\"\n", []string{".File.Decls[0] KeywordComments \" a\\n\""}},
}

// TestOwnershipContract checks the owners of the comments of each case on
// each frontend.
func TestOwnershipContract(t *testing.T) {
	for _, fe := range contractFrontends {
		for _, tc := range ownershipCases {
			t.Run(fe.name+"/"+tc.name, func(t *testing.T) {
				g, err := fe.parse(tc.src)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, o := range ownedComments(g) {
					got = append(got, o.String())
				}
				slices.Sort(got)
				want := slices.Clone(tc.want)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Errorf("owners of %q:\ngot  %q\nwant %q", tc.src, got, want)
				}
				if err := checkSpans(tc.src, g); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

// contractGrammars returns the grammars of the repository that the insertion
// property runs on.
func contractGrammars(t *testing.T, all bool) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	patterns := []string{"spec/language/testdata/comment-boundaries.sxir", "examples/calculator/calc.sxir"}
	if all {
		patterns = []string{"sxir.sxir", "examples/*/*.sxir", "testdata/grammars/*/*.sxir", "spec/language/testdata/*.sxir", "bench/grammars/*/*.sxir"}
	}
	out := map[string]string{}
	for _, p := range patterns {
		files, err := filepath.Glob(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			out[filepath.ToSlash(f)] = string(data)
		}
	}
	if len(out) == 0 {
		t.Fatal("no grammars")
	}
	return out
}

// insertionLabel is the text of the comment that the insertion property adds.
const insertionLabel = "Lk7"

// insertedAsText reports whether the inserted form is text rather than a
// comment in g: at lies inside a literal or a character class, whose text the
// AST keeps as runes and ranges, or a string of the AST outside the comments
// holds the label, as in a string of an action or an attribute argument.
func insertedAsText(g *ast.Grammar, at ast.Pos) bool {
	for _, n := range sourceSpans(g) {
		if (n.Type == "Literal" || n.Type == "CharClass") && posLess(n.Info.Pos, at) && posLess(at, n.Info.End) {
			return true
		}
	}
	found := false
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() && !seen[v.Pointer()] {
				seen[v.Pointer()] = true
				walk(v.Elem())
			}
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(k)
				walk(v.MapIndex(k))
			}
		case reflect.String:
			found = found || strings.Contains(v.String(), insertionLabel)
		case reflect.Struct:
			if v.Type() == sourceInfoType || v.Type() == reflect.TypeOf(ast.Comment{}) {
				return
			}
			for i := 0; i < v.NumField(); i++ {
				if f := v.Type().Field(i); f.IsExported() && !strings.Contains(f.Name, "Comments") {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(g))
	return found
}

// insertionProblem inserts the comment form at the rune offset off of src and
// checks the result on parse: the comment is owned at most once, outside the
// span of its owner on the side its kind says; it is kept as exactly one raw
// comment token; the spans are well formed; and formatting keeps it with the
// same owner. It returns "" when the source with the comment does not parse.
func insertionProblem(parse func(string) (*ast.Grammar, error), src []rune, off int, form string) string {
	text := string(src[:off]) + form + string(src[off:])
	g, err := parse(text)
	if err != nil {
		return ""
	}
	r := newRuneIndex(text)
	at := r.pos(off + strings.Index(form, insertionLabel) - 2)
	var owners []ownedComment
	for _, o := range ownedComments(g) {
		if strings.Contains(o.Text, insertionLabel) {
			owners = append(owners, o)
		}
	}
	if len(owners) > 1 {
		return fmt.Sprintf("owned %d times: %v", len(owners), owners)
	}
	if len(owners) == 1 && owners[0].Pos.IsValid() {
		o := owners[0]
		switch o.Kind {
		case "leading", "detached":
			// A postfix pragma is completed at its "#" before its operand
			// joins it, and keeps the comments before the "#".
			postfix := (o.Node == "Atomic" || o.Node == "Stream" || o.Node == "Recovery" || o.Node == "Repeat") &&
				strings.HasPrefix(strings.TrimLeft(text[len(string(src[:off]))+len(form):], " \t\r\n"), "#")
			if !posLess(at, o.Pos) && !postfix {
				return fmt.Sprintf("%v leads %s at %v", at, o.Path, o.Pos)
			}
		case "trailing":
			if posLess(at, o.End) {
				return fmt.Sprintf("%v trails %s ending at %v", at, o.Path, o.End)
			}
		default:
			if posLess(at, o.Pos) || posLess(o.End, at) {
				return fmt.Sprintf("%v in the header of %s at %v-%v", at, o.Path, o.Pos, o.End)
			}
		}
	}
	raw := 0
	for _, c := range g.AllComments() {
		raw += strings.Count(c.Text, insertionLabel)
	}
	if raw == 0 && len(owners) == 0 && insertedAsText(g, at) {
		// The form landed inside a literal or a character class, where it is
		// text rather than a comment.
		return ""
	}
	if raw != 1 {
		return fmt.Sprintf("kept as %d raw comment tokens", raw)
	}
	if problem := invalidCommentAccepted(g); problem != "" {
		return problem
	}
	if err := checkSpans(text, g); err != nil {
		return err.Error()
	}
	out := format.Format(g)
	again, err := parse(out)
	if err != nil {
		return fmt.Sprintf("formatted source does not parse: %v\n%s", err, out)
	}
	var paths []string
	for _, o := range ownedComments(again) {
		if strings.Contains(o.Text, insertionLabel) {
			paths = append(paths, o.Path)
		}
	}
	if len(owners) == 1 && !slices.Equal(paths, []string{owners[0].Path}) || len(owners) == 0 && len(paths) > 0 {
		return fmt.Sprintf("formatting moves the comment from %v to %v\n%s", owners, paths, out)
	}
	if format.Format(again) != out {
		return "formatting is not idempotent"
	}
	return ""
}

// invalidCommentAccepted checks that validation reaches the raw comment that holds the label
// wherever the parser keeps it: with a byte that is not valid UTF-8 added to its text, g must
// fail validation for that byte. It returns "" when g is invalid to begin with.
func invalidCommentAccepted(g *ast.Grammar) string {
	if ast.Validate(g) != nil {
		return ""
	}
	for _, c := range g.AllComments() {
		if !strings.Contains(c.Text, insertionLabel) {
			continue
		}
		text := c.Text
		c.Text = strings.Replace(text, insertionLabel, insertionLabel+"\xff", 1)
		err := ast.Validate(g)
		c.Text = text
		if err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
			return fmt.Sprintf("a comment that is not valid UTF-8 passes validation: %v", err)
		}
	}
	return ""
}

// checkInsertions runs insertionProblem at every offset of each grammar next to
// white space, with a block and a line comment, on each frontend.
func checkInsertions(t *testing.T, grammars map[string]string) {
	for _, fe := range contractFrontends {
		for name, src := range grammars {
			t.Run(fe.name+"/"+filepath.Base(name), func(t *testing.T) {
				failures := 0
				for _, w := range insertionWindows(t, fe.parse, src) {
					for _, off := range insertionOffsets(w.src, w.lo, w.hi) {
						for _, form := range []string{" /*" + insertionLabel + "*/ ", " //" + insertionLabel + "\n"} {
							if p := insertionProblem(fe.parse, w.src, off, form); p != "" {
								t.Errorf("%q at offset %d: %s", form, w.from+off, p)
								if failures++; failures > 10 {
									t.FailNow()
								}
							}
						}
					}
				}
			})
		}
	}
}

// insertionWindow is a part of a grammar that parses by itself. The offsets
// from lo through hi are tried in it; from is the offset of the window in the
// grammar.
type insertionWindow struct {
	src          []rune
	from, lo, hi int
}

// insertionWindows splits src at the first tokens of its declarations. The
// window of a declaration runs from its first token to the first token of the
// declaration after the next one, so that the comments inserted before the
// next declaration lie between the two declarations, and the offsets tried in
// it are those after the first token of the declaration through the first
// token of the next one. The first window also holds the package clause
// and the comments before the first declaration. Each window parses in a time
// independent of the size of the grammar, so the property is linear in it.
func insertionWindows(t *testing.T, parse func(string) (*ast.Grammar, error), src string) []insertionWindow {
	t.Helper()
	g, err := parse(src)
	if err != nil {
		t.Fatal(err)
	}
	runes := []rune(src)
	r := newRuneIndex(src)
	starts := []int{0}
	for _, d := range g.File.Decls {
		if off := r.offset(d.(ast.SourceNode).Source().Pos); off > starts[len(starts)-1] {
			starts = append(starts, off)
		}
	}
	starts = append(starts, len(runes))
	var out []insertionWindow
	for i := 0; i+1 < len(starts); i++ {
		end := starts[min(i+2, len(starts)-1)]
		lo := 1
		if i == 0 {
			lo = 0
		}
		w := insertionWindow{src: runes[starts[i]:end], from: starts[i], lo: lo, hi: starts[i+1] - starts[i]}
		if _, err := parse(string(w.src)); err != nil {
			t.Fatalf("window at offset %d does not parse: %v", w.from, err)
		}
		out = append(out, w)
	}
	return out
}

// insertionOffsets returns the offsets from lo through hi in src at which a
// comment lands in a distinct place: the start and the end of each run of white space,
// the start of each line in it, and the ends of src. The other offsets in a
// run give the same groups and owners.
func insertionOffsets(src []rune, lo, hi int) []int {
	var out []int
	add := func(off int) {
		if lo <= off && off <= hi && (len(out) == 0 || out[len(out)-1] != off) {
			out = append(out, off)
		}
	}
	add(0)
	for i := 0; i < len(src); i++ {
		if !isSpace(src[i]) {
			continue
		}
		add(i)
		for ; i < len(src) && isSpace(src[i]); i++ {
			if src[i] == '\n' {
				add(i + 1)
			}
		}
		add(i)
	}
	add(len(src))
	return out
}

// TestOwnershipInsertion checks the ownership contract with a comment inserted
// next to white space in the grammars of the repository (see
// insertionAllGrammars for the tier).
func TestOwnershipInsertion(t *testing.T) {
	checkInsertions(t, contractGrammars(t, insertionAllGrammars))
}
