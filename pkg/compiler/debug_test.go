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
func parses(p runtime.Parser, input string, opts ...runtime.ParseOption) string {
	n, err := p.Parse(input, opts...)
	return fmt.Sprint(n, err)
}

// missingDebugGrammar is debugGrammar with a #missing literal, whose parses with a missing-token
// limit run the insertion program derived from the call's build.
var missingDebugGrammar = grammar.FromString("missing.sxir", `@start
rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+ / "(" [a-z]* ")" #missing
rule num = [0-9]+
`)

// WithDebug writes the dumps of the run-time builds during the call: Compile those of the bytecode,
// which the recursive and the iterative VM both run, NewReferenceParser those of the closures' Core
// module, each labeled with its backend; a parse afterwards, on either VM or with the closures that
// build their module again, writes nothing more, also one that inserts missing tokens and so runs
// the insertion program. Where the stages coincide, the Core IR of the table, the dumps agree with
// each other and, for a grammar without #missing, with those of a generated Go parser with
// Documents, whose Node table the run-time backends build.
func TestDebugRunTimeBuilds(t *testing.T) {
	for _, c := range []struct {
		name    string
		g       grammar.Source
		inputs  []string
		missing bool
	}{
		{"without #missing", debugGrammar, []string{"if x 12 else"}, false},
		{"with #missing", missingDebugGrammar, []string{"if x 12 else", "if (x 12", "(x) (y"}, true},
	} {
		t.Run(c.name, func(t *testing.T) { checkDebugRunTimeBuilds(t, c.g, c.inputs, c.missing) })
	}
}

func checkDebugRunTimeBuilds(t *testing.T, g grammar.Source, inputs []string, missing bool) {
	all := func(w *bytes.Buffer) compiler.Option {
		return compiler.WithDebug(compiler.Debug{PrintAfter: []string{"all"}, Output: w})
	}
	var opts []runtime.ParseOption
	if missing {
		opts = append(opts, runtime.WithMissingTokens(4))
	}

	var vmOut bytes.Buffer
	p, err := compiler.Compile(g, all(&vmOut))
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
	var recursive []string
	for _, input := range inputs {
		r, i := parses(p, input, opts...), parses(p.(*vm.Parser).Iterative(), input, opts...)
		if r != i {
			t.Errorf("the VMs parse %q differently: %s and %s", input, r, i)
		}
		recursive = append(recursive, r)
	}
	if vmOut.Len() != written {
		t.Errorf("the parses on the VMs wrote %q", vmOut.String()[written:])
	}
	if missing && !strings.Contains(strings.Join(recursive, "\n"), "syntax error") {
		t.Errorf("no parse inserted a missing token: %q", recursive)
	}

	var clOut bytes.Buffer
	ref, err := compiler.NewReferenceParser(g, all(&clOut))
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
	for i, input := range inputs {
		if got := parses(ref, input, opts...); got != recursive[i] {
			t.Errorf("the closures parse %q as %s, the VM as %s", input, got, recursive[i])
		}
		// Recognition returns the errors of the parse, the recovered ones of insertions included.
		_, perr := ref.Parse(input, opts...)
		if err := ref.Recognize(input, opts...); fmt.Sprint(err) != fmt.Sprint(perr) {
			t.Errorf("Recognize %q: %v, the parse %v", input, err, perr)
		}
	}
	if clOut.Len() != written {
		t.Errorf("the parses on the closures wrote %q", clOut.String()[written:])
	}

	for _, step := range []string{"elaborate", "guards", "scans"} {
		if v, c := vmDumps[step+" [vm/rules]"], clDumps[step+" [closure/rules]"]; v == "" || v != c {
			t.Errorf("the dumps after %s differ: vm %d bytes, closure %d", step, len(v), len(c))
		}
	}
	if missing {
		return // generated parsers do not run #missing
	}
	var genOut bytes.Buffer
	if _, err := compiler.GenerateGo(g, compiler.GoConfig{Module: "example.com/m", Lib: compiler.LibImport, Document: true}, all(&genOut)); err != nil {
		t.Fatal(err)
	}
	genDumps := dumpsOf(genOut.String())
	for _, step := range []string{"elaborate", "guards", "scans"} {
		if v, g := vmDumps[step+" [vm/rules]"], genDumps[step+" [rules]"]; v != g {
			t.Errorf("the dumps after %s differ: vm %d bytes, generated Go %d", step, len(v), len(g))
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
