package ast

import (
	"math"
	"strconv"
	"strings"
)

// MaxNestingLevels is the deepest nesting of grammar syntax that the reference parser reads,
// counted on the tokens as Limits in the language specification counts it: brackets, prefix and
// postfix operators and "=>" open levels, and a block comment nests on its own count, one level
// for the comment and one for each comment nested in it. Validation rejects an AST whose text, as
// Format writes it, nests deeper, and a comment string that Format writes as a block comment
// nesting deeper, so that the reference parser reads back the text of every valid AST.
const MaxNestingLevels = 500

// MaxNestingDepth is the deepest structural nesting of a grammar AST, which the reference
// parser and validation both bound, so that the walks over a tree read from a source or
// validated stay bounded where the level count of the text is not, as in a chain of pragmas,
// of a left-associative operator or of field accesses. The depth of a declaration, and of an
// attribute of the package clause, is 1; each node below is one deeper than its parent, except
// a Group, ActionGroup or TypeGroup, which are syntax and add nothing, and a negative IntLit
// other than math.MinInt, which adds two: Format writes it as "-" and its magnitude, which read
// back as a Unary and an IntLit. A tree with a node deeper than MaxNestingDepth is "nesting too
// deep". The text that Format writes for a valid AST reads back no deeper, so the parser reads
// it.
const MaxNestingDepth = 50000

// structuralWeight is the depth that node adds below its parent (see MaxNestingDepth).
func structuralWeight(node any) int {
	switch n := node.(type) {
	case *Grammar, *Group, *ActionGroup, *TypeGroup:
		return 0
	case *IntLit:
		if n.Value < 0 && n.Value != math.MinInt {
			return 2
		}
	}
	return 1
}

// msgNestingTooDeep is the error of an AST whose text nests deeper than MaxNestingLevels, the
// message of the reference parser for the text.
const msgNestingTooDeep = "nesting too deep"

// nestKind is the kind of a level of nesting that a token opens, as the lexer counts levels.
type nestKind uint8

const (
	nestDelimiter nestKind = iota // "(" or "[", until its closing delimiter
	nestBrace                     // "{", until its closing delimiter
	nestPrefix                    // a token that may be a prefix operator, until its operand
	nestArrow                     // "=>", until the item, statement or bracket that holds it ends
)

// nestToken is the class of a token for the level count.
type nestToken uint8

const (
	tokWord    nestToken = iota // a name, a literal, a number, a class or a capture reference
	tokOpen                     // "(" or "["
	tokBrace                    // "{"
	tokClose                    // ")", "]" or "}"
	tokSep                      // "," or ";", and a line break inside braces
	tokArrow                    // "=>"
	tokPrefix                   // "!", "&", "-", "*", "+", "?", "[]"
	tokColon                    // ":", a prefix that continues a chain of captures
	tokOperand                  // punctuation that is an operand: "$$", "$", "^^", "^", "_|_", "...", "."
	tokOther                    // any other punctuation
)

// nestFrame follows the levels of nesting of the tokens of one node as Format writes them,
// relative to the levels open before the node. The levels open before it matter only through the
// prefix levels on top of them, which the node's first operand may end, so the frame records the
// deepest nesting while those are open and after they end. A summary of a node does not depend on
// the levels below, which lets a subtree shared by several parents be counted once.
type nestFrame struct {
	own []nestKind
	// entry is true while the prefix levels open right before the node are open.
	entry bool
	// pending is true after a word, whose following token ends the open prefix levels unless it
	// is the ":" of a capture.
	pending bool
	// maxEntry and maxAfter are the most levels in own while entry is true and after it turned
	// false, or -1.
	maxEntry, maxAfter int
	// popsEntry is true once the node ended the prefix levels open before it.
	popsEntry bool
	// deep is true when the node alone opens more than MaxNestingLevels levels.
	deep bool
}

func newNestFrame(pending bool) *nestFrame {
	return &nestFrame{entry: true, pending: pending, maxEntry: -1, maxAfter: -1}
}

func (f *nestFrame) record() {
	n := len(f.own)
	if n > MaxNestingLevels {
		f.deep = true
	}
	if f.entry {
		f.maxEntry = max(f.maxEntry, n)
	} else {
		f.maxAfter = max(f.maxAfter, n)
	}
}

func (f *nestFrame) open(k nestKind) {
	f.own = append(f.own, k)
	f.record()
}

// popPrefixes ends the prefix levels on top, and those open before the node once its own are
// gone.
func (f *nestFrame) popPrefixes() {
	for len(f.own) > 0 && f.own[len(f.own)-1] == nestPrefix {
		f.own = f.own[:len(f.own)-1]
	}
	if len(f.own) == 0 && f.entry {
		f.entry, f.popsEntry = false, true
	}
}

