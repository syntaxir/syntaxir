package print

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
)

// The templates of a TypeScript parser are compositions that the printer prints into the parser's code
// for one rule each, with the body of that rule in place of rule.body: the rule's invocation is
// specialized for the rule as the library's methods are specialized for the features of the parser,
// with no closure and no call of the body through the rule. The library requests them for the parser's
// file (tsParserFile), specialized for the features that the parser's options give them
// (TemplateRequests), and the printer prints each with the target of its rule (tsTemplateTarget).

// tsParserFile is the file of the library's requests of the templates: the parser's code.
const tsParserFile = "parser.ts"

// tsTemplates are the compositions that the parser prints as templates, under their names, with the
// features they are specialized for under limit controls and those they are specialized for whatever
// the limits. invokePlain is the function r<id> of a plain rule that a call site calls directly
// (callsPlain); invokePlainMissing is that function for a rule of a table of the insertion program that
// can insert a missing token, which a failure also drops (only a parser with the missing-token mode
// has it).
var tsTemplates = []struct {
	fn, name   string
	on, always []string
}{
	{fn: "invokePlain", name: "invokePlain", on: []string{exec.Units}},
	{fn: "invokePlain", name: "invokePlainMissing", on: []string{exec.Missing, exec.Units}, always: []string{exec.Missing}},
}

// TemplateRequests returns the requests of the templates of a parser, with limit controls if limits and
// with the missing-token mode if missing: a parser with limit controls counts logical compute units
// (exec.Units), whatever features the plan reports, since the templates print the units of the
// parser's own calls.
func TemplateRequests(limits, missing bool) []library.Request {
	var rs []library.Request
	for _, t := range tsTemplates {
		if slices.Contains(t.always, exec.Missing) && !missing {
			continue
		}
		on := t.always
		if limits {
			on = t.on
		}
		rs = append(rs, library.Request{File: tsParserFile, Fn: t.fn, On: on, Name: t.name})
	}
	return rs
}

// libraryFuncs returns the specialized functions of lib that belong to the library's file (the requests
// that LibraryRequests makes) and those of the templates, which follow them.
func libraryFuncs(lib *library.Library) (methods []library.Request, templates map[string]*code.Func, err error) {
	if lib == nil || len(lib.Funcs) != len(lib.Requests) {
		return nil, nil, fmt.Errorf("tslib: the library holds no specialized function for each of its requests")
	}
	templates = map[string]*code.Func{}
	for i, r := range lib.Requests {
		switch r.File {
		case "":
			if len(templates) > 0 {
				return nil, nil, fmt.Errorf("tslib: the request %s of the library follows a template", r.Name)
			}
			methods = append(methods, r)
		case tsParserFile:
			templates[r.Name] = lib.Funcs[i]
		default:
			return nil, nil, fmt.Errorf("tslib: the request %s is for the file %q, which a TypeScript parser does not have", r.Name, r.File)
		}
	}
	return methods, templates, nil
}

// template returns the template fn of the library of the tables being printed, specialized for the
// parser.
func (g *Generator) template(fn string) (*code.Func, error) {
	_, ts, err := libraryFuncs(g.tables.Library)
	if err != nil {
		return nil, err
	}
	f := ts[fn]
	if f == nil {
		return nil, fmt.Errorf("internal error: the library of the parser has no template %s", fn)
	}
	return f, nil
}

// tsThis matches the receiver of the library's methods in the text of an operation.
var tsThis = regexp.MustCompile(`\bthis\b`)

// tsTemplateTarget returns the target that prints a template for one rule: the function of the
// parser's code with the parser p as the receiver of the operations, the rule's constant rule for the
// parameter r, the call of the rule's body body for rule.body, and what the rule is as constants: rec
// for rule.cyclic (a rule on a cycle of calls) and novalue for rule.novalue, so that the printer keeps
// only the branches of the rule.
func tsTemplateTarget(rule, body string, rec, novalue bool) *Target {
	t := *tsTarget
	self := func(s string) string { return tsThis.ReplaceAllString(s, "p") }
	t.Self = "p"
	t.Bind = map[string]string{"r": rule}
	t.Reg = func(name string) string { return self(tsTarget.Reg(name)) }
	t.Op = func(c *code.Call, a []string) string {
		switch c.Callee {
		case "rule.cyclic":
			return strconv.FormatBool(rec)
		case "rule.novalue":
			return strconv.FormatBool(novalue)
		case "rule.body":
			return body
		}
		return self(tsTarget.Op(c, a))
	}
	t.Stmt = func(c *code.Call, a []string, dst []string, decl string) (string, bool) {
		if c.Callee == "units.check" {
			// A statement of its own, which needs no semicolon after its block.
			return self(tsTarget.Op(c, a)), true
		}
		s, ok := tsTarget.Stmt(c, a, dst, decl)
		return self(s), ok
	}
	return &t
}
