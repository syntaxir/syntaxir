package complexity

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The classifier walks the rules it classifies, part as grammar expressions and part as their
// Core IR nodes, and reads the facts of the nodes: each expression of the rules stands for the
// node of the view, or of the module, that it is written from (classifier.nodes), and each such
// node for the expression (classifier.exprs). Each expression that the classifier builds itself,
// such as the repetition that stands for the later iterations of another, stands for a node built
// with it, and the node for it.

// nodeMap maps the expressions of rules to the Core IR nodes they stand for and back.
type nodeMap struct {
	nodes map[ast.Expr]core.Node
	exprs map[core.Node]ast.Expr
}

// originNodes maps each grammar expression that a node of m is built from to the outermost such
// node, and each node with an origin to it (core.Module.Origin). A node of an attribute below the
// outermost of an expression of several attributes, which has no origin, and the expression of
// the attributes up to it map to each other (chainLink.innerExpr).
func originNodes(m *core.Module) nodeMap {
	nm := nodeMap{nodes: map[ast.Expr]core.Node{}, exprs: map[core.Node]ast.Expr{}}
	chains := attrChains(m)
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		core.Walk(r.Body, func(n core.Node) {
			e := m.Origin.Of(n)
			if e == nil {
				l, ok := chains[n]
				if !ok {
					return
				}
				e = l.innerExpr()
			}
			if _, seen := nm.nodes[e]; !seen {
				nm.nodes[e] = n
			}
			nm.exprs[n] = e
		})
	}
	return nm
}

// nodeOf returns the Core IR node that the expression e stands for. Every expression of the rules
// and every expression that the classifier builds stands for one; nil stands for nil.
func (k *classifier) nodeOf(e ast.Expr) core.Node {
	if e == nil {
		return nil
	}
	n, ok := k.nodes[e]
	if !ok {
		panic(fmt.Sprintf("complexity: no Core IR node for %T %s", e, k.show(e)))
	}
	return n
}

// exprOf returns the grammar expression that the Core IR node n stands for. Every node that an
// expression of the rules is written from, and every node that the classifier builds, stands for
// one; nil stands for nil.
func (k *classifier) exprOf(n core.Node) ast.Expr {
	if n == nil {
		return nil
	}
	e, ok := k.exprs[n]
	if !ok {
		panic(fmt.Sprintf("complexity: no grammar expression for %T", n))
	}
	return e
}

// callOf returns a call of rule.
func (k *classifier) callOf(rule string) *core.Call {
	n := &core.Call{Rule: k.ruleIDs[rule]}
	k.spell(n, &ast.Ref{Name: rule})
	return n
}

// rightOperand returns the node that stands for a right operand of an operator part of the
// Operators Rule of rule. The runtime parses it with the loop of the Operators Rule at a new
// position, in the scope of a call but without the memo: the node is the inlined call of the rule
// whose body is the Operators Rule (core.Nest). The classifier tells no level of operators apart,
// so it stands for what the right operand of any operator part parses.
func (k *classifier) rightOperand(rule string) *core.Nest {
	n := &core.Nest{Rule: k.ruleIDs[rule], Body: k.operatorsOf(rule)}
	k.spell(n, &ast.Ref{Name: rule, Level: rhsLevel})
	return n
}

// isRightOperand reports whether the node n stands for a right operand of an operator part: an
// inlined call whose body is the loop of an Operators Rule (rightOperand).
func isRightOperand(n core.Node) bool {
	nest, ok := n.(*core.Nest)
	if !ok {
		return false
	}
	_, ok = nest.Body.(*core.OperatorsRule)
	return ok
}

// prefixOf returns the sequence of the items of s before index cut, which stands for the
// sequence of their expressions.
func (k *classifier) prefixOf(s *core.Seq, cut int) *core.Seq {
	items := s.Items[:cut:cut]
	exprs := make([]ast.Expr, cut)
	for i, it := range items {
		exprs[i] = k.spelling(it)
	}
	e := k.spelling(s).(*ast.Seq)
	n := &core.Seq{Items: items}
	k.spell(n, &ast.Seq{SourceInfo: e.SourceInfo, Items: exprs})
	return n
}

// succeeds reports a proof that the expression e cannot fail (corepass.Facts.Succeeds).
func (k *classifier) succeeds(e ast.Expr) bool { return k.succeedsOf(k.nodeOf(e)) }

// leftCalls returns the rules that the expression e may call before it consumes input, in the
// order of e, a rule called at several such places listed at each (corepass.Facts.LeftCalls).
func (k *classifier) leftCalls(e ast.Expr) []string {
	ids := k.facts.source.Facts.LeftCalls(k.nodeOf(e), false)
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = k.rules[id].Name
	}
	return names
}

// literalRest returns the literal node of the characters rest, the rest of a literal after a
// prefix the exclusion analysis compared, matched without case if fold, which stands for a literal
// of them. The runes give valid UTF-8, the text of the node as the front end builds it.
func (k *classifier) literalRest(rest []rune, fold bool) *core.Lit {
	text := string(rest)
	n := &core.Lit{Text: text, Fold: fold}
	k.spell(n, &ast.Literal{Value: text, IgnoreCase: fold})
	return n
}

// looseCut reports whether e has a cut that commits the choice, option or repetition around it
// (corepass.Facts.LooseCut).
func (k *classifier) looseCut(e ast.Expr) bool { return k.looseCutOf(k.nodeOf(e)) }
