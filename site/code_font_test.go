package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cssRule matches one rule of a style sheet without nested blocks: its
// selector and its declarations.
var cssRule = regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)

// monoFont matches a declaration that sets the monospace face, directly or
// through the playground editor's font shorthand.
var monoFont = regexp.MustCompile(`(?:^|[;\s])font(?:-family)?\s*:[^;]*var\(--(?:font-mono|editor-font)\)`)

// fontShorthand matches a font shorthand, which resets font-variant-ligatures.
var fontShorthand = regexp.MustCompile(`(?:^|[;\s])font\s*:`)

// TestCodeLigaturesOff holds every rule that sets the monospace face to turning
// programming ligatures off after any font shorthand, so code such as -> or !=
// shows the characters written.
func TestCodeLigaturesOff(t *testing.T) {
	var checked int
	err := filepath.WalkDir("static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".css" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range cssRule.FindAllStringSubmatch(string(data), -1) {
			selector, body := strings.TrimSpace(m[1]), m[2]
			if !monoFont.MatchString(body) {
				continue
			}
			checked++
			off := strings.LastIndex(body, "font-variant-ligatures: none")
			if off < 0 {
				t.Errorf("%s: %q sets the monospace face without font-variant-ligatures: none", path, selector)
				continue
			}
			if loc := fontShorthand.FindAllStringIndex(body, -1); len(loc) > 0 && loc[len(loc)-1][0] > off {
				t.Errorf("%s: %q resets font-variant-ligatures with a later font shorthand", path, selector)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no rule sets the monospace face; the check matched nothing")
	}
}
