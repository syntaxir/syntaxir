package print

import (
	"fmt"
	"slices"
	"strings"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/exec"
	"zeroparse.dev/zp/internal/ir/code/exec/compose"
)

// OperationHarness is the printed code of operations, one method each, for the conformance tests of the
// TypeScript library (internal/testing/tsconformtest): the text of the class and the operations that
// the printer does not print.
type OperationHarness struct {
	// Source is TypeScript that extends the library's source: the class PrintedParser, a Parser with
	// a method for each printed operation, and printedOps, which calls each of them with the arguments
	// and the results of a call of the operation of the conformance suite.
	Source string
	// Unprinted are the operations that have no printed method, each with the reason.
	Unprinted map[string]string
}

// opParts are the locals that hold a value of a handle type that a TypeScript target holds in several
// locals, in the order of a tuple: the parts of Target.Parts, and those of the record that an isolation
// sets aside, which exp.isolate and exp.unisolate print as the locals xFar and xBase.
var opParts = map[string][]string{
	exec.Backtrack: {"Pos", "Env", "Trail", "Rec"},
	exec.Set:       {"From", "To"},
	exec.Mark:      {"Far", "Base"},
}

// OperationMethods prints the operations names with the TypeScript target of the generated parsers.
//
// Each operation is a method of a class that extends the library's Parser, whose body is what the
// printer prints for a call of the operation in a composition. A value that the target holds in
// several locals (opParts) is a parameter or a result of several, and printedOps folds them in a tuple
// for the arguments and the results that the conformance harness exchanges. An operation the target
// does not implement (its Op panics) is reported in Unprinted, with the operations that have no printed form
// of their own (a variadic operation).
func OperationMethods(names []string) (OperationHarness, error) {
	m := compose.Build()
	type wrapper struct {
		op   *code.Op
		name string
	}
	var ws []wrapper
	out := OperationHarness{Unprinted: map[string]string{}}
	for _, n := range names {
		o := m.Op(n)
		if o == nil {
			return out, fmt.Errorf("tsgen: no operation %s", n)
		}
		if o.Variadic {
			out.Unprinted[n] = "a variadic operation"
			continue
		}
		ws = append(ws, wrapper{o, "op_" + strings.NewReplacer(".", "_").Replace(n)})
	}
	var methods, table strings.Builder
	for _, w := range ws {
		body, err := printOperation(m, w.op)
		if err != nil {
			out.Unprinted[w.op.Name] = err.Error()
			continue
		}
		fmt.Fprintf(&methods, "  %s(%s): any {\n%s  }\n", w.name, strings.Join(opParams(w.op), ", "), body)
		fmt.Fprintf(&table, "  %q: (P, a, c) => %s,\n", w.op.Name, opAdapter(w.op, w.name))
	}
	out.Source = "export class PrintedParser extends Parser {\n" + methods.String() + "}\n\n" +
		"export const printedOps: Record<string, (P: PrintedParser, a: any[], c: any) => any[]> = {\n" + table.String() + "};\n"
	return out, nil
}

// opParams returns the parameters of the method of op.
func opParams(o *code.Op) []string {
	var ps []string
	for i, p := range o.Params {
		if parts := opParts[p.Type.Handle]; parts != nil {
			for _, s := range parts {
				ps = append(ps, fmt.Sprintf("p%d%s: any", i, s))
			}
			continue
		}
		ps = append(ps, fmt.Sprintf("p%d: any", i))
	}
	return ps
}

