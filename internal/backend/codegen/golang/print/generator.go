package print

import (
	"cmp"
	"fmt"
	"go/format"
	"maps"
	"math"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	rprogram "syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// defaultPackageName returns the package name of the generated code for a module path: its last
// element, without a trailing major version element (example.com/m/v2 gives m), as Go tooling
// names a package.
func defaultPackageName(module string) string {
	base := path.Base(module)
	if dir := path.Dir(module); dir != "." && dir != "/" && len(base) >= 2 && base[0] == 'v' && base[1] != '0' && strings.Trim(base[1:], "0123456789") == "" {
		if base[1] != '1' || len(base) > 2 {
			return path.Base(dir)
		}
	}
	return base
}

// Hooks let a test observe or change the decisions of one generation, which reach the printer
// through its target (driver.Target.GoHooks). They are for tests: production leaves them nil, which
// changes nothing, like the zero value, and no package other than a test sets them. A test reads the
// units of the compilation, such as the tables it prints, through the observers of the compilation
// (driver.Options.Observers), not through hooks.
type Hooks struct {
	// Generator receives the generator of print-go after it numbered the expectation table, before
	// it prints the Node and recognizer tables.
	Generator func(*Generator)
	// Features replaces the features that a parser generated with FeatureWitness records (a negative
	// control of the analysis).
	Features func(common.FeatureSet) common.FeatureSet
	// Reach receives the reachable rules of the plan of the program before any table is printed; a
	// test removes a root from it to show that the output then fails.
	Reach func(*liveRules)
	// Numbering replaces the expectation table numbered from the lowered tables before any table is
	// printed, so that a test has the printing of the Node and recognizer tables number it instead.
	Numbering func(descs []string) []string
	// Printed receives the expectation table after the Node and recognizer tables are printed, before
	// the generation checks that printing them recorded no expectation that the numbering lacks.
	Printed func(descs []string)
	// LeanRoute is called with the rule table and the rule for each rule that takes the lean route
	// in rules, in a Node or recognizer table, so that a test checks the route that is chosen, not
	// the text of the generated code.
	LeanRoute func(table string, r *common.Rule)
	// FramedRoute is called like LeanRoute for each rule whose framed body is printed from Exec IR
	// (nodeFrameBody), in a Node or recognizer table.
	FramedRoute func(table string, r *common.Rule)
	// OperatorsRoute is called like LeanRoute for each line of a Node or recognizer table that is
	// printed from Exec IR for an Operators Rule.
	OperatorsRoute func(table string, r *common.Rule)
	// StreamRoute is called like LeanRoute for each ParseStream start rule whose stream body is
	// printed from Exec IR (streamBodyFunc).
	StreamRoute func(table string, r *common.Rule)
	// RefuseRecRule makes the plan of the Node and recognizer tables refuse the flat functions of
	// the rules it selects (flowplan.Unit), so that the framed body that takes their place is
	// exercised. No manifest grammar reaches that fallback.
	RefuseRecRule func(*common.Rule) bool
	// VarsMutant makes every variable of a printed function unread: the generated code must
	// then fail to compile, which shows that the tests of generated code catch a wrong decision.
	VarsMutant bool
	// VarsMutantUses ignores the reads of the values of assignments that stay, which leaves
	// variables that only they read undeclared: the generated code must then fail to compile. In the
	// functions printed from Exec IR (execRoute) it leaves undeclared the locals that no statement
	// other than an assignment reads.
	VarsMutantUses bool
	// PrimTarget replaces the Go implementation of the primitive operations of grammar code at the Exec
	// stage (primPrinter.execTarget), given the printer's own and the address of the statements that the
	// expression printed last needs before it (those of the action expression of an Eval), so that the conformance
	// test of the Flow operations wraps each call in a probe of its own; tables are the tables of the
	// module, which the immediates of the calls index.
	PrimTarget func(t *Target, tables *prim.Tables, pre *string) *Target
}

// A Parser is a Go parser of a program as the Go backend decided it before its tables were built:
// the options as the program completes them, the program, the recognizer and the start rule.
type Parser struct {
	Opts common.GenOptions
	Prog *common.Program
	Rec  *common.Program // the recognizer, or nil
	// Ins is the insertion program of a grammar that uses #missing, whose tables the parses with
	// missing-token insertion enabled run, and InsRec its recognizer with Recognize; nil otherwise.
	Ins, InsRec *common.Program
	Start       *common.Rule
}

// Params are the values of the parameters of the step print-go that the Go printer reads: those of
// both printers and the Go printer's own.
type Params struct {
	common.Params
	// TypedObjectSlabs and TypedListSlabs allocate typed objects and lists in slabs, and
	// TypedOutputBootstrap sizes the first slab of the typed output from the input.
	TypedObjectSlabs, TypedListSlabs, TypedOutputBootstrap bool
	// Control, if not nil, is the control of the printing step (cancel.New of the context of the step), which the
	// printers of the grammar's code poll (Target.Control).
	Control *cancel.Control
}

// NoNodeRules reports whether the Node table of a parser with the options o holds no rule bodies: a
// typed-only or recognize-only parser that does not keep its Node rules, whose rows the typed table
// or the recognizer shares.
func NoNodeRules(o common.GenOptions) bool {
	return (o.TypedOnly || o.RecognizeOnly) && !o.KeepNodes
}

// newGenerator returns a generator of the parser p, printing the Node table.
func newGenerator(p *Parser) *Generator {
	o := p.Opts
	nodes := NoNodeRules(o)
	g := &Generator{typedOnly: nodes, keepTableRules: nodes, Document: o.Document, Ordinary: !o.Document, Prog: p.Prog, Table: "rules"}
	g.Logical = o.LogicalUnits()
	if o.Stream {
		g.streamStart = p.Start
	}
	return g
}

// TypedEntries returns what the typed entry points of the parser p enter, decided from the types of
// its start rules before anything is printed: the typed rules they enter, whether the parser needs
// the typed table, and whether it needs the Node rules, which an entry point converts the result of.
func TypedEntries(p *Parser) (roots []plandata.Entry, typed, nodes bool, err error) {
	g := newGenerator(p)
	e, err := g.typedEntries(p.Start, p.Opts.ConvertTypes)
	if err != nil {
		return nil, false, false, err
	}
	return g.typedRoots, e.needTyped, e.needNodes, nil
}

// Print returns the files of the Go parser p, printed from its tables t at the Exec stage with the
// parameters params and the hooks h of a test.
func Print(p *Parser, params Params, t *execdata.Tables, h Hooks) ([]common.GeneratedFile, error) {
	files, _, err := printTables(p, params, t, h)
	return files, err
}

// printTables prints the files of the Go parser p from its tables t at the Exec stage, with the
// parameters params and the hooks h of a test, and returns them with the generator that printed them.
func printTables(p *Parser, params Params, t *execdata.Tables, h Hooks) (files []common.GeneratedFile, genGo *Generator, err error) {
	opts, prog, rec, start := p.Opts, p.Prog, p.Rec, p.Start
	insertion := p.insertionTables()
	info, err := loadLib(opts)
	if err != nil {
		return nil, nil, err
	}
	genGo = newGenerator(p)
	genGo.Params, genGo.params, genGo.hooks, genGo.tables = params.Params, params, h, t
	genGo.ins = p.Ins
	genGo.Desc(core.FixedDescs[0]) // the fixed expectations come first
	var entries typedEntries
	if opts.Types {
		if entries, err = genGo.typedEntries(start, opts.ConvertTypes); err != nil {
			return nil, nil, err
		}
	}
	genGo.program = t.Plan
	if !opts.KeepUnreachable {
		genGo.live = &liveRules{Roots: t.Plan.Roots, Reach: t.Plan.Reach, plan: t.Plan}
	}
	if h.Reach != nil && genGo.live != nil {
		h.Reach(genGo.live)
	}
	// The expectation table of the Node and recognizer tables is numbered from them before they are
	// printed; the typed code and the traced copies add their own after it as they are printed.
	descs := t.Descs
	if h.Numbering != nil {
		descs = h.Numbering(descs)
	}
	if len(genGo.Descs) != len(core.FixedDescs) {
		return nil, nil, fmt.Errorf("internal error: %d expectations are recorded before the expectation table is numbered", len(genGo.Descs))
	}
	for _, d := range descs {
		genGo.Desc(d)
	}
	numbered := len(genGo.Descs)
	if h.Generator != nil {
		h.Generator(genGo)
	}
	if err := genGo.useTable(); err != nil {
		return nil, nil, err
	}
	genGo.rules()
	genGo.Exec = nil
	if genGo.streamErr != nil {
		return nil, nil, genGo.streamErr
	}
	if genGo.tableErr != nil {
		return nil, nil, genGo.tableErr
	}
	if genGo.PrintErr != nil {
		return nil, nil, genGo.PrintErr
	}
	if rec != nil {
		genGo.Prog, genGo.Table = rec, "recRules"
		if err := genGo.useTable(); err != nil {
			return nil, nil, err
		}
		genGo.rules()
		genGo.Exec = nil
		genGo.Prog, genGo.Table = prog, "rules"
		if genGo.tableErr != nil {
			return nil, nil, genGo.tableErr
		}
		if genGo.PrintErr != nil {
			return nil, nil, genGo.PrintErr
		}
	}
	for _, t := range insertion {
		// The tables of the insertion program, which the parses with missing-token insertion enabled
		// run (SetInsertion).
		genGo.Prog, genGo.Table = t.prog, t.table
		if err := genGo.useTable(); err != nil {
			return nil, nil, err
		}
		genGo.rules()
		genGo.Exec = nil
		genGo.Prog, genGo.Table = prog, "rules"
		if err := cmp.Or(genGo.tableErr, genGo.PrintErr); err != nil {
			return nil, nil, err
		}
	}
	if h.Printed != nil {
		h.Printed(append([]string(nil), genGo.Descs...))
	}
	if len(genGo.Descs) != numbered {
		return nil, nil, fmt.Errorf("internal error: the Node, recognizer and insertion tables record the expectation %q, which the numbering of their lowered tables does not", genGo.Descs[numbered])
	}
	fmt.Fprintf(&genGo.Init, "\tsxirGrammar.Starts = %s\n", goStrings(prog.StartRules))
	fmt.Fprintf(&genGo.Init, "\tsxirGrammar.DefaultStart = %q\n", prog.DefaultStartRule)
	features, err := common.ParserFeatures(p.ruleTables(), opts, genGo.Params, t.Plan)
	if err != nil {
		return nil, nil, err
	}
	if opts.FeatureWitness {
		witnessed := features
		if h.Features != nil {
			witnessed = h.Features(witnessed)
		}
		fmt.Fprintf(&genGo.Init, "\tsxirGrammar.WitnessFeatures(%d) // %s\n", witnessed, witnessed)
	}
	if !opts.TypedOnly {
		genGo.writeDescs(opts)
	}
	if opts.Observability {
		genGo.Init.WriteString("\tsxirGrammar.Observable = true\n")
	}
	if opts.NoLimitControls {
		genGo.Init.WriteString("\tsxirGrammar.NoBudgetControls = true\n")
	}
	if prog.Run.RecordsSourceInfo {
		genGo.Init.WriteString("\tsxirGrammar.SourceInfo = true\n")
	}
	if prog.Run.HasMissing {
		genGo.Init.WriteString("\tsxirGrammar.HasMissing = true\n")
	}
	if t := prog.Vars; t != nil {
		var names, vals []string
		for _, d := range t.List {
			names = append(names, d.Name)
			vals = append(vals, fmt.Sprintf("%#v", d.Init))
		}
		fmt.Fprintf(&genGo.Init, "\tsxirGrammar.SetVars(%s, []any{%s})\n", goStrings(names), strings.Join(vals, ", "))
	}
	writeRunKeys(&genGo.Init, "sxirGrammar", prog)

	if rec != nil && !opts.TypedOnly {
		genGo.Init.WriteString("\tsxirRecGrammar.Share(sxirGrammar)\n")
		writeRunKeys(&genGo.Init, "sxirRecGrammar", rec)
	}
	for _, t := range insertion {
		fmt.Fprintf(&genGo.Init, "\t%s.Share(sxirGrammar)\n", t.grammar)
		writeRunKeys(&genGo.Init, t.grammar, t.prog)
		fmt.Fprintf(&genGo.Init, "\tSetInsertion(%s, %s)\n", t.of, t.table)
		if start != nil && opts.Stream && t.table == "insRules" {
			genGo.Init.WriteString("\tSetInsertion([]*Rule{streamRule}, []*Rule{insStreamRule})\n")
		}
	}

	var out strings.Builder
	if opts.Observability && opts.Document {
		out.WriteString("// ErrDocumentBusy is returned by a Parse, ParseValue or Edit of a Document that\n// is already parsing, such as one made by a trace callback of that parse.\n// Schedule the work after the parse returns, or use another Document.\nvar ErrDocumentBusy = DocumentBusy\n\n")
	}
	out.WriteString("// --- Generated code ---\n\n")
	if !opts.TypedOnly && !opts.RecognizeOnly {
		fmt.Fprintf(&out, "// Parse parses the whole input with the rule %s. Options select position units, depth, cancellation and compute limits.\n", start.Name)
		out.WriteString("// If the parse recovered from errors with #recover, it returns the node together with SyntaxErrors.\n")
		fmt.Fprintf(&out, "func Parse(input string, opts ...ParseOption) (*Node, error) { return RunParse(rules[%d], input, opts) }\n\n", start.ID)
	}
	if opts.Stream {
		fmt.Fprintf(&out, "// ParseStream parses the input read from r with the rule %s, passing each committed element of\n", start.Name)
		out.WriteString("// its #stream repetition to emit (nil if the element has no value) without retaining it. Committed\n// elements are final even if the parse fails later. A non-EOF error from r or an error returned by\n// emit ends the parse and is returned unchanged. A nil emit collects the repetition instead.\n")
		out.WriteString("func ParseStream(r io.Reader, emit func(*Node) error, opts ...ParseOption) error {\n\treturn RunParseStream(streamRule, r, emit, opts)\n}\n\n")
		out.WriteString(`// Iterate returns an iterator over the elements that ParseStream would pass to emit, parsing only as
// far as each call to Next needs. Options are validated before it returns; r is first read by Next.
// The parser's lookahead and buffering may read ahead of the returned elements.
func Iterate(r io.Reader, opts ...ParseOption) (*StreamIterator, error) {
	return IterateNodes(sxirGrammar, opts, func(emit func(*Node) error) error { return RunParseStream(streamRule, r, emit, opts) })
}

`)
	}
	if opts.Document {
		fmt.Fprintf(&out, "// NewDocument creates an incremental Node document for the start rule %s.\n", start.Name)
		if opts.Types {
			fmt.Fprintf(&out, "func NewDocument(text string, opts ...ParseOption) (*Document, error) {\n\td, err := MakeDocument(rules[%d], text, opts)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn &Document{d}, nil\n}\n\n", start.ID)
		} else {
			fmt.Fprintf(&out, "func NewDocument(text string, opts ...ParseOption) (sxirruntime.Document, error) {\n\td, err := MakeDocument(rules[%d], text, opts)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn d, nil\n}\n\n", start.ID)
		}
	}
	if rec != nil {
		fmt.Fprintf(&out, "// Recognize checks that the whole input matches the rule %s without building a tree, and returns the\n", start.Name)
		out.WriteString("// syntax errors Parse would return (SyntaxErrors for errors recovered with #recover). Actions needed by\n// predicates are evaluated and can report runtime errors; other actions are skipped.\n")
		fmt.Fprintf(&out, "func Recognize(input string, opts ...ParseOption) error {\n\t_, err := RunParse(recRules[%d], input, opts)\n\treturn err\n}\n\n", rec.ByName[start.Name].ID)
		fmt.Fprintf(&out, "// recRules is the rule table of Recognize, and sxirRecGrammar its grammar.\nvar recRules []*Rule\nvar sxirRecGrammar = NewGrammar(%d)\n\n", common.GeneratedMaxDepth(opts))
	}
	fmt.Fprintf(&out, "// rules is the rule table, and sxirGrammar its grammar.\nvar rules []*Rule\nvar sxirGrammar = NewGrammar(%d)\n\n", common.GeneratedMaxDepth(opts))
	for _, t := range insertion {
		fmt.Fprintf(&out, "// %s is the rule table of the insertion program that the parses of %s with missing tokens\n// enabled run, and %s its grammar.\nvar %[1]s []*Rule\nvar %[3]s = NewGrammar(%[4]d)\n\n", t.table, t.of, t.grammar, common.GeneratedMaxDepth(opts))
	}
	if !opts.TypedOnly && !opts.RecognizeOnly {
		out.WriteString(`// ParseRule parses the whole input with the start rule name, a rule marked @start; an empty
// name selects the grammar's default start rule.
func ParseRule(name, input string, opts ...ParseOption) (*Node, error) {
	o, err := ResolveOptions(sxirGrammar, opts)
	if err != nil {
		return nil, err
	}
	r, err := sxirGrammar.StartRule(name)
	if err != nil {
		return nil, err
	}
	return RunParseResolved(r, input, o)
}

`)
	}
	var typesBody string
	if opts.Types {
		typed, err := genGo.typedGo(start, entries)
		if err != nil {
			return nil, nil, err
		}
		if opts.TypedOnly {
			// The expectations of the typed bodies are known only now.
			genGo.writeDescs(opts)
		}
		out.WriteString("\n// --- Typed values ---\n\n")
		if opts.Document {
			out.WriteString(typedDocumentDecl)
		}
		out.WriteString(typed.parser)
		out.WriteString("\n")
		typesBody = typesPreamble(prog.MissingType) + typed.types
	}
	if rec != nil && opts.TypedOnly {
		// The recognizer grammar shares sxirGrammar by copying, so Share follows the expectation table,
		// which a typed-only parser writes only after its typed code.
		genGo.Init.WriteString("\tsxirRecGrammar.Share(sxirGrammar)\n")
	}
	// The bodies above are those of a parser without observability; an observed parse runs the
	// traced copies of the tables, generated last.
	var tracedInit string
	if opts.Observability {
		if tracedInit, err = genGo.tracedTables(prog, rec, insertion); err != nil {
			return nil, nil, err
		}
	}
	if err := genGo.finishReach(); err != nil {
		return nil, nil, err
	}
	out.WriteString(genGo.Vars.String())
	out.WriteString("\nfunc init() {\n")
	out.WriteString(genGo.Init.String())
	out.WriteString("}\n\n")
	if tracedInit != "" {
		// After the init function above, which builds the tables that the traced copies copy.
		out.WriteString("func init() {\n" + tracedInit + "}\n\n")
	}
	out.WriteString(genGo.Methods.String())
	whole, err := format.Source([]byte("package " + opts.Package + "\n" + out.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("generated code does not compile: %w\n%s", err, out.String())
	}
	body := shakeVars(strings.TrimPrefix(string(whole), "package "+opts.Package+"\n"))
	var tp *typesPackage
	var typesFile []byte
	if opts.Types {
		if typesFile, tp, err = typesPackageFile(opts, typesBody, prog.MissingType); err != nil {
			return nil, nil, err
		}
		// The one reference to the schema of the types package (see schemaName).
		body += "\nconst _ = " + tp.schema + "\n"
	}
	if opts.Lib == common.LibImport {
		// The parser builds only against a library that implements the protocol it was generated
		// for: another library does not declare the constant.
		body += "\nconst _ = " + info.protocol + "\n"
	}
	if genGo.chunksInLib {
		body = chunkNames.Replace(body)
	}
	body, err = qualifyLib(libImport(opts), body, info, tp)
	if err != nil {
		return nil, nil, err
	}
	var file strings.Builder
	file.WriteString(common.Header() + "\n\n")
	if !opts.OmitPackageDoc {
		fmt.Fprintf(&file, "// Package %s is a parser generated from a SyntaxIR grammar.\n", opts.Package)
	}
	fmt.Fprintf(&file, "package %s\n\n%s", opts.Package, body)
	src, err := format.Source([]byte(file.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("generated code does not compile: %w", err)
	}
	if !opts.KeepComments {
		if src, err = trimComments(src); err != nil {
			return nil, nil, fmt.Errorf("generated code does not compile: %w", err)
		}
	}
	api, err := apiSource(opts, len(p.insertionTables()) > 0, info)
	if err != nil {
		return nil, nil, err
	}
	files = []common.GeneratedFile{{Path: "parser.go", Content: src}, {Path: "api.go", Content: []byte(api)}}
	if opts.Types {
		files = append(files, common.GeneratedFile{Path: "types/types.go", Content: typesFile})
	}
	if opts.Lib == common.LibEmbed {
		var roots, users [][]byte
		for _, f := range files {
			roots = append(roots, f.Content)
			if f.Path != "parser.go" {
				users = append(users, f.Content)
				continue
			}
			surface, err := userSurface(f.Content)
			if err != nil {
				return nil, nil, err
			}
			users = append(users, surface)
		}
		rt, err := embeddedLib(opts, features, genGo.Params.UsedFeatures, roots, users)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, rt...)
	}
	return files, genGo, nil
}

// goFieldType writes a field type as a generated fieldType value.
func goFieldType(t *types.FieldType) string {
	var b strings.Builder
	fmt.Fprintf(&b, "&FieldType{Kind: %q, Text: %q", rune(t.Kind), t.Text)
	if t.Name != "" {
		fmt.Fprintf(&b, ", Name: %q", t.Name)
	}
	if t.Elem != nil {
		b.WriteString(", Elem: " + goFieldType(t.Elem))
	}
	if len(t.Alts) > 0 {
		alts := make([]string, len(t.Alts))
		for i, a := range t.Alts {
			alts[i] = goFieldType(a)
		}
		b.WriteString(", Alts: []*FieldType{" + strings.Join(alts, ", ") + "}")
	}
	return b.String() + "}"
}

// Generator generates Go. It shares the expectation table, names and analyses with the
// TypeScript generator through common.Generator.
type Generator struct {
	common.Generator
	// params are the parameters of the printing (Params), whose common part is common.Generator.Params.
	params              Params
	directTypedDocument bool
	// varNames names the package variables that hold the tables of the parser (classes, class steps,
	// terminal programs) by their content, so that each is declared once.
	varNames     map[string]string
	shaperNames  map[string]string
	shaperKeys   []string
	recoveryProg map[*common.Program]recoveryFacts // whether a program can recover from errors or insert missing tokens
	tableErr     error                             // the rule table does not fit the form of the library's RuleSpec
	hooks        Hooks                             // the zero value changes nothing
	tables       *execdata.Tables                  // the tables being printed, at the Exec stage
	// chunksInLib is set when the typed values take their storage from the library (Chunk).
	chunksInLib    bool
	ruleTypesDone  bool // the types of the rule table entries are written
	FlatRules      int
	FramedRules    int
	ExecFuncs      int            // the functions printed from Exec IR (execRoute)
	ExecTables     map[string]int // ExecFuncs by table
	LoweredActions int            // the action expressions printed from Exec IR (lowered)
	// NodeScopes records the frame layouts of the printed Node table rows, which the typed rule
	// metadata shares.
	NodeScopes map[*common.Rule][]string
	// typedOnly leaves out the untyped Node family: the rule table keeps only the metadata the
	// typed table shares.
	typedOnly bool
	// caps maps capture names to the Go variables that hold them in a flat rule of the typed
	// library (see rule_shapes.go), instead of frame slots.
	caps map[string]string
	// streamStart is the start rule of ParseStream, whose separate body
	// (generated while streamBody is set) emits its #stream elements.
	streamStart *common.Rule
	streamBody  bool
	streamErr   error
	// plainCalled holds the plain rules that a generated call refers to by their wrapper
	// (ruleCall). wrappers are the methods of the plain rules, which dropUnusedWrappers keeps only
	// for the rules in plainCalled.
	plainCalled map[*common.Rule]bool
	wrappers    []wrapperSpan
	// program is the plan of the program (flowplan.Planning), which routes every call of a rule, and live
	// the rules that it reaches, which the generator prints, or nil if it prints every rule
	// (GenOptions.KeepUnreachable).
	program *plandata.Program
	live    *liveRules
	// keepTableRules marks a typed-only parser without Node rules: its Node and recognizer tables
	// have no rule bodies to check; the typed table leaves its unreached rules out as usual.
	keepTableRules bool
	// convertsNodes reports that a typed entry point converts the Node result of its rule, which
	// needs the Node rule bodies.
	convertsNodes bool
	// typedRoots are the typed rules whose Entry a typed entry point runs, and typedEntry their IDs.
	typedRoots []ruleKey
	typedEntry map[int]bool
	bodySet    map[ruleKey]bool
	reachErr   error
	// typedTable reports that the typed rule table is generated.
	typedTable bool
	// ins is the insertion program of a grammar that uses #missing (Parser.Ins), whose typed table
	// (insTrules) the typed parses with missing-token insertion enabled run; nil otherwise.
	ins *common.Program
	// plan is the plan of the table being printed (flowplan.Plan, flowplan.PlanTyped), with the
	// decisions of its lowering taken, and table the table that its lowering made, at the Exec stage.
	plan  *plandata.Plan
	table *plandata.Table[code.Exec]
}

func (g *Generator) literal(value string) string {
	name, shared := g.LiteralName(value)
	if !shared {
		fmt.Fprintf(&g.Vars, "var %s = []rune(%q)\n", name, value)
	}
	return name
}

func goStrings(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = strconv.Quote(x)
	}
	return "[]string{" + strings.Join(q, ", ") + "}"
}

// ruleTableLimits reports whether a rule table with nstrs lists of strings and nseen deferred
// memoization indexes fits the fields of the library's RuleSpec (Scope and Vars are 16 bits, Seen 32).
func ruleTableLimits(nstrs, nseen int) error {
	if nstrs > math.MaxUint16+1 {
		return fmt.Errorf("the rule table needs %d distinct capture or variable lists, more than %d", nstrs, math.MaxUint16+1)
	}
	if nseen > math.MaxInt32 {
		return fmt.Errorf("the rule table needs %d deferred memoization indexes, more than %d", nseen, math.MaxInt32)
	}
	return nil
}

// saveRec returns the variables that a backtracking point saves, start, rec if the program can
// recover, and mm if it can insert missing tokens, followed by more; saveRecValues their values.
// restoreRec returns the statement that goes back to them.
func (g *Generator) saveRec(more string) string {
	switch {
	case g.inserts():
		return "start, rec, mm" + more
	case g.recovers():
		return "start, rec" + more
	}
	return "start" + more
}

func (g *Generator) saveRecValues(more string) string {
	switch {
	case g.inserts():
		return "p.Pos, len(p.Recovered), p.MissingLen()" + more
	case g.recovers():
		return "p.Pos, len(p.Recovered)" + more
	}
	return "p.Pos" + more
}

func (g *Generator) restoreRec(pos, rec string) string {
	switch {
	case g.inserts():
		return fmt.Sprintf("p.RestoreMissing(%s, %s, mm)", pos, rec)
	case g.recovers():
		return fmt.Sprintf("p.Restore(%s, %s)", pos, rec)
	}
	return fmt.Sprintf("p.Pos = %s", pos)
}

// recoveryFacts are what the rules of a program can append to p.Recovered with: a recovery
// (#recover), and a #missing site, which inserts in the tables of the insertion program only.
type recoveryFacts struct {
	recovers, missing bool
}

// recovers reports whether a parse of the table being generated can append to p.Recovered: only
// recovery (#recover) and the insertion of a missing token (#missing) in a table of the insertion
// program do. Without them p.Recovered stays empty, so the positions that backtracking restores need
// no saved length of it.
func (g *Generator) recovers() bool { return g.recoveryFacts().recovers || g.inserts() }

// inserts reports whether the table being generated inserts missing tokens: a table of the insertion
// program (insertionTable) of a program with a #missing site, which it lowers with their insertions
// (flowlower.Inserts). Its backtracking points also go back to the insertions on the path they saw
// (MissingLen).
func (g *Generator) inserts() bool {
	return (g.Table == "insRules" || g.Table == "insRecRules" || g.Table == "insTrules") && g.recoveryFacts().missing
}

func (g *Generator) recoveryFacts() recoveryFacts {
	if v, ok := g.recoveryProg[g.Prog]; ok {
		return v
	}
	var found recoveryFacts
	check := func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Recovery:
			found.recovers = true
		case *ast.Attributed:
			for _, a := range e.Attrs {
				switch a.Name {
				case "recover":
					found.recovers = true
				case "missing":
					found.missing = true
				}
			}
		}
	}
	for _, rs := range [][]*common.Rule{g.Prog.RuleList, g.Prog.Twins} {
		for _, r := range rs {
			astinspect.WalkExpr(r.Def.Expr, check)
			astinspect.WalkExpr(r.BodyExpr(), check)
		}
	}
	if g.recoveryProg == nil {
		g.recoveryProg = map[*common.Program]recoveryFacts{}
	}
	g.recoveryProg[g.Prog] = found
	return found
}

