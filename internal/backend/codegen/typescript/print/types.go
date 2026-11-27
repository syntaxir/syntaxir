package print

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TsReservedTypes are the names types.ts declares itself, or imports. A grammar type with one of
// these names would shadow or redeclare it, so the generator rejects it.
var TsReservedTypes = []string{"Span", "TNode", "StringNode", "TupleNode", "ListNode", "RecoveredError", "TypedParseResult", "TypedValue", "RuntimeNode"}

// TsMissingReservedTypes are the names types.ts also declares for a grammar with #missing, or one
// that names the type missing: the missing values (Missing) and the values of type error
// (ErrorValue). (Missing is also a reserved type name of the grammar language.)
var TsMissingReservedTypes = []string{"Missing", "ErrorValue"}

// tsTypedNameRef matches the identifiers of a TypeScript type expression of typedTS.
var tsTypedNameRef = regexp.MustCompile(`[\p{L}_$][\p{L}\p{N}_$]*`)

// qualifyTypes prefixes the names of the declarations of types.ts in a type expression with Types.,
// the namespace parser.ts imports them as.
func qualifyTypes(typ string) string {
	return tsTypedNameRef.ReplaceAllStringFunc(typ, func(id string) string {
		switch id {
		case "null", "number", "bigint", "string", "boolean", "unknown", "readonly":
			return id
		}
		return "Types." + id
	})
}