// token follows one token, as the lexer's level count does.
func (f *nestFrame) token(t nestToken) {
	if f.pending {
		f.pending = false
		if t != tokColon {
			f.popPrefixes()
		}
	}
	switch t {
	case tokWord:
		f.pending = true
	case tokOpen:
		f.open(nestDelimiter)
	case tokBrace:
		f.open(nestBrace)
	case tokClose:
		// Format writes balanced delimiters inside each node.
		for len(f.own) > 0 {
			k := f.own[len(f.own)-1]
			f.own = f.own[:len(f.own)-1]
			if k == nestDelimiter || k == nestBrace {
				break
			}
		}
		f.popPrefixes()
	case tokSep:
		// Format writes separators only inside delimiters of the node.
		for len(f.own) > 0 && f.own[len(f.own)-1] != nestDelimiter && f.own[len(f.own)-1] != nestBrace {
			f.own = f.own[:len(f.own)-1]
		}
	case tokArrow:
		f.open(nestArrow)
	case tokPrefix, tokColon:
		f.open(nestPrefix)
	case tokOperand:
		f.popPrefixes()
	}
}

// apply follows the tokens of a node with the summary s, written at the end of f.
func (f *nestFrame) apply(s *nestSummary) {
	f.pending = s.pending
	base := len(f.own)
	f.recordAt(base + s.maxEntry)
	if s.popsEntry {
		f.popPrefixes()
		f.recordAt(len(f.own) + s.maxAfter)
	}
	f.own = append(f.own, s.exit...)
	if s.deep {
		f.deep = true
	}
}

// recordAt records n open levels in f, or nothing when n is negative (no level of the summary).
func (f *nestFrame) recordAt(n int) {
	if n < 0 {
		return
	}
	if n > MaxNestingLevels {
		f.deep = true
	}
	if f.entry {
		f.maxEntry = max(f.maxEntry, n)
	} else {
		f.maxAfter = max(f.maxAfter, n)
	}
}

// A nestSummary is the effect of the tokens of a node on the level count.
type nestSummary struct {
	maxEntry, maxAfter int
	popsEntry          bool
	exit               []nestKind
	pending            bool
	deep               bool
}

// nestMode is the printer path that writes a rule expression.
type nestMode uint8

const (
	modeExpr     nestMode = iota // a rule expression at a binding level (formatExpr)
	modeItem                     // an item of a rule body or group (the printer's itemNode)
	modeBody                     // a rule body or the body of a group
	modeAction                   // an action expression at a binding level
	modeType                     // a type expression
	modeTypeTerm                 // a type expression as the operand of a type operator
	modeArg                      // an expression as an attribute argument
)

type nestKey struct {
	node    any
	mode    nestMode
	outer   int
	pending bool
}

// nester counts the levels of the text of a grammar AST.
type nester struct {
	memo    map[nestKey]*nestSummary
	control *validationControl
}

// child counts the tokens that write runs in a frame of its own, once for each key, and applies
// them to f.
func (n *nester) child(f *nestFrame, key nestKey, write func(*nestFrame)) {
	n.control.poll()
	key.pending = f.pending
	s, ok := n.memo[key]
	if !ok {
		c := newNestFrame(f.pending)
		write(c)
		s = &nestSummary{maxEntry: c.maxEntry, maxAfter: c.maxAfter, popsEntry: c.popsEntry, exit: c.own, pending: c.pending, deep: c.deep}
		n.memo[key] = s
	}
	f.apply(s)
}

// tooDeepDecl returns the path of the first declaration of g whose text, as Format writes it,
// nests deeper than MaxNestingLevels, with its node, or "". A line break outside brackets ends
// every level, and Format starts each declaration on a line of its own.
func tooDeepDecl(g *Grammar, control *validationControl) (string, SourceNode) {
	n := &nester{memo: map[nestKey]*nestSummary{}, control: control}
	declaration := func(write func(*nestFrame)) bool {
		f := newNestFrame(false)
		write(f)
		return f.deep
	}
	if g.File.Pkg.Name != "" {
		pkg := &g.File.Pkg
		if declaration(func(f *nestFrame) {
			n.attributes(f, pkg.Attrs)
			f.token(tokWord) // package
			f.token(tokWord)
		}) {
			return "$.file.pkg", pkg
		}
	}
	for i, d := range g.File.Decls {
		if declaration(func(f *nestFrame) { n.decl(f, d) }) {
			node, _ := d.(SourceNode)
			return "$.file.decls[" + strconv.Itoa(i) + "]", node
		}
	}
	return "", nil
}