// useTable starts printing the table being generated (IRTable) from its table at the Exec stage.
func (g *Generator) useTable() error {
	t := g.tables.Table(g.IRTable())
	if t == nil {
		return fmt.Errorf("internal error: the table %s is not lowered", g.IRTable())
	}
	g.Exec = t
	return nil
}

func (g *Generator) rules() {
	init := &g.Init
	methodsBefore := g.Methods.String()
	bodies := map[*common.Rule]string{}
	scopes := map[*common.Rule]*check.Scope{}
	flats := map[*common.Rule]*flatRule{}
	all := append(append([]*common.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...)
	g.plan, g.table = nil, nil
	if g.Exec != nil {
		// The functions of every table, and the frame layout of each rule, were planned in the middle
		// end and lowered as one module; the bodies follow the plan.
		t := g.tables.Table(g.IRTable())
		g.plan, g.table = t.Plan, t
	}
	for _, r := range all {
		s := check.NewScope()
		scopes[r] = s
		if g.pruned(r) {
			// Not printed; a row of the Node or recognizer table keeps the frame layout of the plan,
			// and a typed row none.
			if !g.typed() {
				s.Names = append(s.Names, g.plan.Layouts[r.ID]...)
			}
			continue
		}
		_, operatorsRule := r.Def.Expr.(*ast.OperatorsRule)
		if g.typed() {
			// Typed flat rules need no separate body; the printer prints every typed rule from Exec IR.
			if d := g.typedRuleFunc(r); d != nil {
				flats[r] = d
				continue
			}
			if !operatorsRule {
				if body := g.frameBody(r, s); body != "" {
					bodies[r] = body
					continue
				}
				g.Unprintable("the body of rule " + r.Name)
			}
			continue
		}
		// The lines of an Operators Rule are printed with its table row (operatorsRule).
		spec, ok := g.plan.Func(plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
		if ok && spec.Form == lowerdata.Flat {
			// recRuleFunc returns nil only if the printed form breaks the plan's decision, which fails
			// the generation (Generator.PrintErr).
			if d := g.recRuleFunc(r, spec); d != nil {
				if g.hooks.LeanRoute != nil && !g.Traced {
					g.hooks.LeanRoute(g.Table, r)
				}
				flats[r] = d
				if g.slotReached(r) {
					bodies[r] = g.leanRuleBody(r, d)
				}
			}
			continue
		}
		if ok {
			if g.hooks.FramedRoute != nil && !g.Traced {
				g.hooks.FramedRoute(g.Table, r)
			}
			bodies[r] = g.nodeFrameBody(r, spec, s)
			continue
		}
		if !operatorsRule {
			g.Unprintable("the body of rule " + r.Name)
		}
	}
	if g.plan != nil && !g.typed() {
		// Every row keeps the frame layout of the plan, which the VM's rule scopes derive from too.
		for _, r := range all {
			if err := g.plan.CheckLayout(r.ID, r.Name, scopes[r].Names); err != nil && g.PrintErr == nil {
				g.PrintErr = err
			}
		}
	}
	if g.Table == "rules" && !g.Traced {
		g.NodeScopes = make(map[*common.Rule][]string, len(all))
		for _, r := range all {
			g.NodeScopes[r] = append([]string(nil), scopes[r].Names...)
		}
	}
	if g.Traced {
		// The traced copy of the table: the same rows with the traced bodies, which the init function
		// that follows the one that builds the table sets.
		copyOf := "TraceRules"
		if g.typed() {
			copyOf = "TraceTRules"
		}
		fmt.Fprintf(init, "\t%s = %s(%s)\n", g.tableVar(), copyOf, g.Table)
		fmt.Fprintf(&g.Methods, "// %s is the traced copy of %s, which an observed parse runs.\nvar %[1]s []*%[3]s\n\n", g.tableVar(), g.Table, g.pick("Rule", "TRule"))
	} else if g.Table == "trules" {
		// The typed rules share the analysis results of the rules.
		init.WriteString("\ttrules = NewTRules(rules)\n\tdefer tinit()\n")
		g.Methods.WriteString("// trules is the typed rule table.\nvar trules []*TRule\n\n")
	} else if g.Table == "insTrules" {
		// Those of the insertion program share the analysis results of its rules, and the typed
		// parses with missing-token insertion enabled run them (SetTInsertion).
		init.WriteString("\tinsTrules = NewTRules(insRules)\n\tSetTInsertion(trules, insTrules)\n")
		g.Methods.WriteString("// insTrules is the typed rule table of the insertion program that the typed parses with missing\n// tokens enabled run.\nvar insTrules []*TRule\n\n")
	} else {
		// The table is static data of a compact form (RuleSpec); the init function builds the rules.
		var tab strings.Builder
		strs := []string{"nil"}
		strIdx := map[string]int{"nil": 0}
		strsOf := func(xs []string) int {
			if len(xs) == 0 {
				return 0
			}
			lit := goStrings(xs)
			if k, ok := strIdx[lit]; ok {
				return k
			}
			strIdx[lit] = len(strs)
			strs = append(strs, strings.TrimPrefix(lit, "[]string"))
			return strIdx[lit]
		}
		var extras []string
		for i, r := range all {
			if r.ID != i {
				panic("rule ids are not positions in the rule table")
			}
			value := "nil"
			if _, ok := r.Def.Expr.(*ast.OperatorsRule); !ok { // the lines build the value of an Operators Rule
				value = "&" + g.shaper(r.BodyExpr())
			}
			var flags []string
			if r.Memo && !r.Transient {
				flags = append(flags, "RuleMemo")
			}
			if r.Leader {
				flags = append(flags, "RuleLeader")
			}
			if r.Novalue {
				flags = append(flags, "RuleNovalue")
			}
			if g.Document && (r.Leader || r.Memo) {
				flags = append(flags, "RuleDocMemo")
			}
			if g.Document && r.Positional {
				flags = append(flags, "RulePositional")
			}
			if r.Splice {
				flags = append(flags, "RuleSplice")
			}
			if r.Rec {
				flags = append(flags, "RuleRec")
			}
			if r.Missing {
				flags = append(flags, "RuleMissing")
			}
			flagsLit := "0"
			if len(flags) > 0 {
				flagsLit = strings.Join(flags, " | ")
			}
			seen := 0
			if r.Seen >= 0 {
				seen = r.Seen + 1
			}
			fmt.Fprintf(&tab, "\t{%q, %s, %s, %d, %d, %d},\n", r.Name, value, flagsLit, seen, strsOf(scopes[r].Names), strsOf(r.Vars))
			w := r.WhereName()
			if w == r.Name {
				w = ""
			}
			if w != "" || r.NodeType != "" || r.OpNodeType != "" || r.Cycle != 0 {
				extras = append(extras, fmt.Sprintf("\t{%d, %q, %q, %q, %d},\n", r.ID, w, r.NodeType, r.OpNodeType, r.Cycle))
			}
		}
		if err := ruleTableLimits(len(strs), g.Prog.Run.NSeen); err != nil {
			g.tableErr = err
		}
		spec, strTable, extraTable := g.Table+"Spec", g.Table+"Strs", g.Table+"Extras"
		// The entries are positional, which vet accepts only for a type of the package; NewRules takes
		// any type of the layout of RuleSpec and RuleExtra.
		if !g.ruleTypesDone {
			g.ruleTypesDone = true
			g.Vars.WriteString("// ruleSpec and ruleExtra have the layout of RuleSpec and RuleExtra, with the fields in this order:\n// name, value, flags, seen, scope, vars; and id, where, node type, operators rule node type, cycle.\ntype ruleSpec struct {\n\tName  string\n\tValue *Shaper\n\tFlags uint8\n\tSeen  int32\n\tScope uint16\n\tVars  uint16\n}\n\ntype ruleExtra struct {\n\tId         int\n\tWhere      string\n\tNodeType   string\n\tOpNodeType string\n\tCycle      int32\n}\n\n")
		}
		fmt.Fprintf(&g.Vars, "// %s is the compact form of the rule table, built by NewRules.\nvar %s = [...]ruleSpec{\n%s}\n\n", spec, spec, tab.String())
		fmt.Fprintf(&g.Vars, "var %s = [...][]string{\n", strTable)
		for _, x := range strs {
			if x == "nil" {
				g.Vars.WriteString("\tnil,\n")
			} else {
				fmt.Fprintf(&g.Vars, "\t%s,\n", x)
			}
		}
		g.Vars.WriteString("}\n\n")
		fmt.Fprintf(&g.Vars, "var %s = [...]ruleExtra{\n%s}\n\n", extraTable, strings.Join(extras, ""))
		fmt.Fprintf(init, "\t%s = NewRules(%s[:], %s[:], %s[:])\n", g.Table, spec, strTable, extraTable)
		fmt.Fprintf(init, "\t%s.SetRules(%s, %d)\n", g.grammarVar(), g.Table, g.Prog.Run.NSeen)
	}
	nodeCode := g.Table == "rules" && g.typedOnly
	if nodeCode {
		// The Node bodies were generated for the frame layouts they fix; their methods are not
		// kept, and the typed rules need none of the Node entries.
		g.Methods.Reset()
		g.Methods.WriteString(methodsBefore)
	}
	for _, r := range all {
		if nodeCode {
			break
		}
		if g.pruned(r) {
			continue
		}
		if pr, ok := r.Def.Expr.(*ast.OperatorsRule); ok {
			g.Cur = r
			g.operatorsRule(r, pr)
			if r.Action != nil && !r.Novalue && !g.Traced {
				// The whole-rule action (a traced copy has it from its row), which the library runs on the folded value after each call.
				locals := map[string]string{rprogram.OperatorsRuleValue: "c.Value"}
				fmt.Fprintf(init, "\t%s[%d].Action = func(c *%s) (any, any) { %s }\n", g.Table, r.ID, g.ctxType(), g.actionBody(g.RuleSite(r), check.NewScope(), locals))
			}
			continue
		}
		if d := flats[r]; d != nil && g.typed() {
			g.typedCall(r, "", scopes[r], d)
			continue
		}
		if g.slotReached(r) {
			fmt.Fprintf(init, "\t%s[%d].Body = %s\n", g.tableVar(), r.ID, bodies[r])
			g.printedBody(r)
		}
		if g.Traced {
			// A traced copy has the action of its row.
		} else if r.Action != nil && g.typed() {
			// A function of its own, which typedCall calls directly.
			fmt.Fprintf(&g.Methods, "// The action of %s.\nfunc %s(c *TActCtx) (any, any) { %s }\n\n", r.Name, g.typedAction(r), g.actionBody(g.RuleSite(r), scopes[r], nil))
			fmt.Fprintf(init, "\t%s[%d].Action = %s\n", g.Table, r.ID, g.typedAction(r))
		} else if r.Action != nil {
			fmt.Fprintf(init, "\t%s[%d].Action = func(c *%s) (any, any) { %s }\n", g.Table, r.ID, g.ctxType(), g.actionBody(g.RuleSite(r), scopes[r], nil))
		}
		if g.typed() {
			g.typedCall(r, bodies[r], scopes[r], nil)
		} else if r.Plain {
			start := g.Methods.Len()
			if d := flats[r]; d != nil {
				g.flatMethod(r, d, g.ruleFunc(g.plainPrefix(), r.ID), "called as by invokePlain", true, true)
			} else {
				g.plainCall(r, bodies[r])
			}
			g.wrappers = append(g.wrappers, wrapperSpan{r: r, start: start, end: g.Methods.Len()})
		}
	}
	if stream := streamRuleVar(g.Table); g.streamStart != nil && stream != "" {
		// ParseStream calls a copy of the start rule whose body emits the
		// #stream elements, generated last so that the batch code is the same
		// as without streams. Its captures have a layout of their own. The
		// insertion table has its own copy, which a stream parse with
		// missing-token insertion enabled runs (SetInsertion).
		r := g.Prog.ByName[g.streamStart.Name]
		s := check.NewScope()
		body := g.streamBodyFunc(r, s)
		if body != "" {
			if g.hooks.StreamRoute != nil && !g.Traced {
				g.hooks.StreamRoute(g.Table, r)
			}
		} else {
			g.Unprintable("the stream body of rule " + r.Name)
		}
		if strings.Join(s.Names, "\x00") != strings.Join(scopes[r].Names, "\x00") {
			// The copied action addresses captures by the batch body's slots.
			g.streamErr = fmt.Errorf("internal error: the stream body of %s has a different capture layout", r.Name)
		}
		if g.Traced {
			fmt.Fprintf(init, "\t%[1]sTraced = TraceRule(%[1]s)\n\t%[1]sTraced.Body = %[2]s\n", stream, body)
			fmt.Fprintf(&g.Methods, "// %[1]sTraced is the traced copy of %[1]s, which an observed parse runs.\nvar %[1]sTraced *Rule\n\n", stream)
		} else {
			fmt.Fprintf(init, "\t%[1]s = new(Rule)\n\t*%[1]s = *%[2]s[%[3]d]\n\t%[1]s.Scope = %[4]s\n\t%[1]s.Body = %[5]s\n", stream, g.Table, r.ID, goStrings(s.Names), body)
			if g.Table == "rules" {
				g.Methods.WriteString("// streamRule is the start rule of ParseStream.\nvar streamRule *Rule\n\n")
			} else {
				g.Methods.WriteString("// insStreamRule is the start rule of ParseStream in the insertion table.\nvar insStreamRule *Rule\n\n")
			}
		}
	}
	g.dropUnusedWrappers()
	if g.Table != "rules" || g.Traced {
		return // the recognizer and the traced copy share the struct fields
	}
	// Struct fields, used to detect references to undeclared fields at run time and to read the
	// fields that a constructor left unset.
	var names []string
	for name, spec := range g.Prog.Types {
		if td, ok := spec.(*ast.TypeDef); ok && td.Struct() != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		sz := g.Prog.Run.StructZeroOf(name)
		var fields []string
		for _, f := range sz.Names {
			fields = append(fields, fmt.Sprintf("%q, %q", f, sz.Zeros[f]))
		}
		fmt.Fprintf(init, "\tsxirGrammar.DeclareStruct(%q%s)\n", name, joinPrefixed(fields))
		fmt.Fprintf(init, "\tsxirGrammar.TypeKinds[%q] = KindOf(%q, \"\")\n", name, name)
	}
	// The kinds of the named node types, under their names and those of their aliases.
	names = names[:0]
	for name := range g.Prog.Run.NodeKinds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(init, "\tsxirGrammar.NodeTypes[%q] = NewNodeTypeKinds(%q)\n", name, g.Prog.Run.NodeKinds[name][0].Type)
	}
}

// nodeTypeNames returns the names of the grammar's named node types, sorted, in the order of their
// generated indexes from 1 (nodeTypeIndex).
func (g *Generator) nodeTypeNames() []string {
	var names []string
	for name, spec := range g.Prog.Types {
		if td, ok := spec.(*ast.TypeDef); ok && td.Struct() == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// nodeTypeIndex returns the generated index of the named node type name, from 1.
func (g *Generator) nodeTypeIndex(name string) int {
	n, ok := g.Prog.Check.NodeTypeOf(name)
	if !ok {
		panic("internal error: " + name + " is not a named node type")
	}
	return sort.SearchStrings(g.nodeTypeNames(), n.Name) + 1
}

// nodeTypeID returns the Go expression of the registered index of the named node type name in the
// typed library (nt in tmatch and tnode).
func (g *Generator) nodeTypeID(name string) string {
	return fmt.Sprintf("%s[%d]", nodeTypesName, g.nodeTypeIndex(name))
}

// typedResultConversion returns the statement that converts the value v of rule r to the rule's
// named node type in the typed library (what finish does), or "" if r has none. body, if not "",
// holds the fresh node of the rule's body (tfreshNode), which converts in place.
func (g *Generator) typedResultConversion(r *common.Rule, v, body string) string {
	if r.NodeType == "" {
		return ""
	}
	if body != "" {
		return fmt.Sprintf("%s = p.TConvertResultOwned(%s[%d], %s, %s)", v, g.Table, r.ID, v, body)
	}
	return fmt.Sprintf("%s = p.TConvertResult(%s[%d], %s)", v, g.Table, r.ID, v)
}

// ownsBody reports whether finishing the value of rule r in the typed library converts the fresh
// node of its body in place: the rule has a named node type and returns its body's value.
func (g *Generator) ownsBody(r *common.Rule) bool {
	return r.NodeType != "" && !r.Novalue && r.Action == nil
}

// typedCall writes the methods that call the rule r in the typed library: s<id> for a rule never
// memoized and without captures (what invokePlain does), and otherwise u<id> (what call does, the
// memo kept by callMemo) and i<id> (what invoke does). Each calls the body's method and the action
// directly and finishes the value as the rule's kind requires (what finish does), so a call goes
// through no rule table entry or function value.
//
// For a flat rule (d, see rule_shapes.go), the method that runs the body (s<id>, v<id> or i<id>)
// has it inlined, with the action.
func (g *Generator) typedCall(r *common.Rule, body string, s *check.Scope, d *flatRule) {
	if _, operatorsRule := r.Def.Expr.(*ast.OperatorsRule); operatorsRule || r.Leader {
		return // the general call
	}
	m := &g.Methods
	entry := g.ruleFunc("u", r.ID)
	if r.Plain {
		entry = g.ruleFunc("s", r.ID)
	}
	if g.typedEntry[r.ID] {
		// Only a typed entry point runs an Entry; every other call names the function.
		fmt.Fprintf(&g.Init, "\t%s[%d].Entry = %s\n", g.tableVar(), r.ID, entry)
	}
	ncaps := len(s.Names)
	if d != nil {
		ncaps = len(r.Scope.Names)
	}
	finish := func(frame string) {
		body := ""
		if g.ownsBody(r) {
			body = "body"
			m.WriteString("\t\tbody := TFreshNode(v)\n")
		}
		switch {
		case r.Novalue:
			m.WriteString("\t\tv = nil\n")
		case r.Action != nil:
			fmt.Fprintf(m, "\t\tc := p.UseCtx(TActCtx{P: p, F: %s, Start: start, End: p.Pos, Cbase: len(p.Created)})\n", frame)
			fmt.Fprintf(m, "\t\tv = c.Result(%s, %q)\n", g.typedAction(r), r.WhereName())
		default:
			fmt.Fprintf(m, "\t\tv = p.ScopeValue(%s[%d].Value, v, start)\n", g.Table, r.ID)
			if len(s.Names) > 0 {
				fmt.Fprintf(m, "\t\tv = p.AttachCaptures(v, %s[%d].Scope, %s.Vals, start, p.Pos)\n", g.Table, r.ID, frame)
			}
			m.WriteString("\t\tif n := AsTval(v); n != nil {\n\t\t\tn.TSetFresh(false)\n\t\t}\n")
		}
		if conv := g.typedResultConversion(r, "v", body); conv != "" {
			fmt.Fprintf(m, "\t\t%s\n", conv)
		}
	}
	// plain writes the method name that runs the body as invokePlain does; with call it also
	// counts the unit of the call, which a call site that calls it directly counts through it.
	plain := func(name string, call bool) {
		if d != nil {
			g.flatMethod(r, d, name, "called as by invokePlain", true, call)
			return
		}
		if g.Observability && r.Plain && !g.directTypedDocument {
			name = g.tracePlainWrapper(r, name)
		}
		fmt.Fprintf(m, "// %s, called as by invokePlain\nfunc %s(p *TParser) (any, bool) {\n", r.Name, name)
		if call {
			m.WriteString(g.callUnit("\tp", r))
		}
		m.WriteString("\t" + g.saveRec(", trail") + " := " + g.saveRecValues(", len(p.Trail)") + "\n\tprevEnv, prevCut := p.Env, p.Cut\n\tp.Cut = false\n" + enterRec(r))
		if g.Observability && (!r.Plain || g.directTypedDocument) {
			m.WriteString("p.Observation.Evaluated()\n")
		}
		fmt.Fprintf(m, "\tv, ok := %s(p, 0)\n%s\tp.Cut = prevCut\n\tp.DropTrail(trail)\n\tif ok {\n", body, leaveRec(r))
		finish("EmptyTFrame")
		m.WriteString("\t} else {\n\t\t" + g.restoreRec("start", "rec") + "\n\t}\n\tp.Env = prevEnv\n\treturn v, ok\n}\n\n")
	}
	if r.Plain {
		if g.directTypedDocument {
			name := g.ruleFunc("s", r.ID)
			if g.Observability {
				name = g.traceCallWrapper(r, name, false)
			}
			ds := g.ruleFunc("ds", r.ID)
			fmt.Fprintf(m, "func %s(p *TParser) (any, bool) {\n if p.TypedDocument != nil { return p.DocumentCall(%s[%d], 0, %s) }\n return %[4]s(p)\n}\n", name, g.Table, r.ID, ds)
			plain(ds, false)
			return
		}
		plain(g.ruleFunc("s", r.ID), true)
		return
	}
	// u<id>: the call. An unmemoized call of a rule without captures is plain.
	name := g.ruleFunc("u", r.ID)
	if g.Observability {
		name = g.traceCallWrapper(r, name, false)
	}
	fmt.Fprintf(m, "// rule %s\nfunc %s(p *TParser) (any, bool) {\n", r.Name, name)
	// The invocation of a flat rule without captures is its plain method: the two bodies differ
	// only in the rollback to the start on failure, which the callers of the invocation repeat
	// harmlessly, so one body serves both. A Document records the exact end of the input the body
	// examined, which does not depend on the position a failed invocation leaves.
	merged := d != nil && ncaps == 0
	iname := g.ruleFunc("i", r.ID)
	if merged {
		iname = g.ruleFunc("v", r.ID)
	}
	// The calls below name the row of the typed table (trules or insTrules), also in the traced pass:
	// they read only its analysis (Seen, Id, Memo, Scope), which the traced copy shares, and run the
	// body they are passed. Only the general call runs a row's body, and the early return above keeps
	// Leaders and Operators Rules on it.
	if g.directTypedDocument {
		fmt.Fprintf(m, " if p.TypedDocument != nil { return p.DocumentCall(%s[%d], 0, %s) }\n", g.Table, r.ID, iname)
	}
	// The call charges its unit and, for a rule that a parse from scratch memoizes, the values of
	// its key, whether or not its memoization is deferred (budget.chargeKey). Only parses in the
	// missing-token mode run the typed table of the insertion program, and they memoize no rule that
	// can insert a missing token (rule.memo).
	memo := r.Memo && !r.Transient && !(g.inserts() && r.Missing)
	m.WriteString(g.callUnit("\tp", r))
	if memo && g.Logical && len(r.Vars) > 0 {
		fmt.Fprintf(m, "\tp.KeyUnits(%s[%d])\n", g.nodeRows(), r.ID)
	}
	if memo {
		fmt.Fprintf(m, "\tif !p.FirstCall(%s[%d]) {\n\t\treturn p.CallMemo(%s[%d], %s)\n\t}\n", g.nodeRows(), r.ID, g.Table, r.ID, iname)
	}
	if ncaps == 0 {
		fmt.Fprintf(m, "\treturn %s(p)\n}\n\n", g.ruleFunc("v", r.ID))
		plain(g.ruleFunc("v", r.ID), false)
	} else {
		fmt.Fprintf(m, "\t%s := %s\n\tv, ok := %s(p)\n\tif !ok {\n\t\t%s\n\t}\n\treturn v, ok\n}\n\n", g.saveRec(""), g.saveRecValues(""), g.ruleFunc("i", r.ID), g.restoreRec("start", "rec"))
	}
	// i<id>: the invocation (also used by the memo).
	if merged {
		return // v<id>
	}
	if d != nil {
		g.flatMethod(r, d, g.ruleFunc("i", r.ID), "invoked as by invoke", false, false)
		return
	}
	fmt.Fprintf(m, "// %s, invoked as by invoke\nfunc %s(p *TParser) (any, bool) {\n", r.Name, g.ruleFunc("i", r.ID))
	fmt.Fprintf(m, "\tf := p.NewFrame(%d)\n\tprevFrame, prevEnv, prevCut, trail := p.Frame, p.Env, p.Cut, len(p.Trail)\n\tp.Frame, p.Cut = f, false\n%s\tstart := p.Pos\n\t_ = start\n", len(s.Names), enterRec(r))
	if g.Observability {
		m.WriteString("p.Observation.Evaluated()\n")
	}
	fmt.Fprintf(m, "\tv, ok := %s(p, 0)\n%s\tp.Frame, p.Cut = prevFrame, prevCut\n\tp.DropTrail(trail)\n\tif ok {\n", body, leaveRec(r))
	finish("f")
	m.WriteString("\t}\n\tp.Env = prevEnv\n\tp.FreeFrame(f)\n\treturn v, ok\n}\n\n")
}

// streamRuleVar names the copy of the start rule that ParseStream runs in the rule table table, or
// returns "" if the table has none: the Node table and the insertion table have one.
func streamRuleVar(table string) string {
	switch table {
	case "rules":
		return "streamRule"
	case "insRules":
		return "insStreamRule"
	}
	return ""
}

// tracedTables prints the traced copies of the rule tables that the generator printed (Traced), which an
// observed parse runs, and returns the statements of the init function that makes them. It runs after
// the tables, whose bodies stay those of a parser without observability.
func (g *Generator) tracedTables(prog, rec *common.Program, insertion []insertionTable) (string, error) {
	init, descs, ordinary := g.Init.String(), len(g.Descs), g.Ordinary
	g.Init.Reset()
	g.Traced, g.Observability, g.Ordinary = true, true, false
	defer func() { g.Traced, g.Observability, g.Ordinary = false, false, ordinary }()
	pass := func(p *common.Program, table string, start func() error) error {
		g.Prog, g.Table, g.plainCalled = p, table, nil
		defer func() { g.Prog, g.Table, g.Exec = prog, "rules", nil }()
		if err := start(); err != nil {
			return err
		}
		g.rules()
		return cmp.Or(g.streamErr, g.tableErr, g.PrintErr)
	}
	if !g.typedOnly {
		if err := pass(prog, "rules", g.useTable); err != nil {
			return "", err
		}
	}
	if rec != nil {
		if err := pass(rec, "recRules", g.useTable); err != nil {
			return "", err
		}
	}
	if g.typedTable {
		if err := pass(prog, "trules", g.useTable); err != nil {
			return "", err
		}
	}
	if g.typedTable && g.ins != nil {
		if err := pass(g.ins, "insTrules", g.useTable); err != nil {
			return "", err
		}
	}
	for _, t := range insertion {
		if err := pass(t.prog, t.table, g.useTable); err != nil {
			return "", err
		}
	}
	if len(g.Descs) != descs {
		return "", fmt.Errorf("internal error: the traced bodies record expectations that the rule tables do not")
	}
	traced := g.Init.String()
	g.Init.Reset()
	g.Init.WriteString(init)
	return traced, nil
}

// wrapperSpan is the span [start, end) of the methods of a plain rule's wrapper in generator.methods.
type wrapperSpan struct {
	r          *common.Rule
	start, end int
}

// dropUnusedWrappers removes the wrappers of the plain rules that no generated call refers to. It
// runs after the bodies and the stream body, which are the last generated calls, so that a wrapper
// is kept exactly when a call of it was generated. Spans are removed from the last one to the first,
// so the earlier spans keep their offsets.
func (g *Generator) dropUnusedWrappers() {
	text := g.Methods.String()
	for i := len(g.wrappers) - 1; i >= 0; i-- {
		w := g.wrappers[i]
		if !strings.HasPrefix(text[w.start:w.end], "// ") || !strings.HasSuffix(text[w.start:w.end], "}\n\n") {
			panic(fmt.Sprintf("internal error: the wrapper of %s is not at its recorded span", w.r.Name))
		}
		if !g.plainCalled[w.r] {
			text = text[:w.start] + text[w.end:]
		}
	}
	g.wrappers = nil
	g.Methods.Reset()
	g.Methods.WriteString(text)
}

// plainPrefix is the prefix of the methods written by plainCall, and seenVar the variable that
// holds the number of rules with rule.seen set, for the rule table being generated.
func (g *Generator) plainPrefix() string {
	switch g.Table {
	case "rules":
		return "r"
	case "trules", "insTrules":
		return "s"
	case "insRules":
		return "ir"
	case "insRecRules":
		return "iq"
	}
	return "q"
}

// tableVar is the variable of the rule table being generated: the table of the kind Table, or its
// traced copy in the traced pass.
func (g *Generator) tableVar() string { return g.Table + g.TracedSuffix() }

// ruleFunc names the generated function prefix<id> of the rule id, with the suffix of the traced
// pass (TracedSuffix), which prints a copy of each function for observed parses. The functions of the
// typed table of the insertion program (insTrules) have the prefix m before.
func (g *Generator) ruleFunc(prefix string, id int) string {
	if g.Table == "insTrules" {
		prefix = "m" + prefix
	}
	return prefix + strconv.Itoa(id) + g.TracedSuffix()
}

// typedAction names the function of the action of the typed rule r, which the rows of the typed table
// being generated and its traced copy share: ta<id>, or mta<id> in the typed table of the insertion
// program.
func (g *Generator) typedAction(r *common.Rule) string {
	if g.Table == "insTrules" {
		return "mta" + strconv.Itoa(r.ID)
	}
	return "ta" + strconv.Itoa(r.ID)
}

// typed reports whether the table being generated is a table of the typed library: the typed table
// (trules), or that of the insertion program (insTrules).
func (g *Generator) typed() bool { return g.Table == "trules" || g.Table == "insTrules" }

// nodeRows names the Node rule table whose rows the typed table being generated shares (NewTRules):
// the Node table of its program.
func (g *Generator) nodeRows() string {
	if g.Table == "insTrules" {
		return "insRules"
	}
	return "rules"
}

// pick returns untyped for the Node library and typed for the typed library (trules, insTrules).
func (g *Generator) pick(untyped, typed string) string {
	if g.typed() {
		return typed
	}
	return untyped
}

func (g *Generator) recv() string    { return g.pick("Parser", "TParser") }
func (g *Generator) valType() string { return g.pick("*Node", "any") }
func (g *Generator) ctxType() string { return g.pick("ActCtx", "TActCtx") }

func (g *Generator) frameVar() string { return g.pick("EmptyFrame", "EmptyTFrame") }
func (g *Generator) rt() string       { return g.pick("rt", "trt") }

// grammarVar returns the generated grammar of the rule table being generated.
func (g *Generator) grammarVar() string {
	switch g.Table {
	case "rules":
		return "sxirGrammar"
	case "insRules":
		return "sxirInsGrammar"
	case "insRecRules":
		return "sxirInsRecGrammar"
	}
	return "sxirRecGrammar"
}

// insertionTable is a rule table of the insertion program of a grammar that uses #missing: the program,
// the table, its grammar, and the table whose parses with missing-token insertion enabled run it.
type insertionTable struct {
	prog               *common.Program
	table, grammar, of string
}

// insertionTables returns the rule tables of the insertion program of p: none for a grammar without
// #missing, the Node table, and with Recognize the recognizer.
func (p *Parser) insertionTables() []insertionTable {
	var t []insertionTable
	if p.Ins != nil {
		t = append(t, insertionTable{p.Ins, "insRules", "sxirInsGrammar", "rules"})
	}
	if p.InsRec != nil {
		t = append(t, insertionTable{p.InsRec, "insRecRules", "sxirInsRecGrammar", "recRules"})
	}
	return t
}

// ruleTables returns the rule tables of the Node library that p has, for the feature analysis: the
// Node table, the recognizer and the tables of the insertion program, which insert missing tokens.
func (p *Parser) ruleTables() []common.RuleTable {
	t := common.Tables(p.Prog, p.Rec)
	for _, ins := range p.insertionTables() {
		t = append(t, common.RuleTable{Prog: ins.prog, Name: ins.table, Inserts: true})
	}
	return t
}

// Method emits a method with the given body and returns its name.
func (g *Generator) Method(comment, body string) string {
	name, shared := g.MethodName(g.recv()+"/"+g.valType(), body)
	if shared {
		return name
	}
	if comment != "" {
		fmt.Fprintf(&g.Methods, "// %s\n", comment)
	}
	fmt.Fprintf(&g.Methods, "func %s(p *%s) (%s, bool) {\n%s}\n\n", name, g.recv(), g.valType(), body)
	return name
}

// bodyMethod emits the function of a rule body and returns its name. It has the signature of the
// Body field of the rule table, so the table names it without a closure; the Operators Rule level
// it ignores is 0 where the generated code calls it.
func (g *Generator) bodyMethod(comment, body string) string {
	name, shared := g.MethodName(g.recv()+"/"+g.valType()+"/body", body)
	if shared {
		return name
	}
	if comment != "" {
		fmt.Fprintf(&g.Methods, "// %s\n", comment)
	}
	fmt.Fprintf(&g.Methods, "func %s(p *%s, _ int) (%s, bool) {\n%s}\n\n", name, g.recv(), g.valType(), body)
	return name
}

// shaper returns the name of the package variable that holds the library shaper of the expression
// e, its static shape (internal/frontend/check/value_shape.go). Few distinct shapes occur, so the variables are shared.
func (g *Generator) shaper(e ast.Expr) string {
	return g.shaperOf(shaperLit(g.Prog, e))
}

// shaperOf returns the name of the package variable that holds the shaper of the literal lit,
// declaring it on first use.
func (g *Generator) shaperOf(lit string) string {
	if g.shaperNames == nil {
		g.shaperNames = map[string]string{}
	}
	if name := g.shaperNames[lit]; name != "" {
		return name
	}
	name := g.Name("sh")
	g.shaperNames[lit] = name
	g.shaperKeys = append(g.shaperKeys, lit)
	fmt.Fprintf(&g.Vars, "var %s = %s\n", name, lit)
	return name
}

// shaperLit returns the literal of the library shaper of the expression e.
func shaperLit(prog *common.Program, e ast.Expr) string {
	return fmt.Sprintf("Shaper{Elems: %v, K: %d, Mode: %d, Tuple: %v, Shared: %v}", check.IsElems(prog.Checker(), e), check.ValuedCount(prog.Checker(), e), check.SingleModeOf(prog.Checker(), e), check.ScopeTuple(prog.Checker(), e), check.IsElems(prog.Checker(), e) && check.SharesTuple(prog.Checker(), e))
}

// plainCall writes the method r<id>, which does what invokePlain does for the rule r (a rule never
// memoized and without captures) with a direct call of its body.
func (g *Generator) plainCall(r *common.Rule, body string) {
	name := g.ruleFunc(g.plainPrefix(), r.ID)
	if g.Observability {
		name = g.tracePlainWrapper(r, name)
	}
	fmt.Fprintf(&g.Methods, "// %s, called as by invokePlain\nfunc %s(p *%s) (%s, bool) {\n%s", r.Name, name, g.recv(), g.valType(), g.callUnit("p", r))
	g.Methods.WriteString("\t" + g.saveRec(", trail") + " := " + g.saveRecValues(", len(p.Trail)") + "\n")
	g.Methods.WriteString(`	prevEnv, prevCut := p.Env, p.Cut
	p.Cut = false
` + enterRec(r))
	fmt.Fprintf(&g.Methods, "\tv, ok := %s(p, 0)\n", body)
	g.Methods.WriteString(leaveRec(r) + "\tp.Cut = prevCut\n" + g.pick("\tp.Trail = p.Trail[:trail]\n", "\tp.DropTrail(trail)\n") + "\tif ok {\n")
	if r.Novalue {
		g.Methods.WriteString("\t\tv = nil\n")
	} else {
		fmt.Fprintf(&g.Methods, "\t\tv = p.Finish(%s[%d], %s, v, start)\n", g.tableVar(), r.ID, g.frameVar())
	}
	g.Methods.WriteString("\t} else {\n\t\t" + g.restoreRec("start", "rec") + "\n\t}\n")
	g.Methods.WriteString(`	p.Env = prevEnv
	return v, ok
}

`)
}

// enterRec returns the statement that counts the entry of the rule r against the nesting limit: only
// a rule on a cycle of calls counts.
func enterRec(r *common.Rule) string {
	if r.Rec {
		return "\tp.Enter()\n"
	}
	return ""
}

// leaveRec returns the statement that ends what enterRec counted.
func leaveRec(r *common.Rule) string {
	if r.Rec {
		return "\tp.Depth--\n"
	}
	return ""
}

// depthLimit returns the nesting guard needed before calling depth rules.
func depthLimit(depth int) string {
	if depth == 0 {
		return "p.Depth <= p.MaxDepth"
	}
	return fmt.Sprintf("p.Depth+%d <= p.MaxDepth", depth)
}

// NegateCondition inverts a generated condition without retaining a leading
// logical negation as a double negation.
func NegateCondition(condition string) string {
	if strings.HasPrefix(condition, "!") {
		return strings.TrimPrefix(condition, "!")
	}
	return "!(" + condition + ")"
}

// classReject returns a Go condition on ch that is true when ch is not accepted by the class.
func classReject(e *ast.CharClass) string {
	var in []string
	for _, r := range e.Ranges {
		if r.Lo == r.Hi {
			in = append(in, fmt.Sprintf("ch == %d", r.Lo))
		} else {
			in = append(in, fmt.Sprintf("ch >= %d && ch <= %d", r.Lo, r.Hi))
		}
	}
	if len(in) == 0 {
		in = []string{"ch < 0"} // never true; mentions ch so that it is used
	}
	cond := strings.Join(in, " || ")
	if e.Negated {
		return "(" + cond + ")"
	}
	return "!(" + cond + ")"
}

func (g *Generator) operatorsRule(r *common.Rule, e *ast.OperatorsRule) {
	var b strings.Builder
	fmt.Fprintf(&b, "\t%s[%d].OperatorsRule = &%s{\n", g.tableVar(), r.ID, g.pick("OperatorsRule", "TOperatorsRule"))
	// The lines of the rule's Core IR, in the order of e, if they are printed from Exec IR.
	co := g.operatorsRuleOf(r)
	if e.Skip != nil {
		s := check.NewScope()
		m := g.operatorsRuleBody(plandata.Key{Use: plandata.OperatorsSkip, Rule: r.ID}, s, false)
		if m == "" {
			g.Unprintable("the skip of an Operators Rule of rule " + g.Cur.Name)
		}
		fmt.Fprintf(&b, "\t\tSkip: %s,\n", m)
	}
	fmt.Fprintf(&b, "\t\tOperands: []*%s{\n", g.pick("OperatorsRuleLine", "TOperatorsRuleLine"))
	for i, o := range e.Operands {
		var l *core.OpLine
		if co != nil {
			l = co.Operands[i]
		}
		fmt.Fprintf(&b, "\t\t\t%s,\n", g.operatorsRuleLine(plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: i}, o.Expr, o.Action, false, l))
	}
	b.WriteString("\t\t},\n")
	var prefix, led []string
	id := 0
	for i, l := range e.Levels {
		for _, op := range l.Operators {
			var l *core.OpLine
			if co != nil {
				l = co.Ops[id].Line
			}
			s := fmt.Sprintf("{Id: %d, Kind: %q, Assoc: %q, Level: %d, Line: %s}", id, op.Kind, op.Assoc, i+1, g.operatorsRuleLine(plandata.Key{Use: plandata.OperatorsLine, Rule: r.ID, Line: len(e.Operands) + id}, op.Expr, op.Action, true, l))
			id++
			if op.Kind == ast.Prefix {
				prefix = append(prefix, s)
			} else {
				led = append(led, s)
			}
		}
	}
	op := g.pick("OperatorsRuleOp", "TOperatorsRuleOp")
	fmt.Fprintf(&b, "\t\tPrefix: []*%s{%s},\n", op, strings.Join(prefix, ",\n"))
	fmt.Fprintf(&b, "\t\tLed: []*%s{%s},\n", op, strings.Join(led, ",\n"))
	b.WriteString("\t}\n")
	fmt.Fprintf(&b, "\t%s[%d].Body = func(p *%s, min int) (%s, bool) { return p.OperatorsRuleParse(%s[%d], min) }\n", g.tableVar(), r.ID, g.recv(), g.valType(), g.tableVar(), r.ID)
	g.printedBody(r)
	g.Init.WriteString(b.String())
}

// operatorsRuleLine returns the line of an Operators Rule with the expression e and the action;
// l is its Core IR if it is printed from Exec IR, or nil.
func (g *Generator) operatorsRuleLine(k plandata.Key, e ast.Expr, action ast.ActionExpr, operator bool, l *core.OpLine) string {
	s := check.NewScope()
	var locals []string
	if operator {
		locals = []string{"lhs", "rhs", "op"}
	}
	// Lines whose value is unused are matched without values, as in the engine; so are all lines
	// of a rule without a value (in the recognizer).
	build := !g.Cur.Novalue && !check.LeanLine(action, locals)
	m := g.operatorsRuleBody(k, s, build)
	if m == "" {
		g.Unprintable("a line of an Operators Rule of rule " + g.Cur.Name)
	}
	act := "nil"
	if action != nil && !g.Cur.Novalue {
		var locals map[string]string
		if operator {
			locals = map[string]string{"lhs": "c.Lhs", "rhs": "c.Rhs", "op": "c.Op"}
		}
		act = fmt.Sprintf("func(c *%s) (any, any) { %s }", g.ctxType(), g.actionBody(g.LineSite(l), s, locals))
	}
	return fmt.Sprintf("&%s{Scope: %s, M: %s, Action: %s, Value: %s}", g.pick("OperatorsRuleLine", "TOperatorsRuleLine"), goStrings(s.Names), m, act, g.shaper(e))
}

// ruleCall returns the call of the rule r from Operators Rule level min.
func (g *Generator) ruleCall(r *common.Rule, min int) string {
	_, operatorsRule := r.Def.Expr.(*ast.OperatorsRule)
	wrapper := g.callWrapper(g.Table, r)
	g.checkCall(r, wrapper)
	switch {
	case wrapper && r.Plain:
		// Never memoized and without captures: call does nothing but invokePlain, which a
		// method of its own does with a direct call of the body (see plainCalls).
		if g.plainCalled == nil {
			g.plainCalled = map[*common.Rule]bool{}
		}
		g.plainCalled[r] = true
		return g.ruleFunc(g.plainPrefix(), r.ID) + "(p)"
	case wrapper:
		// The typed library calls a method of the rule's own (typedCall).
		return g.ruleFunc("u", r.ID) + "(p)"
	case g.Document && g.Table == "rules":
		// Document parses route every call through call, which memoizes per rule.docMemo,
		// including otherwise plain rules.
	case g.directTypedDocument && g.typed() && operatorsRule:
	case r.Plain:
		return fmt.Sprintf("p.InvokePlain(%s[%d], %d)", g.tableVar(), r.ID, min)
	}
	return fmt.Sprintf("p.Call(%s[%d], %d)", g.tableVar(), r.ID, min)
}

// atEnd and atLineStart name the library methods that test the anchors; a parser without
// ParseStream uses the forms that need not consider a stream, which inline.
func (g *Generator) atEnd() string {
	if g.streamStart == nil {
		return g.helper("AtEndBatch")
	}
	return g.helper("AtEnd")
}

func (g *Generator) atLineStart() string {
	if g.streamStart == nil {
		return g.helper("AtLineStartBatch")
	}
	return g.helper("AtLineStart")
}

// helper names the library method that a generated execution function calls on its parser. A
// parser generated without budget controls and without Documents has no controlled form of its
// bodies, so they call the Ordinary form of the method.
func (g *Generator) helper(name string) string {
	if g.Logical && logicalHelperName(name) {
		return name + "Logical"
	}
	if g.Ordinary && ordinaryHelperName(name) {
		return name + "Ordinary"
	}
	if g.tracedRead(name) {
		return name + "Traced"
	}
	return name
}

// tracedRead reports whether a traced body calls the traced form of the read name: the Node
// rules, which a stream parse runs, read a stream through it so that only an observed parse
// records the examined end of a stream read.
func (g *Generator) tracedRead(name string) bool {
	return g.Traced && g.Table == "rules" && tracedHelperName(name)
}

// leanTyped reports whether typed rule bodies are emitted in their ordinary form only.
func (g *Generator) leanTyped() bool {
	return g.Ordinary && g.typed()
}

// classTable returns the name of the package variable that holds the membership table of e,
// declaring it on first use.
func (g *Generator) classTable(e *ast.CharClass) string {
	key := fmt.Sprintf("cls%v%#v", e.Negated, e.Ranges)
	if g.varNames == nil {
		g.varNames = map[string]string{}
	}
	if name := g.varNames[key]; name != "" {
		return name
	}
	name := g.Name("cls")
	g.varNames[key] = name
	ascii, wideRunes := classBits(e)
	var wide []string
	for _, r := range wideRunes {
		wide = append(wide, strconv.Itoa(int(r)))
	}
	fmt.Fprintf(&g.Vars, "var %s = Class{ASCII: [2]uint64{%#x, %#x}", name, ascii[0], ascii[1])
	if len(wide) > 0 {
		fmt.Fprintf(&g.Vars, ", Wide: []rune{%s}", strings.Join(wide, ", "))
	}
	if e.Negated {
		g.Vars.WriteString(", Neg: true")
	}
	g.Vars.WriteString("}\n")
	return name
}

// classSteps returns the name of the package variable that lists the class steps of items, which
// are character classes, declaring it on first use.
func (g *Generator) classSteps(items []ast.Expr) string {
	var steps []string
	for _, it := range items {
		c := it.(*ast.CharClass)
		steps = append(steps, fmt.Sprintf("{C: &%s, ID: %d}", g.classTable(c), g.Desc(core.CharClassString(c))))
	}
	body := strings.Join(steps, ", ")
	key := "steps" + body
	if name := g.varNames[key]; name != "" {
		return name
	}
	name := g.Name("steps")
	g.varNames[key] = name
	fmt.Fprintf(&g.Vars, "var %s = []ClassStep{%s}\n", name, body)
	return name
}

// classBits returns the membership tables of a character class as the library's Class holds
// them: the bitset of the matching characters below 128, with the negation applied, and the
// inclusive pairs of the ranges at or above 128 (a range that starts below is cut at 128).
func classBits(e *ast.CharClass) (ascii [2]uint64, wide []rune) {
	for c := rune(0); c < 128; c++ {
		in := false
		for _, r := range e.Ranges {
			in = in || (c >= r.Lo && c <= r.Hi)
		}
		if in != e.Negated {
			ascii[c>>6] |= 1 << (uint(c) & 63)
		}
	}
	for _, r := range e.Ranges {
		if r.Hi >= 128 {
			wide = append(wide, max(r.Lo, 128), r.Hi)
		}
	}
	return ascii, wide
}

// writeDescs gives the generated parser the table of expectation descriptions, which a parse
// records only when it passes WithExpectations, when the options ask for it.
func (g *Generator) writeDescs(opts common.GenOptions) {
	if !opts.Expectations {
		return
	}
	fmt.Fprintf(&g.Init, "\tsxirGrammar.Descs = %s\n", goStrings(g.Descs))
	g.Init.WriteString("\tsxirGrammar.Expectations = true\n")
}

// joinPrefixed returns the items separated by commas, with a comma before the first.
func joinPrefixed(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return ", " + strings.Join(items, ", ")
}

// writeRunKeys writes the calls that give the grammar g the keys of the run sites of prog that are
// not free of the environment, by site, as the slots of their variables.
func writeRunKeys(w *strings.Builder, g string, prog *common.Program) {
	keys := prog.Run.RunKeys
	for _, site := range slices.Sorted(maps.Keys(keys)) {
		k := keys[site]
		fmt.Fprintf(w, "\t%s.SetRunKey(%d, %s, %s)\n", g, site, goSlots(prog, k.Vars), goSlots(prog, k.Bind))
	}
}

// goSlots returns the Go expression of the slots of the variables names of prog, nil for none.
func goSlots(prog *common.Program, names []string) string {
	if len(names) == 0 {
		return "nil"
	}
	slots := make([]string, len(names))
	for i, n := range names {
		slots[i] = strconv.Itoa(prog.Vars.Index(n))
	}
	return "[]int{" + strings.Join(slots, ", ") + "}"
}

// typedDocumentDecl declares the Document of a parser with typed values: the methods of
// runtime.Document over the library's Document, and ParseValue, which the typed code adds. It holds
// the library's Document in an unexported field, so that the methods that are for generated parsers
// are not the API of the parser.
const typedDocumentDecl = `// Document is an incremental document: it parses its text again after edits, reusing the
// results of the parse before that it can. It implements runtime.Document, and ParseValue returns
// the typed value.
type Document struct{ doc *` + libAlias + `.Document }

var _ sxirruntime.Document = (*Document)(nil)

// Text returns the current text.
func (d *Document) Text() string { return d.doc.Text() }

// Unit returns the position unit of the document, fixed at construction.
func (d *Document) Unit() PositionUnit { return d.doc.Unit() }

// Edit replaces the range [start, end) of the text, in the document's position unit, with text.
func (d *Document) Edit(start, end int, text string) error { return d.doc.Edit(start, end, text) }

// Parse parses the current text. opts apply once over the construction defaults.
func (d *Document) Parse(opts ...ParseOption) (*Node, error) { return d.doc.Parse(opts...) }

// Stats returns the evaluation and reuse counts of the last Parse.
func (d *Document) Stats() ParseStats { return d.doc.Stats() }

// CacheStats inspects the retained incremental history without changing it.
func (d *Document) CacheStats() CacheStats { return d.doc.CacheStats() }

// ResetHistory discards the incremental reuse history while keeping the text, the construction
// options, and the statistics of the last parse.
func (d *Document) ResetHistory() { d.doc.ResetHistory() }

`
