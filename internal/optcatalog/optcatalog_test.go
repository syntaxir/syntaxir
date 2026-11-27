package optcatalog

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
)

const root = "../.."

// The generated regions of the catalog, the guide and the mechanism pages are what go generate
// writes now, and a registered mechanism without a page fails.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	files, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range slices.Sorted(maps.Keys(files)) {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Errorf("%v: run go generate ./internal/optcatalog", err)
			continue
		}
		if string(data) != string(files[path]) {
			t.Errorf("%s is not current: run go generate ./internal/optcatalog", path)
		}
	}
}

// The registry of the program that links every part keeps the rules of the catalog and of the
// profiles.
func TestRegistryKeepsTheCatalogRules(t *testing.T) {
	if err := pass.CheckCatalog(pass.Flags()); err != nil {
		t.Error(err)
	}
	if err := driver.CheckRegistry(); err != nil {
		t.Error(err)
	}
}

// Each flag names a record that decides the state the flag has, each state that a record decides
// is a registered flag's.
func TestRecordsDecideTheStates(t *testing.T) {
	records, err := ReadRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range pass.Flags() {
		st, _, err := StateOf(f)
		if err != nil {
			t.Fatal(err)
		}
		r, ok := records[f.Record]
		if !ok {
			t.Errorf("flag %s names the record %03d, which does not exist", f.Name, f.Record)
			continue
		}
		if got, ok := r.Decides[f.Name]; !ok || got != st {
			t.Errorf("flag %s is %s, but its record %s decides %q for it", f.Name, st, r.File, got)
		}
	}
	for _, r := range records {
		for name := range r.Decides {
			if _, ok := pass.Lookup(name); !ok {
				t.Errorf("record %s decides the state of %s, which is not a registered flag", r.File, name)
			}
		}
	}
}

// Every declared mechanism has its page with its hand-written summary and sections, every page
// belongs to a declared or an unclassified mechanism and starts with its ID, and no page belongs to a
// former ID.
func TestMechanismPages(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(root, MechanismsDir))
	if err != nil {
		t.Fatal(err)
	}
	var pages []string
	for _, e := range entries {
		pages = append(pages, e.Name())
	}
	owned := map[string]string{}
	for _, m := range pass.Mechanisms() {
		if !slices.Contains(pages, m.Page) {
			t.Errorf("mechanism %s has no page %s", m.ID, m.Page)
		}
		if !strings.HasPrefix(m.Page, m.ID+"-") {
			t.Errorf("the page %s of mechanism %s does not start with its ID", m.Page, m.ID)
		}
		owned[m.Page] = m.ID
		if data, err := os.ReadFile(filepath.Join(root, MechanismsDir, m.Page)); err == nil {
			if err := CheckPageProse(m.Page, data); err != nil {
				t.Error(err)
			}
		}
	}
	for _, id := range pass.Unclassified() {
		i := slices.IndexFunc(pages, func(p string) bool { return strings.HasPrefix(p, id+"-") })
		if i < 0 {
			t.Errorf("unclassified mechanism %s has no page", id)
			continue
		}
		owned[pages[i]] = id
	}
	for _, p := range pages {
		if _, ok := owned[p]; !ok {
			t.Errorf("the page %s belongs to no declared or unclassified mechanism", p)
		}
	}
}

