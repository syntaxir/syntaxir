package libprint

import (
	"go/format"
	"slices"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
)

// The conformance bindings of the operations that a target prints inline: for each, a function that
// calls only the operation, printed by the target's own printer, so that the conformance suite
// (internal/ir/code/exec/conform) runs the code that the target's compositions run, not a copy of it.

// engineExpBindingsFile is the file of the engine's conformance bindings of the expectation record,
// relative to the root of the module. It is a test file of package state of the engine.
const engineExpBindingsFile = "internal/runtime/state/exp_bind_exec_test.go"

// expBindingMemoOps are the operations of the memo that keep the expectations of an entry.
var expBindingMemoOps = []string{"memo.far", "memo.expected", "memo.setExpected"}

// expBindingName returns the name of the printed binding of the operation op: "conf" and the parts of
// its name, each capitalized, as confExpIsolate for exp.isolate.
func expBindingName(op string) string {
	var b strings.Builder
	b.WriteString("conf")
	for part := range strings.SplitSeq(op, ".") {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// docBindingEngineOps are the doc operations that the engine has a Go form for: it has no observation of
// calls, so it has neither doc.examinedBegin nor doc.examinedEnd.
var docBindingEngineOps = []string{"doc.enter", "doc.leave", "doc.active", "doc.touch", "doc.touchFrom", "doc.low", "doc.examined",
	"doc.reused", "doc.evaluated"}

// docBindingOps are the doc operations of the node library.
var docBindingOps = []string{"doc.examinedBegin", "doc.examinedEnd", "doc.enter", "doc.leave", "doc.active", "doc.tracking", "doc.touch",
	"doc.touchFrom", "doc.low", "doc.examined", "doc.reused", "doc.evaluated"}

// oprBindingDescriptorOps are the operations of the Operators Rule descriptors and of the matches of
// operators that conformance cases cover. oprBindingEngineOps adds those of the records of the split
// forms of the loop, which the engine has and the node library does not.
var oprBindingDescriptorOps = []string{
	"opr.led", "opr.prefix", "opr.count", "opr.at", "opr.operands", "opr.operand", "opr.line", "opr.level",
	"opr.infix", "opr.postfix", "opr.right", "opr.nonAssoc", "opr.scope", "opr.hasSkip",
	"opr.attempt", "opr.found", "opr.op", "opr.end", "opr.cut", "opr.env", "opr.errs",
	"opr.setCutFailed", "opr.cutFailed", "opr.setNudBound", "opr.nudBound", "opr.setAfterEmpty", "opr.afterEmpty",
}

// oprBindingEngineOps returns the operations of the Operators Rule family that the engine binds: the
// descriptors, those that run the grammar code of a rule (conform.OprMatchOps), those that make its
// values (conform.OprValueBuildOps), those of the missing-token mode (conform.OprMissingOps), and the
// records.
func oprBindingEngineOps(m *code.Module) []string {
	names := append(slices.Clone(oprBindingDescriptorOps), "opr.skip", "opr.match", "opr.lineValue", "opr.build",
		"opr.attemptMissing", "opr.replay")
	for _, o := range m.Ops {
		if o.Component == exec.Suspended && strings.HasPrefix(o.Name, "opr") {
			names = append(names, o.Name)
		}
	}
	return names
}

// expBindingOps returns the operations of the expectation record of the compositions module m and of
// expBindingMemoOps.
func expBindingOps(m *code.Module) []string {
	var names []string
	for _, o := range m.Ops {
		if o.Component == exec.Exp {
			names = append(names, o.Name)
		}
	}
	return append(names, expBindingMemoOps...)
}

// bindings returns a copy of the compositions module m with a binding function for each operation
// of names, and the bindings in that order. A context state (exec.Ctx) and a growth (exec.Grow) are values
// that the operations change in place, so a binding returns each one it takes after its results, for
// the harness to keep.
func bindings(m *code.Module, names []string) (*code.Module, []*code.Func) {
	m = m.Clone()
	var fs []*code.Func
	for _, name := range names {
		o := m.Op(name)
		var states []code.Expr
		results := slices.Clone(o.Results)
		for _, p := range o.Params {
			if isState(p.Type) {
				states = append(states, code.L(p.Name))
				results = append(results, p.Type)
			}
		}
		f := m.AddFunc(expBindingName(name), o.Params, results, o.Features, func(b *code.Builder) {
			args := make([]code.Expr, len(o.Params))
			for i, p := range o.Params {
				args[i] = code.L(p.Name)
			}
			switch len(o.Results) {
			case 0:
				b.Do(name, args...)
				if len(states) > 0 {
					b.Return(states...)
				}
			case 1:
				// A declared result, since a nil result has no type of its own.
				// A result that a parameter does not name.
				res := "r"
				if slices.ContainsFunc(o.Params, func(p code.Param) bool { return p.Name == res }) {
					res = "res"
				}
				b.Var(res, o.Results[0])
				b.Set(res, code.C(name, args...))
				b.Return(append([]code.Expr{code.L(res)}, states...)...)
			default:
				rs := make([]string, len(o.Results))
				for i := range rs {
					rs[i] = "r" + string(rune('0'+i))
				}
				b.Return(append(b.LetN(rs, code.C(name, args...)), states...)...)
			}
		})
		f.Doc = expBindingName(name) + " runs " + name + " for the conformance suite."
		fs = append(fs, f)
	}
	return m, fs
}

// isState reports whether t is a handle that the targets hold by value and the operations change in
// place: a context state or a growth.
func isState(t code.Type) bool { return t == code.THandle(exec.Ctx) || t == code.THandle(exec.Grow) }

// bindingFile prints the bindings of the compositions module m with the target t into a Go file of
// package pkg. A file of the
// engine's package state or of the library imports the packages that it names.
func bindingFile(m *code.Module, pkg string, names []string, t *print.Target, table string) (string, error) {
	m, fs := bindings(m, names)
	name := t.FuncName
	t.FuncName = func(n string) string {
		if strings.HasPrefix(n, "conf") {
			return n
		}
		return name(n)
	}
	var b strings.Builder
	for _, f := range fs {
		src, err := print.Func(m, f, t)
		if err != nil {
			return "", err
		}
		b.WriteString("\n" + src)
	}
	if table != "" {
		// The table of the bindings by operation, for a harness that cannot name unexported methods.
		b.WriteString("\n// " + table + " returns the bindings of p by the name of the operation.\nfunc " + table + "(" + t.Recv + ") map[string]any {\n\treturn map[string]any{\n")
		for i, f := range fs {
			b.WriteString("\t\t" + strconv.Quote(names[i]) + ": p." + f.Name + ",\n")
		}
		b.WriteString("\t}\n}\n")
	}
	header := "// Code generated by internal/backend/codegen/golang/libprint; DO NOT EDIT.\n\npackage " + pkg + "\n"
	switch pkg {
	case "state":
		header += engineImports(b.String(), nil)
	case "libgo":
		header += libImports(b.String(), nil)
	}
	if table != "" {
		out, err := format.Source([]byte(header + b.String()))
		return string(out), err
	}
	return header + b.String(), nil
}

// printEngineExpBindings returns the Go test file of the conformance bindings of the expectation record
// for package state of the engine.
func printEngineExpBindings(u file) (string, error) {
	return bindingFile(u.module(), "state", expBindingOps(u.module()), engineTarget(), "")
}

// GoExpBindings returns the Go test file of the conformance bindings of the expectation record for
// package libgo, for the node library's Parser.
func GoExpBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", expBindingOps(compose.Build()), goTarget(goSpec{fn: "expect", name: "Expect"}), "")
}

// GoTypedExpBindings returns the Go test file of the conformance bindings of the operations of the memo
// that keep the expectations of an entry for package libgo, for the typed library's TParser, whose
// entries differ from the node library's; the record itself is the node Parser's (GoExpBindings).
func GoTypedExpBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", expBindingMemoOps, goTarget(goSpec{fn: "reset", typed: true, name: "Reset"}), "")
}

