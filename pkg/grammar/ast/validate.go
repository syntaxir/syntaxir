package ast

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ValidationError describes a structural defect in a grammar AST. Path uses
// JSON field names and zero-based array indexes. Pos is the closest known
// diagnostic position, when the AST carries source information.
type ValidationError struct {
	Path string
	Pos  Pos
	Msg  string
}

func (e *ValidationError) Error() string {
	message := e.Path + ": " + e.Msg
	if e.Pos.IsValid() {
		return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, message)
	}
	return message
}

type validationFrame struct {
	node  any
	field string
	index int
	exit  bool
	// place is the placement of the node, set on its exit frame.
	place placement
	// depth is the structural depth of the node on the path (see
	// MaxNestingDepth), and below the height of its deepest child so far.
	depth, below int
}

// Validate checks required children, names, repetition and character-range
// bounds, and pointer cycles without mutating g, and rejects every tree that
// grammar syntax cannot write, so that Format of a valid AST parses. It returns
// the first *ValidationError in deterministic depth-first order, checking a
// node's bounds and placement before its children, or nil.
//
// Optional children may be nil, but an interface containing a nil pointer is
// invalid. Shared subtrees are allowed, each checked where it stands. A collection may be empty where grammar
// syntax writes it empty; Repeat.Max is -1 for an unbounded repetition. Source
// layout is not checked. Name resolution, types, attribute arguments and
// OperatorsRule semantics are checked by the compiler. The text of a valid AST,
// as Format writes it, nests no deeper than MaxNestingLevels, and neither does a
// comment string that Format writes as a block comment: the error of a deeper
// one is "nesting too deep", as the reference parser reports for its text. So
// is the error of a tree with a node deeper than MaxNestingDepth, which the
// parser rejects too. Validation imposes no size limit.
func Validate(g *Grammar) error {
	return validate(g, nil)
}

// ValidateContext checks the same grammar invariants as Validate, but stops
// without returning partial diagnostics when ctx is cancelled. It polls while
// traversing the AST; individual string checks and sorting are not interruptible.
func ValidateContext(ctx context.Context, g *Grammar) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ctx.Done() == nil {
		return Validate(g)
	}
	control := &validationControl{ctx: ctx}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(validationCancellation); ok {
				err = cancelled.err
			} else {
				panic(failure)
			}
		}
	}()
	err = validate(g, control)
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	return err
}

type validationCancellation struct{ err error }

type validationControl struct {
	ctx   context.Context
	steps uint32
}

func (c *validationControl) poll() {
	if c == nil {
		return
	}
	c.steps++
	if c.steps%256 == 0 {
		c.check()
	}
}

func (c *validationControl) check() {
	if err := c.ctx.Err(); err != nil {
		panic(validationCancellation{err})
	}
}

// visitKey identifies a checked node: a shared node is checked once in each
// placement and parameter scope that the checks of where it stands read, as its
// validity may differ between them.
type visitKey struct {
	node  any
	place placement
	scope *paramScope
}

// placement is what the checks of a node read of where it stands (unwritable):
// the field that holds it, the kind of its parent and of its grandparent, the
// field that holds the parent, and, for an Operators Rule, whether the parent is
// an Attributed whose attributes are all #ignorecase. A node shared under
// parents that agree on these is checked once.
type placement struct {
	field, parentField   string
	parent, grandparent  reflect.Type
	parentIgnoreCaseOnly bool
}

// placementOf returns the placement of the node of frame f, whose parent and
// grandparent frames are parent and grandparent, zero at the root.
func placementOf(f, parent, grandparent validationFrame) placement {
	p := placement{field: f.field, parentField: parent.field}
	if parent.node != nil {
		p.parent = reflect.TypeOf(parent.node)
	}
	if grandparent.node != nil {
		p.grandparent = reflect.TypeOf(grandparent.node)
	}
	if _, ok := f.node.(*OperatorsRule); ok {
		// Only the check of an Operators Rule reads the attributes of its parent,
		// so no other node walks them once for each sibling.
		if a, ok := parent.node.(*Attributed); ok {
			p.parentIgnoreCaseOnly = ignoreCaseOnly(a.Attrs)
		}
	}
	return p
}