// CheckPageProse accepts a page whose summary and sections are written, and rejects a stub, an empty
// section, a missing section and sections out of order.
func TestCheckPageProse(t *testing.T) {
	facts := "<!-- generated:facts -->\n# M999: X\n<!-- /generated:facts -->\n\n"
	full := facts + "Summary.\n\n## Mechanism and Invariants\n\nA.\n\n## Scope\n\nB.\n\n## Controls and Measurements\n\nC.\n\n## Evidence and Implementation\n\nD.\n\n[Optimization Catalog](../README.md)\n"
	if err := CheckPageProse("ok.md", []byte(full)); err != nil {
		t.Errorf("complete page: %v", err)
	}
	for name, page := range map[string]string{
		"stub":          stubPage(),
		"no facts":      strings.Replace(full, "<!-- /generated:facts -->", "", 1),
		"no summary":    strings.Replace(full, "Summary.", "<!-- prompt -->", 1),
		"empty section": strings.Replace(full, "C.", "", 1),
		"missing":       strings.Replace(full, "## Scope\n\nB.\n\n", "", 1),
		"out of order":  strings.Replace(strings.Replace(full, "## Scope", "## X", 1), "## Controls and Measurements", "## Scope", 1),
	} {
		if err := CheckPageProse(name, []byte(page)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// The markers of former IDs and the rejected records are read as the generator reads them: an ID that
// two pages carry, a marker before the page's title, and a rejected record without its result or
// revisit condition are errors.
func TestFormerMarkersAndRejectedRecords(t *testing.T) {
	write := func(t *testing.T, files map[string]string) string {
		t.Helper()
		dir := t.TempDir()
		for name, text := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	dir := write(t, map[string]string{
		DesignDir + "/a.md":   "# Page A\n\n<!-- former-mechanism R010: a contract depends on it -->\n",
		DesignDir + "/x/b.md": "# Page B\n<!-- former-mechanism G080: retired -->\n",
	})
	got, err := ReadFormer(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]FormerPage{
		"R010": {ID: "R010", Page: DesignDir + "/a.md", Title: "Page A", Reason: "a contract depends on it"},
		"G080": {ID: "G080", Page: DesignDir + "/x/b.md", Title: "Page B", Reason: "retired"},
	}
	if len(got) != len(want) || got["R010"] != want["R010"] || got["G080"] != want["G080"] {
		t.Errorf("ReadFormer: %v, want %v", got, want)
	}
	for name, files := range map[string]map[string]string{
		"carried twice": {DesignDir + "/a.md": "# A\n<!-- former-mechanism R010: x -->\n", DesignDir + "/b.md": "# B\n<!-- former-mechanism R010: y -->\n"},
		"before title":  {DesignDir + "/a.md": "<!-- former-mechanism R010: x -->\n# A\n"},
	} {
		if _, err := ReadFormer(write(t, files)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	rec := "# 900. An Attempt\n\n- **Status**: Rejected\n- **Mechanisms**: A090\n"
	for name, c := range map[string]struct {
		text string
		ok   bool
	}{
		"complete":      {rec + "- **Result**: no gain\n- **Revisit when**: never\n", true},
		"no result":     {rec + "- **Revisit when**: never\n", false},
		"no revisit":    {rec + "- **Result**: no gain\n", false},
		"bad mechanism": {"# 900. X\n\n- **Status**: Adopted\n- **Mechanisms**: guards\n", false},
	} {
		records, err := ReadRecords(write(t, map[string]string{RecordsDir + "/900-an-attempt.md": c.text}))
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if c.ok {
			r := records[900]
			if r.Title != "An Attempt" || r.Status != Rejected || !slices.Equal(r.Mechanisms, []string{"A090"}) || r.Result != "no gain" || r.Revisit != "never" {
				t.Errorf("%s: %+v", name, r)
			}
		}
	}
	for line, want := range map[string][]string{
		"[A280](mechanisms/A280-x.md); it changes A280 and [G240](mechanisms/G240-y.md)": {"A280", "G240"},
		"R060 (generated Go Node Documents)":                                             {"R060"},
		"none (the generated form of an engine design)":                                  nil,
	} {
		text := "# 900. X\n\n- **Status**: Adopted\n- **Mechanisms**: " + line + "\n"
		records, err := ReadRecords(write(t, map[string]string{RecordsDir + "/900-x.md": text}))
		if err != nil {
			t.Errorf("%q: %v", line, err)
		} else if got := records[900].Mechanisms; !slices.Equal(got, want) {
			t.Errorf("%q: mechanisms %v, want %v", line, got, want)
		}
	}
}