// engineDocBindingsFile is the file of the engine's conformance bindings of the doc operations,
// relative to the root of the module. It is a test file of package state of the engine.
const engineDocBindingsFile = "internal/runtime/state/doc_bind_exec_test.go"

// printEngineDocBindings returns the Go test file of the conformance bindings of the doc operations for
// package state of the engine.
func printEngineDocBindings(u file) (string, error) {
	return bindingFile(u.module(), "state", docBindingEngineOps, engineTarget(), "")
}

// GoDocBindings returns the Go test file of the conformance bindings of the doc operations for
// package libgo, for the node library's Parser.
func GoDocBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", docBindingOps, goTarget(goSpec{fn: "expect", name: "Expect"}), "")
}

// engineOprBindingsFile is the file of the engine's conformance bindings of the Operators Rule
// operations, relative to the root of the module. It is a test file of package state of the
// engine.
const engineOprBindingsFile = "internal/runtime/state/opr_bind_exec_test.go"

// printEngineOprBindings returns the Go test file of the conformance bindings of the Operators Rule
// operations for package state of the engine.
func printEngineOprBindings(u file) (string, error) {
	return bindingFile(u.module(), "state", oprBindingEngineOps(u.module()), engineTarget(), "confOprBindings")
}

// oprBindingGoOps returns the Operators Rule operations of a Go library: the descriptors, those that run
// the grammar code of a rule (conform.OprMatchOps) and those that make its values, with opr.build for the
// node library and opr.buildTyped for the typed one. The libraries have neither the missing-token mode nor
// the records of the split forms of the loop.
func oprBindingGoOps(typed bool) []string {
	build := "opr.build"
	if typed {
		build = "opr.buildTyped"
	}
	return append(slices.Clone(oprBindingDescriptorOps), "opr.skip", "opr.match", "opr.lineValue", build)
}

