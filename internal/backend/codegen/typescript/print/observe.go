package print

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func (g *Generator) tracePlainCall(r *common.Rule, name string) string {
	if !g.Observability {
		return name
	}
	body := name + "ObservedBody"
	fmt.Fprintf(&g.Methods, `// Observes one actual generated rule call.
function %s(p: Parser): R {
  if (p.observation === null) return %s(p);
  return (p as ObservedParser).observePlain(%s, %s);
}

`, name, body, TsString(r.Name), body)
	return body
}

// traceSkipped returns the report that the guard of a choice skips the alternative that begins with
// the rule call x: the call's rule and the index of the level it names, 0 if it names none.
func (g *Generator) traceSkipped(x prim.ObservedSkip) string {
	if !g.Observability {
		return ""
	}
	min := 0
	if x.Level != "" {
		for i, l := range g.Prog.ByName[x.Rule].Def.Expr.(*ast.OperatorsRule).Levels {
			if l.Name == x.Level {
				min = i
			}
		}
	}
	return fmt.Sprintf("    p.observation?.skipped(%s, p.pos, %d);\n", TsString(x.Rule), min)
}