// commentNesting returns the levels that text, a comment string, opens when Format writes it as
// a block comment: one for the comment, and one for each comment nested in it. Only the "/*" and
// "*/" of text that pair up nest, as Format breaks the others apart.
func commentNesting(text string, control *validationControl) int {
	var open []int
	paired := map[int]bool{}
	for i := 0; i+1 < len(text); i++ {
		control.poll()
		switch text[i : i+2] {
		case "/*":
			open = append(open, i)
			i++
		case "*/":
			if len(open) > 0 {
				paired[open[len(open)-1]] = true
				open = open[:len(open)-1]
			}
			i++
		}
	}
	if len(paired) == 0 {
		return 1
	}
	depth, deepest := 1, 1
	for i := 0; i+1 < len(text); i++ {
		control.poll()
		switch text[i : i+2] {
		case "/*":
			if paired[i] {
				depth++
				deepest = max(deepest, depth)
			}
			i++
		case "*/":
			if depth > 1 {
				depth--
			}
			i++
		}
	}
	return deepest
}

// tooDeepComment returns the field of the first comment string of node that Format writes as a
// block comment nesting deeper than MaxNestingLevels, or "". Format writes a string as a block
// comment unless it ends in a line break; a trailing comment over several lines is one block
// comment, and so is each header comment.
func tooDeepComment(node any, control *validationControl) string {
	deep := func(text string, block bool) bool {
		return block && commentNesting(text, control) > MaxNestingLevels
	}
	ends := func(text string) bool { return strings.HasSuffix(text, "\n") }
	indexed := func(name string, i int) string { return name + "[" + strconv.Itoa(i) + "]" }
	source := func(prefix string, s *SourceInfo) string {
		if deep(s.LeadingComments, !ends(s.LeadingComments)) {
			return prefix + "source.leadingComments"
		}
		for i, text := range s.LeadingDetachedComments {
			if deep(text, !ends(text)) {
				return prefix + "source." + indexed("leadingDetachedComments", i)
			}
		}
		for i, text := range s.TrailingComments {
			if deep(text, !ends(text) || strings.Count(text, "\n") > 1) {
				return prefix + "source." + indexed("trailingComments", i)
			}
		}
		return ""
	}
	keyword := func(prefix, text string, detached, header []string) string {
		if deep(text, !ends(text)) {
			return prefix + "keywordComments"
		}
		for i, text := range detached {
			if deep(text, !ends(text)) {
				return prefix + indexed("keywordDetachedComments", i)
			}
		}
		for i, text := range header {
			if deep(text, true) {
				return prefix + indexed("headerComments", i)
			}
		}
		return ""
	}
	if g, ok := node.(*Grammar); ok {
		pkg := &g.File.Pkg
		if field := source("file.pkg.", &pkg.SourceInfo); field != "" {
			return field
		}
		return keyword("file.pkg.", pkg.KeywordComments, pkg.KeywordDetachedComments, pkg.HeaderComments)
	}
	if n, ok := node.(SourceNode); ok && n.Source() != nil {
		if field := source("", n.Source()); field != "" {
			return field
		}
	}
	switch n := node.(type) {
	case *RuleDef:
		return keyword("", n.KeywordComments, n.KeywordDetachedComments, n.HeaderComments)
	case *TypeDef:
		return keyword("", "", nil, n.HeaderComments)
	case *TypeAlias:
		return keyword("", "", nil, n.HeaderComments)
	case *VarDef:
		return keyword("", "", nil, n.HeaderComments)
	case *Field:
		return keyword("", "", nil, n.HeaderComments)
	}
	return ""
}

func (n *nester) decl(f *nestFrame, d Decl) {
	switch d := d.(type) {
	case *TypeDef:
		f.token(tokWord) // type
		f.token(tokWord)
		f.token(tokOther) // =
		if d.Type != nil && d.Struct() == nil {
			n.typ(f, d.Type)
			return
		}
		var fields []*Field
		if st := d.Struct(); st != nil {
			fields = st.Fields
		}
		n.fields(f, fields)
	case *TypeAlias:
		f.token(tokWord) // type
		f.token(tokWord)
		f.token(tokOther) // ~
		n.typ(f, d.Type)
	case *VarDef:
		f.token(tokWord) // var
		f.token(tokWord)
		if d.Type != nil {
			n.typ(f, d.Type)
		}
		if d.Value != nil {
			f.token(tokOther) // =
			n.action(f, d.Value, 0)
		}
	case *RuleDef:
		n.attributes(f, d.Attrs)
		f.token(tokWord) // rule
		f.token(tokWord)
		if d.Type != nil {
			n.typ(f, d.Type)
		}
		f.token(tokOther) // =
		if pr, attributed := RuleOperators(d.Expr); pr != nil {
			f.token(tokOther) // #
			f.token(tokWord)  // operators
			n.operatorsRule(f, pr)
			if attributed != nil {
				n.attributes(f, attributed.Attrs)
			}
		} else if d.BodyBreak != nil || d.ActionBreak != nil || bodyLayoutControlled(d.Expr) {
			// Format writes a body over several lines in parentheses, as the end of a line
			// outside brackets would end the declaration.
			f.token(tokOpen)
			n.body(f, d.Expr)
			f.token(tokClose)
		} else {
			n.body(f, d.Expr)
		}
		if d.DefaultAction {
			f.token(tokArrow)
			f.token(tokOperand) // ...
		} else if d.Action != nil {
			f.token(tokArrow)
			n.action(f, d.Action, 0)
		}
	}
}

