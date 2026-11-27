package optcatalog

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
)

const root = "../.."

// The generated regions of the catalog and the guide are what go generate writes now.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	files, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{CatalogPath, GuidePath} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
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
// is a registered flag's, and each run of the measurement file is a registered flag's.
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
	measured, err := ReadMeasured(root)
	if err != nil {
		t.Fatal(err)
	}
	for name := range measured {
		if _, ok := pass.Lookup(name); !ok {
			t.Errorf("%s names %s, which is not a registered flag", MeasuredPath, name)
		}
	}
}

// unclassifiedRow matches a row of the hand-written tables of the mechanisms that are not classified
// yet, which link the mechanism's page.
var unclassifiedRow = regexp.MustCompile(`^\| \[([EMRAG]\d{3})\]\(mechanisms/([^)]+)\) \|`)

// unclassifiedRows returns the ID and page of every row of the catalog's section of unclassified
// mechanisms.
func unclassifiedRows(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, CatalogPath))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n## Unclassified Mechanisms\n")
	if !ok {
		t.Fatal("the catalog has no section of unclassified mechanisms")
	}
	if i := strings.Index(section, "\n## "); i >= 0 {
		section = section[:i]
	}
	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if m := unclassifiedRow.FindStringSubmatch(line); m != nil {
			rows[m[1]] = m[2]
		}
	}
	return rows
}

// Every declared mechanism has its page, every page belongs to a declared or an unclassified
// mechanism, and the catalog's rows of unclassified mechanisms are the registry's list.
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
	}
	rows := unclassifiedRows(t)
	for id, page := range rows {
		owned[page] = id
		if !slices.Contains(pages, page) {
			t.Errorf("unclassified mechanism %s has no page %s", id, page)
		}
	}
	for _, p := range pages {
		if _, ok := owned[p]; !ok {
			t.Errorf("the page %s belongs to no declared or unclassified mechanism", p)
		}
	}
	var ids []string
	for id := range rows {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if want := pass.Unclassified(); !slices.Equal(ids, want) {
		t.Errorf("the catalog lists the unclassified mechanisms %v, the registry %v", ids, want)
	}
}