// typedTS returns the declarations of types.ts and the code that parser.ts adds for typed values. With
// stream, it also resolves the type of the elements of the stream of the start rule (streamEntry).
func (g *Generator) typedTS(start *common.Rule, stream bool) (typesSrc, parser string, err error) {
	if g.Prog.Typed == nil {
		return "", "", fmt.Errorf("typed values need the grammar's types (type checking did not run)")
	}
	t := &tsTypedGen{info: g.Prog.Typed, names: map[string]string{}, joins: map[string]bool{}, ids: map[string]int{}, missing: g.Prog.MissingType}
	t.ValueResolver = common.NewValueResolver(g.Prog, func(x types.Ty) bool { return t.joins[x.String()] })
	reservedNames := TsReservedTypes
	if t.missing {
		reservedNames = append(slices.Clip(reservedNames), TsMissingReservedTypes...)
	}
	reserved := map[string]bool{}
	for _, n := range reservedNames {
		reserved[n] = true
	}
	for name := range g.Prog.Types {
		t.all = append(t.all, name)
	}
	sort.Strings(t.all)
	for _, name := range t.all {
		if reserved[name] {
			return "", "", fmt.Errorf("the grammar type %s cannot be generated for TypeScript: types.ts declares a type with that name (reserved: %s)", name, strings.Join(reservedNames, ", "))
		}
		t.names[name] = name
		if a, ok := t.info.Aliases[name].(types.JoinTy); ok && common.NodeJoin(a) {
			t.joins[a.String()] = true
		}
	}
	// A join that no alias declares is a union of its members, too.
	for _, j := range t.AnonJoins() {
		t.joins[j.Members.String()] = true
		for _, s := range j.Spellings {
			t.joins[s] = true
		}
	}
	var b strings.Builder
	b.WriteString(tsTypedDeclarations)
	open := []string{"RuntimeNode", "StringNode", "TNode<unknown>", "TypedValue[]", "RecoveredError", "number", "bigint", "string", "boolean", "null"}
	if t.missing {
		b.WriteString(tsMissingDeclarations)
		open = append(open, "Missing")
	}
	for _, name := range t.all {
		switch spec := g.Prog.Types[name].(type) {
		case *ast.TypeDef:
			open = append(open, t.names[name])
			if n, ok := g.Prog.Check.NodeTypeOf(name); ok && spec.Struct() == nil {
				// A named node type has its name as its type: it extends TNode, not the node of its kind.
				under := "TNode<unknown>"
				if u := n.Underlying(); u.Value != types.TyTop && u.Value != types.TyAny {
					under = "TNode<" + t.tsType(u.Value) + ">"
				}
				fmt.Fprintf(&b, "/** Grammar node type %s = %s. */\nexport interface %s extends %s {\n  readonly type: %s;\n}\n\n", name, n.Underlying(), t.names[name], under, TsString(name))
				t.recipe(n)
				continue
			}
			fmt.Fprintf(&b, "/** Grammar struct %s. */\nexport interface %s extends Span {\n  readonly type: %s;\n", name, t.names[name], TsString(name))
			for _, f := range t.info.Fields[name] {
				fmt.Fprintf(&b, "  readonly %s: %s;\n", TsString(f.Name), t.tsType(f.T))
				t.recipe(f.T)
			}
			b.WriteString("}\n\n")
			t.recipe(types.NamedTy{Name: name, Kind: 's'})
		case *ast.TypeAlias:
			fmt.Fprintf(&b, "/** Grammar alias %s = %s. */\nexport type %s = %s;\n\n", name, syntaxfmt.FormatType(spec.Type), t.names[name], t.tsType(t.info.Aliases[name]))
			t.recipe(t.info.Aliases[name])
		}
	}
	fmt.Fprintf(&b, "%s%s;\n\n", tsTypedValueDeclaration, strings.Join(open, " | "))
	typesSrc = b.String()
	b.Reset()
	st := t.RuleRef(start.Name)
	repr := t.recipe(st)
	stType := qualifyTypes(t.tsType(st))
	g.startType, g.startRepr = stType, repr
	if stream {
		v, ok := common.StreamValue(g.Prog, start.Def)
		if !ok {
			return "", "", fmt.Errorf("internal error: no element type for the #stream repetition of %s", start.Name)
		}
		et := t.ResolveValue(v)
		g.streamType, g.streamRepr = qualifyTypes(t.tsType(et)), t.recipe(et)
	}
	b.WriteString("\n// --- Typed values (sxir gen -types) ---\n\n")
	// With limit controls every parse is controlled, so parseValue takes the path of parseValueWithOptions.
	astBody := fmt.Sprintf("astResult<%s>(run(G, R%d, nseen, input, unit), %d)", stType, start.ID, repr)
	if g.limits {
		astBody = "parseValueWithOptions(input, { unit: unit ?? CodePoints })"
	}
	fmt.Fprintf(&b, tsTypedAPI, stType, start.ID, repr, g.optionsType, astBody)
	// parseRuleValue converts a result by the type of its start rule, selected by the rule's ID as in Go. A name
	// written as a literal narrows to the type of its rule: each start rule has an overload of each entry point.
	var cases, overloads, optionOverloads strings.Builder
	for _, name := range g.Prog.StartRules {
		rt := t.RuleRef(name)
		ty := qualifyTypes(t.tsType(rt))
		fmt.Fprintf(&cases, "    case %d: return astResult<%s>(parsed, %d, control);\n", g.Prog.ByName[name].ID, ty, t.recipe(rt))
		names := []string{name}
		if name == g.Prog.DefaultStartRule {
			// An empty name selects the default start rule.
			names = append(names, "")
		}
		for _, n := range names {
			fmt.Fprintf(&overloads, "export function parseRuleValue(name: %s, input: string | Uint8Array, unit?: Unit): Types.TypedParseResult<%s>;\n", TsString(n), ty)
			fmt.Fprintf(&optionOverloads, "export function parseRuleValueWithOptions(name: %s, input: string | Uint8Array, options?: %s): Types.TypedParseResult<%s>;\n", TsString(n), g.optionsType, ty)
		}
	}
	ruleBody := "const r = startIn(rules, ruleNames, starts, defaultStart, name);\n  if (r instanceof Error) return { value: null, error: r };\n  return convertRule(r.id, run(G, r, nseen, input, unit));"
	if g.limits {
		ruleBody = "return parseRuleValueWithOptions(name, input, { unit: unit ?? CodePoints });"
	}
	fmt.Fprintf(&b, tsTypedRuleAPI, g.optionsType, ruleBody, overloads.String(), optionOverloads.String(), cases.String())
	b.WriteString(tsTypedLib)
	// recipe() reserves an ID before visiting child types, so recursive fields
	// reference the same representation. Write the table after all registration.
	stringID, errorID := t.recipe(types.TyStringNode), t.recipe(types.TyErr)
	missingID := -1
	if t.missing {
		missingID = t.recipe(types.TyMissing)
	}
	openID, elemsID := t.recipe(openNode), t.recipe(types.ListTy{Elem: types.TyAny})
	b.WriteString(tsTypedRecipeTable)
	for _, r := range t.recipes {
		b.WriteString("  " + r + ",\n")
	}
	b.WriteString(tsTypedNodeRecipeTable)
	fmt.Fprintf(&b, "  [%s, %d],\n  [%s, %d],\n", TsString(valueimpl.TypeString), stringID, TsString(valueimpl.TypeError), errorID)
	if missingID >= 0 {
		fmt.Fprintf(&b, "  [%s, %d],\n", TsString(valueimpl.TypeMissing), missingID)
	}
	// Open values: tuple and list nodes become TNode<unknown>, and tuple and list values arrays.
	for _, name := range []string{valueimpl.TypeTuple, valueimpl.TypeList} {
		fmt.Fprintf(&b, "  [%s, %d],\n", TsString(name), openID)
	}
	for _, name := range []string{valueimpl.TypeTupleValue, valueimpl.TypeListValue} {
		fmt.Fprintf(&b, "  [%s, %d],\n", TsString(name), elemsID)
	}
	for _, name := range t.all {
		if td, ok := g.Prog.Types[name].(*ast.TypeDef); ok {
			if n, ok := g.Prog.Check.NodeTypeOf(name); ok && td.Struct() == nil {
				fmt.Fprintf(&b, "  [%s, %d],\n", TsString(name), t.recipe(n))
				continue
			}
			fmt.Fprintf(&b, "  [%s, %d],\n", TsString(name), t.recipe(types.NamedTy{Name: name, Kind: 's'}))
		}
	}
	b.WriteString("]);\n\n")
	return typesSrc, b.String(), nil
}