// printOperation returns the statements that the printer prints for a call of op in a function of the module
// m, and a return of its results.
func printOperation(m *code.Module, o *code.Op) (body string, err error) {
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("%v", x)
		}
	}()
	f := &code.Func{Name: "op", Body: &code.Block{}, Features: o.Features}
	args := make([]code.Expr, len(o.Params))
	for i, p := range o.Params {
		name := fmt.Sprintf("p%d", i)
		f.Params = append(f.Params, code.P(name, p.Type))
		args[i] = code.L(name)
	}
	call := code.C(o.Name, args...)
	var rets []string
	switch len(o.Results) {
	case 0:
		f.Body.Stmts = append(f.Body.Stmts, &code.Do{Call: call})
	default:
		names := make([]string, len(o.Results))
		for i, r := range o.Results {
			if i == 1 && packedPair(o) {
				names[i] = "r1" // the bool of a packed pair is the test of its handle, which is not declared
				break
			}
			names[i] = fmt.Sprintf("r%d", i)
			if parts := opParts[r.Handle]; parts != nil {
				for _, s := range parts {
					rets = append(rets, names[i]+s)
				}
			} else {
				rets = append(rets, names[i])
			}
		}
		f.Body.Stmts = append(f.Body.Stmts, &code.Let{Names: names, Value: call})
		// The printer declares only what a later statement reads, so the function returns its results; the
		// text of the return is dropped below for the tuple of the parts.
		f.Results = o.Results
		var rv []code.Expr
		for i := range o.Results {
			if i == 1 && packedPair(o) {
				break
			}
			rv = append(rv, code.L(names[i]))
		}
		if packedPair(o) {
			rv = append(rv, code.L(names[1]))
		}
		f.Body.Stmts = append(f.Body.Stmts, &code.Return{Values: rv})
	}
	mod := *m
	mod.Funcs = append(slices.Clone(m.Funcs), f)
	if err := code.Verify(&mod); err != nil {
		return "", err
	}
	src, err := Statements(&mod, f, tsTarget, 2)
	if err != nil {
		return "", err
	}
	if len(rets) > 0 {
		i := strings.LastIndex(src, "    return ")
		if i < 0 || strings.Count(src[i:], "\n") != 1 {
			return "", fmt.Errorf("the printed function does not end in a return:\n%s", src)
		}
		src = src[:i] + "    return [" + strings.Join(rets, ", ") + "];\n"
	}
	return src, nil
}

// packedPair reports whether the results of o are the pair of the packed handle and a bool.
func packedPair(o *code.Op) bool {
	return len(o.Results) == 2 && o.Results[0].Handle == tsTarget.Packed && o.Results[1].Kind == code.KBool
}

// opAdapter returns the TypeScript function that calls the method name of op with the arguments of a
// call of the conformance suite, a tuple for a value of several locals, and returns its results as
// the suite takes them: a result of several locals as a tuple, and the pair of the packed handle and
// a bool as the handle and the test of it.
func opAdapter(o *code.Op, name string) string {
	var args []string
	for i, p := range o.Params {
		arg := fmt.Sprintf("a[%d]", i)
		if opParts[p.Type.Handle] != nil {
			args = append(args, "..."+arg)
		} else {
			args = append(args, fmt.Sprintf("c.in(%q, %s)", p.Type.Handle, arg))
		}
	}
	call := fmt.Sprintf("P.%s(%s)", name, strings.Join(args, ", "))
	if len(o.Results) == 0 {
		return "(" + call + ", [])"
	}
	if packedPair(o) {
		return fmt.Sprintf("((r: any[]) => [c.out(%q, r[0] ?? null, %q, a), r[0] !== undefined])(%s)", o.Results[0].Handle, o.Name, call)
	}
	slots := 0
	var res []string
	for _, r := range o.Results {
		if parts := opParts[r.Handle]; parts != nil {
			res = append(res, fmt.Sprintf("c.out(%q, r.slice(%d, %d), %q, a)", r.Handle, slots, slots+len(parts), o.Name))
			slots += len(parts)
			continue
		}
		res = append(res, fmt.Sprintf("c.out(%q, r[%d], %q, a)", r.Handle, slots, o.Name))
		slots++
	}
	return fmt.Sprintf("((r: any[]) => [%s])(%s)", strings.Join(res, ", "), call)
}