// paramScope is the set of lambda parameter names in scope. Scopes are interned
// by their names, so a pointer cycle through a lambda revisits the same key.
type paramScope struct {
	names map[string]bool
}

// astPackage is the import path of the package of the AST nodes.
var astPackage = reflect.TypeFor[Grammar]().PkgPath()

func validate(g *Grammar, control *validationControl) error {
	stack := []validationFrame{{node: g, index: -1}}
	var path []validationFrame
	// active holds the nodes on the current path, which a pointer cycle
	// reaches again; done holds the nodes checked in a parameter scope.
	active := make(map[any]bool)
	done := make(map[visitKey]bool)
	var scopes []*paramScope
	interned := map[string]*paramScope{}
	scope := func() *paramScope {
		if len(scopes) == 0 {
			return nil
		}
		return scopes[len(scopes)-1]
	}
	enterLambda := func(l *Lambda) {
		names := map[string]bool{}
		if s := scope(); s != nil {
			for name := range s.names {
				control.poll()
				names[name] = true
			}
		}
		for _, name := range l.Params {
			control.poll()
			names[name] = true
		}
		keys := make([]string, 0, len(names))
		for name := range names {
			control.poll()
			keys = append(keys, name)
		}
		sort.Strings(keys)
		key := strings.Join(keys, ",")
		s, ok := interned[key]
		if !ok {
			s = &paramScope{names: names}
			interned[key] = s
		}
		scopes = append(scopes, s)
	}
	inScope := func(name string) bool {
		s := scope()
		return s != nil && s.names[name]
	}
	fail := func(field, message string) error {
		var b strings.Builder
		b.WriteByte('$')
		pos := Pos{}
		for _, f := range path {
			if f.field != "" {
				b.WriteByte('.')
				b.WriteString(f.field)
			}
			if f.index >= 0 {
				b.WriteByte('[')
				b.WriteString(strconv.Itoa(f.index))
				b.WriteByte(']')
			}
			// Only error paths inspect positions; successful validation does not
			// reflect over fields or allocate a string for every visited node.
			v := reflect.ValueOf(f.node)
			if v.IsValid() && v.Kind() == reflect.Pointer && !v.IsNil() && v.Type().Elem().PkgPath() == astPackage {
				if n, ok := f.node.(SourceNode); ok && n.Source().Diagnostic().IsValid() {
					pos = n.Source().Diagnostic()
				}
			}
		}
		if field != "" {
			b.WriteByte('.')
			b.WriteString(field)
		}
		return &ValidationError{Path: b.String(), Pos: pos, Msg: message}
	}
	// heights holds the structural height of each checked node: its weight and
	// that of its deepest descendant chain.
	heights := map[any]int{}
	// leave ends the node on top of the path, whose height is h.
	leave := func(h int) {
		path = path[:len(path)-1]
		if len(path) > 0 {
			top := &path[len(path)-1]
			top.below = max(top.below, h)
		}
	}
	push := func(node any, field string, index int, optional bool) {
		control.poll()
		if optional && node == nil {
			return
		}
		stack = append(stack, validationFrame{node: node, field: field, index: index})
	}
	for len(stack) != 0 {
		control.poll()
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.exit {
			if _, ok := f.node.(*Lambda); ok {
				scopes = scopes[:len(scopes)-1]
			}
			delete(active, f.node)
			done[visitKey{f.node, f.place, scope()}] = true
			top := path[len(path)-1]
			heights[f.node] = structuralWeight(f.node) + top.below
			leave(heights[f.node])
			continue
		}
		path = append(path, f)
		if f.node == nil {
			return fail("", "required node is nil")
		}
		switch f.node.(type) {
		case int, string, bool:
			leave(0)
			continue
		}
		v := reflect.ValueOf(f.node)
		if v.Kind() != reflect.Pointer {
			return fail("", fmt.Sprintf("unsupported node %T", f.node))
		}
		if v.IsNil() {
			return fail("", "required node is nil")
		}
		weight := structuralWeight(f.node)
		if len(path) >= 2 {
			path[len(path)-1].depth = path[len(path)-2].depth + weight
		}
		if path[len(path)-1].depth > MaxNestingDepth {
			return fail("", msgNestingTooDeep)
		}
		if field, name := invalidIdentifier(f.node, control); field != "" {
			if name != "" && name[0] == '_' {
				return fail(field, "identifier "+name+" is reserved")
			}
			return fail(field, fmt.Sprintf("invalid identifier %q: use ASCII letters, digits and underscores", name))
		}
		if field, kind, name := literalName(f.node); field != "" {
			return fail(field, kind+" name "+name+" is reserved; it is a literal")
		}
		var parent, grandparent validationFrame
		if len(path) >= 2 {
			parent = path[len(path)-2]
		}
		if len(path) >= 3 {
			grandparent = path[len(path)-3]
		}
		place := placementOf(f, parent, grandparent)
		if field, message := unwritable(f.node, place, inScope); message != "" {
			return fail(field, message)
		}
		if v.Type().Elem().PkgPath() != astPackage {
			// A node of another package is unsupported; the switch below reports it.
		} else if field := tooDeepComment(f.node, control); field != "" {
			return fail(field, msgNestingTooDeep)
		}
		// Leaf nodes need no cycle tracking. Non-leaf nodes stay active until
		// their exit frame, distinguishing a pointer cycle from a shared child.
		switch n := f.node.(type) {
		case *TypeRef, *Ref, *Literal, *Any,
			*Cut, *Top, *Bottom, *BeginInput, *EndInput, *BeginLine, *EndLine,
			*IntLit, *StringLit, *BoolLit, *NilLit, *CaptureRef, *VarRef:
			leave(weight)
			continue
		case *CharClass:
			if len(n.Ranges) == 0 {
				// No source text writes an empty class: [] and [^] do not parse.
				return fail("ranges", "character class must have at least one range")
			}
			for start := 0; start < len(n.Ranges); {
				end := len(n.Ranges)
				if control != nil {
					control.check()
					end = start + min(256, end-start)
				}
				// The ordinary path validates the full slice without a polling
				// branch per range; controlled validation checks each chunk.
				for offset, r := range n.Ranges[start:end] {
					i := start + offset
					if !utf8.ValidRune(r.Lo) {
						return fail("ranges["+strconv.Itoa(i)+"].lo", "character range lower bound must be a Unicode scalar value")
					}
					if !utf8.ValidRune(r.Hi) {
						return fail("ranges["+strconv.Itoa(i)+"].hi", "character range upper bound must be a Unicode scalar value")
					}
					if r.Hi < r.Lo {
						return fail("ranges["+strconv.Itoa(i)+"].hi", "character range upper bound is less than lower bound")
					}
				}
				start = end
			}
			leave(weight)
			continue
		case *Repeat:
			if n.Min < 0 {
				return fail("min", "repetition minimum must be nonnegative")
			}
			if n.Max < -1 {
				return fail("max", "repetition maximum must be -1 (unbounded) or nonnegative")
			}
			if n.Max >= 0 && n.Max < n.Min {
				return fail("max", "repetition maximum is less than minimum")
			}
		case *TypeDef:
			switch t := n.Type.(type) {
			case nil, *StructType:
			case *TypeApp:
				if t.Name != "node" || len(t.Args) != 1 {
					return fail("type", "a type definition defines a struct type or node[T]")
				}
			default:
				return fail("type", "a type definition defines a struct type or node[T]")
			}
		case *Constructor:
			if len(n.Args) != 0 && len(n.Fields) != 0 {
				return fail("args", "a constructor takes named fields or one value, not both")
			}
		case *Grammar:
			if len(n.File.Pkg.Attrs) > 0 && n.File.Pkg.Name == "" {
				return fail("file.pkg.attrs", "declaration attributes of the package clause need a package name")
			}
		case *RuleDef:
			if n.DefaultAction && n.Action != nil {
				return fail("action", "default action cannot also contain an action expression")
			}
		case *OperatorsRuleOperand:
			if n.DefaultAction && n.Action != nil {
				return fail("action", "default action cannot also contain an action expression")
			}
		case *OperatorsRuleOperator:
			if n.DefaultAction && n.Action != nil {
				return fail("action", "default action cannot also contain an action expression")
			}
		case *InlineAction:
			if n.DefaultAction && n.Action != nil {
				return fail("action", "default action cannot also contain an action expression")
			}
		}
		if active[f.node] {
			return fail("", "pointer cycle in grammar AST")
		}
		if done[visitKey{f.node, place, scope()}] {
			// A shared subtree checked before is as high here; its deepest node
			// lies that far below the parent.
			h := heights[f.node]
			if path[len(path)-1].depth-weight+h > MaxNestingDepth {
				return fail("", msgNestingTooDeep)
			}
			leave(h)
			continue
		}
		active[f.node] = true
		stack = append(stack, validationFrame{node: f.node, exit: true, place: place})
		if l, ok := f.node.(*Lambda); ok {
			enterLambda(l)
		}
		// Push children in reverse order so diagnostics visit source order.
		switch n := f.node.(type) {
		case *Grammar:
			for i := len(n.File.Decls) - 1; i >= 0; i-- {
				push(n.File.Decls[i], "file.decls", i, false)
			}
			for i := len(n.File.Pkg.Attrs) - 1; i >= 0; i-- {
				push(n.File.Pkg.Attrs[i], "file.pkg.attrs", i, false)
			}
		case *TypeDef:
			push(n.Type, "type", -1, false)
		case *TypeAlias:
			push(n.Type, "type", -1, false)
		case *VarDef:
			push(n.Value, "value", -1, true)
			push(n.Type, "type", -1, true)
		case *RuleDef:
			push(n.Action, "action", -1, true)
			push(n.Expr, "expr", -1, false)
			push(n.Type, "type", -1, true)
			for i := len(n.Attrs) - 1; i >= 0; i-- {
				push(n.Attrs[i], "attrs", i, false)
			}
		case *StructType:
			for i := len(n.Fields) - 1; i >= 0; i-- {
				push(n.Fields[i], "fields", i, false)
			}
		case *Field:
			push(n.Type, "type", -1, false)
		case *ListType:
			push(n.Elem, "elem", -1, false)
		case *OptionalType:
			push(n.Elem, "elem", -1, false)
		case *TypeGroup:
			push(n.Type, "type", -1, false)
		case *JoinType:
			for i := len(n.Types) - 1; i >= 0; i-- {
				push(n.Types[i], "types", i, false)
			}
		case *MeetType:
			for i := len(n.Types) - 1; i >= 0; i-- {
				push(n.Types[i], "types", i, false)
			}
		case *TupleType:
			for i := len(n.Types) - 1; i >= 0; i-- {
				push(n.Types[i], "types", i, false)
			}
		case *TypeApp:
			for i := len(n.Args) - 1; i >= 0; i-- {
				push(n.Args[i], "args", i, false)
			}
		case *Seq:
			for i := len(n.Items) - 1; i >= 0; i-- {
				push(n.Items[i], "items", i, false)
			}
		case *Choice:
			for i := len(n.Alts) - 1; i >= 0; i-- {
				push(n.Alts[i], "alts", i, false)
			}
		case *Repeat:
			push(n.Expr, "expr", -1, false)
		case *Group:
			push(n.Expr, "expr", -1, false)
		case *InlineAction:
			push(n.Action, "action", -1, n.DefaultAction)
			push(n.Expr, "expr", -1, false)
		case *Optional:
			push(n.Expr, "expr", -1, false)
		case *And:
			push(n.Expr, "expr", -1, false)
		case *Not:
			push(n.Expr, "expr", -1, false)
		case *Atomic:
			push(n.Expr, "expr", -1, false)
		case *Recovery:
			push(n.Skip, "skip", -1, false)
			push(n.Expr, "expr", -1, false)
		case *Stream:
			push(n.Expr, "expr", -1, false)
		case *Discard:
			push(n.Expr, "expr", -1, false)
		case *Capture:
			push(n.Expr, "expr", -1, false)
		case *Predicate:
			push(n.Action, "action", -1, false)
		case *Attributed:
			for i := len(n.Attrs) - 1; i >= 0; i-- {
				push(n.Attrs[i], "attrs", i, false)
			}
			push(n.Expr, "expr", -1, false)
		case *Attribute:
			keys := make([]string, 0, len(n.KwArgs))
			for key := range n.KwArgs {
				control.poll()
				keys = append(keys, key)
			}
			sort.Strings(keys)
			validValue := func(value any) bool {
				switch value.(type) {
				case nil, int, string, bool, Expr:
					return true
				}
				return false
			}
			for _, key := range keys {
				control.poll()
				if !validValue(n.KwArgs[key]) {
					return fail("kwArgs["+strconv.Quote(key)+"]", "attribute value must be a scalar or parser expression")
				}
			}
			for i, value := range n.Args {
				control.poll()
				if !validValue(value) {
					return fail("args["+strconv.Itoa(i)+"]", "attribute value must be a scalar or parser expression")
				}
			}
			for i := len(keys) - 1; i >= 0; i-- {
				key := keys[i]
				push(n.KwArgs[key], "kwArgs["+strconv.Quote(key)+"]", -1, true)
			}
			for i := len(n.Args) - 1; i >= 0; i-- {
				push(n.Args[i], "args", i, true)
			}
		case *OperatorsRule:
			for i := len(n.Levels) - 1; i >= 0; i-- {
				push(n.Levels[i], "levels", i, false)
			}
			for i := len(n.Operands) - 1; i >= 0; i-- {
				push(n.Operands[i], "operands", i, false)
			}
			push(n.Skip, "skip", -1, true)
		case *OperatorsRuleOperand:
			push(n.Action, "action", -1, true)
			push(n.Expr, "expr", -1, false)
		case *OperatorsRuleLevel:
			for i := len(n.Operators) - 1; i >= 0; i-- {
				push(n.Operators[i], "operators", i, false)
			}
		case *OperatorsRuleOperator:
			push(n.Action, "action", -1, true)
			push(n.Expr, "expr", -1, false)
		case *Block:
			for i := len(n.Statements) - 1; i >= 0; i-- {
				push(n.Statements[i], "statements", i, false)
			}
		case *FieldAccess:
			push(n.X, "x", -1, false)
		case *ActionGroup:
			push(n.Action, "action", -1, false)
		case *Constructor:
			for i := len(n.Args) - 1; i >= 0; i-- {
				push(n.Args[i], "args", i, false)
			}
			for i := len(n.Fields) - 1; i >= 0; i-- {
				push(n.Fields[i], "fields", i, false)
			}
		case *TupleLit:
			for i := len(n.Elems) - 1; i >= 0; i-- {
				push(n.Elems[i], "elems", i, false)
			}
		case *ListConstructor:
			for i := len(n.Items) - 1; i >= 0; i-- {
				push(n.Items[i], "items", i, false)
			}
			push(n.Elem, "elem", -1, false)
		case *FieldInit:
			push(n.Value, "value", -1, false)
		case *Call:
			for i := len(n.Args) - 1; i >= 0; i-- {
				push(n.Args[i], "args", i, false)
			}
		case *Lambda:
			push(n.Body, "body", -1, false)
		case *Binary:
			push(n.R, "r", -1, false)
			push(n.L, "l", -1, false)
		case *Unary:
			push(n.X, "x", -1, false)
		case *Cast:
			push(n.Type, "type", -1, false)
			push(n.X, "x", -1, false)
		case *Bind:
			push(n.Value, "value", -1, false)
		default:
			return fail("", fmt.Sprintf("unsupported node %T", n))
		}
	}
	// The text of a valid AST nests no deeper than the reference parser reads.
	if path, node := tooDeepDecl(g, control); path != "" {
		var pos Pos
		if node != nil && node.Source().Diagnostic().IsValid() {
			pos = node.Source().Diagnostic()
		}
		return &ValidationError{Path: path, Pos: pos, Msg: msgNestingTooDeep}
	}
	return nil
}