type tsTypedGen struct {
	info *check.TypeInfo
	// ValueResolver selects the rules whose typed values come from their value types, as in the Go
	// generator: node[T] becomes TNode<T>, a tuple a readonly array type and () the empty
	// tuple type.
	common.ValueResolver
	names   map[string]string
	all     []string
	joins   map[string]bool
	ids     map[string]int
	recipes []string
	// missing is whether the grammar has #missing or names the type missing: its values of type
	// error are ErrorValues, a RecoveredError or a Missing, and its missing values are Missings.
	missing bool
}

func (t *tsTypedGen) tsType(x types.Ty) string {
	switch x := x.(type) {
	case types.BasicTy:
		switch x {
		case types.TyInt:
			return "number | bigint"
		case types.TyString:
			return "string"
		case types.TyBool:
			return "boolean"
		case types.TyErr:
			if t.missing {
				return "ErrorValue | null"
			}
			return "RecoveredError | null"
		}
	case common.Vrule:
		return t.tsType(t.Vres[x.Name])
	case types.TupleTy:
		elems := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			elems[i] = t.tsType(e)
		}
		return "readonly [" + strings.Join(elems, ", ") + "]"
	case types.NodeTy:
		if x.Name != "" {
			return t.names[x.Name] + " | null"
		}
		if x.Value == types.TyTop || x.Value == types.TyAny {
			return "TNode<unknown> | null" // the value of an open node is its text or its elements
		}
		// A node of one kind has that kind as its type (nodeKind), so a union narrows by it.
		switch k, _ := nodeKind(x); k {
		case valueimpl.TypeString:
			return "StringNode | null"
		case valueimpl.TypeTuple:
			return "TupleNode<" + t.tsType(x.Value) + "> | null"
		case valueimpl.TypeList:
			return "ListNode<" + t.tsType(x.Value) + "> | null"
		}
		return "TNode<" + t.tsType(x.Value) + "> | null"
	case types.NamedTy:
		if x.Kind == 'c' {
			return "Missing | null"
		}
		return t.names[x.Name] + " | null"
	case types.OptTy:
		if optKind(t.conversionKey(x.Elem)) != "" {
			return t.tsType(x.Elem) + " | null"
		}
		return t.tsType(x.Elem)
	case types.ListTy:
		return "readonly (" + t.tsType(x.Elem) + ")[] | null"
	case types.RecordTy:
		return "TNode<unknown> | null"
	case types.JoinTy:
		if t.joins[x.String()] {
			// A join holds a recovered value, and in a grammar with #missing a missing one, whatever its
			// members.
			parts := []string{"RecoveredError", "null"}
			if t.missing {
				parts = []string{"RecoveredError", "Missing", "null"}
			}
			for _, a := range x.Alts {
				if a != types.TyErr && a != types.TyMissing {
					parts = append(parts, strings.TrimSuffix(t.tsType(a), " | null"))
				}
			}
			return strings.Join(parts, " | ")
		}
		if _, ok := t.commonRecipe(x); ok {
			return t.tsType(x.Alts[0])
		}
	}
	return "TypedValue"
}

