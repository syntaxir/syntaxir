package main

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:generate go run . readme

// README.md shows the landing page's architecture diagram under its description. The diagram is
// generated from the landing template (templates/landing.html), so the two cannot drift: "go generate"
// in site rewrites the README's block between
//
//	<!-- BEGIN landing:diagram ... -->
//	<!-- END landing:diagram -->
//
// and the diagram's SVG files in site/readme, and TestReadmeUpToDate fails when either is stale.
//
// GitHub does not render inline SVG, so the diagram becomes two standalone files, one per color
// scheme, which the README picks with <picture> and prefers-color-scheme. Each file embeds the diagram
// style sheet (static/assets/diagram.css) under the site's tokens (the :root block of
// static/assets/site.css) with light-dark() resolved for its scheme. The site's web fonts are not
// embedded; the token's fallback fonts draw the text.

// readmeAssets is the directory of the generated diagrams, relative to the repository.
const readmeAssets = "site/readme"

// readmeOutput is the generated README and the diagram files it shows, by path relative to the
// repository.
type readmeOutput struct {
	readme []byte
	files  map[string][]byte
}

// readmeCmd is "site readme": it regenerates the README's landing blocks and diagrams.
func readmeCmd(args []string) error {
	fs := flag.NewFlagSet("readme", flag.ContinueOnError)
	repo := fs.String("repo", "..", "root of the SyntaxIR repository")
	if err := fs.Parse(args); err != nil {
		return err
	}
	out, err := generateReadme(*repo)
	if err != nil {
		return err
	}
	dir := filepath.Join(*repo, filepath.FromSlash(readmeAssets))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stale, err := staleReadmeAssets(*repo, out)
	if err != nil {
		return err
	}
	for _, p := range stale {
		if err := os.Remove(filepath.Join(*repo, filepath.FromSlash(p))); err != nil {
			return err
		}
	}
	for p, data := range out.files {
		if err := os.WriteFile(filepath.Join(*repo, filepath.FromSlash(p)), data, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(*repo, "README.md"), out.readme, 0o644)
}

// staleReadmeAssets lists the files of the diagram directory that the generation no longer writes.
func staleReadmeAssets(repo string, out *readmeOutput) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(repo, filepath.FromSlash(readmeAssets)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var stale []string
	for _, e := range entries {
		p := readmeAssets + "/" + e.Name()
		if _, ok := out.files[p]; !ok {
			stale = append(stale, p)
		}
	}
	return stale, nil
}

// generateReadme returns README.md with its landing blocks generated, and the diagram files.
func generateReadme(repo string) (*readmeOutput, error) {
	body, err := executeLanding(newLandingData())
	if err != nil {
		return nil, err
	}
	root, err := parseHTML(body)
	if err != nil {
		return nil, fmt.Errorf("landing page: %w", err)
	}
	styles, err := diagramStyles(repo)
	if err != nil {
		return nil, err
	}
	g := &readmeGen{repo: repo, src: body, styles: styles, files: map[string][]byte{}}
	blocks := map[string]string{}
	if blocks["diagram"], err = g.diagramBlock(root); err != nil {
		return nil, err
	}
	src, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		return nil, err
	}
	readme, err := replaceBlocks(src, blocks)
	if err != nil {
		return nil, fmt.Errorf("README.md: %w", err)
	}
	return &readmeOutput{readme: readme, files: g.files}, nil
}

var blockRe = regexp.MustCompile(`(?s)(<!-- BEGIN landing:([a-z]+) [^\n]*-->\n).*?(<!-- END landing:([a-z]+) -->)`)

// replaceBlocks puts each block's content between its markers; every block must appear once.
func replaceBlocks(src []byte, blocks map[string]string) ([]byte, error) {
	seen := map[string]bool{}
	var err error
	out := blockRe.ReplaceAllFunc(src, func(m []byte) []byte {
		sm := blockRe.FindSubmatch(m)
		name := string(sm[2])
		content, ok := blocks[name]
		switch {
		case string(sm[4]) != name:
			err = fmt.Errorf("the block landing:%s ends with landing:%s", name, sm[4])
		case !ok:
			err = fmt.Errorf("unknown block landing:%s", name)
		case seen[name]:
			err = fmt.Errorf("the block landing:%s appears twice", name)
		}
		seen[name] = true
		return []byte(string(sm[1]) + "\n" + content + "\n\n" + string(sm[3]))
	})
	if err != nil {
		return nil, err
	}
	for name := range blocks {
		if !seen[name] {
			return nil, fmt.Errorf("missing the block <!-- BEGIN landing:%s ... --> ... <!-- END landing:%s -->", name, name)
		}
	}
	return out, nil
}

type readmeGen struct {
	repo   string
	src    []byte            // the rendered landing page
	styles map[string]string // the style sheet of a standalone diagram, by color scheme
	files  map[string][]byte
}