// invalidIdentifier checks source names without changing literal data or
// treating the Top node's underscore spelling as an identifier. Empty optional
// names (package and Operators Rule level) are valid.
func invalidIdentifier(node any, control *validationControl) (field, name string) {
	// A rule name may begin with an underscore, which hides the rule; any other name may not.
	invalidName := func(s string, hidden bool) bool {
		if s == "" || s == "_" {
			return true
		}
		if s[0] == '_' && !hidden {
			return true
		}
		for i := 0; i < len(s); i++ {
			c := s[i]
			if i == 0 && c >= '0' && c <= '9' ||
				!(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
				return true
			}
		}
		return false
	}
	invalid := func(s string) bool { return invalidName(s, false) }
	switch n := node.(type) {
	case *Grammar:
		if n.File.Pkg.Name == "" {
			return "", ""
		}
		field, name = "file.pkg.name", n.File.Pkg.Name
	case *RuleDef:
		if invalidName(n.Name, true) {
			return "name", n.Name
		}
		return "", ""
	case *VarDef:
		field, name = "name", n.Name
	case *TypeDef:
		field, name = "name", n.Name
	case *TypeAlias:
		field, name = "name", n.Name
	case *TypeRef:
		if n.Name == "_" || n.Name == "_|_" {
			return "", "" // the top and bottom types
		}
		field, name = "name", n.Name
	case *TypeApp:
		if n.Name == "_" {
			return "", "" // the top type, which takes no type arguments
		}
		field, name = "name", n.Name
	case *Field:
		field, name = "name", n.Name
	case *Ref:
		if invalidName(n.Name, true) {
			return "name", n.Name
		}
		if n.Level == "" {
			return "", ""
		}
		field, name = "level", n.Level
	case *Capture:
		field, name = "name", n.Name
	case *CaptureRef:
		field, name = "name", n.Name
	case *VarRef:
		field, name = "name", n.Name
	case *Bind:
		field, name = "name", n.Name
	case *FieldAccess:
		// x.$y names the capture y.
		field, name = "name", strings.TrimPrefix(n.Name, "$")
	case *Constructor:
		field, name = "type", n.Type
	case *FieldInit:
		field, name = "name", n.Name
	case *Call:
		field, name = "func", n.Func
	case *OperatorsRuleLevel:
		if n.Name == "" {
			return "", ""
		}
		field, name = "name", n.Name
	case *Attribute:
		if invalid(n.Name) {
			return "name", n.Name
		}
		bad := ""
		found := false
		for key := range n.KwArgs {
			control.poll()
			if invalid(key) && (!found || key < bad) {
				bad = key
				found = true
			}
		}
		if found {
			return "kwArgs[" + strconv.Quote(bad) + "]", bad
		}
	case *Lambda:
		for i, param := range n.Params {
			control.poll()
			if invalid(param) {
				return "params[" + strconv.Itoa(i) + "]", param
			}
		}
	}
	if invalid(name) {
		return field, name
	}
	return "", ""
}

