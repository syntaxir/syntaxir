package main

// The timed benchmark results of SyntaxIR are measured and kept in the repository
// https://github.com/syntaxir/bench, which publishes them as GitHub releases tagged results-vN with
// two assets: RESULTS.md, the results page, and figures.json, the figures that the documents of
// this repository quote. site/benchmarks.json pins one release with the SHA-256 digests of both
// assets, and site/figures.json is the pinned release's figures.json. The site build downloads the
// pinned RESULTS.md, checks it against the pin and renders it as the page of docs/benchmarks.md;
// a test checks every quoted figure (<!-- fig:NAME -->...<!-- /fig -->) against site/figures.json.
// go run ./cmd/timed pin of the bench repository writes the pin, the figures and the quotes in one
// change.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ornew/zerosite/render"
	"github.com/ornew/zerosite/sitegen"
	"github.com/yuin/goldmark/ast"
)

// Repository paths of the benchmark results.
const (
	benchPinFile     = "site/benchmarks.json"
	benchFiguresFile = "site/figures.json"
	benchmarksSource = "docs/benchmarks.md" // the source path of the results page
)

// Assets of a results release.
const (
	benchResultsAsset = "RESULTS.md"
	benchFiguresAsset = "figures.json"
	benchResultsLimit = int64(4 << 20)
)

var benchTagPattern = regexp.MustCompile(`^results-v[1-9][0-9]*$`)

// benchPin names the results release that the site publishes and the digests of its assets.
type benchPin struct {
	Repository    string `json:"repository"`    // GitHub repository URL, https://github.com/<owner>/<name>
	Tag           string `json:"tag"`           // release tag, results-v<n>
	ResultsSHA256 string `json:"resultsSHA256"` // SHA-256 of RESULTS.md
	FiguresSHA256 string `json:"figuresSHA256"` // SHA-256 of figures.json, which site/figures.json copies
}

func (p benchPin) validate() error {
	if !githubRepoPattern.MatchString(p.Repository) {
		return fmt.Errorf("%s: repository %q is not a GitHub repository URL", benchPinFile, p.Repository)
	}
	if !benchTagPattern.MatchString(p.Tag) {
		return fmt.Errorf("%s: tag %q is not a results-v<n> tag", benchPinFile, p.Tag)
	}
	if !digestPattern.MatchString(p.ResultsSHA256) || !digestPattern.MatchString(p.FiguresSHA256) {
		return fmt.Errorf("%s: digests must be 64 lowercase hexadecimal characters", benchPinFile)
	}
	return nil
}

// releaseURL returns the page of the pinned release.
func (p benchPin) releaseURL() string { return p.Repository + "/releases/tag/" + p.Tag }

// downloadURL returns the URL from which GitHub serves the assets of the pinned release.
func (p benchPin) downloadURL() string { return p.Repository + "/releases/download/" + p.Tag + "/" }

// readBenchPin reads the pin of repo and checks that site/figures.json is the pinned figures.json.
func readBenchPin(repo string) (benchPin, error) {
	var p benchPin
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(benchPinFile)))
	if err != nil {
		return p, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("%s: %w", benchPinFile, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return p, fmt.Errorf("%s: trailing data", benchPinFile)
	}
	if err := p.validate(); err != nil {
		return p, err
	}
	figures, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(benchFiguresFile)))
	if err != nil {
		return p, err
	}
	if got := hashBytes(figures); got != p.FiguresSHA256 {
		return p, fmt.Errorf("%s has SHA-256 %s, but %s pins %s: write both with go run ./cmd/timed pin of %s", benchFiguresFile, got, benchPinFile, p.FiguresSHA256, p.Repository)
	}
	return p, nil
}

// fetchBenchResults downloads RESULTS.md of the pinned release from base (the pin's download URL
// unless a test replaces it) and returns it once it matches the pinned digest.
func fetchBenchResults(pin benchPin, base string) ([]byte, error) {
	if base == "" {
		base = pin.downloadURL()
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	data, code, err := fetchAsset(client, u, benchResultsAsset, benchResultsLimit)
	if err != nil {
		return nil, fmt.Errorf("benchmark results %s: %w (build with -benchmarks=false to link to the release instead)", pin.Tag, err)
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("benchmark results %s has no %s (HTTP %d)", pin.Tag, benchResultsAsset, code)
	}
	if got := hashBytes(data); got != pin.ResultsSHA256 {
		return nil, fmt.Errorf("benchmark results %s: %s has SHA-256 %s, but %s pins %s", pin.Tag, benchResultsAsset, got, benchPinFile, pin.ResultsSHA256)
	}
	return data, nil
}

// benchmarksMarkdown returns the Markdown of the results page from the release's RESULTS.md, whose
// relative links name files of the bench repository at the release's tag.
func benchmarksMarkdown(pin benchPin, results []byte) []byte {
	base := pin.Repository + "/blob/" + pin.Tag + "/"
	return markdownLink.ReplaceAllFunc(results, func(m []byte) []byte {
		target := string(markdownLink.FindSubmatch(m)[1])
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") || strings.HasPrefix(target, "mailto:") {
			return m
		}
		return []byte("](" + base + target + ")")
	})
}

// markdownLink matches the target of an inline Markdown link.
var markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// figureMarker matches a quoted figure of a document.
var figureMarker = regexp.MustCompile(`<!-- fig:([a-z0-9-]+) -->(.*?)<!-- /fig -->`)

