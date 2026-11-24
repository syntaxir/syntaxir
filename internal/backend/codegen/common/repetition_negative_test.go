package common_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// Generated parsers retain the unbounded maximum -1 directly in their implementation-int loops.
// Check their hand-built AST contract beside bytecode lowering: -1 is unbounded, and a maximum
// below it is rejected before compilation and generation.
func TestGeneratedNegativeMaximum(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedNegativeMaximum)
}

func testGeneratedNegativeMaximum(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	large := int64(-1) << 32
	maximum := int(large)
	if strconv.IntSize < 64 {
		maximum = -2
	}
	const src = `rule main []node[string] = v1:"é"* $$ => $v1.Value`
	below := codegentest.GrammarWithNegativeMaximum(t, src, maximum)
	_, err := entry.Compile(below, entry.Options{})
	codegentest.WantNegativeMaximumRejected(t, err)
	_, err = codegentest.GenerateCode(t, below, common.GenOptions{Package: "main", Start: "main"})
	codegentest.WantNegativeMaximumRejected(t, err)
	g := codegentest.GrammarWithNegativeMaximum(t, src, -1)
	p, err := entry.Compile(g, entry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var expected []string
	for _, input := range []string{"", "é", "ééé", "x", "éx"} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			n, err := p.ParseWith("main", input, engine.ParseOptions{Unit: unit, Expectations: true})
			expected = append(expected, codegentest.ResultJSON(n, err))
		}
	}
	want := strings.Join(expected, "\n") + "\n"
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("Go typed=%v", typed), func(t *testing.T) {
			code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "main", Start: "main", Types: typed})
			if err != nil {
				t.Fatal(err)
			}
			astCheck := ""
			if typed {
				astCheck = `ast, ae := ParseValue(input, WithUnit(unit)); if fmt.Sprint(ae) != fmt.Sprint(err) || err == nil && len(ast) != len([]rune(input)) { panic("typed negative-maximum result differs") }`
			}
			harness := `package main
import ("encoding/json";"fmt")
func main() {
 for _, input := range []string{"","é","ééé","x","éx"} {
  for _, unit := range []PositionUnit{CodePoints,Bytes} {
   n, err := Parse(input,WithUnit(unit))
   ` + astCheck + `
   out := map[string]any{"node":n}
   if err != nil {out["err"]=err.Error()}
   data,_ := json.Marshal(out);fmt.Println(string(data))
  }
 }
}`
			dir := t.TempDir()
			for name, content := range map[string]string{"go.mod": codegentest.GoMod("negative"), "parser.go": string(code), "main.go": harness} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != want {
				t.Fatalf("generated Go: %v\ngot %s\nwant %s", err, out, want)
			}
		})
	}
	t.Run("TypeScript", func(t *testing.T) {
		got := codegentest.RunTSGrammarScript(t, g, `import { parse, CodePoints, Bytes } from "./parser.ts";
for (const input of ["","é","ééé","x","éx"]) {
 for (const unit of [CodePoints, Bytes]) {
  const result=parse(input,unit);
  const out: Record<string,unknown>={};
  if(result.error) out["err"]=result.error.message;
  out["node"]=result.node;
  console.log(JSON.stringify(out));
 }
}`)
		if got != want {
			t.Fatalf("generated TS:\ngot %s\nwant %s", got, want)
		}
	})
}
