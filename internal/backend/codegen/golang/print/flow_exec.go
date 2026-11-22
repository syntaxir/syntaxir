package print

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"strings"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	irc "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/execlower"
)

// Go rules printed from the Exec stage of Code IR.
//
// The rules of every table (the typed table, the Node table and the recognizer, and the stream
// body), in the ordinary form, with budget controls and with a Document, are written at the Flow
// stage and converted to the Exec stage by the lowering of each planned table in the compilation
// (internal/driver), and printed by the Go backend of the Exec stage (print.Body), which prints the
// compositions of the library too. The primitives of grammar code are printed in place by the flow
// printer (flowPrinter.op).

// execBody prints the body of the lowered function l with the flow printer p, for a function that is
// printed whatever its body: it checks the variables (execVars) and counts the function (execCount).
func (p *flowPrinter) execBody(l *execlower.Lowered) BodyResult {
	res := p.execPrint(l)
	p.execVars(l, &res)
	p.execCount()
	return res
}

// execPrint prints the body of the lowered function l with the flow printer p.
func (p *flowPrinter) execPrint(l *execlower.Lowered) BodyResult {
	return Body(l.Module, l.Func, p.execTarget(), l.Vars)
}

// execVars checks the variables of the printed body res of the lowered function l: a variable that
// the body names but that is not declared is an internal error (Generator.FlowErr). Hooks.FlowVarsMutant and Hooks.FlowVarsMutantUses instead leave variables
// undeclared, without the check.
func (p *flowPrinter) execVars(l *execlower.Lowered, res *BodyResult) {
	if p.g.hooks.FlowVarsMutant {
		// The locals that the body names stay undeclared.
		for _, v := range l.Vars {
			res.Stores.Dead[v] = true
		}
		return
	}
	if p.g.hooks.FlowVarsMutantUses {
		// The locals that only the values of assignments read stay undeclared.
		hard := otherReads(l.Func.Body)
		for _, v := range l.Vars {
			if !hard[v] {
				res.Stores.Dead[v] = true
			}
		}
		return
	}
	if p.g.FlowErr == nil {
		for _, id := range freeIdents(res.Text) {
			if res.Stores.Dead[id] {
				p.g.FlowErr = fmt.Errorf("internal error: the printed body names the variable %s, which is not declared", id)
				break
			}
		}
	}
}

// execCount counts a function printed from Exec IR that the generated code includes.
func (p *flowPrinter) execCount() {
	p.g.ExecFuncs++
	if p.g.ExecTables == nil {
		p.g.ExecTables = map[string]int{}
	}
	p.g.ExecTables[p.g.Table]++
}

// otherReads returns the locals that the statements of b other than assignments read, whatever
// their reachability: the reads that Hooks.FlowVarsMutantUses keeps.
func otherReads(b *code.Block) map[string]bool {
	out := map[string]bool{}
	read := func(x code.Expr) {
		if x == nil {
			return
		}
		code.Walk(&code.Block{Stmts: []code.Stmt{&code.Assert{Cond: x}}}, nil, func(e code.Expr) {
			if l, ok := e.(*code.Local); ok {
				out[l.Name] = true
			}
		})
	}
	header := func(s *code.Set) {
		if s != nil {
			for _, x := range s.Values {
				read(x)
			}
		}
	}
	var block func(*code.Block)
	block = func(b *code.Block) {
		if b == nil {
			return
		}
		for _, st := range b.Stmts {
			switch st := st.(type) {
			case *code.Block:
				block(st)
			case *code.Loop:
				header(st.Init)
				read(st.Cond)
				header(st.Post)
				block(st.Body)
			case *code.If:
				header(st.Init)
				read(st.Cond)
				block(st.Then)
				block(st.Else)
			case *code.Let:
				read(st.Value)
			case *code.Do:
				read(st.Call)
			case *code.Assert:
				read(st.Cond)
			case *code.Return:
				for _, x := range st.Values {
					read(x)
				}
			}
		}
	}
	block(b)
	return out
}

// execTarget returns the Go implementation of the primitive operations of grammar code at the Exec
// stage, which the flow printer prints in place (flowPrinter.op).
func (p *flowPrinter) execTarget() *Target {
	t := p.target()
	if h := p.g.hooks.FlowTarget; h != nil {
		return h(t, &p.pre)
	}
	return t
}

// target returns the Go implementation of execTarget that Hooks.FlowTarget does not replace.
func (p *flowPrinter) target() *Target {
	return &Target{
		Reg: func(name string) string {
			return map[string]string{prim.Pos: "p.Pos", prim.Cut: "p.Cut"}[name]
		},
		Handle: func(name string) string { return name },
		Op:     p.op,
		Stmt: func(c *code.Call, args []string, dst []string, _ bool) (string, bool) {
			switch execlower.FlowOp(c.Callee) {
			case flow.OpPeek:
				return strings.TrimSuffix(p.peekText(dst[0], dst[1], dst[2]), "\n"), true
			case flow.OpAppend:
				return p.appendText(c, args), true
			case flow.OpEval:
				v := p.op(c, args)
				pre := p.pre
				p.pre = ""
				assign := strings.Join(dst, ", ") + " = " + v
				if pre != "" {
					return "{ " + pre + assign + "\n}", true
				}
				return assign, true
			}
			return "", false
		},
		Negate: func(c *code.Call, args []string) (string, bool) { return p.negate(c, args), true },
		Prec:   goPrec,
		Pure: func(c *code.Call) bool {
			switch execlower.FlowOp(c.Callee) {
			case flow.OpStr, flow.OpMarkPos, flow.OpEnv, flow.OpFrame:
				return true
			}
			return false
		},
	}
}

