package print_test

import (
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// brokenGuards is a pass that runs after guards and leaves the first literal guard it finds out of
// step with its alternative, which core.Verify rejects.
var brokenGuards = pipe.New(pipe.Def[*core.Module, *core.Module]{
	Decl: pipe.Decl{
		Name:       "break-guard",
		Test:       true,
		After:      []string{corepass.PassGuards},
		Obligation: "none: it breaks the module on purpose",
	},
	In: corepass.IR, Out: corepass.IR,
	Run: func(_ *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
		for _, r := range m.Rules {
			if r == nil {
				continue
			}
			broke := false
			core.Walk(r.Body, func(n core.Node) {
				c, ok := n.(*core.Choice)
				if !ok || broke {
					return
				}
				for _, a := range c.Alts {
					if a.Guard != nil && a.Guard.Lit {
						g := *a.Guard
						g.Rune++
						a.Guard = &g
						broke = true
						return
					}
				}
			})
			if broke {
				break
			}
		}
		return m, pass.PreserveNone(), nil
	},
})

// withBrokenGuards returns hooks whose pipeline of Core passes has brokenGuards added after the
// pass guards. The mutant is no registered optimization: it declares that it is a test pass.
func withBrokenGuards() generate.Hooks {
	var ps []*pipe.Pass[*core.Module, *core.Module]
	for _, p := range corepass.Passes {
		ps = append(ps, p)
		if p.Name() == corepass.PassGuards {
			ps = append(ps, brokenGuards)
		}
	}
	return generate.Hooks{
		CorePipeline: func(string) pipe.Pipeline[*core.Module, *core.Module] { return pipe.Seq(corepass.IR, ps...) },
	}
}

// A pass that breaks the Core IR is caught: at the end of the run without VerifyEachPass, and by name
// right after the pass with it. The mutant is no pass of the pipeline, so this shows that the check
// between passes works in the generators, not that the passes are right.
func TestVerifyEachPassNamesTheBreakingPass(t *testing.T) {
	pg, err := parseTest(`rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)
	if err != nil {
		t.Fatal(err)
	}
	opts := common.GenOptions{Types: true, TypedOnly: true, Module: "example.com/p", Lib: common.LibImport}
	hooks := withBrokenGuards()

	_, err = print.GenerateParser(pg, codegentest.Compile, generate.Passes(opts, hooks), codegentest.WithLib(opts), nil)
	if err == nil || !strings.Contains(err.Error(), "verify core:") || strings.Contains(err.Error(), "break-guard") {
		t.Fatalf("without VerifyEachPass: %v", err)
	}
	opts.Debug.VerifyEach = true
	_, err = print.GenerateParser(pg, codegentest.Compile, generate.Passes(opts, hooks), codegentest.WithLib(opts), nil)
	if err == nil || !strings.Contains(err.Error(), "internal error: break-guard: ") {
		t.Fatalf("with VerifyEachPass: %v", err)
	}
	if !slices.Contains(corepass.Names(), corepass.PassGuards) {
		t.Fatal("no guards pass")
	}
}

// pipelineTables are builds that print functions of the given table from Flow IR and Exec IR: the
// Node table, the recognizer table and the typed table. The label of the table is the one of the
// dumps and errors.
var pipelineTables = []struct {
	table string
	opts  common.GenOptions
}{
	{"rules", common.GenOptions{Module: "example.com/p", Lib: common.LibImport}},
	{"recRules", common.GenOptions{Recognize: true, Module: "example.com/p", Lib: common.LibImport}},
	{"typed", common.GenOptions{Types: true, TypedOnly: true, Module: "example.com/p", Lib: common.LibImport}},
}

// Every function that the generator prints from Flow IR runs through the Flow pipeline, in each
// table, and a Flow pass that breaks a function is caught by the Flow verifier: at the end of the run
// without VerifyEachPass, and by name right after the pass with it.
func TestVerifyEachPassNamesTheBreakingFlowPass(t *testing.T) {
	pg, err := parseTest(`rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range pipelineTables {
		t.Run(c.table, func(t *testing.T) {
			seen := 0
			hooks := generate.Hooks{FlowPipeline: codegentest.BrokenFlowPipeline(c.table, &seen)}
			opts := c.opts
			_, err := print.GenerateParser(pg, codegentest.Compile, generate.Passes(opts, hooks), codegentest.WithLib(opts), nil)
			if err == nil || !strings.Contains(err.Error(), "of table "+c.table+": ") || !strings.Contains(err.Error(), "internal error: verify code@flow [") || strings.Contains(err.Error(), "break-flow") {
				t.Fatalf("without VerifyEachPass: %v", err)
			}
			if seen == 0 {
				t.Fatal("no function ran through the Flow pipeline")
			}
			opts.Debug.VerifyEach = true
			_, err = print.GenerateParser(pg, codegentest.Compile, generate.Passes(opts, hooks), codegentest.WithLib(opts), nil)
			if err == nil || !strings.Contains(err.Error(), "of table "+c.table+": ") || !strings.Contains(err.Error(), "internal error: break-flow [") {
				t.Fatalf("with VerifyEachPass: %v", err)
			}
		})
	}
}

// Every function that the generator prints from Exec IR runs through the Exec pipeline, in each
// table, and an Exec pass that breaks a module is caught by the Exec verifier: at the end of the run
// without VerifyEachPass, and by name right after the pass with it.
func TestVerifyEachPassNamesTheBreakingExecPass(t *testing.T) {
	pg, err := parseTest(`rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range pipelineTables {
		t.Run(c.table, func(t *testing.T) {
			seen := 0
			hooks := generate.Hooks{ExecPipeline: codegentest.BrokenExecPipeline(c.table, &seen)}
			opts := c.opts
			_, err := print.GenerateParser(pg, codegentest.Compile, generate.Passes(opts, hooks), codegentest.WithLib(opts), nil)
			if err == nil || !strings.Contains(err.Error(), "of table "+c.table+": ") || !strings.Contains(err.Error(), "internal error: verify code@exec [") || strings.Contains(err.Error(), "break-exec") {
				t.Fatalf("without VerifyEachPass: %v", err)
			}
			if seen == 0 {
				t.Fatal("no module ran through the Exec pipeline")
			}
			opts.Debug.VerifyEach = true
			_, err = print.GenerateParser(pg, codegentest.Compile, generate.Passes(opts, hooks), codegentest.WithLib(opts), nil)
			if err == nil || !strings.Contains(err.Error(), "of table "+c.table+": ") || !strings.Contains(err.Error(), "internal error: break-exec [") {
				t.Fatalf("with VerifyEachPass: %v", err)
			}
		})
	}
}

// A request to print the Core IR needs a writer to print to.
func TestPrintAfterNeedsAWriter(t *testing.T) {
	pg, err := parseTest("rule main = [a-z]+ $$\n")
	if err != nil {
		t.Fatal(err)
	}
	opts := common.GenOptions{Types: true, TypedOnly: true, Module: "example.com/p", Lib: common.LibImport, Debug: common.Debug{PrintAfter: []string{"guards"}}}
	if _, err := print.GenerateParser(pg, codegentest.Compile, codegentest.Passes(opts), codegentest.WithLib(opts), nil); err == nil || !strings.Contains(err.Error(), "Output") {
		t.Fatalf("err %v", err)
	}
}

// An Exec pass keeps the function that the lowering made and the locals of the Flow IR function in
// it, which the printers print: a pass that removes the function or drops such a local fails the
// generation. The pass that drops the locals renames them with their uses, so that the module it
// leaves is well formed and the check of the lowered function, not the Exec verifier, catches it.
func TestExecPassKeepsTheLoweredFunction(t *testing.T) {
	pg, err := parseTest(`rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, want string
		pass       *pipe.Pass[codepass.Unit, codepass.Unit]
	}{
		{"remove", "replaced or removed the function", testExecRemove},
		{"rename locals", "removed the local", testExecRenameLocals},
	} {
		t.Run(c.name, func(t *testing.T) {
			hooks := generate.Hooks{ExecPipeline: func(string) pipe.Pipeline[codepass.Unit, codepass.Unit] {
				return codepass.OnFuncs(append(slices.Clone(codepass.Passes), c.pass)...)
			}}
			opts := common.GenOptions{Module: "example.com/p", Lib: common.LibImport}
			if _, err := print.GenerateParser(pg, codegentest.Compile, generate.Passes(opts, hooks), codegentest.WithLib(opts), nil); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
		})
	}
}

// testExecRemove and testExecRenameLocals are Exec passes of the tests that break the lowered
// function on purpose: the first removes it from its unit, the second renames its locals.
var (
	testExecRemove = pipe.New(pipe.Def[codepass.Unit, codepass.Unit]{
		Decl: pipe.Decl{Name: "test-exec-remove", Test: true, Obligation: "none: it removes the function on purpose"},
		In:   code.ExecStage, Out: code.ExecStage,
		Run: func(_ *pipe.Ctx[codepass.Unit], u codepass.Unit) (codepass.Unit, pass.PreservedSet, error) {
			m := u.Module()
			m.Funcs = m.Funcs[1:]
			return u, pass.PreserveNone(), nil
		},
	})
	testExecRenameLocals = pipe.New(pipe.Def[codepass.Unit, codepass.Unit]{
		Decl: pipe.Decl{Name: "test-exec-rename-locals", Test: true, Obligation: "none: it renames the locals on purpose"},
		In:   code.ExecStage, Out: code.ExecStage,
		Run: func(_ *pipe.Ctx[codepass.Unit], u codepass.Unit) (codepass.Unit, pass.PreservedSet, error) {
			for _, f := range u.Module().Funcs {
				renameLocals(f)
			}
			return u, pass.PreserveNone(), nil
		},
	})
)

// renameLocals renames the locals of f and their uses, which keeps the function well formed.
func renameLocals(f *code.Func) {
	const suffix = "_renamed"
	names := map[string]bool{}
	for i := range f.Locals {
		names[f.Locals[i].Name] = true
		f.Locals[i].Name += suffix
	}
	seen := map[*code.Local]bool{}
	code.Walk(f.Body, func(s code.Stmt) {
		if st, ok := s.(*code.Set); ok {
			for i, d := range st.Dst {
				if !d.Reg && names[d.Name] {
					st.Dst[i].Name += suffix
				}
			}
		}
	}, func(e code.Expr) {
		if x, ok := e.(*code.Local); ok && names[x.Name] && !seen[x] {
			seen[x] = true
			x.Name += suffix
		}
	})
}
