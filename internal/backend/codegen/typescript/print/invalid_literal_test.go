package print_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// literalTextUnits computes JS text units from the documented byte-preserving
// surrogate convention, independently of the TypeScript matcher.
func literalTextUnits(input string, unit sxirinput.PositionUnit) []rune {
	var units []rune
	for i := 0; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])
		if unit == sxirinput.Bytes && r == utf8.RuneError && size == 1 {
			units = append(units, 0xdc00+rune(input[i]))
		} else if r > 0xffff {
			r -= 0x10000
			units = append(units, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		} else {
			units = append(units, r)
		}
		i += size
	}
	return units
}

func TestGeneratedTSInvalidUTF8Literals(t *testing.T) {
	t.Parallel()
	for _, tc := range codegentest.InvalidLiteralCorpus() {
		if !codegentest.Sampled(tc.Name) {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			g, err := compiletest.Parse(tc.Src)
			if err != nil {
				t.Fatal(err)
			}
			prog, err := entry.Compile(g, entry.Options{})
			if err != nil {
				t.Fatal(err)
			}
			code, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", Recognize: true})
			if err != nil {
				t.Fatal(err)
			}
			var script, want strings.Builder
			script.WriteString(`import {parse,recognize,marshal,Node,CodePoints,Bytes} from "./parser.ts";` + "\n")
			for _, input := range tc.Inputs {
				fmt.Fprintf(&script, `for (const unit of [CodePoints,Bytes]){const input=new Uint8Array(Buffer.from(%q,"base64"));const r=parse(input,unit);const rec=recognize(input,unit);if((rec?.message??null)!==(r.error?.message??null))throw new Error("recognition differs");const child=r.node?.field("T");const expected=unit===Bytes?String.fromCharCode(...%s):String.fromCharCode(...%s);if(!r.error && (!(child instanceof Node)||child.text!==expected))throw new Error("matched bytes differ");const out:Record<string,unknown>={};if(r.error)out["err"]=r.error.message;out["node"]=JSON.parse(marshal(r.node));console.log(JSON.stringify(out));}`+"\n", base64.StdEncoding.EncodeToString([]byte(input)), print.TsInts(literalTextUnits(input, sxirinput.Bytes)), print.TsInts(literalTextUnits(input, sxirinput.CodePoints)))
				for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
					n, err := prog.ParseWith("main", input, engine.ParseOptions{Expectations: true, Unit: unit})
					want.WriteString(codegentest.ResultJSON(n, err) + "\n")
				}
			}
			dir := t.TempDir()
			for name, data := range map[string][]byte{"package.json": []byte(`{"type":"module"}`), "parser.ts": code, "main.ts": []byte(script.String())} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			node := tooltest.RequireNode(t)
			out, err := tooltest.NodeOutput(t, node, dir, "main.ts")
			if err != nil || string(out) != want.String() {
				t.Fatalf("generated TS: %v\n%s\ngot %s\nwant %s", err, tooltest.NodeStderr(err), out, want.String())
			}
			tsc := tooltest.RequireTSC(t)
			cmd := exec.Command(tsc, "--strict", "--target", "ES2022", "--module", "esnext", "--noEmit", "--skipLibCheck", "parser.ts")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("strict tsc: %v\n%s", err, out)
			}
		})
	}
}

// The generator rejects a grammar AST whose literal is not valid UTF-8, which validation rejects.
func TestGeneratedRawLiteralRejected(t *testing.T) {
	t.Parallel()
	for _, raw := range codegentest.RawLiteralValues() {
		g, err := compiletest.Parse("type N = { T node[string] }\nrule main N = t:\"x\" $$ => N{T:$t}")
		if err != nil {
			t.Fatal(err)
		}
		codegentest.ReplaceFirstLiteral(g.Rules()[0].Expr, raw)
		if _, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", Recognize: true}); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
			t.Errorf("%x: got %v; want the invalid UTF-8 error of the literal", raw, err)
		}
	}
}
