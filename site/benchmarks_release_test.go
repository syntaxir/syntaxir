package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/zerosite/sitegen"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/text"
)

// TestBenchPin requires the pin of the repository to be valid and site/figures.json to be the
// pinned release's figures.json.
func TestBenchPin(t *testing.T) {
	if _, err := readBenchPin(".."); err != nil {
		t.Fatal(err)
	}
}

// TestBenchFiguresQuoted requires every figure that README.md and docs/ quote to be the value of
// the figure in site/figures.json, and every quote to name a figure.
func TestBenchFiguresQuoted(t *testing.T) {
	figures, err := readBenchFigures("..")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := staleFigures("..", figures)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stale {
		t.Error(s)
	}
}

// TestStaleFigures checks the reports of staleFigures on a repository of its own.
func TestStaleFigures(t *testing.T) {
	repo := t.TempDir()
	put := func(path, text string) {
		full := filepath.Join(repo, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("README.md", "It takes <!-- fig:a -->1.00×<!-- /fig --> and <!-- fig:b -->old<!-- /fig -->.\n")
	put("docs/guide/x.md", "Same <!-- fig:a -->1.00×<!-- /fig -->, unknown <!-- fig:c -->1<!-- /fig -->.\n")
	put("docs/notes.txt", "<!-- fig:c -->ignored<!-- /fig -->\n")
	stale, err := staleFigures(repo, map[string]string{"a": "1.00×", "b": "0.5–0.7×"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`README.md: figure b quotes "old", but site/figures.json gives "0.5–0.7×"`,
		"docs/guide/x.md: figure c is not in site/figures.json",
	}
	if !slices.Equal(stale, want) {
		t.Errorf("stale figures:\n%s\nwant:\n%s", strings.Join(stale, "\n"), strings.Join(want, "\n"))
	}
}

func TestFetchBenchResults(t *testing.T) {
	results := []byte("# Benchmark Results\n")
	pin := benchPin{Repository: "https://github.com/syntaxir/bench", Tag: "results-v1", ResultsSHA256: hashBytes(results), FiguresSHA256: hashBytes(nil)}
	serve := func(body []byte) string {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/releases/download/results-v1/RESULTS.md" || body == nil {
				http.NotFound(w, r)
				return
			}
			w.Write(body)
		}))
		t.Cleanup(s.Close)
		return s.URL + "/releases/download/results-v1/"
	}
	got, err := fetchBenchResults(pin, serve(results))
	if err != nil || string(got) != string(results) {
		t.Fatalf("fetchBenchResults = %q, %v; want the pinned file", got, err)
	}
	if _, err := fetchBenchResults(pin, serve([]byte("# Other\n"))); err == nil || !strings.Contains(err.Error(), "pins") {
		t.Errorf("a file that does not match the pin: err = %v", err)
	}
	if _, err := fetchBenchResults(pin, serve(nil)); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("a release without the file: err = %v", err)
	}
	if _, err := fetchBenchResults(pin, "http://127.0.0.1:1/"); err == nil || !strings.Contains(err.Error(), "-benchmarks=false") {
		t.Errorf("an unreachable release: err = %v, want the offline hint", err)
	}
}