// bodyLayoutControlled reports whether Format writes line breaks in the rule body e, as the
// printer's expressionLayoutControlled does.
func bodyLayoutControlled(e Expr) bool {
	for {
		switch x := e.(type) {
		case *Seq:
			if anyBreak(x.Breaks) {
				return true
			}
			for _, it := range x.Items {
				if bodyLayoutControlled(it) {
					return true
				}
			}
			return false
		case *Choice:
			if anyBreak(x.Breaks) {
				return true
			}
			for _, a := range x.Alts {
				if bodyLayoutControlled(a) {
					return true
				}
			}
			return false
		case *Attributed:
			return anyBreak(x.Breaks)
		case *Atomic:
			return x.Break != nil
		case *Repeat:
			return x.PragmaBreak != nil
		case *Recovery:
			return x.Break != nil
		case *Stream:
			return x.Break != nil
		case *Capture:
			e = x.Expr
		case *And:
			e = x.Expr
		case *Not:
			e = x.Expr
		case *Discard:
			e = x.Expr
		default:
			return false
		}
	}
}

func anyBreak(bs []*LineBreak) bool {
	for _, b := range bs {
		if b != nil {
			return true
		}
	}
	return false
}

// fields writes the braces and fields of a struct type. Format separates the fields by line
// breaks or ";", which end the levels inside the braces alike.
func (n *nester) fields(f *nestFrame, fields []*Field) {
	f.token(tokBrace)
	for i, fd := range fields {
		if i > 0 {
			f.token(tokSep)
		}
		f.token(tokWord)
		n.typ(f, fd.Type)
	}
	f.token(tokClose)
}

func (n *nester) attributes(f *nestFrame, attrs []*Attribute) {
	for _, a := range attrs {
		n.attribute(f, a)
	}
}

func (n *nester) attribute(f *nestFrame, a *Attribute) {
	f.token(tokOther) // @ or #
	f.token(tokWord)
	if len(a.Args) == 0 && len(a.KwArgs) == 0 {
		if a.ArgsLayout != nil && listLayoutHasComments(a.ArgsLayout) {
			// Format keeps the comments of an empty argument list in its parentheses.
			f.token(tokOpen)
			f.token(tokClose)
		}
		return
	}
	f.token(tokOpen)
	first := true
	sep := func() {
		if !first {
			f.token(tokSep)
		}
		first = false
	}
	for _, v := range a.Args {
		sep()
		n.attributeValue(f, v)
	}
	// The order of the keyword arguments does not change the count: each is a word, "=" and a
	// value, after a ",".
	for _, v := range a.KwArgs {
		sep()
		f.token(tokWord)
		f.token(tokOther) // =
		n.attributeValue(f, v)
	}
	if a.TrailingComma {
		f.token(tokSep)
	}
	f.token(tokClose)
}

// listLayoutHasComments reports whether the layout of a list holds a comment.
func listLayoutHasComments(l *ListLayout) bool {
	has := func(b *LineBreak) bool { return b != nil && (len(b.Comments) > 0 || b.Trailing != nil) }
	if has(l.OpenBreak) || has(l.BodyBreak) || has(l.CloseBreak) {
		return true
	}
	for _, e := range l.Elems {
		if e != nil && (len(e.Before) > 0 || len(e.After) > 0) {
			return true
		}
	}
	return false
}

func (n *nester) attributeValue(f *nestFrame, v any) {
	switch v := v.(type) {
	case int:
		n.int(f, v)
	case Expr:
		n.child(f, nestKey{node: v, mode: modeArg}, func(c *nestFrame) {
			if leadsWithConstantName(v) || isCaseSensitiveLiteral(v) {
				// An argument that would read as a constant or a string is in parentheses.
				c.token(tokOpen)
				n.expr(c, v, precChoiceLevel)
				c.token(tokClose)
				return
			}
			n.expr(c, v, precChoiceLevel)
		})
	default:
		f.token(tokWord) // nil, a string or a boolean
	}
}

// int writes an integer constant: a negative one is "-" and its magnitude, except the minimum
// int, which is one token.
func (n *nester) int(f *nestFrame, v int) {
	if v < 0 && v != math.MinInt {
		f.token(tokPrefix)
	}
	f.token(tokWord)
}

func isCaseSensitiveLiteral(e Expr) bool {
	lit, ok := e.(*Literal)
	return ok && !lit.IgnoreCase
}