// literalName reports a declaration whose name is one of the literals true, false and nil. An
// action could not read such a name, because the literal wins, so the declaration is rejected.
// It returns the field holding the name and the kind of the declaration.
func literalName(node any) (field, kind, name string) {
	isLiteral := func(s string) bool { return s == "true" || s == "false" || s == "nil" }
	switch n := node.(type) {
	case *RuleDef:
		if isLiteral(n.Name) {
			return "name", "rule", n.Name
		}
	case *VarDef:
		if isLiteral(n.Name) {
			return "name", "variable", n.Name
		}
	case *TypeDef:
		if isLiteral(n.Name) {
			return "name", "type", n.Name
		}
	case *TypeAlias:
		if isLiteral(n.Name) {
			return "name", "type", n.Name
		}
	case *Capture:
		if isLiteral(n.Name) {
			return "name", "capture", n.Name
		}
	case *Lambda:
		for i, param := range n.Params {
			if isLiteral(param) {
				return "params[" + strconv.Itoa(i) + "]", "parameter", param
			}
		}
	}
	return "", "", ""
}

// unwritable reports a node that grammar syntax cannot write where it stands, so
// that Format of a valid AST always parses: a node of a kind or with a field
// value that has no spelling, or one whose spelling would read back as another
// node. place is where the node stands, which is all that these checks read of
// it, as validation checks a shared node once for each placement; inScope
// reports the parameters of enclosing lambdas.
func unwritable(node any, place placement, inScope func(string) bool) (field, message string) {
	isLiteral := func(s string) bool { return s == "true" || s == "false" || s == "nil" }
	ruleDef := reflect.TypeFor[*RuleDef]()
	switch n := node.(type) {
	case *Seq:
		if len(n.Items) == 0 {
			return "items", "a sequence needs at least one item"
		}
	case *Choice:
		if len(n.Alts) == 0 {
			return "alts", "a choice needs at least one alternative"
		}
	case *Attributed:
		if len(n.Attrs) == 0 {
			return "attrs", "an attributed expression needs at least one attribute"
		}
	case *Attribute:
		if !n.Pragma {
			break
		}
		if place.parent != reflect.TypeFor[*Attributed]() {
			return "pragma", "a pragma follows a parser expression; a declaration takes @ attributes"
		}
		if n.Name != "ignorecase" && n.Name != "missing" {
			return "name", "unknown pragma #" + n.Name + ": the pragmas held as attributes are #ignorecase and #missing"
		}
		if len(n.Args) != 0 || len(n.KwArgs) != 0 {
			return "args", "#" + n.Name + " takes no arguments"
		}
	case *OperatorsRule:
		if place.parent == ruleDef && place.field == "expr" {
			break
		}
		if place.parent == reflect.TypeFor[*Attributed]() && place.field == "expr" && place.parentField == "expr" &&
			place.grandparent == ruleDef && place.parentIgnoreCaseOnly {
			break
		}
		return "", "an Operators Rule is the whole body of a rule, followed by no pragma other than #ignorecase"
	case *OperatorsRuleOperator:
		switch n.Kind {
		case Prefix, Postfix:
			if n.Assoc != "" {
				return "assoc", "only an infix operator has an associativity"
			}
		case Infix:
			switch n.Assoc {
			case AssocLeft, AssocRight, AssocNone:
			default:
				return "assoc", "an infix operator has the associativity left, right or none"
			}
		default:
			return "kind", "an operator is prefix, postfix or infix"
		}
	case *TypeRef:
		if replacement, ok := valueTypeNames[n.Name]; ok {
			return "name", n.Name + " is not a type; write " + replacement
		}
		if n.Name == "node" {
			return "name", "node takes a type argument: write node[T] with the type T of the node's value"
		}
	case *TypeApp:
		if replacement, ok := valueTypeNames[n.Name]; ok {
			return "name", n.Name + " is not a type; write " + replacement
		}
		if len(n.Args) == 0 {
			return "args", "a type application needs at least one type argument"
		}
	case *JoinType:
		if len(n.Types) < 2 {
			return "types", "a join type needs at least two types"
		}
	case *MeetType:
		if len(n.Types) < 2 {
			return "types", "a meet type needs at least two types"
		}
	case *Block:
		if place.field == "action" {
			switch place.parent {
			case ruleDef, reflect.TypeFor[*InlineAction](), reflect.TypeFor[*Predicate](),
				reflect.TypeFor[*OperatorsRuleOperand](), reflect.TypeFor[*OperatorsRuleOperator]():
				return "", ""
			}
		}
		return "", "a block is the whole action of a rule, an inline action, a predicate, or an operand or operator line"
	case *Bind:
		switch place.parent {
		case reflect.TypeFor[*Block]():
			return "", ""
		case reflect.TypeFor[*Predicate]():
			if place.field == "action" {
				return "", ""
			}
		}
		return "", "a binding is a statement of a block or a predicate"
	case *CaptureRef:
		if n.Param && !inScope(n.Name) {
			return "param", "no enclosing lambda has the parameter " + n.Name
		}
		if !n.Param && inScope(n.Name) {
			return "param", n.Name + " is a parameter of an enclosing lambda, which is read without $: set param"
		}
	case *VarRef:
		if isLiteral(n.Name) {
			return "name", "variable name " + n.Name + " is reserved; it is a literal"
		}
		if inScope(n.Name) {
			return "name", n.Name + " is a parameter of an enclosing lambda: read it with a CaptureRef that sets param"
		}
	case *Call:
		if isLiteral(n.Func) {
			return "func", "function name " + n.Func + " is reserved; it is a literal"
		}
	case *Constructor:
		if isLiteral(n.Type) {
			return "type", "type name " + n.Type + " is reserved; it is a literal"
		}
	case *FieldAccess:
		if capturesMember(n.Name) {
			return "name", "a capture is read with .$" + n.Name + "; a field name begins with an uppercase letter"
		}
	case *Binary:
		if _, ok := binaryOperators[n.Op]; !ok {
			return "op", "unknown binary operator " + strconv.Quote(n.Op)
		}
	case *Unary:
		if n.Op != "-" && n.Op != "!" {
			return "op", "unknown prefix operator " + strconv.Quote(n.Op) + ": the prefix operators are - and !"
		}
	}
	return "", ""
}

