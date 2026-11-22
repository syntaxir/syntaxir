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
	Grammar     string // grammar of the live example
	Input       string // its default input
}

var inputRe = regexp.MustCompile(`-i '([^']*)'`)

func (s *Site) renderLanding() error {
	p := s.bySrc["README.md"]
	src, err := os.ReadFile(filepath.Join(s.cfg.Repo, "README.md"))
	if err != nil {
		return err
	}
	m := s.parseMarkdown(p, src)
	d := landingData{Description: landingDescription}
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

	tmpl, err := template.ParseFS(templateFS, "templates/landing.html")
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