// leadsWithConstantName reports whether the text of e may begin with a rule reference named
// true, false or nil, as the printer's leadsWithLiteralName does.
func leadsWithConstantName(e Expr) bool {
	for {
		switch x := e.(type) {
		case *Ref:
			return x.Level == "" && (x.Name == "true" || x.Name == "false" || x.Name == "nil")
		case *Seq:
			if len(x.Items) == 0 {
				return false
			}
			e = x.Items[0]
		case *Choice:
			if len(x.Alts) == 0 {
				return false
			}
			e = x.Alts[0]
		case *Repeat:
			e = x.Expr
		case *Optional:
			e = x.Expr
		case *Stream:
			e = x.Expr
		case *Atomic:
			e = x.Expr
		case *Recovery:
			e = x.Expr
		case *Attributed:
			e = x.Expr
		default:
			return false
		}
	}
}

// Binding levels of rule expressions, as the printer's.
const (
	precChoiceLevel = iota
	precSeqLevel
	precPrefixLevel
	precSuffixLevel
)

// spelledWithPlaceholder reports whether Format writes e with "_" or "_|_": a sequence of one
// item or a choice of one alternative.
func spelledWithPlaceholder(e Expr) bool {
	switch e := e.(type) {
	case *Seq:
		return len(e.Items) == 1
	case *Choice:
		return len(e.Alts) == 1
	}
	return false
}

// body writes a rule body or the body of a group: the alternatives of a top-level choice, each a
// sequence of items.
func (n *nester) body(f *nestFrame, e Expr) {
	n.child(f, nestKey{node: e, mode: modeBody}, func(c *nestFrame) {
		ch, ok := e.(*Choice)
		if !ok || spelledWithPlaceholder(ch) {
			n.sequence(c, e, precChoiceLevel)
			return
		}
		for i, a := range ch.Alts {
			if i > 0 {
				c.token(tokOther) // /
			}
			n.sequence(c, a, precSeqLevel)
		}
	})
}

func (n *nester) sequence(f *nestFrame, e Expr, outer int) {
	if spelledWithPlaceholder(e) {
		n.expr(f, e, outer)
		return
	}
	switch e := e.(type) {
	case *Choice:
		n.expr(f, e, outer)
	case *Seq:
		for _, it := range e.Items {
			n.item(f, it)
		}
	default:
		n.item(f, e)
	}
}

// hasLayoutBreak reports whether the operand e of a suffix holds a line break or a comment that
// only the printer writes, as the printer's hasBreak does.
func hasLayoutBreak(e Expr) bool {
	for {
		switch x := e.(type) {
		case *Group:
			return true
		case *Atomic:
			if x.Break != nil {
				return true
			}
			e = x.Expr
		case *Stream:
			if x.Break != nil {
				return true
			}
			e = x.Expr
		case *Recovery:
			if x.Break != nil {
				return true
			}
			e = x.Expr
		case *Attributed:
			if x.Breaks != nil {
				return true
			}
			e = x.Expr
		case *Repeat:
			if x.PragmaBreak != nil {
				return true
			}
			e = x.Expr
		case *Optional:
			e = x.Expr
		default:
			return false
		}
	}
}

// item writes an item of a rule body as the printer's itemNode does. It differs from expr only
// in how it writes a repetition with a line break before its pragma, and in that a prefix
// operator writes its operand as an item.
func (n *nester) item(f *nestFrame, e Expr) {
	n.child(f, nestKey{node: e, mode: modeItem}, func(c *nestFrame) {
		switch e := e.(type) {
		case *Group:
			n.group(c, e)
		case *Capture:
			c.token(tokWord)
			c.token(tokColon)
			n.item(c, e.Expr)
		case *And:
			c.token(tokPrefix)
			n.item(c, e.Expr)
		case *Not:
			c.token(tokPrefix)
			n.item(c, e.Expr)
		case *Discard:
			c.token(tokPrefix)
			n.item(c, e.Expr)
		case *Atomic:
			n.suffixOperand(c, e.Expr)
			pragma(c)
		case *Stream:
			n.suffixOperand(c, e.Expr)
			pragma(c)
		case *Recovery:
			n.suffixOperand(c, e.Expr)
			n.recoverPragma(c, e)
		case *Repeat:
			switch {
			case e.PragmaBreak == nil && hasLayoutBreak(e.Expr):
				n.item(c, e.Expr)
				n.repeatSuffix(c, e)
			case e.PragmaBreak == nil:
				n.expr(c, e, precPrefixLevel)
			default:
				n.suffixOperand(c, e.Expr)
				n.repeatPragma(c, e)
			}
		case *Optional:
			if hasLayoutBreak(e.Expr) {
				n.item(c, e.Expr)
				c.token(tokPrefix) // ?
				return
			}
			n.expr(c, e, precPrefixLevel)
		case *Attributed:
			if _, nested := e.Expr.(*Attributed); nested {
				c.token(tokOpen)
				n.expr(c, e.Expr, precChoiceLevel)
				c.token(tokClose)
			} else {
				n.suffixOperand(c, e.Expr)
			}
			n.attributes(c, e.Attrs)
		default:
			n.expr(c, e, precPrefixLevel)
		}
	})
}

