package print

import (
	"fmt"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
)

// OperationHarness is the printed code of operations, one method each, for the conformance tests of the
// TypeScript library (internal/testing/tsconformtest): the text of the classes and the operations that
// the printer does not print in each.
type OperationHarness struct {
	// Source is TypeScript that extends the library's source: for each class of OperationClasses, a
	// subclass of its library class with a method for each printed operation, and its table, which calls
	// each of them with the arguments and the results of a call of the operation of the conformance suite.
	Source string
	// Unprinted are, for each class by name, the operations that have no printed method in it, each
	// with the reason.
	Unprinted map[string]map[string]string
}

// OperationClass is a class of the library whose methods the printer prints with a target of its own:
// the harness class Name extends Base, and the table Table calls its methods.
type OperationClass struct {
	Name, Base, Table string
	target            *Target
}

// OperationClasses are the classes that OperationMethods prints: the batch Parser (tsTarget), the
// DocumentParser of a Document parse (tsDocTarget) and the ObservedParser of an observed parse without a
// Document (tsObsTarget). An operation prints in each as in the library methods of that class.
var OperationClasses = []OperationClass{
	{Name: "PrintedParser", Base: "Parser", Table: "printedOps", target: tsTarget},
	{Name: "PrintedDocumentParser", Base: "DocumentParser", Table: "printedDocOps", target: tsDocTarget},
	{Name: "PrintedObservedParser", Base: "ObservedParser", Table: "printedObsOps", target: tsObsTarget},
}

// opPartNames returns the suffixes of the parts that the TypeScript target holds a value of the handle type
// in (Target.Parts), in the order of a tuple, or nil.
func opPartNames(handle string) []string {
	var out []string
	for _, p := range tsTarget.Parts[handle] {
		out = append(out, p.Suffix)
	}
	return out
}

// OperationMethods prints the operations names with the TypeScript target of the generated parsers, for
// each class of OperationClasses whose name keys its operations in names.
//
// Each operation is a method of a class that extends the library's class, whose body is what the
// printer prints for a call of the operation in a composition of that class. A value that the target
// holds in several locals (Target.Parts) is a parameter or a result of several, and the table folds them
// in a tuple for the arguments and the results that the conformance harness exchanges. An operation the
// target does not implement (its Op panics) is reported in Unprinted, with the operations that have no
// printed form of their own (a variadic operation).
func OperationMethods(names map[string][]string) (OperationHarness, error) {
	m := compose.Build()
	out := OperationHarness{Unprinted: map[string]map[string]string{}}
	var src strings.Builder
	for _, c := range OperationClasses {
		ops, ok := names[c.Name]
		if !ok {
			continue
		}
		unprinted := map[string]string{}
		out.Unprinted[c.Name] = unprinted
		var methods, table strings.Builder
		for _, n := range ops {
			o := m.Op(n)
			if o == nil {
				return out, fmt.Errorf("tsgen: no operation %s", n)
			}
			if o.Variadic {
				unprinted[n] = "a variadic operation"
				continue
			}
			name := "op_" + strings.NewReplacer(".", "_").Replace(n)
			body, err := printOperation(m, o, c.target)
			if err != nil {
				unprinted[n] = err.Error()
				continue
			}
			fmt.Fprintf(&methods, "  %s(%s): any {\n%s  }\n", name, strings.Join(opParams(o), ", "), body)
			fmt.Fprintf(&table, "  %q: (P, a, c) => %s,\n", n, opAdapter(o, name))
		}
		fmt.Fprintf(&src, "export class %s extends %s {\n%s}\n\n", c.Name, c.Base, methods.String())
		fmt.Fprintf(&src, "export const %s: Record<string, (P: %s, a: any[], c: any) => any[]> = {\n%s};\n\n", c.Table, c.Name, table.String())
	}
	out.Source = src.String()
	return out, nil
}

// opParams returns the parameters of the method of op.
func opParams(o *code.Op) []string {
	var ps []string
	for i, p := range o.Params {
		if parts := opPartNames(p.Type.Handle); parts != nil {
			for _, s := range parts {
				ps = append(ps, fmt.Sprintf("p%d%s: any", i, s))
			}
			continue
		}
		ps = append(ps, fmt.Sprintf("p%d: any", i))
	}
	return ps
}

// printOperation returns the statements that the target t prints for a call of op in a function of the
// module m, and a return of its results.
func printOperation(m *code.Module, o *code.Op, t *Target) (body string, err error) {
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
			if parts := opPartNames(r.Handle); parts != nil {
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
	src, err := Statements(&mod, f, t, 2)
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
		if opPartNames(p.Type.Handle) != nil {
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
		if parts := opPartNames(r.Handle); parts != nil {
			res = append(res, fmt.Sprintf("c.out(%q, r.slice(%d, %d), %q, a)", r.Handle, slots, slots+len(parts), o.Name))
			slots += len(parts)
			continue
		}
		res = append(res, fmt.Sprintf("c.out(%q, r[%d], %q, a)", r.Handle, slots, o.Name))
		slots++
	}
	return fmt.Sprintf("((r: any[]) => [%s])(%s)", strings.Join(res, ", "), call)
}
