package compiler_test

import (
	"bytes"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/vm"
)

// WithOptimize and the PrintAfter field of WithDebug are the flags -optimize, -no-opt and -print-after
// of sxir gen: a name that is turned off changes the code, an unknown name or profile is an error that
// lists the valid ones, and a request to print needs a writer.
func TestOptimizationFields(t *testing.T) {
	src := grammar.FromString("opt.sxir", `@start
rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)
	type gens struct {
		name     string
		generate func(optimize string, noOpt []string, printAfter string, w *bytes.Buffer) ([]compiler.File, error)
	}
	for _, c := range []gens{
		{"go", func(optimize string, noOpt []string, printAfter string, w *bytes.Buffer) ([]compiler.File, error) {
			c := compiler.GoConfig{Module: "opt", Lib: compiler.LibImport}
			return compiler.GenerateGo(src, c, compiler.WithStart("main"), compiler.WithOptimize(compiler.Optimize{Profile: optimize, Off: noOpt}), printing(printAfter, w))
		}},
		{"ts", func(optimize string, noOpt []string, printAfter string, w *bytes.Buffer) ([]compiler.File, error) {
			return compiler.GenerateTypeScript(src, compiler.TSConfig{}, compiler.WithStart("main"), compiler.WithOptimize(compiler.Optimize{Profile: optimize, Off: noOpt}), printing(printAfter, w))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			base, err := c.generate("", nil, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			speed, err := c.generate("speed", nil, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(joinFiles(base), joinFiles(speed)) {
				t.Error("the empty profile and speed differ")
			}
			off, err := c.generate("", []string{"guards"}, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(joinFiles(base), joinFiles(off)) {
				t.Error("-no-opt guards changes nothing")
			}
			if _, err := c.generate("", []string{"nonsense"}, "", nil); err == nil || !strings.Contains(err.Error(), "valid: dispatch, guards, outline, scans, field-compare,") {
				t.Errorf("unknown optimization: %v", err)
			}
			if _, err := c.generate("nosuch", nil, "", nil); err == nil || !strings.Contains(err.Error(), "valid: speed") {
				t.Errorf("unknown profile: %v", err)
			}
			if _, err := c.generate("", nil, "guards", nil); err == nil || !strings.Contains(err.Error(), "Output") {
				t.Errorf("print without a writer: %v", err)
			}
			if _, err := c.generate("", nil, "nonsense", new(bytes.Buffer)); err == nil || !strings.Contains(err.Error(), "unknown pass") {
				t.Errorf("unknown pass: %v", err)
			}
			var w bytes.Buffer
			printed, err := c.generate("", nil, "guards", &w)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(joinFiles(base), joinFiles(printed)) {
				t.Error("printing changes the output")
			}
			if !strings.Contains(w.String(), "-- after guards [") || !strings.Contains(w.String(), "alt guard=") {
				t.Errorf("printed:\n%s", w.String())
			}
		})
	}
}

// printing returns the debugging setting that prints after the named pass, if any, to w, if not nil.
func printing(printAfter string, w *bytes.Buffer) compiler.Option {
	d := compiler.Debug{}
	if printAfter != "" {
		d.PrintAfter = []string{printAfter}
	}
	if w != nil {
		d.Output = w
	}
	return compiler.WithDebug(d)
}

// TestOptimizeAcceptedByEveryFunction checks that every function of the package takes WithOptimize:
// it accepts every profile and a known optimization turned off, and rejects an unknown profile or
// optimization, whatever artifact it makes, and the parsers that a profile makes agree with the
// one that no setting made.
func TestOptimizeAcceptedByEveryFunction(t *testing.T) {
	src := grammar.FromString("opt.sxir", `@start
rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)
	type artifact struct {
		name string
		make func(o compiler.Optimize) error
	}
	artifacts := []artifact{
		{"Compile", func(o compiler.Optimize) error { _, err := compiler.Compile(src, compiler.WithOptimize(o)); return err }},
		{"NewReferenceParser", func(o compiler.Optimize) error {
			_, err := compiler.NewReferenceParser(src, compiler.WithOptimize(o))
			return err
		}},
		{"Marshal", func(o compiler.Optimize) error {
			_, err := compiler.Marshal(src, vm.CompiledParse, compiler.WithOptimize(o))
			return err
		}},
		{"OutputSchema", func(o compiler.Optimize) error {
			_, err := compiler.OutputSchema(src, compiler.WithOptimize(o))
			return err
		}},
		{"GenerateGo", func(o compiler.Optimize) error {
			_, err := compiler.GenerateGo(src, compiler.GoConfig{Module: "opt", Lib: compiler.LibImport}, compiler.WithOptimize(o))
			return err
		}},
		{"GenerateTypeScript", func(o compiler.Optimize) error {
			_, err := compiler.GenerateTypeScript(src, compiler.TSConfig{}, compiler.WithOptimize(o))
			return err
		}},
	}
	for _, a := range artifacts {
		t.Run(a.name, func(t *testing.T) {
			for _, ok := range []compiler.Optimize{{}, compiler.Speed, compiler.Memory, compiler.Size, {Off: []string{"guards"}}} {
				if err := a.make(ok); err != nil {
					t.Errorf("%+v: %v", ok, err)
				}
			}
			for _, bad := range []compiler.Optimize{{Profile: "fastest"}, {Off: []string{"nonsense"}}, {Values: map[string]int{"nonsense": 1}}} {
				if err := a.make(bad); err == nil {
					t.Errorf("%+v: accepted", bad)
				}
			}
		})
	}
	p0, err := compiler.Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := compiler.Compile(src, compiler.WithOptimize(compiler.Size))
	if err != nil {
		t.Fatal(err)
	}
	n0, err0 := p0.Parse("if x 12")
	n1, err1 := p1.Parse("if x 12")
	if err0 != nil || err1 != nil || n0.String() != n1.String() {
		t.Errorf("the profile changes the parser: %v %v\n%v\n%v", err0, err1, n0, n1)
	}
}

