package main

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// The landing page is site/templates/landing.html. It shares two things with README.md, so that the
// site and the repository make the same promise with the same example: the tagline (the bold or
// emphasized line under the README's title) and the grammar of the live example (the README's first
// sxir code block, with the -i input of its first bash block as the example's default input). The
// example runs on the playground's WebAssembly build of the engine (site/static/assets/landing.js).

// landingDescription is the lead of the landing page and its meta description.
const landingDescription = "Write one typed grammar. SyntaxIR compiles it through a multi-stage IR into a parser for any " +
	"target: generated code in your language (Go and TypeScript today), a bytecode VM or WebAssembly in the browser, " +
	"with cost analysis, compute limits and a full language toolchain."

// landingData is the data of the landing template.
type landingData struct {
	Tagline     string
	Phrases     []string // the phrases of the tagline, each kept on one line
	Description string
	Grammar     string       // grammar of the live example
	Input       string       // its default input
	Tools       []string     // the tools of the comparison, SyntaxIR first
	Compare     []compareRow // the rows of the comparison
}

// A compareRow is a capability in the comparison table, with one cell per tool.
type compareRow struct {
	Capability string
	Cells      []compareCell
}

// A compareCell is a tool's support for a capability. Kind is "pos" (a strength), "neg" (missing or
// weak) or "" (neither); the page marks it with an icon and a hidden word as well as a color. Rating,
// when not zero, is a speed rating from 1 to 5 (see speedRating) shown instead of Kind's icon.
type compareCell struct {
	Kind   string
	Text   template.HTML
	Rating int
}

// speedRating rates generated Go typed ParseValue against Go's hand-written standard-library parsers on
// the five workloads of docs/why-syntaxir.md (Measured Today): the geometric mean of the ratios 1.1,
// 1.3, 3.1, 3.4 and 5.4 is 2.4, which is within 4x (3 of 5). The bands: 5 as fast or faster, 4 within
// 2x, 3 within 4x, 2 within 8x, 1 slower. The landing page's footnote states the same basis.
const speedRating = 3

// compareTools and compareRows are the comparison table of the landing page. The facts about other
// tools are those of docs/why-syntaxir.md (How SyntaxIR Compares), checked against the versions it
// names, and of their documented APIs (tree-sitter's api.h, ANTLR's Parser, pigeon's and participle's
// options). No benchmark compares SyntaxIR with them, so their speed is "not measured".
var compareTools = []string{"SyntaxIR", "Tree-sitter", "ANTLR 4", "Participle / pigeon"}

var compareRows = []compareRow{
	{"Target languages", []compareCell{
		{Kind: "pos", Text: "Any target language from one grammar, as generated code, a bytecode VM or WebAssembly"},
		{Kind: "pos", Text: "C runtime with bindings for Go, Rust, Node, Wasm and others"},
		{Kind: "pos", Text: "Ten targets; the tool needs Java"},
		{Kind: "neg", Text: "Go only"},
	}},
	{"Speed", []compareCell{
		{Rating: speedRating, Text: "Measured against hand-written parsers"},
		{Text: "Not measured"},
		{Text: "Not measured"},
		{Text: "Not measured"},
	}},
	{"Typed output", []compareCell{
		{Kind: "pos", Text: "The grammar's types as the target language's own types"},
		{Kind: "neg", Text: "Dynamic nodes"},
		{Text: "Generated context classes per rule"},
		{Text: "participle: your Go structs; pigeon: <code>any</code>"},
	}},
	{"Incremental parsing", []compareCell{
		{Kind: "pos", Text: "A <code>Document</code> reuses every rule result an edit did not touch, with no annotations"},
		{Kind: "pos", Text: "Subtree reuse for any grammar, with changed ranges"},
		{Kind: "neg", Text: "No"},
		{Kind: "neg", Text: "No"},
	}},
	{"Error recovery", []compareCell{
		{Kind: "pos", Text: "Declared <code>#recover</code> rules keep a tree through errors"},
		{Kind: "pos", Text: "Automatic; a tree for every input"},
		{Kind: "pos", Text: "Automatic token insertion and deletion, then resynchronization"},
		{Text: "participle: a partial tree up to the error; pigeon: recovery expressions"},
	}},
	{"Streaming", []compareCell{
		{Kind: "pos", Text: "<code>#stream</code> delivers elements as input arrives, in a bounded working set"},
		{Text: "Input read in chunks; the whole tree is built"},
		{Text: "Unbuffered streams in the Java runtime"},
		{Kind: "neg", Text: "participle: no; pigeon: not documented"},
	}},
	{"Compute and memory limits", []compareCell{
		{Kind: "pos", Text: "Compute and nesting limits and cancellation, on by default"},
		{Text: "A progress callback for cancellation"},
		{Kind: "neg", Text: "Not documented"},
		{Text: "pigeon: an expression count limit; participle: not documented"},
	}},
	{"Complexity classification", []compareCell{
		{Kind: "pos", Text: "<code>sxir lint -classify</code> proves linear cost, or names the growth and its cause"},
		{Kind: "neg", Text: "Not documented"},
		{Kind: "neg", Text: "Not documented per grammar; O(n⁴) in theory"},
		{Kind: "neg", Text: "Not documented; pigeon is exponential without memoization"},
	}},
	{"Language server (LSP)", []compareCell{
		{Kind: "pos", Text: "For grammars: diagnostics, completion, hover, go to definition, references, rename and formatting"},
		{Text: "Highlight, injection and tag queries for editors"},
		{Text: "IDE plugins for <code>.g4</code> grammars"},
		{Kind: "neg", Text: "None"},
	}},
	{"Profiling and observation", []compareCell{
		{Kind: "pos", Text: "<code>trace</code>, <code>profile</code> and <code>explain</code>: every rule call, the cost per rule and the cause of an error"},
		{Text: "A parse logger and DOT graphs"},
		{Text: "Parse tracing; decision profiling in the Java runtime"},
		{Text: "Debug tracing; pigeon also counts statistics"},
	}},
	{"Grammar testing", []compareCell{
		{Kind: "pos", Text: "Input sampling, edit fuzzing against fresh parses and fixture tests"},
		{Kind: "pos", Text: "Corpus tests; <code>tree-sitter fuzz</code> tests random edits"},
		{Kind: "neg", Text: "Not documented"},
		{Kind: "neg", Text: "Not documented"},
	}},
	{"Runtime dependencies", []compareCell{
		{Kind: "pos", Text: "Only SyntaxIR's own runtime library: no engine, no cgo, no third-party packages"},
		{Kind: "neg", Text: "A C runtime; the Go binding needs cgo"},
		{Text: "A runtime library per target"},
		{Kind: "pos", Text: "Pure Go"},
	}},
}

