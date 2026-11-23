package print

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

func (g *Generator) tracePlainWrapper(r *common.Rule, name string) string {
	return g.traceCallWrapper(r, name, true)
}

func (g *Generator) traceCallWrapper(r *common.Rule, name string, evaluated bool) string {
	body := name + "ObservedBody"
	eval := ""
	if evaluated {
		eval = "p.Observation.Evaluated()\n"
	}
	fmt.Fprintf(&g.Methods, `// %[1]s observes one actual direct rule call.
func %[1]s(p *%[2]s) (%[3]s, bool) {
 if p.Observation == nil { return %[4]s(p) }
 p.Observation.Enter(%[5]q, p.Pos, 0)
 h := p.ExaminedBegin()
 %[6]sv, ok := %[4]s(p)
 x := p.ExaminedEnd(h)
 p.Observation.Exit(ok, p.Pos, x)
 return v, ok
}

`, name, g.recv(), g.valType(), body, r.Name, eval)
	return body
}

// traceSkip returns the report that the guard of a choice skips the alternative that begins with the
// rule call x. A guard may suppress a whole sequence or a chain of rule references; the report names
// only its first rule call site, without inventing nested calls.
func (g *Generator) traceSkip(x prim.ObservedSkip) string {
	if !g.Observability {
		return ""
	}
	return fmt.Sprintf("p.Observation.Skipped(%q, p.Pos, %d)", x.Rule, x.Level)
}