// optKind returns how an option of a type whose recipe key is key converts: "int", "string" or
// "bool" for a nullable scalar, "tuple" for a nullable tuple, and "" where the recipe of the type
// itself converts an absent value to null. It decides by the recipe, not by the form the element is
// written in, so that a rule or a join that stands for a scalar or a tuple decides as that type, and
// an absent value is null.
func optKind(key string) string {
	switch {
	case key == "int" || key == "string" || key == "bool":
		return key
	case strings.HasPrefix(key, "tuple("):
		return "tuple"
	}
	return ""
}

// commonRecipe reports whether the arguments of a union share a recipe (and with it a TypeScript
// type), and returns its key.
func (t *tsTypedGen) commonRecipe(x types.JoinTy) (string, bool) {
	common := ""
	for _, a := range x.Alts {
		key := t.conversionKey(a)
		if common != "" && key != common {
			return "", false
		}
		common = key
	}
	return common, common != "" && common != "any"
}

// conversionKey identifies a canonical recipe without expanding named fields.
// Optional node recipes and broad any/node/terminal recipes can share outputs;
// list element recipes retain distinctions such as optional scalars and joins.
func (t *tsTypedGen) conversionKey(x types.Ty) string {
	switch x := x.(type) {
	case types.BasicTy:
		if x == types.TyInt || x == types.TyString || x == types.TyBool {
			return x.String()
		}
		if x == types.TyErr {
			return "named:Error"
		}
	case common.Vrule:
		return t.conversionKey(t.Vres[x.Name])
	case types.TupleTy:
		keys := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			keys[i] = t.conversionKey(e)
		}
		return "tuple(" + strings.Join(keys, ",") + ")"
	case types.NodeTy:
		if x.Name != "" {
			return "tnode:named:" + x.Name
		}
		return "tnode:" + tnodeMode(x) + ":" + t.conversionKey(x.Value)
	case types.NamedTy:
		if x.Kind == 'c' {
			return "named:Missing"
		}
		return "named:" + x.Name
	case types.RecordTy:
		return t.conversionKey(openNode)
	case types.OptTy:
		key := t.conversionKey(x.Elem)
		switch k := optKind(key); k {
		case "":
			return key
		case "tuple":
			return "opt:" + key
		default:
			return "opt" + k
		}
	case types.ListTy:
		return "list:" + t.conversionKey(x.Elem)
	case types.JoinTy:
		if t.joins[x.String()] {
			return "union:" + x.String()
		}
		if key, ok := t.commonRecipe(x); ok {
			return key
		}
	}
	return "any"
}

// nodeKind returns the kind of the nodes of the unnamed node type x (types.NodeKinds), and whether it
// has exactly one. Such a node's typed value has its kind as its type, also when the node is one of
// a named node type assignable to x: the type of a value is the type it converts to.
func nodeKind(x types.NodeTy) (string, bool) {
	if x.Name != "" {
		return "", false
	}
	ks := types.NodeKinds(x)
	if len(ks) != 1 {
		return "", false
	}
	return ks[0], true
}

// openNode is the type of the nodes of CST types: their values are their text or their elements.
var openNode = types.NodeTy{Value: types.TyTop}

// tnodeMode returns where the value of a node of type x comes from: its text ("text"), the node
// itself for a tuple or list value ("self"), or the text or elements of the node ("value").
func tnodeMode(x types.NodeTy) string {
	switch x.Value.(type) {
	case types.TupleTy, types.ListTy:
		return "self"
	}
	if x.Value == types.TyString {
		return "text"
	}
	return "value"
}