// GoOprBindings returns the Go test file of the conformance bindings of the Operators Rule operations
// for package libgo, for the node library's Parser.
func GoOprBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", oprBindingGoOps(false), goTarget(goSpec{fn: "expect", name: "Expect"}), "confOprBindings")
}

// GoTypedOprBindings returns the Go test file of the conformance bindings of the Operators Rule
// operations for package libgo, for the typed library's TParser.
func GoTypedOprBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", oprBindingGoOps(true), goTarget(goSpec{fn: "reset", typed: true, name: "Reset"}), "confTOprBindings")
}

// ctxBindingOps are the operations of the dependency memo of left recursion and of the growth outside
// a context that conformance cases cover.
var ctxBindingOps = []string{
	"ctx.enter", "ctx.found", "ctx.ok", "ctx.value", "ctx.end", "ctx.errs", "ctx.far", "ctx.expected",
	"ctx.allows", "ctx.active", "ctx.lookup", "ctx.push", "ctx.pop", "ctx.storeMemo", "ctx.storeCtx", "memo.setPresent",
	"ctx.keep", "ctx.done", "grow.begin", "grow.ctx", "grow.longer", "grow.adopt",
	"grow.memoStep", "grow.isolate", "grow.keepStep", "grow.grew", "grow.adoptStep", "grow.end",
}

// engineCtxBindingsFile is the file of the engine's conformance bindings of the dependency memo,
// relative to the root of the module. It is a test file of package state of the engine.
const engineCtxBindingsFile = "internal/runtime/state/ctx_bind_exec_test.go"

// printEngineCtxBindings returns the Go test file of the conformance bindings of the dependency memo for
// package state of the engine.
//
// The engine's grow.end is not here: its printed call names the rule of the enclosing composition, which
// the operation does not take, so the suite binds it by hand (ctxMachine.growEnd).
func printEngineCtxBindings(u file) (string, error) {
	ops := slices.DeleteFunc(slices.Clone(ctxBindingOps), func(n string) bool { return n == "grow.end" })
	return bindingFile(u.module(), "state", ops, engineTarget(), "confCtxBindings")
}