// diagramBlock is the architecture diagram under the README's description.
func (g *readmeGen) diagramBlock(root *hnode) (string, error) {
	fig := root.find(func(n *hnode) bool { return n.hasClass("lp-arch") })
	if fig == nil {
		return "", errors.New("landing page: no architecture figure (class lp-arch)")
	}
	return g.picture(fig, "architecture")
}

// picture writes the light and dark files of the figure's diagram (its wide form when it has two) and
// returns the <picture> that shows them.
func (g *readmeGen) picture(fig *hnode, name string) (string, error) {
	svgs := fig.findAll(func(n *hnode) bool { return n.name == "svg" })
	var svg *hnode
	for _, s := range svgs {
		if s.hasClass("lp-wide") || (len(svgs) == 1 && !s.hasClass("lp-narrow")) {
			svg = s
		}
	}
	if svg == nil {
		return "", fmt.Errorf("landing page: the figure of %s has no diagram", name)
	}
	viewBox := strings.Fields(svg.attr["viewBox"])
	label := svg.attr["aria-label"]
	if len(viewBox) != 4 || label == "" {
		return "", fmt.Errorf("landing page: the diagram of %s needs a viewBox and an aria-label", name)
	}
	paths := map[string]string{}
	for _, scheme := range []string{"light", "dark"} {
		var b bytes.Buffer
		fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%s" height="%s" viewBox="%s" role="img" aria-label="%s">`,
			viewBox[2], viewBox[3], strings.Join(viewBox, " "), html.EscapeString(label))
		b.WriteString("\n<style>\n" + g.styles[scheme] + "</style>")
		inner, err := g.embedImages(g.src[svg.inner:svg.innerEnd], scheme)
		if err != nil {
			return "", fmt.Errorf("the diagram of %s: %w", name, err)
		}
		b.Write(inner)
		b.WriteString("</svg>\n")
		if err := checkXML(b.Bytes()); err != nil {
			return "", fmt.Errorf("the %s diagram of %s: %w", scheme, name, err)
		}
		p := readmeAssets + "/" + name + "-" + scheme + ".svg"
		if _, dup := g.files[p]; dup {
			return "", fmt.Errorf("two diagrams are named %s", name)
		}
		g.files[p] = b.Bytes()
		paths[scheme] = p
	}
	return fmt.Sprintf("<picture>\n  <source media=\"(prefers-color-scheme: dark)\" srcset=\"%s\">\n  <img src=\"%s\" alt=\"%s\" width=\"%s\">\n</picture>",
		paths["dark"], paths["light"], html.EscapeString(label), viewBox[2]), nil
}

// embedImages makes a diagram's images part of its file, since an SVG shown as an image loads nothing
// else: an image of the other color scheme (class only-light or only-dark) is left out, and the site
// asset of each other image becomes a data URL.
func (g *readmeGen) embedImages(svg []byte, scheme string) ([]byte, error) {
	var err error
	out := imageRe.ReplaceAllFunc(svg, func(tag []byte) []byte {
		class := attrOf(tag, "class")
		if strings.Contains(" "+class+" ", " only-light ") && scheme != "light" ||
			strings.Contains(" "+class+" ", " only-dark ") && scheme != "dark" {
			return nil
		}
		href := attrOf(tag, "href")
		if !strings.HasPrefix(href, "assets/") || filepath.Ext(href) != ".svg" {
			err = fmt.Errorf("image %q is not an SVG asset of the site", href)
			return tag
		}
		data, rerr := os.ReadFile(filepath.Join(g.repo, "site", "static", filepath.FromSlash(href)))
		if rerr != nil {
			err = rerr
			return tag
		}
		url := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(data)
		return bytes.Replace(tag, []byte(`href="`+href+`"`), []byte(`href="`+url+`"`), 1)
	})
	return out, err
}

var imageRe = regexp.MustCompile(`<image\b[^>]*>`)