// recipe follows the existing Go Node-converter recipes, including anyValue
// for mixed/open or nondeclared joins and CST retention in declared joins.
func (t *tsTypedGen) recipe(x types.Ty) int {
	kind, key := "any", "any"
	switch a := x.(type) {
	case types.BasicTy:
		switch a {
		case types.TyInt, types.TyString, types.TyBool:
			kind, key = string(a), string(a)
		case types.TyErr:
			kind, key = "error", t.conversionKey(a)
		}
	case common.Vrule:
		return t.recipe(t.Vres[a.Name])
	case types.TupleTy:
		kind, key = "tuple", t.conversionKey(a)
	case types.NodeTy:
		kind, key = "tnode", t.conversionKey(a)
		if a.Name != "" {
			// The type's definition gives its value, also inside the type itself.
			x, _ = t.Prog.Check.NodeTypeOf(a.Name)
		}
	case types.NamedTy:
		if a.Kind == 'c' {
			kind, key = "missing", t.conversionKey(a)
			break
		}
		kind, key = "struct", "named:"+a.Name
	case types.OptTy:
		if k := optKind(t.conversionKey(a.Elem)); k != "" && k != "tuple" {
			kind, key = "opt"+k, "opt"+k
		} else if k == "tuple" {
			kind, key = "opt", t.conversionKey(a)
		} else {
			return t.recipe(a.Elem)
		}
	case types.ListTy:
		kind, key = "list", "list:"+t.conversionKey(a.Elem)
	case types.RecordTy:
		return t.recipe(openNode)
	case types.JoinTy:
		if t.joins[a.String()] {
			kind, key = "union", "union:"+a.String()
		} else if _, ok := t.commonRecipe(a); ok {
			return t.recipe(a.Alts[0])
		}
	}
	if id, ok := t.ids[key]; ok {
		return id
	}
	id := len(t.recipes)
	t.ids[key] = id
	t.recipes = append(t.recipes, "")
	r := "{ kind: " + TsString(kind)
	switch a := x.(type) {
	case types.BasicTy:
		if kind == "error" {
			r += ", name: " + TsString(valueimpl.TypeError)
			if t.missing {
				// A value of type error may be a missing value too.
				r += ", missing: " + strconv.Itoa(t.recipe(types.TyMissing))
			}
		}
	case types.NamedTy:
		if a.Kind == 'c' {
			r += ", name: " + TsString(valueimpl.TypeMissing)
			break
		}
		var fields []string
		for _, f := range t.info.Fields[a.Name] {
			fields = append(fields, fmt.Sprintf("{ name: %s, repr: %d }", TsString(f.Name), t.recipe(f.T)))
		}
		r += ", name: " + TsString(a.Name) + ", fields: [" + strings.Join(fields, ", ") + "]"
	case types.ListTy:
		r += ", elem: " + strconv.Itoa(t.recipe(a.Elem))
	case types.OptTy:
		r += ", elem: " + strconv.Itoa(t.recipe(a.Elem))
	case types.NodeTy:
		if a.Name != "" {
			r += ", name: " + TsString(a.Name)
		}
		if k, ok := nodeKind(a); ok {
			r += ", type: " + TsString(k)
		}
		r += ", mode: " + TsString(tnodeMode(a)) + ", elem: " + strconv.Itoa(t.recipe(a.Value))
	case types.TupleTy:
		elems := make([]string, len(a.Elems))
		for i, e := range a.Elems {
			elems[i] = strconv.Itoa(t.recipe(e))
		}
		r += ", elems: [" + strings.Join(elems, ", ") + "]"
	case types.JoinTy:
		if kind == "union" {
			// A value converts to the member its node type names, or to the unnamed node member of
			// its kind, also a node of a named node type that is no member; node[_] takes every
			// other node.
			members := []string{fmt.Sprintf("[%s, %d]", TsString(valueimpl.TypeError), t.recipe(types.TyErr))}
			if t.missing {
				members = append(members, fmt.Sprintf("[%s, %d]", TsString(valueimpl.TypeMissing), t.recipe(types.TyMissing)))
			}
			named := map[string]bool{}
			for _, m := range a.Alts {
				switch m := m.(type) {
				case types.NamedTy:
					if m.Kind == 'c' {
						continue // a member of every join of the grammar
					}
					members = append(members, fmt.Sprintf("[%s, %d]", TsString(m.Name), t.recipe(m)))
				case types.NodeTy:
					if m.Name != "" {
						named[m.Name] = true
						members = append(members, fmt.Sprintf("[%s, %d]", TsString(m.Name), t.recipe(m)))
					}
				}
			}
			kinds, open := common.JoinNodeMembers(a)
			if !open {
				byKind := map[string]types.NodeTy{}
				for _, k := range kinds {
					byKind[k.Kind] = k.Member
				}
				for _, name := range t.all {
					n, ok := t.Prog.Check.NodeTypeOf(name)
					if !ok || named[name] {
						continue
					}
					if td, ok := t.Prog.Types[name].(*ast.TypeDef); !ok || td.Struct() != nil {
						continue
					}
					if k, ok := common.NamedNodeKind(n.Underlying()); ok {
						if m, ok := byKind[k]; ok {
							members = append(members, fmt.Sprintf("[%s, %d]", TsString(name), t.recipe(m)))
						}
					}
				}
				for _, k := range kinds {
					members = append(members, fmt.Sprintf("[%s, %d]", TsString(k.Kind), t.recipe(k.Member)))
				}
			}
			r += ", members: new Map([" + strings.Join(members, ", ") + "])"
			if open {
				r += ", fallback: " + strconv.Itoa(t.recipe(openNode))
			}
		}
	}
	t.recipes[id] = r + " }"
	return id
}