var inputRe = regexp.MustCompile(`-i '([^']*)'`)

func (s *Site) renderLanding() error {
	p := s.bySrc["README.md"]
	src, err := os.ReadFile(filepath.Join(s.cfg.Repo, "README.md"))
	if err != nil {
		return err
	}
	m := s.parseMarkdown(p, src)
	d := landingData{Description: landingDescription, Tools: compareTools, Compare: compareRows}
	for n := m.doc.FirstChild(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok && h.Level == 2 {
			break // the tagline and the example come before the first section
		}
		switch n := n.(type) {
		case *ast.Paragraph:
			if d.Tagline == "" && n.ChildCount() == 1 && (n.FirstChild().Kind() == ast.KindEmphasis) {
				d.Tagline = m.text(n)
			}
		case *ast.FencedCodeBlock:
			var code strings.Builder
			for i := 0; i < n.Lines().Len(); i++ {
				seg := n.Lines().At(i)
				code.Write(seg.Value(m.src))
			}
			switch string(n.Language(m.src)) {
			case "sxir":
				if d.Grammar == "" {
					d.Grammar = code.String()
				}
			case "bash":
				if mm := inputRe.FindStringSubmatch(code.String()); mm != nil && d.Input == "" {
					d.Input = mm[1]
				}
			}
		}
	}
	if d.Tagline == "" || d.Grammar == "" || d.Input == "" {
		return fmt.Errorf("README.md: before its first section, the landing page needs the tagline (a bold line), an sxir code block and a bash block with -i '...'")
	}
	d.Phrases = taglinePhrases(d.Tagline)

	tmpl, err := template.New("landing.html").Funcs(template.FuncMap{
		// ratingSlots lists the five icons of a rating: true for each one that is on.
		"ratingSlots": func(n int) []bool {
			slots := make([]bool, 5)
			for i := range slots {
				slots[i] = i < n
			}
			return slots
		},
	}).ParseFS(templateFS, "templates/landing.html")
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, d); err != nil {
		return err
	}
	p.Title = "SyntaxIR: " + d.Tagline
	p.Description = d.Description
	p.Body = template.HTML(b.String())
	p.Styles = []string{"assets/landing.css"}
	p.Scripts = []string{"assets/landing.js"}
	return nil
}

// taglinePhrases splits a tagline after each comma or sentence, so that "One Grammar, Every Parser."
// breaks between its two phrases rather than inside one.
func taglinePhrases(t string) []string {
	var out []string
	start := 0
	for i := 0; i < len(t); i++ {
		if (t[i] == ',' || t[i] == '.') && i+1 < len(t) && t[i+1] == ' ' {
			out = append(out, strings.TrimSpace(t[start:i+1]))
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(t[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}