// goPrec returns the precedence of the loosest binary operator of the Go expression text outside
// parentheses, or 6 for an operand.
func goPrec(text string) int {
	e, err := goparser.ParseExpr(text)
	if err != nil {
		return 0
	}
	if b, ok := e.(*goast.BinaryExpr); ok {
		return b.Op.Precedence()
	}
	return token.UnaryPrec
}

// execFlatRule prints the lowered local function l as a flat rule (flowTypedRule).
func (g *Generator) execFlatRule(p *flowPrinter, l *execlower.Lowered) *flatRule {
	fn := l.Func
	res := p.execBody(l)
	st := res.Stores
	a := prim.AttrsOf(fn)
	read := func(v string) bool { return v != "" && st.Read[v] }
	out := &flatRule{fail: res.Exited, succeed: res.End, binds: a.Binds, start: read(a.Start), ctx: read(a.Ctx)}
	var unread []string
	for _, v := range prim.VarsOf(fn) {
		out.vars = append(out.vars, dvar{v.Name, goType(prim.TypeOf(v.Type))})
		switch {
		case st.Dead[v.Name]:
			out.markVar(v.Name, true)
		case !st.Named[v.Name]:
			unread = append(unread, v.Name)
		case !st.Read[v.Name]:
			out.markVar(v.Name, false)
		}
	}
	out.body = res.Text
	if len(unread) > 0 {
		out.body = "\t" + strings.Repeat("_, ", len(unread)-1) + "_ = " + strings.Join(unread, ", ") + "\n" + out.body
	}
	g.FlowRules++
	return out
}

// execRecRule prints the lowered local function l of a rule of the Node library as a flat rule. The
// plan has accepted the function as the lowering wrote it (flowplan.Unit); if the forms that the
// Go passes chose make codepass.RefusesFlat reject it, the generation fails with an internal error
// (Generator.FlowErr) and execRecRule returns nil. It counts only a rule it prints.
func (g *Generator) execRecRule(p *flowPrinter, l *execlower.Lowered) *flatRule {
	fn := l.Func
	res := p.execPrint(l)
	if a := prim.AttrsOf(fn); codepass.RefusesFlat(fn, a.Start, a.Ctx, prim.VarsOf(fn)) {
		if g.FlowErr == nil {
			g.FlowErr = fmt.Errorf("internal error: the plan runs %s flat, but in the forms of the Go passes it reads its start or its context, or keeps a value", fn.Name)
		}
		return nil
	}
	st := res.Stores
	vars := prim.VarsOf(fn)
	p.execVars(l, &res)
	p.execCount()
	out := &flatRule{fail: res.Exited, succeed: res.End}
	var unread []string
	for _, v := range vars {
		t, ok := untypedGoType(prim.TypeOf(v.Type))
		if !ok {
			panic(fmt.Errorf("internal error: the flat rule %s has a variable %s of a type the Node library cannot name", fn.Name, v.Name)) // RefusesFlat holds
		}
		out.vars = append(out.vars, dvar{v.Name, t})
		switch {
		case st.Dead[v.Name]:
			out.markVar(v.Name, true)
		case !st.Named[v.Name]:
			unread = append(unread, v.Name)
		case !st.Read[v.Name]:
			out.markVar(v.Name, false)
		}
	}
	out.body = res.Text
	if len(unread) > 0 {
		out.body = "\t" + strings.Repeat("_, ", len(unread)-1) + "_ = " + strings.Join(unread, ", ") + "\n" + out.body
	}
	g.FlowRules++
	return out
}

// execFramed prints the lowered framed function l as flowFramed does.
func (g *Generator) execFramed(p *flowPrinter, l *execlower.Lowered, slots []irc.Slot, scope *check.Scope, comment string, ruleBody bool) string {
	fn := l.Func
	res := p.execBody(l)
	st := res.Stores
	var vars []dvar
	dead, quiet := map[string]bool{}, map[string]bool{}
	var unread []string
	for _, v := range prim.VarsOf(fn) {
		vars = append(vars, dvar{v.Name, p.goType(prim.TypeOf(v.Type))})
		switch {
		case st.Dead[v.Name]:
			dead[v.Name] = true
		case !st.Named[v.Name]:
			unread = append(unread, v.Name)
		case !st.Read[v.Name]:
			quiet[v.Name] = true
		}
	}
	var b strings.Builder
	b.WriteString(varLinesExcept(vars, dead, quiet))
	if len(unread) > 0 {
		fmt.Fprintf(&b, "%s_ = %s\n", strings.Repeat("_, ", len(unread)-1), strings.Join(unread, ", "))
	}
	b.WriteString(res.Text)
	*scope = *check.NewScope()
	for _, sl := range slots {
		scope.Slot(sl.Name)
	}
	g.FlowFrames++
	if ruleBody {
		return g.bodyMethod(comment, b.String())
	}
	return g.Method(comment, b.String())
}