// Reuse declaration templates for emission and reservation so adding a public
// or private typed helper automatically reserves its actual binding name.
const tsTypedDeclarations = `export interface Span { readonly start: number; readonly end: number }
/** A node with the value T: a string node has its text, other nodes their elements. */
export interface TNode<T> extends Span { readonly type: string; readonly value: T }
export interface StringNode extends TNode<string> { readonly type: "string" }
/** A tuple node: its value is its elements. */
export interface TupleNode<T> extends TNode<T> { readonly type: "tuple" }
/** A list node: its value is its elements. */
export interface ListNode<T> extends TNode<T> { readonly type: "list" }
export interface RecoveredError extends Span {
  readonly type: "error";
  readonly message: string;
}
export interface TypedParseResult<T> { readonly value: T | null; readonly error: Error | null }

`

// tsMissingDeclarations are the declarations types.ts adds for a grammar with #missing.
const tsMissingDeclarations = `/** A literal that #missing inserted, at an empty range: the literal and the message of its syntax error. */
export interface Missing extends Span {
  readonly type: "missing";
  readonly expected: string;
  readonly message: string;
}
/** A value of type error: input skipped by error recovery or a literal that #missing inserted. */
export type ErrorValue = RecoveredError | Missing;

`

const tsTypedAPI = `/** Parses the configured start rule once and converts its completed Node result.
 * Recovery preserves SyntaxErrors; absent values and incompatible recovered fields are null.
 * Positions and integer/text representations are those of parse. Conversion is invocation-local.
 */
export function parseValue(input: string | Uint8Array, unit?: Unit): Types.TypedParseResult<%[1]s> {
  return %[5]s;
}

/** Typed configured-start parsing with options read and validated once. */
export function parseValueWithOptions(input: string | Uint8Array, options?: %[4]s): Types.TypedParseResult<%[1]s> {
  const o = resolveOptions(G, options, input);
  if (o instanceof Error) return { value: null, error: o };
  return withObservedOptions<Types.TypedParseResult<%[1]s>>(o,
    control => astResult<%[1]s>(run(G, R%[2]d, nseen, input, o.unit, o.maxDepth, control, o), %[3]d, control),
    error => ({ value: null, error }));
}

`

