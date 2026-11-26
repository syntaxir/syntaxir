package print_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// constValue returns the literal value of the constant name declared in the Go source src.
func constValue(t *testing.T, file, src, name string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			v := s.(*ast.ValueSpec)
			for i, n := range v.Names {
				if n.Name == name && i < len(v.Values) {
					if lit, ok := v.Values[i].(*ast.BasicLit); ok {
						return lit.Value
					}
				}
			}
		}
	}
	t.Fatalf("%s declares no literal constant %s", file, name)
	return ""
}

// The local chunk helpers that a parser generated with the measurement references prints grow with the
// library's ratio: the generator's astChunkRatio is the library's chunkRatio.
func TestLocalChunkRatioIsTheLibrarys(t *testing.T) {
	codegentest.EachMode(t, testLocalChunkRatioIsTheLibrarys)
}

func testLocalChunkRatioIsTheLibrarys(t *testing.T) {
	lib := filepath.Join("..", "..", "..", "..", "..", "pkg", "runtime", "libgo", "chunk.go")
	src, err := os.ReadFile(lib)
	if err != nil {
		t.Fatal(err)
	}
	want := constValue(t, lib, string(src), "chunkRatio")
	g, err := parseTest(`type Item = { Text string }
type Root = { Items []Item }
rule main = xs:item* $$ => Root{Items: $xs.Value}
rule item = t:"a" => Item{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "chunks", Start: "main", Types: true, NoOpt: []string{"typed-output-bootstrap"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := constValue(t, "parser.go", string(code), "astChunkRatio"); got != want {
		t.Errorf("the generated astChunkRatio is %s, the library's chunkRatio %s", got, want)
	}
}

// With typed-chunk-growth off a typed parser keeps its chunks at their fixed size: it prints the local
// chunk helper that stops at the fixed limit and tells the library to keep the match chunks fixed. With
// it on (the default) it uses the library's chunks and leaves the grammar's match chunks growing.
func TestTypedChunkGrowthControl(t *testing.T) {
	codegentest.EachMode(t, testTypedChunkGrowthControl)
}

func testTypedChunkGrowthControl(t *testing.T) {
	g, err := parseTest(`type Item = { Text string }
type Root = { Items []Item }
rule main = xs:item* $$ => Root{Items: $xs.Value}
rule item = t:"a" => Item{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, conv := range []bool{false, true} {
		on, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "chunks", Start: "main", Types: true, ConvertTypes: conv})
		if err != nil {
			t.Fatal(err)
		}
		off, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "chunks", Start: "main", Types: true, ConvertTypes: conv, NoOpt: []string{"typed-chunk-growth"}})
		if err != nil {
			t.Fatal(err)
		}
		if s := string(on); strings.Contains(s, "sxirGrammar.FixedMatchChunks = -1") || strings.Contains(s, "astChunk") {
			t.Errorf("conversion %v: the default parser keeps its chunks fixed or prints local chunk helpers", conv)
		}
		s := string(off)
		if !strings.Contains(s, "sxirGrammar.FixedMatchChunks = -1") {
			t.Errorf("conversion %v: the parser without typed-chunk-growth lets the match chunks grow", conv)
		}
		if !strings.Contains(s, "s.next = min(size*2, limit)") || strings.Contains(s, "astChunkRatio") {
			t.Errorf("conversion %v: the parser without typed-chunk-growth prints growing chunk helpers", conv)
		}
	}
}