// attrOf returns the value of a double-quoted attribute of a tag.
func attrOf(tag []byte, name string) string {
	m := regexp.MustCompile(`\s` + name + `="([^"]*)"`).FindSubmatch(tag)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// diagramStyles returns the style sheet of a standalone diagram for each color scheme: the tokens of
// site.css that diagram.css uses, resolved for the scheme, and diagram.css itself.
func diagramStyles(repo string) (map[string]string, error) {
	siteCSS, err := os.ReadFile(filepath.Join(repo, "site", "static", "assets", "site.css"))
	if err != nil {
		return nil, err
	}
	diagramCSS, err := os.ReadFile(filepath.Join(repo, "site", "static", "assets", "diagram.css"))
	if err != nil {
		return nil, err
	}
	tokens, err := rootTokens(string(siteCSS))
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	var use func(css string)
	use = func(css string) {
		for _, m := range varRe.FindAllStringSubmatch(css, -1) {
			if v, ok := tokens[m[1]]; ok && !used[m[1]] {
				used[m[1]] = true
				use(v)
			}
		}
	}
	use(string(diagramCSS))
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	out := map[string]string{}
	for i, scheme := range []string{"light", "dark"} {
		var b strings.Builder
		b.WriteString(":root {\n")
		for _, name := range names {
			v, err := resolveLightDark(tokens[name], i)
			if err != nil {
				return nil, fmt.Errorf("site.css: --%s: %w", name, err)
			}
			fmt.Fprintf(&b, "  --%s: %s;\n", name, v)
		}
		b.WriteString("}\n")
		b.WriteString(commentRe.ReplaceAllString(string(diagramCSS), ""))
		out[scheme] = blankLinesRe.ReplaceAllString(b.String(), "\n")
	}
	return out, nil
}

var (
	varRe        = regexp.MustCompile(`var\(--([\w-]+)`)
	commentRe    = regexp.MustCompile(`(?s)/\*.*?\*/`)
	blankLinesRe = regexp.MustCompile(`\n\s*\n`)
	declRe       = regexp.MustCompile(`--([\w-]+)\s*:\s*([^;]+);`)
)

// rootTokens returns the custom properties of the first :root rule of a style sheet.
func rootTokens(css string) (map[string]string, error) {
	css = commentRe.ReplaceAllString(css, "")
	start := strings.Index(css, ":root {")
	if start < 0 {
		return nil, errors.New("site.css: no :root rule")
	}
	end := strings.Index(css[start:], "}")
	if end < 0 {
		return nil, errors.New("site.css: the :root rule does not end")
	}
	tokens := map[string]string{}
	for _, m := range declRe.FindAllStringSubmatch(css[start:start+end], -1) {
		tokens[m[1]] = strings.TrimSpace(m[2])
	}
	return tokens, nil
}

// resolveLightDark replaces each light-dark(a, b) of a value with its argument for the scheme: 0 for
// light, 1 for dark.
func resolveLightDark(v string, scheme int) (string, error) {
	const fn = "light-dark("
	for {
		i := strings.Index(v, fn)
		if i < 0 {
			return v, nil
		}
		depth, comma, end := 0, -1, -1
		for j := i + len(fn); j < len(v) && end < 0; j++ {
			switch v[j] {
			case '(':
				depth++
			case ')':
				if depth == 0 {
					end = j
				}
				depth--
			case ',':
				if depth == 0 && comma < 0 {
					comma = j
				}
			}
		}
		if comma < 0 || end < 0 {
			return "", fmt.Errorf("malformed light-dark() in %q", v)
		}
		args := []string{v[i+len(fn) : comma], v[comma+1 : end]}
		v = v[:i] + strings.TrimSpace(args[scheme]) + v[end+1:]
	}
}

// checkXML reports whether data is well-formed XML, as a standalone SVG file must be.
func checkXML(data []byte) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		if _, err := d.Token(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}
}

// An hnode is an element of a parsed HTML page. The offsets locate its content in the source.
type hnode struct {
	name  string
	attr  map[string]string
	kids  []*hnode // the child elements
	inner int64    // the start of the content, after the start tag
	// innerEnd is the end of the content, before the end tag.
	innerEnd int64
}

// parseHTML parses the HTML that the site's templates write. It is not a general HTML parser: it reads
// the page as lenient XML with HTML's void elements and entities, which those templates satisfy.
func parseHTML(src []byte) (*hnode, error) {
	d := xml.NewDecoder(bytes.NewReader(src))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	root := &hnode{name: "#root"}
	stack := []*hnode{root}
	for {
		off := d.InputOffset()
		tok, err := d.Token()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			n := &hnode{name: t.Name.Local, attr: map[string]string{}, inner: d.InputOffset()}
			for _, a := range t.Attr {
				n.attr[a.Name.Local] = a.Value
			}
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 1 {
				return nil, fmt.Errorf("unexpected </%s>", t.Name.Local)
			}
			top.innerEnd = off
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 1 {
		return nil, fmt.Errorf("<%s> does not end", stack[len(stack)-1].name)
	}
	return root, nil
}

func (n *hnode) hasClass(c string) bool {
	return n != nil && strings.Contains(" "+n.attr["class"]+" ", " "+c+" ")
}

// find returns the first descendant of n, in document order, for which ok holds.
func (n *hnode) find(ok func(*hnode) bool) *hnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if ok(k) {
			return k
		}
		if f := k.find(ok); f != nil {
			return f
		}
	}
	return nil
}

// findAll returns the descendants of n for which ok holds, in document order, without those nested
// in a match.
func (n *hnode) findAll(ok func(*hnode) bool) []*hnode {
	var out []*hnode
	for _, k := range n.kids {
		if ok(k) {
			out = append(out, k)
		} else {
			out = append(out, k.findAll(ok)...)
		}
	}
	return out
}