// pragma writes "#" and the name of a pragma without arguments.
func pragma(f *nestFrame) {
	f.token(tokOther)
	f.token(tokWord)
}

func (n *nester) suffixOperand(f *nestFrame, e Expr) {
	if _, group := e.(*Group); group || hasLayoutBreak(e) {
		n.item(f, e)
		return
	}
	n.expr(f, e, precSuffixLevel)
}

func (n *nester) group(f *nestFrame, g *Group) {
	f.token(tokOpen)
	if a, ok := g.Expr.(*InlineAction); ok {
		n.body(f, a.Expr)
		n.inlineAction(f, a)
	} else {
		n.body(f, g.Expr)
	}
	f.token(tokClose)
}

// inlineAction writes the "=>" of an inline action and its action.
func (n *nester) inlineAction(f *nestFrame, a *InlineAction) {
	f.token(tokArrow)
	if a.DefaultAction {
		f.token(tokOperand) // ...
		return
	}
	n.action(f, a.Action, 0)
}

func (n *nester) recoverPragma(f *nestFrame, e *Recovery) {
	pragma(f) // #recover
	f.token(tokOpen)
	f.token(tokWord)  // skip
	f.token(tokOther) // =
	n.expr(f, e.Skip, precChoiceLevel)
	f.token(tokClose)
}

// repeatSuffix writes the operator of a repetition after its operand: "*" or "+", or the pragma
// when its spelling holds comments.
func (n *nester) repeatSuffix(f *nestFrame, e *Repeat) {
	if !repeatHasComments(e) && e.Max < 0 && (e.Min == 0 || e.Min == 1) {
		f.token(tokPrefix)
		return
	}
	n.repeatPragma(f, e)
}

func repeatHasComments(e *Repeat) bool {
	if e.Spelling != nil {
		for _, g := range e.Spelling.Gaps {
			if len(g.Comments) > 0 {
				return true
			}
		}
	}
	return false
}

// repeatPragma writes #repeat with its bounds in the canonical spelling.
func (n *nester) repeatPragma(f *nestFrame, e *Repeat) {
	pragma(f)
	f.token(tokOpen)
	switch {
	case e.Min == e.Max:
		f.token(tokWord)
	case e.Max < 0:
		f.token(tokWord)
		f.token(tokSep)
	case e.Min == 0:
		f.token(tokSep)
		f.token(tokWord)
	default:
		f.token(tokWord)
		f.token(tokSep)
		f.token(tokWord)
	}
	f.token(tokClose)
}

// paren writes write, in parentheses when inner binds more loosely than outer.
func paren(f *nestFrame, inner, outer int, write func()) {
	if inner < outer {
		f.token(tokOpen)
		write()
		f.token(tokClose)
		return
	}
	write()
}

// expr writes the rule expression e at the binding level outer, as the printer's formatExprNode
// does.
func (n *nester) expr(f *nestFrame, e Expr, outer int) {
	n.child(f, nestKey{node: e, mode: modeExpr, outer: outer}, func(c *nestFrame) {
		switch e := e.(type) {
		case *InlineAction:
			c.token(tokOpen)
			n.expr(c, e.Expr, precChoiceLevel)
			n.inlineAction(c, e)
			c.token(tokClose)
		case *Choice:
			paren(c, precChoiceLevel, outer, func() {
				for i, a := range e.Alts {
					if i > 0 {
						c.token(tokOther) // /
					}
					n.expr(c, a, precSeqLevel)
				}
				if len(e.Alts) == 1 {
					c.token(tokOther)   // /
					c.token(tokOperand) // _|_
				}
			})
		case *Seq:
			paren(c, precSeqLevel, outer, func() {
				for _, it := range e.Items {
					n.expr(c, it, precPrefixLevel)
				}
				if len(e.Items) == 1 {
					c.token(tokWord) // _
				}
			})
		case *Capture:
			paren(c, precPrefixLevel, outer, func() {
				c.token(tokWord)
				c.token(tokColon)
				n.expr(c, e.Expr, precPrefixLevel)
			})
		case *And:
			n.prefixed(c, e.Expr, outer)
		case *Not:
			n.prefixed(c, e.Expr, outer)
		case *Discard:
			n.prefixed(c, e.Expr, outer)
		case *Atomic:
			n.expr(c, e.Expr, precSuffixLevel)
			pragma(c)
		case *Repeat:
			n.expr(c, e.Expr, precSuffixLevel)
			n.repeatSuffix(c, e)
		case *Optional:
			n.expr(c, e.Expr, precSuffixLevel)
			c.token(tokPrefix) // ?
		case *Stream:
			n.expr(c, e.Expr, precSuffixLevel)
			pragma(c)
		case *Recovery:
			n.expr(c, e.Expr, precSuffixLevel)
			n.recoverPragma(c, e)
		case *Attributed:
			if _, nested := e.Expr.(*Attributed); nested {
				c.token(tokOpen)
				n.expr(c, e.Expr, precSuffixLevel)
				c.token(tokClose)
			} else {
				n.expr(c, e.Expr, precSuffixLevel)
			}
			n.attributes(c, e.Attrs)
		case *Group:
			n.group(c, e)
		case *Ref:
			if e.Level != "" {
				c.token(tokWord) // level
				c.token(tokOpen)
				c.token(tokWord)
				c.token(tokSep)
				c.token(tokWord)
				c.token(tokClose)
				return
			}
			c.token(tokWord)
		case *Literal:
			c.token(tokWord)
			if e.IgnoreCase {
				c.token(tokWord) // i
			}
		case *CharClass, *Top:
			c.token(tokWord)
		case *Cut:
			pragma(c)
		case *Any, *Bottom, *BeginInput, *EndInput, *BeginLine, *EndLine:
			c.token(tokOperand)
		case *Predicate:
			if b, ok := e.Action.(*Block); ok {
				n.action(c, b, 0)
				return
			}
			c.token(tokBrace)
			n.action(c, e.Action, 0)
			c.token(tokClose)
		case *OperatorsRule:
			n.operatorsRule(c, e)
		}
	})
}