// GoCtxBindings returns the Go test file of the conformance bindings of the dependency memo for
// package libgo, for the node library's Parser.
func GoCtxBindings() (string, error) {
	// The library's growEnd returns the result by value, where the operation's result is the handle of
	// a result, so the suite binds grow.end by hand (ctxMachine.growEnd).
	ops := slices.DeleteFunc(slices.Clone(ctxBindingOps), func(n string) bool { return n == "grow.end" })
	return bindingFile(compose.Build(), "libgo", ops, goTarget(goSpec{fn: "expect", name: "Expect"}), "confCtxBindings")
}

// frameBindingOps are the frame operations that conformance cases cover.
var frameBindingOps = []string{
	"frame.mark", "frame.markPos", "frame.markRec", "frame.markEnv", "frame.markMissing", "frame.markTrail",
	"frame.undo", "frame.current", "frame.use", "frame.open", "frame.set", "frame.close", "frame.slots",
	"frame.save", "frame.restore",
}

// frameBindingGoOps are the frame operations of the Go library, which has no missing-token record.
var frameBindingGoOps = slices.DeleteFunc(slices.Clone(frameBindingOps), func(n string) bool { return n == "frame.markMissing" })

// engineFrameBindingsFile is the file of the engine's conformance bindings of the capture frames,
// relative to the root of the module. It is a test file of package state of the engine.
const engineFrameBindingsFile = "internal/runtime/state/frame_bind_exec_test.go"

// printEngineFrameBindings returns the Go test file of the conformance bindings of the capture frames for
// package state of the engine.
func printEngineFrameBindings(u file) (string, error) {
	return bindingFile(u.module(), "state", frameBindingOps, engineTarget(), "confFrameBindings")
}

// GoFrameBindings returns the Go test file of the conformance bindings of the capture frames for
// package libgo, for the node library's Parser.
func GoFrameBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", frameBindingGoOps, goTarget(goSpec{fn: "expect", name: "Expect"}), "confFrameBindings")
}

// GoTypedFrameBindings returns the Go test file of the conformance bindings of the capture frames for
// package libgo, for the typed library's TParser.
func GoTypedFrameBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", frameBindingGoOps, goTarget(goSpec{fn: "reset", typed: true, name: "Reset"}), "confTFrameBindings")
}

// engineRuleBindingsFile is the file of the engine's conformance bindings of the operations of the
// rules, relative to the root of the module. It is a test file of package state of the engine.
const engineRuleBindingsFile = "internal/runtime/state/rule_bind_exec_test.go"

// ruleBindingEngineOps are the operations of the rules that the engine provides, with those of the
// deferral of memoization (seen.defers, seen.first), which firstCall composes with rule.seen.
var ruleBindingEngineOps = []string{"rule.memo", "rule.leader", "rule.plain", "rule.docOnlyMemo", "rule.docPlain",
	"rule.positional", "rule.id", "rule.seen", "rule.invoke", "rule.invokePlain", "seen.defers", "seen.first"}

// ruleBindingOps are the operations of the rules that the node library provides, with those of the
// deferral of memoization.
var ruleBindingOps = []string{"rule.memo", "rule.leader", "rule.plain", "rule.docMemo", "rule.docOnlyMemo",
	"rule.positional", "rule.id", "rule.seen", "rule.invoke", "rule.invokePlain", "seen.defers", "seen.first"}

// printEngineRuleBindings returns the Go test file of the conformance bindings of the operations of the rules
// for package state of the engine.
func printEngineRuleBindings(u file) (string, error) {
	return bindingFile(u.module(), "state", ruleBindingEngineOps, engineTarget(), "")
}

// GoRuleBindings returns the Go test file of the conformance bindings of the operations of the rules for
// package libgo, for the node library's Parser.
func GoRuleBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", ruleBindingOps, goTarget(goSpec{fn: "expect", name: "Expect"}), "")
}