// ignoreCaseOnly reports whether attrs are all the pragma #ignorecase.
func ignoreCaseOnly(attrs []*Attribute) bool {
	for _, a := range attrs {
		if a == nil || !a.Pragma || a.Name != "ignorecase" {
			return false
		}
	}
	return true
}

// capturesMember reports whether the field access x.name would read as the
// capture name in earlier syntax, which the parser rejects: a field name begins
// with an uppercase letter, a capture is $name, and startPos, endPos, children,
// expected and message are members of every node or of Missing.
func capturesMember(name string) bool {
	switch name {
	case "startPos", "endPos", "children", "expected", "message", "":
		return false
	}
	c := name[0]
	return c != '$' && !(c >= 'A' && c <= 'Z')
}

// valueTypeNames maps the reserved names of concrete syntax tree types, which
// grammar syntax rejects as types, to the value types that replace them.
var valueTypeNames = map[string]string{
	"terminal": "node[string]",
	"Match":    "node[string]",
	"Seq":      "node[_]",
	"List":     "node[_]",
	"Operator": "node[_]",
	"Error":    "error",
	"Missing":  "missing",
}

// binaryOperators are the binary operators of actions; [] is the index x[i].
var binaryOperators = map[string]struct{}{
	"||": {}, "&&": {},
	"==": {}, "!=": {}, "<": {}, "<=": {}, ">": {}, ">=": {},
	"+": {}, "-": {}, "*": {}, "/": {}, "%": {},
	"[]": {},
}