// tsTypedRuleAPI takes the options type, the body of parseRuleValue, the per-rule overloads of parseRuleValue and of
// parseRuleValueWithOptions, and the cases of convertRule. The last overload of each function takes any name.
const tsTypedRuleAPI = `/** Parses the whole input with the start rule name, a rule marked @start; "" selects the default start rule.
 * A name written as a literal has the value type of its rule. Any other name has the value type unknown, as Go
 * returns any, so narrow it by the type of the rule. Unknown names and rules that are not start rules are errors
 * without a value.
 */
%[3]sexport function parseRuleValue(name: string, input: string | Uint8Array, unit?: Unit): Types.TypedParseResult<unknown>;
export function parseRuleValue(name: string, input: string | Uint8Array, unit?: Unit): Types.TypedParseResult<unknown> {
  %[2]s
}

/** Typed parsing of a start rule with options read and validated once; the name types as in parseRuleValue. */
%[4]sexport function parseRuleValueWithOptions(name: string, input: string | Uint8Array, options?: %[1]s): Types.TypedParseResult<unknown>;
export function parseRuleValueWithOptions(name: string, input: string | Uint8Array, options?: %[1]s): Types.TypedParseResult<unknown> {
  const o = resolveOptions(G, options, input);
  if (o instanceof Error) return { value: null, error: o };
  const r = startIn(rules, ruleNames, starts, defaultStart, name);
  if (r instanceof Error) return { value: null, error: r };
  return withObservedOptions<Types.TypedParseResult<unknown>>(o,
    control => convertRule(r.id, run(G, r, nseen, input, o.unit, o.maxDepth, control, o), control),
    error => ({ value: null, error }));
}

// convertRule converts the result of the start rule with the ID id by the type of that rule.
function convertRule(id: number, parsed: ParseResult, control: BudgetControl | null = null): Types.TypedParseResult<unknown> {
  switch (id) {
%[5]s    default: throw new Error("rule " + id + " is not a start rule");
  }
}

`

const tsTypedValueDeclaration = "export type TypedValue = "

const tsTypedRecipeTable = "\nconst valueRecipes: readonly ValueRecipe[] = [\n"
const tsTypedNodeRecipeTable = "];\nconst astNodeRecipes: ReadonlyMap<string, number> = new Map([\n"