// GoTypedRuleBindings returns the Go test file of the conformance bindings of the operations of the
// rules for package libgo, for the typed library's TParser, with rule.run, which runs the invocation
// that a generated typed call passes.
func GoTypedRuleBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", append(slices.Clone(ruleBindingOps), "rule.run"), goTarget(goSpec{fn: "reset", typed: true, name: "Reset"}), "")
}

// obsBindingOps are the operations of the observation of a parse.
var obsBindingOps = []string{"obs.active", "obs.memoHit", "obs.enter", "obs.exit"}

// GoObsBindingsFile is the file of the conformance bindings of the observation that the test of the
// library prints, relative to the copy of the library.
const GoObsBindingsFile = "obs_bind_exec_test.go"

// GoObsBindings returns the Go test file of the conformance bindings of the observation for package
// runtime, for the node library's Parser. The engine has no observation of this kind: it traces a call
// on a path of its own.
func GoObsBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", obsBindingOps, goTarget(goSpec{fn: "expect", name: "Expect"}), "")
}

// engineMissingBindingsFile is the file of the engine's conformance bindings of the missing-token
// mode, relative to the root of the module. It is a test file of package state of the engine.
const engineMissingBindingsFile = "internal/runtime/state/missing_bind_exec_test.go"

// missingBindingOps are the operations of the missing-token mode.
var missingBindingOps = []string{"missing.active", "missing.recompute", "missing.mark", "missing.reset", "missing.replay"}

// printEngineMissingBindings returns the Go test file of the conformance bindings of the missing-token mode
// for package state of the engine, the only target that has the mode.
func printEngineMissingBindings(u file) (string, error) {
	return bindingFile(u.module(), "state", missingBindingOps, engineTarget(), "")
}

// engineCallBindingsFile is the file of the engine's conformance bindings of the traced call path,
// relative to the root of the module. It is a test file of package state of the engine.
const engineCallBindingsFile = "internal/runtime/state/call_bind_exec_test.go"

// callBindingOps are the operations of the traced call path.
var callBindingOps = []string{"call.tracked", "call.track"}

// printEngineCallBindings returns the Go test file of the conformance bindings of the traced call path
// for package state of the engine, the only target that has the path: the generated runtimes observe a
// call with obs and doc operations of their own.
func printEngineCallBindings(u file) (string, error) {
	return bindingFile(u.module(), "state", callBindingOps, engineTarget(), "")
}

// actionStateBindingOps are the operations of the elements gathered and the values made.
var actionStateBindingOps = []string{"act.push", "act.pushAll", "action.kids", "action.dropKids", "action.elemsAbove", "action.keep", "act.made", "action.created", "action.dropCreated"}

// engineActionStateBindingsFile is the file of the engine's conformance bindings of the elements gathered and the
// values made, relative to the root of the module. It is a test file of package state of the engine.
const engineActionStateBindingsFile = "internal/runtime/state/actions_bind_exec_test.go"

// printEngineActionStateBindings returns the Go test file of the conformance bindings of the elements gathered
// and the values made for package state of the engine, printed as the backends print them into their own
// packages (engineActionOp).
func printEngineActionStateBindings(u file) (string, error) {
	return bindingFile(u.module(), "state", actionStateBindingOps, engineTarget(), "confActionStateBindings")
}

// GoActionStateBindings returns the Go test file of the conformance bindings of the elements gathered and the
// values made for package libgo, for the node library's Parser.
func GoActionStateBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", actionStateBindingOps, goTarget(goSpec{fn: "expect", name: "Expect"}), "confActionStateBindings")
}

// GoTypedActionStateBindings returns the Go test file of the conformance bindings of the elements gathered and
// the values made for package libgo, for the typed library's TParser.
func GoTypedActionStateBindings() (string, error) {
	return bindingFile(compose.Build(), "libgo", actionStateBindingOps, goTarget(goSpec{fn: "reset", typed: true, name: "Reset"}), "confTActionStateBindings")
}
