package check

import (
	"sync"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// dropRepeatedErrors removes the errors after the first from that repeat an earlier one at the same
// position. A type is resolved wherever it is used, so an undefined name inside an alias would be
// reported once per use of the alias.
func dropRepeatedErrors(errs *diagnostic.ErrorList, from int) {
	type key struct {
		pos ast.Pos
		msg string
	}
	seen := map[key]bool{}
	kept := (*errs)[:from]
	for _, e := range (*errs)[from:] {
		k := key{e.Pos, e.Msg}
		if !seen[k] {
			seen[k] = true
			kept = append(kept, e)
		}
	}
	*errs = kept
}

// CheckTypes checks the types of the whole grammar.
func CheckTypes(prog *Checker, errs *diagnostic.ErrorList) {
	CheckTypesControlled(prog, errs, nil)
}

// CheckTypesControlled is CheckTypes polling control for cancellation; a nil control never
// cancels.
func CheckTypesControlled(prog *Checker, errs *diagnostic.ErrorList, control *cancel.Control) {
	defer dropRepeatedErrors(errs, len(*errs))
	k := &TypeResolver{control: control, Checker: prog, errs: errs, report: true}
	k.checkTypeDefinitions()
	rules := control.Rules(prog.Grammar)
	for _, rd := range rules {
		control.Poll()
		if rd.Type != nil {
			k.resolveLocation(rd.Type, rd.Diagnostic(), "rule "+rd.Name)
		}
	}

	// Check the values.
	// Every compilation builds Core IR, which reads the types of the program's parts, so the
	// inference records them as it checks.
	nodeMembers := map[*ast.FieldAccess]bool{}
	rec := newValueRecord()
	values, valueErrs := valueTypesChecked(prog, control, nodeMembers, rec)
	*errs = append(*errs, valueErrs...)

	// Keep the types for code generation (typedGo).
	info := &TypeInfo{Fields: map[string][]typedField{}, Aliases: map[string]Ty{}, NodeMembers: nodeMembers, Values: values, valueErrs: valueErrs, rec: rec}
	info.valuesOnce.Do(func() {})
	info.recOnce.Do(func() {})
	info.shown = shownRuleTypes(prog, rules, values, control)
	k.report = false
	for _, td := range control.Types(prog.Grammar) {
		control.Poll()
		switch spec := td.(type) {
		case *ast.TypeDef:
			for _, f := range DefFields(spec) {
				control.Poll()
				info.Fields[td.DeclName()] = append(info.Fields[td.DeclName()], typedField{f.Name, k.Resolve(f.Type)})
			}
		case *ast.TypeAlias:
			info.Aliases[td.DeclName()] = k.ResolveNamed(td.DeclName(), td.Diagnostic())
		}
	}
	prog.Typed = info
}

// TypeInfo holds the types found by the type checker, for code generation.
type TypeInfo struct {
	// Values holds the type of each rule's value in the value model (Checker.ValueTypes).
	Values     map[string]Ty
	valueErrs  diagnostic.ErrorList // the errors of actions in the value model
	shown      map[string]Ty        // the type of each rule as RuleType shows it (shownRuleTypes)
	valuesOnce sync.Once
	Fields     map[string][]typedField // the fields of each struct type, in declaration order
	Aliases    map[string]Ty           // the type each alias stands for
	// NodeMembers reports, for each member access of an action, whether it reads only members of
	// nodes and recovered values (.Start, .End and .Value, and the fields expected and message of
	// missing) and never a struct field.
	NodeMembers map[*ast.FieldAccess]bool
	// rec holds the types of the action expressions, scopes and rules of the checked program, recorded by the
	// inference of the type check (or, for type information made without it, on first use by
	// ValueRecord).
	rec     *ValueRecord
	recOnce sync.Once
}

// ValueRecord returns the types the value inference gives the parts of the type-checked program
// (valueRecord), or nil if its types were not checked. The type check records them; type
// information made without it records them by a run of the inference on first use.
func (c *Checker) ValueRecord() *ValueRecord {
	if c.Typed == nil {
		return nil
	}
	c.Typed.recOnce.Do(func() {
		rec := newValueRecord()
		valueTypesChecked(c, nil, nil, rec)
		c.Typed.rec = rec
	})
	return c.Typed.rec
}

// RuleType is RuleType.
func (c *Checker) RuleType(name string) (string, bool) {
	if c.Typed == nil {
		return "", false
	}
	t, ok := c.Typed.shown[name]
	if !ok {
		return "", false
	}
	return t.String(), true
}

type typedField struct {
	Name string
	T    Ty
}

// ConcreteType resolves aliases and normalized singleton joins to their node type.
// It does not report errors; validation still uses the type expression as written.
func (c *Checker) ConcreteType(t ast.TypeExpr) (string, ast.TypeDecl) {
	if ref, ok := t.(*ast.TypeRef); ok {
		switch spec := c.typeDecls()[ref.Name].(type) {
		case *ast.TypeDef:
			return ref.Name, spec
		}
	}
	k := &TypeResolver{Checker: c}
	if n, ok := k.Resolve(t).(NamedTy); ok && n.Kind == 's' {
		return n.Name, c.typeDecls()[n.Name]
	}
	return "", nil
}

// StructType returns the struct type definition that the type named name denotes, following
// aliases, with the name of that definition; it returns "" and nil when name denotes no struct
// type.
func (c *Checker) StructType(name string) (string, *ast.TypeDef) {
	switch spec := c.typeDecls()[name].(type) {
	case *ast.TypeDef:
		return name, spec
	case *ast.TypeAlias:
		// Resolve only aliases; ordinary constructors need no temporary type expression.
	default:
		return "", nil
	}
	canonical, spec := c.ConcreteType(&ast.TypeRef{Name: name})
	st, _ := spec.(*ast.TypeDef)
	return canonical, st
}

// concreteListElement reports whether the element type t of a list constructor, including the
// elements of nested list types, names no unknown type.
func concreteListElement(t Ty) bool {
	switch t := t.(type) {
	case ListTy:
		return concreteListElement(t.Elem)
	case OptTy:
		return concreteListElement(t.Elem)
	case JoinTy:
		for _, alt := range t.Alts {
			if !concreteListElement(alt) {
				return false
			}
		}
		return true
	}
	return t != TyAny
}

// ActionExprPos returns the position of action expression t for a diagnostic, or def if it has none.
func ActionExprPos(t ast.ActionExpr, def ast.Pos) ast.Pos {
	switch t := t.(type) {
	case *ast.ActionGroup:
		return ActionExprPos(t.Action, def)
	case *ast.CaptureRef:
		return t.Diagnostic()
	case *ast.VarRef:
		return t.Diagnostic()
	case *ast.FieldAccess:
		return t.Diagnostic()
	case *ast.Constructor:
		return t.Diagnostic()
	case *ast.ListConstructor:
		return t.Diagnostic()
	case *ast.TupleLit:
		return t.Diagnostic()
	case *ast.Call:
		return t.Diagnostic()
	case *ast.Binary:
		return t.Diagnostic()
	case *ast.Unary:
		return t.Diagnostic()
	case *ast.Cast:
		return t.Diagnostic()
	case *ast.Bind:
		return t.Diagnostic()
	case *ast.Block:
		return t.Diagnostic()
	}
	return def
}

// Shown returns the type of the rule name as RuleType shows it, before it is written out.
func (i *TypeInfo) Shown(name string) (Ty, bool) {
	t, ok := i.shown[name]
	return t, ok
}
