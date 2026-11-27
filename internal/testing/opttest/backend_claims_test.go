package opttest

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/checked"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func init() { enginetest.UseEngine() }

// generatedModes are the generation options under which the observable of each generated backend is
// taken: a flag changes the backend's parser if it changes the output under one of them.
var generatedModes = map[pass.Backend][]common.GenOptions{
	pass.BackendGoNode:       {{NoLimitControls: true}, {}, {Document: true}},
	pass.BackendGoRecognizer: {{RecognizeOnly: true, NoLimitControls: true}, {RecognizeOnly: true}},
	pass.BackendTypedGo:      {{Types: true, TypedOnly: true, NoLimitControls: true}, {Types: true, TypedOnly: true}, {Types: true}},
	pass.BackendTypeScript:   {{NoLimitControls: true}, {}, {Types: true, NoLimitControls: true}},
}

// optInValues are the values that turn on the opt-ins with a count: their measured choice.
var optInValues = map[string]int{"class-tables": 5}

// toggled returns the setting of the flag against speed: off for a flag that speed turns on, on (or at
// its measured count) for an opt-in.
func toggled(f pass.Flag) map[string]int {
	if f.Default != 0 {
		return map[string]int{f.Name: 0}
	}
	if v, ok := optInValues[f.Name]; ok {
		return map[string]int{f.Name: v}
	}
	return map[string]int{f.Name: 1}
}

// observable returns what backend b builds of g with the values set over speed: the printed Core IR for
// the closure engine, the disassembled bytecode for the VMs, and the generated files, under each of the
// backend's modes, for the generated backends.
func observable(t *testing.T, g *ast.Grammar, b pass.Backend, set map[string]int) string {
	t.Helper()
	if pass.Engines.Has(b) {
		s := pass.Settings{}
		for name, v := range set {
			var err error
			if s, err = s.With(name, v); err != nil {
				t.Fatal(err)
			}
		}
		src, err := checked.Check(g, checked.Options{Settings: s}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if b == pass.BackendClosure {
			core, err := src.Core(nil)
			if err != nil {
				t.Fatal(err)
			}
			return ir.Print(core.(*closure.Module).Core, true)
		}
		m, err := src.Module()
		if err != nil {
			t.Fatal(err)
		}
		return m.Disassemble()
	}
	var out strings.Builder
	for _, o := range generatedModes[b] {
		o.Opt = set
		var files []common.GeneratedFile
		var err error
		if b == pass.BackendTypeScript {
			files, err = codegentest.GenerateTSRaw(g, o)
		} else {
			o.Module, o.Package = "example.com/p", "p"
			files, err = codegentest.GenerateRaw(t, g, o)
		}
		if err != nil {
			t.Fatalf("%s %+v: %v", b, o, err)
		}
		for _, f := range files {
			out.WriteString(f.Path + "\n")
			out.Write(f.Content)
		}
	}
	return out.String()
}

// corpus holds the cases of Corpus with their grammars, and the default observables, which every flag
// compares with.
type corpus struct {
	cases    []codegentest.Case
	grammars map[string]*ast.Grammar
	mu       sync.Mutex
	defaults map[[2]string]string
}

func newCorpus(t *testing.T) *corpus {
	c := &corpus{cases: Corpus(), grammars: map[string]*ast.Grammar{}, defaults: map[[2]string]string{}}
	for _, k := range c.cases {
		g, err := compiletest.Parse(k.Src)
		if err != nil {
			t.Fatalf("%s: %v", k.Name, err)
		}
		c.grammars[k.Name] = g
	}
	return c
}

// changes reports whether turning f against speed changes the parser of backend b for the case.
func (c *corpus) changes(t *testing.T, f pass.Flag, name string, b pass.Backend) bool {
	t.Helper()
	key := [2]string{name, b.String()}
	c.mu.Lock()
	def, ok := c.defaults[key]
	c.mu.Unlock()
	if !ok {
		def = observable(t, c.grammars[name], b, nil)
		c.mu.Lock()
		c.defaults[key] = def
		c.mu.Unlock()
	}
	return observable(t, c.grammars[name], b, toggled(f)) != def
}

// Each flag's witnesses are cases of the corpus, and on each backend the flag lists, turning the flag
// against speed changes the parser of one of its witnesses: the registry's backend claims hold. The
// long tier checks the other half of the claims over the whole corpus: the flag changes the parser of
// no backend it does not list.
func TestBackendClaims(t *testing.T) {
	c := newCorpus(t)
	for _, f := range pass.Flags() {
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()
			for _, w := range f.Witnesses {
				if _, ok := c.grammars[w]; !ok {
					t.Fatalf("the witness %q is not a case of the corpus", w)
				}
			}
			for _, b := range f.Backends.List() {
				if !slices.ContainsFunc(f.Witnesses, func(w string) bool { return c.changes(t, f, w, b) }) {
					t.Errorf("turning %s against speed changes the %s parser of none of its witnesses %q", f.Name, b, f.Witnesses)
				}
			}
			if !codegentest.Long() {
				return
			}
			for _, b := range pass.AllBackends() {
				if f.Backends.Has(b) {
					continue
				}
				for _, k := range c.cases {
					if c.changes(t, f, k.Name, b) {
						t.Errorf("turning %s against speed changes the %s parser of %q, a backend it does not list", f.Name, b, k.Name)
					}
				}
			}
		})
	}
}

// The comparison notices a change: a flag that the corpus shows changing a backend is reported as
// changing it, and a backend it does not touch as unchanged.
func TestBackendClaimsNoticeChanges(t *testing.T) {
	c := newCorpus(t)
	guards, ok := pass.Lookup("guards")
	if !ok {
		t.Fatal("no flag guards")
	}
	if !c.changes(t, guards, "guards", pass.BackendClosure) {
		t.Error("turning guards off does not change the closure parser of the case guards")
	}
	outline, _ := pass.Lookup("outline")
	if c.changes(t, outline, "outlined keywords", pass.BackendClosure) {
		t.Error("turning outline off changes the closure parser")
	}
}
