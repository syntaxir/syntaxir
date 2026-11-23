package compiler_test

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/vm"
)

// debugGrammar is the grammar of the debugging tests: a few rules with choices and repetitions, so
// that the Core and Flow passes have work.
var debugGrammar = grammar.FromString("debug.sxir", `@start
rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)

// dumpHeader is the header line of a dump: the step instance and the label of its unit.
var dumpHeader = regexp.MustCompile(`(?m)^-- after (\S+) \[([^\]]*)\] --$`)

// dumpsOf splits the dumps that PrintAfter wrote into their bodies by "step [label]".
func dumpsOf(text string) map[string]string {
	out := map[string]string{}
	idx := dumpHeader.FindAllStringSubmatchIndex(text, -1)
	for i, m := range idx {
		end := len(text)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out[text[m[2]:m[3]]+" ["+text[m[4]:m[5]]+"]"] = text[m[1]:end]
	}
	return out
}

// parses parses input with p and returns what it returns as text.
func parses(p runtime.Parser, input string) string {
	n, err := p.Parse(input)
	return fmt.Sprint(n, err)
}

// WithDebug writes the dumps of the run-time builds during the call: Compile those of the bytecode,
// which the recursive and the iterative VM both run, NewReferenceParser those of the closures' Core
// module, each labeled with its backend; a parse afterwards, on either VM or with the closures that
// build their module again, writes nothing more. Where the stages coincide, the Core IR of the
// table, the dumps agree with each other and with those of a generated Go parser with Documents,
// whose Node table the run-time backends build.
func TestDebugRunTimeBuilds(t *testing.T) {
	all := func(w *bytes.Buffer) compiler.Option {
		return compiler.WithDebug(compiler.Debug{PrintAfter: []string{"all"}, Output: w})
	}
	const input = "if x 12 else"

	var vmOut bytes.Buffer
	p, err := compiler.Compile(debugGrammar, all(&vmOut))
	if err != nil {
		t.Fatal(err)
	}
	written := vmOut.Len()
	vmDumps := dumpsOf(vmOut.String())
	for _, step := range []string{"elaborate [vm/rules]", "flowlower [vm/rules]", "flow-finish [vm/rules]", "canonicalize [vm/rules/main]"} {
		if _, ok := vmDumps[step]; !ok {
			t.Errorf("Compile wrote no dump %q; it wrote %d", step, len(vmDumps))
		}
	}
	for label := range vmDumps {
		if !strings.Contains(label, "[vm/") {
			t.Errorf("the dump %q of Compile does not name the backend vm", label)
		}
	}
	recursive, iterative := parses(p, input), parses(p.(*vm.Parser).Iterative(), input)
	if recursive != iterative {
		t.Errorf("the VMs parse differently: %s and %s", recursive, iterative)
	}
	if vmOut.Len() != written {
		t.Errorf("the parses on the VMs wrote %q", vmOut.String()[written:])
	}

	var clOut bytes.Buffer
	ref, err := compiler.NewReferenceParser(debugGrammar, all(&clOut))
	if err != nil {
		t.Fatal(err)
	}
	written = clOut.Len()
	clDumps := dumpsOf(clOut.String())
	if _, ok := clDumps["elaborate [closure/rules]"]; !ok {
		t.Errorf("NewReferenceParser wrote no dump of elaborate; it wrote %d", len(clDumps))
	}
	for label := range clDumps {
		if !strings.Contains(label, "[closure/") {
			t.Errorf("the dump %q of NewReferenceParser does not name the backend closure", label)
		}
	}
	if got := parses(ref, input); got != recursive {
		t.Errorf("the closures parse %s, the VM %s", got, recursive)
	}
	if err := ref.Recognize(input); err != nil {
		t.Errorf("Recognize: %v", err)
	}
	if clOut.Len() != written {
		t.Errorf("the parses on the closures wrote %q", clOut.String()[written:])
	}

	var genOut bytes.Buffer
	if _, err := compiler.GenerateGo(debugGrammar, compiler.GoConfig{Module: "example.com/m", Lib: compiler.LibImport, Document: true}, all(&genOut)); err != nil {
		t.Fatal(err)
	}
	genDumps := dumpsOf(genOut.String())
	for _, step := range []string{"elaborate", "guards", "scans"} {
		v, c, g := vmDumps[step+" [vm/rules]"], clDumps[step+" [closure/rules]"], genDumps[step+" [rules]"]
		if v == "" || v != c || v != g {
			t.Errorf("the dumps after %s differ: vm %d bytes, closure %d, generated Go %d", step, len(v), len(c), len(g))
		}
	}
}

// A call without a debugging setting, or with one that writes nothing, writes nothing, during the
// call or in the parses after it; the pipeline text and the reports of a run-time build name its
// backend.
func TestDebugRunTimeWritesOnlyWhatIsSelected(t *testing.T) {
	for _, d := range []compiler.Debug{{}, {VerifyEach: true}} {
		var out bytes.Buffer
		d.Output = &out
		p, err := compiler.Compile(debugGrammar, compiler.WithDebug(d))
		if err != nil {
			t.Fatal(err)
		}
		ref, err := compiler.NewReferenceParser(debugGrammar, compiler.WithDebug(d))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compiler.Marshal(debugGrammar, vm.CompiledParse, compiler.WithDebug(d)); err != nil {
			t.Fatal(err)
		}
		_, _, _ = parses(p, "if"), parses(p.(*vm.Parser).Iterative(), "if"), parses(ref, "if")
		if out.Len() != 0 {
			t.Errorf("%+v wrote %q", d, out.String())
		}
	}

	var out bytes.Buffer
	if _, err := compiler.Compile(debugGrammar, compiler.WithDebug(compiler.Debug{PrintPipeline: true, Stats: true, Output: &out})); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"\ntarget vm", "\nprogram ", "-- stats --", "vm/elaborate"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output of Compile with PrintPipeline and Stats has no %q:\n%s", want, text)
		}
	}
}