func TestReadBenchPin(t *testing.T) {
	figures := []byte(`{"figures": {"a": "1"}}`)
	for _, tc := range []struct {
		name, pin string
		ok        bool
	}{
		{"valid", `{"repository": "https://github.com/syntaxir/bench", "tag": "results-v2", "resultsSHA256": "` + strings.Repeat("a", 64) + `", "figuresSHA256": "` + hashBytes(figures) + `"}`, true},
		{"figures of another release", `{"repository": "https://github.com/syntaxir/bench", "tag": "results-v2", "resultsSHA256": "` + strings.Repeat("a", 64) + `", "figuresSHA256": "` + strings.Repeat("b", 64) + `"}`, false},
		{"tag", `{"repository": "https://github.com/syntaxir/bench", "tag": "v2", "resultsSHA256": "` + strings.Repeat("a", 64) + `", "figuresSHA256": "` + hashBytes(figures) + `"}`, false},
		{"unknown field", `{"repository": "https://github.com/syntaxir/bench", "tag": "results-v2", "resultsSHA256": "` + strings.Repeat("a", 64) + `", "figuresSHA256": "` + hashBytes(figures) + `", "extra": 1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			os.MkdirAll(filepath.Join(repo, "site"), 0o755)
			os.WriteFile(filepath.Join(repo, "site", "benchmarks.json"), []byte(tc.pin), 0o644)
			os.WriteFile(filepath.Join(repo, "site", "figures.json"), figures, 0o644)
			if _, err := readBenchPin(repo); (err == nil) != tc.ok {
				t.Errorf("readBenchPin: err = %v, want ok %v", err, tc.ok)
			}
		})
	}
}

func TestBenchmarksMarkdown(t *testing.T) {
	pin := benchPin{Repository: "https://github.com/syntaxir/bench", Tag: "results-v3"}
	got := string(benchmarksMarkdown(pin, []byte("See [raw](results/results.txt), [doc](https://example.com/a.md) and [here](#run).\n")))
	want := "See [raw](https://github.com/syntaxir/bench/blob/results-v3/results/results.txt), [doc](https://example.com/a.md) and [here](#run).\n"
	if got != want {
		t.Errorf("results page:\n%s\nwant:\n%s", got, want)
	}
}

func TestWithoutResultsSections(t *testing.T) {
	results := &Page{URL: "docs/benchmarks/"}
	problems := []string{
		"docs/guide/typescript/index.html: link ../../benchmarks/#generated-parser-size to a missing anchor",
		"docs/documentation-style/index.html: link ../benchmarks/#figures-quoted-elsewhere to a missing anchor",
		"docs/guide/typescript/index.html: link ../../performance/#gone to a missing anchor",
		"docs/guide/typescript/index.html: link to missing ../../benchmarks/",
	}
	got := withoutResultsSections(problems, results)
	if !slices.Equal(got, problems[2:]) {
		t.Errorf("kept:\n%s\nwant the last two", strings.Join(got, "\n"))
	}
}

// TestRenderBenchmarks requires the results page to carry the ids of the repository's documents,
// which the links of the documents to its sections name.
func TestRenderBenchmarks(t *testing.T) {
	pin := benchPin{Repository: "https://github.com/syntaxir/bench", Tag: "results-v1"}
	results := "# Benchmark Results\n\n## Results\n\n### Typed Values (`ParseValue` of `sxir gen -types`)\n\n## Figures Quoted Elsewhere\n\nSee [raw](results/results.txt).\n"
	d, body, err := renderBenchmarks(pin, []byte(results))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"results", "typed-values-parsevalue-of-sxir-gen--types", "figures-quoted-elsewhere"} {
		if !strings.Contains(body, ` id="`+id+`"`) {
			t.Errorf("the page has no id %q:\n%s", id, body)
		}
	}
	if toc := d.TOC(2, 3); len(toc) != 3 || toc[1].Level != 3 {
		t.Errorf("table of contents %v, want the three sections", toc)
	}
	if !strings.Contains(body, `href="https://github.com/syntaxir/bench/blob/results-v1/results/results.txt"`) {
		t.Errorf("the link to the raw results does not name the release:\n%s", body)
	}
}

// TestWithoutResultsSectionsOfCheckLinks runs the output's link check on a small site, so that a
// change of the wording of its reports, which withoutResultsSections reads, fails here.
func TestWithoutResultsSectionsOfCheckLinks(t *testing.T) {
	out := t.TempDir()
	put := func(path, text string) {
		full := filepath.Join(out, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("docs/benchmarks/index.html", `<h1 id="benchmark-results">Benchmark Results</h1>`)
	put("docs/performance/index.html", `<h2 id="method">Method</h2>`)
	put("docs/guide/x/index.html", `<a href="../../benchmarks/#generated-parser-size">sizes</a> <a href="../../performance/#gone">gone</a> <a href="../../benchmarks/#benchmark-results">top</a>`)
	problems := checkLinks(out)
	if len(problems) != 2 {
		t.Fatalf("checkLinks = %q, want the two links to missing sections", problems)
	}
	got := withoutResultsSections(problems, &Page{URL: "docs/benchmarks/"})
	want := []string{"docs/guide/x/index.html: link ../../performance/#gone to a missing anchor"}
	if !slices.Equal(got, want) {
		t.Errorf("withoutResultsSections = %q, want %q", got, want)
	}
}

// TestBenchmarksAnchorsLinked reads the sections from the links of the pages' Markdown, not from
// the text around them.
func TestBenchmarksAnchorsLinked(t *testing.T) {
	src := []byte("See [sizes](../benchmarks.md#generated-parser-size), [top](benchmarks.md), [other](performance.md#run) and `benchmarks.md#not-a-link`.\n")
	doc := goldmark.New().Parser().Parse(text.NewReader(src))
	got := benchmarksAnchorsLinked([]*sitegen.Page{{Document: doc}, {}})
	if want := []string{"generated-parser-size"}; !slices.Equal(got, want) {
		t.Errorf("anchors %q, want %q", got, want)
	}
}