// prefixed writes a prefix operator and its operand.
func (n *nester) prefixed(f *nestFrame, operand Expr, outer int) {
	paren(f, precPrefixLevel, outer, func() {
		f.token(tokPrefix)
		n.expr(f, operand, precPrefixLevel)
	})
}

// operatorsRule writes the braces of an Operators Rule and its lines.
func (n *nester) operatorsRule(f *nestFrame, pr *OperatorsRule) {
	f.token(tokBrace)
	first := true
	line := func() {
		if !first {
			f.token(tokSep)
		}
		first = false
	}
	if pr.Skip != nil {
		line()
		f.token(tokWord) // skip
		n.expr(f, pr.Skip, precChoiceLevel)
	}
	for _, o := range pr.Operands {
		line()
		f.token(tokWord) // operand
		n.expr(f, o.Expr, precChoiceLevel)
		if o.DefaultAction && o.Action == nil {
			f.token(tokArrow)
			f.token(tokOperand)
		} else if o.Action != nil {
			f.token(tokArrow)
			n.action(f, o.Action, 0)
		}
	}
	for _, l := range pr.Levels {
		line()
		f.token(tokWord) // level
		if l.Name != "" {
			f.token(tokWord)
		}
		f.token(tokBrace)
		for i, op := range l.Operators {
			if i > 0 {
				f.token(tokSep)
			}
			f.token(tokWord) // kind
			if op.Assoc != "" {
				f.token(tokWord)
			}
			n.expr(f, op.Expr, precChoiceLevel)
			if op.Action != nil {
				f.token(tokArrow)
				n.action(f, op.Action, 0)
			} else if op.DefaultAction {
				f.token(tokArrow)
				f.token(tokOperand)
			}
		}
		f.token(tokClose)
	}
	f.token(tokClose)
}

// Binding levels of action expressions, as the printer's.
const (
	actionPrecUnary   = 6
	actionPrecPostfix = 7
)

var actionPrec = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3,
	"+": 4, "-": 4,
	"*": 5, "/": 5, "%": 5,
}

// operatorToken is the class of the token of a binary or unary operator.
func operatorToken(op string) nestToken {
	switch op {
	case "-", "*", "+", "!", "&":
		return tokPrefix
	}
	return tokOther
}

