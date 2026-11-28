package main

import (
	"bytes"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/bench/probe"
	"syntaxir.dev/sxir/internal/driver/result"
)

// The generation of the settings part, which runs in the process to count its steps, writes what sxir
// gen writes with the options of testdata/grammars/generate.txt (with -lib embed) and those of
// probe.GenTSArgs, for grammars that between them take every option those lines use.
func TestSettingsGenerationIsSxirGen(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the sxir command and generates six parsers")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	sxir, err := probe.BuildSXIR(root, work)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"json", "xml", "golang"} {
		s, err := loadSettingsSubject(root, work, name)
		if err != nil {
			t.Fatal(err)
		}
		g, err := s.generate("speed", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		goDir, tsDir := filepath.Join(work, name, "go"), filepath.Join(work, name, "ts")
		if err := probe.GenerateGo(root, sxir, goDir, name, "-module", settingsModule, "-lib", "embed"); err != nil {
			t.Fatal(err)
		}
		if err := probe.GenerateTS(root, sxir, tsDir, name); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			dir   string
			files []result.File
		}{{goDir, g.goFiles}, {tsDir, g.tsFiles}} {
			want, err := readTree(c.dir)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string][]byte{}
			for _, f := range c.files {
				got[filepath.FromSlash(f.Path)] = f.Content
			}
			if !slices.Equal(slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want))) {
				t.Errorf("%s: the files %v, but sxir gen writes %v", name, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
				continue
			}
			for p, data := range want {
				if !bytes.Equal(got[p], data) {
					t.Errorf("%s: %s differs from what sxir gen writes, first at:\n%s", name, p, firstDiff(got[p], data))
				}
			}
		}
	}
}

// The program of internal/bench/settingsprobe builds for a generated parser and measures every API
// of it.
func TestSettingsProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a measuring program")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	s, err := loadSettingsSubject(root, work, "json")
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.generate("speed", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.measure(root, filepath.Join(work, "probe"), g.goFiles)
	if err != nil {
		t.Fatal(err)
	}
	if m.binary <= 0 || m.retainedBytes <= 0 || m.retainedObjects <= 0 {
		t.Errorf("binary %d bytes, retained %d bytes in %d objects: want each above 0", m.binary, m.retainedBytes, m.retainedObjects)
	}
	for _, api := range []string{"parse", "parse-value", "recognize"} {
		a, ok := m.apis[api]
		if !ok {
			t.Errorf("%s was not measured: %v", api, m.apis)
			continue
		}
		// trailing_comma.txt is rejected on purpose; the measurement counts its error path too.
		if a.Errors != 1 {
			t.Errorf("%s failed on %d of the JSON inputs, want 1 (trailing_comma.txt)", api, a.Errors)
		}
		if api != "recognize" && (a.Objects == 0 || a.Bytes == 0) {
			t.Errorf("%s allocated %d objects and %d bytes: want both above 0", api, a.Objects, a.Bytes)
		}
	}
}

// firstDiff returns the first line that differs between a and b, with its number, in both.
func firstDiff(a, b []byte) string {
	al, bl := bytes.Split(a, []byte("\n")), bytes.Split(b, []byte("\n"))
	for i := range max(len(al), len(bl)) {
		var x, y []byte
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if !bytes.Equal(x, y) {
			return fmt.Sprintf("line %d: in process %q\nline %d: sxir gen %q", i+1, x, i+1, y)
		}
	}
	return "no differing line"
}

// An inactive variant is checked by generating it: one whose parsers equal the profile's passes, and
// one of a flag that does change them is refused.
func TestSettingsCheckInactive(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	s, err := loadSettingsSubject(root, t.TempDir(), "json")
	if err != nil {
		t.Fatal(err)
	}
	if s.generations["memory"], err = s.generate("memory", nil, nil); err != nil {
		t.Fatal(err)
	}
	vs, err := probe.SettingVariants()
	if err != nil {
		t.Fatal(err)
	}
	var inactive, active probe.SettingVariant
	for _, v := range vs {
		switch {
		case v.Profile == "memory" && v.Flag.Name == "typed-output-bootstrap":
			inactive = v
		case v.Profile == "memory" && v.Flag.Name == "typed-chunk-growth":
			active = v
		}
	}
	if !inactive.Inactive || active.Inactive {
		t.Fatalf("typed-output-bootstrap inactive %v and typed-chunk-growth inactive %v in memory, want true and false", inactive.Inactive, active.Inactive)
	}
	if err := s.checkInactive(inactive); err != nil {
		t.Errorf("%s: %v", inactive.Name(), err)
	}
	if err := s.checkInactive(active); err == nil {
		t.Errorf("%s changes the json parser in memory, but checkInactive accepts it", active.Name())
	}
}