const tsTypedLib = `interface ValueRecipe {
  readonly kind: string;
  readonly name?: string;
  readonly fields?: readonly { readonly name: string; readonly repr: number }[];
  readonly elem?: number;
  readonly elems?: readonly number[];
  readonly mode?: string;
  readonly type?: string;
  readonly members?: ReadonlyMap<string, number>;
  readonly fallback?: number;
  readonly missing?: number;
}
type ValueObject = { [name: string]: unknown };
interface ValuePending { readonly node: Node; readonly repr: number; readonly out: ValueObject | unknown[] }

// astKid returns the value of the i-th element of a tuple or list node.
function astKid(n: Node, i: number): Value {
  const kid = n.children[i];
  return kid === undefined ? null : unboxNode(kid);
}

// astResult converts the result of a parse. A parse without a result, failed or nil, has a null value,
// whatever the type of the rule: only the value of a result converts, with its scalars defaulted.
function astResult<T>(r: ParseResult, repr: number, control: BudgetControl | null = null): Types.TypedParseResult<T> {
  if (r.node === null) return { value: null, error: r.error };
  return { value: astConvert(unboxNode(r.node), repr, control) as T | null, error: r.error };
}

// Cache canonical Node/representation pairs, including null, in this call only.
// Register shells before filling edges: shared fold/LR DAGs stay shared and deep
// output graphs do not consume the JavaScript call stack. Source Nodes stay intact.
// The conversion counts no compute units: converting the values of a parse to a representation
// is no event of the parse. It polls the signal and the memory limit every 256 steps.
function astConvert(value: Value, repr: number, control: BudgetControl | null = null): unknown {
  const cache = new Map<Node, Map<number, unknown>>();
  const pending: ValuePending[] = [];
  let steps = 0;
  const step = (): void => { if (control !== null && ++steps % 256 === 0) control.check(); };
  const get = (v: Value, id: number): unknown => {
    for (;;) {
      step();
      const recipe = valueRecipes[id]!;
      switch (recipe.kind) {
        case "int": return isInt(v) ? v : 0;
        case "string": return typeof v === "string" ? v : "";
        case "bool": return typeof v === "boolean" ? v : false;
        case "optint": return isInt(v) ? v : null;
        case "optstring": return typeof v === "string" ? v : null;
        case "optbool": return typeof v === "boolean" ? v : null;
        case "any": {
          if (!(v instanceof Node)) return v;
          if (v.type === typeBox) {
            v = v.box;
            continue;
          }
          const next = astNodeRecipes.get(v.type);
          if (next === undefined) return v;
          id = next;
          continue;
        }
        case "opt":
          if (v === null) return null;
          id = recipe.elem!;
          continue;
        case "error":
          // A value of type error in a grammar with #missing is also a missing value.
          if (recipe.missing !== undefined && v instanceof Node && v.type === "missing") {
            id = recipe.missing;
            continue;
          }
          break;
        case "union": {
          if (!(v instanceof Node)) return null;
          const next = recipe.members!.get(v.type);
          if (next === undefined) {
            if (recipe.fallback === undefined) return null;
            id = recipe.fallback;
            continue;
          }
          id = next;
          continue;
        }
      }
      if (!(v instanceof Node)) {
        if (recipe.kind !== "tuple") return null;
        step();
        return recipe.elems!.map(e => get(null, e));
      }
      if (recipe.kind === "tnode" && (v.type === "error" || v.type === "missing" || v.type === typeTupleValue || v.type === typeListValue)) return null;
      let reps = cache.get(v);
      if (reps === undefined) {
        reps = new Map<number, unknown>();
        cache.set(v, reps);
      }
      if (reps.has(id)) return reps.get(id);
      let out: ValueObject | unknown[] | null = null;
      if (recipe.kind === "list") {
        step();
        out = new Array<unknown>(v.children.length);
      } else if (recipe.kind === "tuple") {
        step();
        out = new Array<unknown>(recipe.elems!.length);
      } else if (recipe.kind === "tnode") {
        // A named node type takes only its own nodes.
        out = recipe.name === undefined || v.type === recipe.name ? { type: recipe.type ?? v.type, start: v.start, end: v.end, value: null } : null;
      } else if (v.type === recipe.name) {
        const obj: ValueObject = { type: v.type, start: v.start, end: v.end };
        if (recipe.kind === "missing") {
          const expected = v.field("expected");
          obj["expected"] = typeof expected === "string" ? expected : "";
        }
        if (recipe.kind === "error" || recipe.kind === "missing") {
          const msg = v.field("message");
          obj["message"] = typeof msg === "string" ? msg : "";
        }
        out = obj;
      }
      reps.set(id, out);
      if (out !== null && (recipe.kind === "list" || recipe.kind === "struct" || recipe.kind === "tuple" || recipe.kind === "tnode")) {
        pending.push({ node: v, repr: id, out });
      }
      return out;
    }
  };
  const out = get(value, repr);
  while (pending.length !== 0) {
    step();
    const work = pending.pop()!;
    const recipe = valueRecipes[work.repr]!;
    if (recipe.kind === "list") {
      const array = work.out as unknown[];
      for (let i = 0; i < work.node.children.length; i++) {
        step();
        array[i] = get(astKid(work.node, i), recipe.elem!);
      }
    } else if (recipe.kind === "tuple") {
      const array = work.out as unknown[];
      for (let i = 0; i < recipe.elems!.length; i++) {
        step();
        array[i] = get(astKid(work.node, i), recipe.elems![i]!);
      }
    } else if (recipe.kind === "tnode") {
      const n = work.node, obj = work.out as ValueObject;
      if (recipe.mode === "text") obj["value"] = n.text;
      else if (recipe.mode === "self") obj["value"] = get(n, recipe.elem!);
      else if (n.terminal) obj["value"] = get(n.text, recipe.elem!);
      else if (valueRecipes[recipe.elem!]!.kind !== "any") obj["value"] = get(null, recipe.elem!);
      else {
        // The elements of an open value, converted like any other open value.
        step();
        obj["value"] = n.children.map((_, i) => get(astKid(n, i), recipe.elem!));
      }
    } else {
      const obj = work.out as ValueObject;
      for (const f of recipe.fields!) {
        step();
        obj[f.name] = get(work.node.field(f.name), f.repr);
      }
    }
  }
  return out;
}
`
