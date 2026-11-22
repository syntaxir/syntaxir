package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadmeUpToDate fails when README.md's diagram block or the diagram files in site/readme differ
// from what the landing page generates, so that the README and the page cannot drift apart.
func TestReadmeUpToDate(t *testing.T) {
	const fix = "; run go generate in site"
	out, err := generateReadme("..")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, out.readme) {
		t.Error("README.md: the diagram block is stale" + fix)
	}
	for p, want := range out.files {
		got, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(p)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is missing or stale%s", p, fix)
		}
	}
	stale, err := staleReadmeAssets("..", out)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range stale {
		t.Errorf("%s is no longer generated%s", p, fix)
	}
}

// TestReadmeDiagrams checks the generated diagram: a light and a dark file, each a standalone SVG whose
// style sheet defines every token it uses.
func TestReadmeDiagrams(t *testing.T) {
	out, err := generateReadme("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, scheme := range []string{"light", "dark"} {
		if p := readmeAssets + "/architecture-" + scheme + ".svg"; out.files[p] == nil {
			t.Errorf("no %s", p)
		}
	}
	for p, data := range out.files {
		light, ok := strings.CutSuffix(p, "-dark.svg")
		if ok {
			if _, ok := out.files[light+"-light.svg"]; !ok {
				t.Errorf("%s has no light variant", p)
			}
		}
		s := string(data)
		if !strings.HasPrefix(s, `<svg xmlns="http://www.w3.org/2000/svg"`) || !strings.Contains(s, "<style>") {
			t.Errorf("%s is not a standalone SVG with its style sheet", p)
		}
		if strings.Contains(s, `href="assets/`) {
			t.Errorf("%s links a site asset, which an SVG shown as an image cannot load", p)
		}
		if strings.Contains(s, "light-dark(") {
			t.Errorf("%s keeps light-dark() unresolved", p)
		}
		for _, m := range varRe.FindAllStringSubmatch(s, -1) {
			if m[1] != "c" && !strings.Contains(s, "--"+m[1]+": ") {
				t.Errorf("%s uses --%s without defining it", p, m[1])
			}
		}
		if !strings.Contains(string(out.readme), `"`+p+`"`) {
			t.Errorf("README.md does not show %s", p)
		}
	}
}

func TestResolveLightDark(t *testing.T) {
	for _, c := range []struct {
		in          string
		light, dark string
	}{
		{"light-dark(#fff, #000)", "#fff", "#000"},
		{"light-dark(rgba(1, 2, 3, 0.1), rgba(4, 5, 6, 0.2))", "rgba(1, 2, 3, 0.1)", "rgba(4, 5, 6, 0.2)"},
		{"0 6px light-dark(#111, #222)", "0 6px #111", "0 6px #222"},
		{"#abc", "#abc", "#abc"},
	} {
		for scheme, want := range []string{c.light, c.dark} {
			if got, err := resolveLightDark(c.in, scheme); err != nil || got != want {
				t.Errorf("resolveLightDark(%q, %d) = %q, %v; want %q", c.in, scheme, got, err, want)
			}
		}
	}
	if _, err := resolveLightDark("light-dark(#fff)", 0); err == nil {
		t.Error("resolveLightDark accepts light-dark() with one argument")
	}
}