// readBenchFigures returns the figures of site/figures.json.
func readBenchFigures(repo string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(benchFiguresFile)))
	if err != nil {
		return nil, err
	}
	var f struct {
		Figures map[string]string `json:"figures"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", benchFiguresFile, err)
	}
	if len(f.Figures) == 0 {
		return nil, fmt.Errorf("%s lists no figures", benchFiguresFile)
	}
	return f.Figures, nil
}

// staleFigures returns, for each document of repo that quotes figures (README.md and the Markdown
// files of docs/), the quotes whose text is not the value of their figure in figures, or that name
// no figure.
func staleFigures(repo string, figures map[string]string) ([]string, error) {
	docs := []string{"README.md"}
	err := filepath.WalkDir(filepath.Join(repo, "docs"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".md") {
			rel, _ := filepath.Rel(repo, path)
			docs = append(docs, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	var stale []string
	for _, doc := range docs {
		data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(doc)))
		if err != nil {
			return nil, err
		}
		for _, m := range figureMarker.FindAllStringSubmatch(string(data), -1) {
			want, ok := figures[m[1]]
			switch {
			case !ok:
				stale = append(stale, fmt.Sprintf("%s: figure %s is not in %s", doc, m[1], benchFiguresFile))
			case m[2] != want:
				stale = append(stale, fmt.Sprintf("%s: figure %s quotes %q, but %s gives %q", doc, m[1], m[2], benchFiguresFile, want))
			}
		}
	}
	return stale, nil
}

// addBenchmarks renders, as the page of docs/benchmarks.md, the RESULTS.md of the pinned release
// when the build renders the benchmarks. docs/benchmarks.md itself only names the bench repository
// and its releases, which is what a build without the release shows: the sections of the results
// are then unknown, so that build checks no link to a section of the page (here for the links of
// the sources, and in withoutResultsSections for those of the output), and the build that renders
// the results checks them all.
func (s *Site) addBenchmarks(ctx *sitegen.Context, mapped map[*sitegen.Page]*Page) error {
	pin, err := readBenchPin(s.cfg.Repo)
	if err != nil {
		return err
	}
	var q *sitegen.Page
	for _, page := range ctx.Pages {
		if page.Source == benchmarksSource {
			q = page
		}
	}
	if q == nil {
		return fmt.Errorf("%s is not a page of the site", benchmarksSource)
	}
	if !s.cfg.Benchmarks {
		for _, a := range benchmarksAnchorsLinked(ctx.Pages) {
			q.Anchors[a] = true
		}
		return nil
	}
	results, err := fetchBenchResults(pin, s.cfg.benchURL)
	if err != nil {
		return err
	}
	d, body, err := renderBenchmarks(pin, results)
	if err != nil {
		return err
	}
	p := mapped[q]
	p.TOC = d.TOC(2, 3)
	p.SourceURL = pin.Repository + "/blob/" + pin.Tag + "/" + benchResultsAsset
	q.RenderedHTML = []byte(body)
	q.Headings = nil
	for _, h := range d.Headings {
		q.Headings = append(q.Headings, sitegen.Heading{ID: h.ID, Title: h.Text, Level: h.Level})
	}
	q.Anchors = map[string]bool{}
	for _, m := range generatedAnchor.FindAllStringSubmatch(body, -1) {
		q.Anchors[html.UnescapeString(m[1])] = true
	}
	return nil
}

// renderBenchmarks renders the results page from the release's RESULTS.md with the Markdown
// options of site/config.json, so that the page gets the heading ids, alerts, heading links and
// formulas of a document of the repository.
func renderBenchmarks(pin benchPin, results []byte) (*render.Document, string, error) {
	d := render.Parse(benchmarksMarkdown(pin, results), render.Options{RawHTML: true, AlignAttributes: true, Alerts: true, HeadingLinks: true, Math: true})
	body, err := d.Render(nil)
	if err != nil {
		return nil, "", fmt.Errorf("%s of %s: %w", benchResultsAsset, pin.Tag, err)
	}
	return d, body, nil
}

// generatedAnchor matches the id of an element of rendered HTML, as sitegen finds the anchors of a
// generated page.
var generatedAnchor = regexp.MustCompile(`(?:^|[\s<])id="([^"]+)"`)

// benchmarksAnchorLink matches a link to a section of the results page.
var benchmarksAnchorLink = regexp.MustCompile(`(?:^|/)benchmarks\.md#(.+)$`)

// benchmarksAnchorsLinked returns the sections of the results page that the links of the pages'
// Markdown, the links that the site checks, name.
func benchmarksAnchorsLinked(pages []*sitegen.Page) []string {
	var anchors []string
	for _, page := range pages {
		if page.Document == nil {
			continue
		}
		ast.Walk(page.Document, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if l, ok := n.(*ast.Link); ok && entering {
				if m := benchmarksAnchorLink.FindStringSubmatch(string(l.Destination)); m != nil {
					anchors = append(anchors, m[1])
				}
			}
			return ast.WalkContinue, nil
		})
	}
	return anchors
}

// missingAnchor matches the report of the output's link check for a link to a missing section.
var missingAnchor = regexp.MustCompile(`^(\S+): link (\S+)#\S* to a missing anchor$`)

// withoutResultsSections drops, from the problems of the output's link check, the links to a
// section of the results page, which a build without the results release cannot check.
func withoutResultsSections(problems []string, results *Page) []string {
	if results == nil {
		return problems
	}
	page := results.URL + "index.html"
	var kept []string
	for _, p := range problems {
		if m := missingAnchor.FindStringSubmatch(p); m != nil {
			target := m[2]
			if target == "" || strings.HasSuffix(target, "/") {
				target += "index.html"
			}
			if path.Join(path.Dir(m[1]), target) == page {
				continue
			}
		}
		kept = append(kept, p)
	}
	return kept
}