// WithOptimize applies to the parsers of Compile and NewReferenceParser as to the generators: the
// profiles leave the bytecode as it is, since their optimizations are those of generated code,
// turning a Core pass off changes it, and the parsers built with the pass off agree with the
// reference evaluator of the language on what they match.
func TestOptimizeReachesTheRunTimeParsers(t *testing.T) {
	src := grammar.FromString("opt.sxir", `@start
rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)
	marshal := func(o compiler.Optimize) []byte {
		b, err := compiler.Marshal(src, vm.CompiledParse, compiler.WithOptimize(o))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	base := marshal(compiler.Optimize{})
	for _, p := range []compiler.Optimize{compiler.Speed, compiler.Memory, compiler.Size} {
		if !bytes.Equal(marshal(p), base) {
			t.Errorf("the profile %s changes the bytecode", p.Profile)
		}
	}
	off := compiler.Optimize{Off: []string{"guards"}}
	if bytes.Equal(marshal(off), base) {
		t.Error("turning guards off leaves the bytecode as it is")
	}
	g, err := grammar.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	onVM, err := compiler.Compile(src, compiler.WithOptimize(off))
	if err != nil {
		t.Fatal(err)
	}
	onClosures, err := compiler.NewReferenceParser(src, compiler.WithOptimize(off))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"", "if x 12", "else", "if", "12 ab", "x1 y", "if 1 ", "  ", "IF", "1a"} {
		ref, _, err := examinedref.Evaluate(g, "main", in)
		if err != nil {
			t.Fatal(err)
		}
		want := ref.Matched && ref.End == len(in)
		for name, p := range map[string]runtime.Parser{"Compile": onVM, "NewReferenceParser": onClosures} {
			if _, err := p.Parse(in); (err == nil) != want {
				t.Errorf("%s with guards off: %q parses with err %v; the reference matches it: %v", name, in, err, want)
			}
		}
	}
}