// action writes the action expression t at the binding level outer, as the printer's
// formatActionExprNode does.
func (n *nester) action(f *nestFrame, t ActionExpr, outer int) {
	n.child(f, nestKey{node: t, mode: modeAction, outer: outer}, func(c *nestFrame) {
		parenAction := func(cond bool, write func()) {
			if cond {
				c.token(tokOpen)
				write()
				c.token(tokClose)
				return
			}
			write()
		}
		list := func(open nestToken, elems int, comma bool, write func(i int)) {
			c.token(open)
			for i := range elems {
				if i > 0 {
					c.token(tokSep)
				}
				write(i)
			}
			if comma && elems > 0 {
				c.token(tokSep)
			}
			c.token(tokClose)
		}
		switch t := t.(type) {
		case *ActionGroup:
			c.token(tokOpen)
			n.action(c, t.Action, 0)
			c.token(tokClose)
		case *Block:
			list(tokBrace, len(t.Statements), false, func(i int) { n.action(c, t.Statements[i], 0) })
		case *IntLit:
			parenAction(t.Value < 0 && outer > actionPrecUnary, func() { n.int(c, t.Value) })
		case *StringLit, *BoolLit, *NilLit, *CaptureRef, *VarRef:
			c.token(tokWord)
		case *FieldAccess:
			n.action(c, t.X, actionPrecPostfix)
			c.token(tokOperand) // .
			c.token(tokWord)
		case *Constructor:
			c.token(tokWord)
			list(tokBrace, len(t.Fields)+len(t.Args), t.TrailingComma, func(i int) {
				if i < len(t.Fields) {
					c.token(tokWord)
					c.token(tokColon)
					n.action(c, t.Fields[i].Value, 0)
					return
				}
				n.action(c, t.Args[i-len(t.Fields)], 0)
			})
		case *TupleLit:
			list(tokOpen, len(t.Elems), len(t.Elems) == 1 || t.TrailingComma, func(i int) { n.action(c, t.Elems[i], 0) })
		case *ListConstructor:
			c.token(tokPrefix) // []
			n.typeTerm(c, t.Elem)
			list(tokBrace, len(t.Items), t.TrailingComma, func(i int) { n.action(c, t.Items[i], 0) })
		case *Call:
			c.token(tokWord)
			list(tokOpen, len(t.Args), t.TrailingComma, func(i int) { n.action(c, t.Args[i], 0) })
		case *Lambda:
			parenAction(outer > 0, func() {
				list(tokOpen, len(t.Params), t.TrailingComma, func(int) { c.token(tokWord) })
				c.token(tokArrow)
				n.action(c, t.Body, 0)
			})
		case *Binary:
			if t.Op == "[]" {
				var wrap bool
				switch x := t.L.(type) {
				case *IntLit:
					wrap = x.Value >= 0
				case *StringLit, *Constructor, *ListConstructor, *Lambda:
					wrap = true
				}
				parenAction(wrap, func() { n.action(c, t.L, actionPrecPostfix) })
				c.token(tokOpen)
				n.action(c, t.R, 0)
				c.token(tokClose)
				return
			}
			p := actionPrec[t.Op]
			lp := p
			if p == actionPrec["=="] {
				lp++
			}
			parenAction(p < outer, func() {
				n.action(c, t.L, lp)
				c.token(operatorToken(t.Op))
				n.action(c, t.R, p+1)
			})
		case *Unary:
			parenAction(outer > actionPrecUnary, func() {
				c.token(operatorToken(t.Op))
				n.action(c, t.X, actionPrecUnary)
			})
		case *Cast:
			lit, ok := t.X.(*IntLit)
			parenAction(ok && lit.Value >= 0, func() { n.action(c, t.X, actionPrecPostfix) })
			c.token(tokOperand) // .
			c.token(tokOpen)
			n.typ(c, t.Type)
			c.token(tokClose)
		case *Bind:
			c.token(tokWord)
			c.token(tokOther) // :=
			n.action(c, t.Value, 0)
		}
	})
}

// typ writes the type expression t, as the printer's formatType does.
func (n *nester) typ(f *nestFrame, t TypeExpr) {
	n.child(f, nestKey{node: t, mode: modeType}, func(c *nestFrame) {
		switch t := t.(type) {
		case *TypeGroup:
			c.token(tokOpen)
			n.typ(c, t.Type)
			c.token(tokClose)
		case *TypeRef:
			c.token(tokWord)
		case *ListType:
			c.token(tokPrefix) // []
			n.typeTerm(c, t.Elem)
		case *OptionalType:
			c.token(tokPrefix) // *
			n.typeTerm(c, t.Elem)
		case *JoinType:
			for i, x := range t.Types {
				if i > 0 {
					c.token(tokOther) // |
				}
				if _, meet := x.(*MeetType); meet {
					n.typ(c, x) // & binds more tightly than |
				} else {
					n.typeTerm(c, x)
				}
			}
		case *MeetType:
			for i, x := range t.Types {
				if i > 0 {
					c.token(tokPrefix) // &
				}
				n.typeTerm(c, x)
			}
		case *TupleType:
			c.token(tokOpen)
			for i, x := range t.Types {
				if i > 0 {
					c.token(tokSep)
				}
				n.typ(c, x)
			}
			if len(t.Types) == 1 || t.TrailingComma && len(t.Types) > 1 {
				c.token(tokSep)
			}
			c.token(tokClose)
		case *StructType:
			n.fields(c, t.Fields)
		case *TypeApp:
			c.token(tokWord)
			c.token(tokOpen) // [
			for i, x := range t.Args {
				if i > 0 {
					c.token(tokSep)
				}
				n.typ(c, x)
			}
			if t.TrailingComma && len(t.Args) > 0 {
				c.token(tokSep)
			}
			c.token(tokClose)
		}
	})
}

// typeTerm writes t as the operand of a type operator: a join or a meet in parentheses.
func (n *nester) typeTerm(f *nestFrame, t TypeExpr) {
	switch t.(type) {
	case *JoinType, *MeetType:
		n.child(f, nestKey{node: t, mode: modeTypeTerm}, func(c *nestFrame) {
			c.token(tokOpen)
			n.typ(c, t)
			c.token(tokClose)
		})
		return
	}
	n.typ(f, t)
}
