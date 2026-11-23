package print

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// tracePlainCall prints the function name of the plain rule r with observability: it calls the body
// of the rule, whose name it returns, and reports the call to the observation of an observed parse.
// The parser of an observed parse is an ObservedParser, and in a -stream parser, whose stream calls
// the function too, an ObservedStreamParser in an observed stream.
func (g *Generator) tracePlainCall(r *common.Rule, name string) string {
	if !g.Observability {
		return name
	}
	body := name + "ObservedBody"
	observed := "ObservedParser"
	if g.streamParser {
		observed = "ObservedParser | ObservedStreamParser"
	}
	fmt.Fprintf(&g.Methods, `// Observes one actual generated rule call.
function %s(p: Parser): R {
  if (p.observation === null) return %s(p);
  return (p as %s).observePlain(%s, %s);
}

`, name, body, observed, TsString(r.Name), body)
	return body
}

// tracePlainGenCall is tracePlainCall for the resumable function name of the plain rule r in the
// stream of a -stream parser (plainGenCall), whose parser is an ObservedStreamParser in an observed
// stream.
func (g *Generator) tracePlainGenCall(r *common.Rule, name string) string {
	if !g.Observability {
		return name
	}
	body := name + "ObservedBody"
	fmt.Fprintf(&g.Methods, `// Observes one actual generated rule call.
function* %s%s {
  if (p.observation === null) return yield* %s(p);
  return yield* (p as ObservedStreamParser).observePlainGen(%s, %s);
}

`, name, tsGenSignature, body, TsString(r.Name), body)
	return body
}

// traceSkipped returns the report that the guard of a choice skips the alternative that begins with
// the rule call x: the call's rule and its minimum binding level.
func (g *Generator) traceSkipped(x prim.ObservedSkip) string {
	if !g.Observability {
		return ""
	}
	return fmt.Sprintf("    p.observation?.skipped(%s, p.pos, %d);\n", TsString(x.Rule), x.Level)
}
