package archtest

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// architectureRow matches a row of the package table of docs/design/architecture.md: the package
// path in backticks, then its description.
var architectureRow = regexp.MustCompile("^\\| `([^`]+)` \\| (.*) \\|$")

// tableExempt are the package trees that the table does not describe one by one: generated
// parsers, whose generator the table names.
var tableExempt = []string{"internal/bench/parsers", "internal/frontend/parser/sxirparser"}

// architectureTable returns the description of each package row of the table, by package path.
func architectureTable(text string) map[string]string {
	rows := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		if m := architectureRow.FindStringSubmatch(l); m != nil {
			rows[m[1]] = m[2]
		}
	}
	return rows
}

// undescribedPackages returns the packages of pkgs (paths relative to the module root) that the
// table rows do not describe. A package is described by its own row, or by a row of an ancestor that
// names it in backticks (by its path below that row or by its last element), as the row of
// internal/driver names its subpackages; a package under tableExempt needs no row.
func undescribedPackages(rows map[string]string, pkgs []string) []string {
	var missing []string
	for _, p := range pkgs {
		if _, ok := rows[p]; ok || slices.ContainsFunc(tableExempt, func(e string) bool { return p == e || strings.HasPrefix(p, e+"/") }) {
			continue
		}
		covered := false
		for a := path.Dir(p); a != "." && a != "/"; a = path.Dir(a) {
			desc, ok := rows[a]
			if !ok {
				continue
			}
			rel := strings.TrimPrefix(p, a+"/")
			covered = strings.Contains(desc, "`"+rel+"`") || strings.Contains(desc, "`"+path.Base(p)+"`") || strings.Contains(desc, "`"+rel+"/`")
			break // the nearest row decides
		}
		if !covered {
			missing = append(missing, p)
		}
	}
	return missing
}

// modulePackages returns the packages of cmd, internal and pkg that have non-test Go files, relative to root.
func modulePackages(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, top := range []string{"cmd", "internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if n := d.Name(); n == "testdata" || strings.HasPrefix(n, "_") || strings.HasPrefix(n, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				rel, _ := filepath.Rel(root, filepath.Dir(p))
				seen[filepath.ToSlash(rel)] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var pkgs []string
	for p := range seen {
		pkgs = append(pkgs, p)
	}
	slices.Sort(pkgs)
	return pkgs
}

// TestArchitectureTableHoldsEveryPackage checks that the package table of the architecture page
// describes every package of the module, and that each row names a package that exists, so that the
// page cannot fall behind the layout.
func TestArchitectureTableHoldsEveryPackage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(moduleRoot, "docs", "design", "architecture.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := architectureTable(string(data))
	pkgs := modulePackages(t, moduleRoot)
	if len(rows) < 40 || len(pkgs) < 40 {
		t.Fatalf("found %d table rows and %d packages: the scan is broken", len(rows), len(pkgs))
	}
	for _, p := range undescribedPackages(rows, pkgs) {
		t.Errorf("package %s has no row in the package table of docs/design/architecture.md", p)
	}
	for row := range rows {
		if !slices.Contains(pkgs, row) && !strings.HasSuffix(row, "/llvm") {
			if _, err := os.Stat(filepath.Join(moduleRoot, filepath.FromSlash(row))); err != nil {
				t.Errorf("the table describes %s, which does not exist", row)
			}
		}
	}
}

// TestArchitectureTableNegativeControl checks the matching: a package with neither a row nor a
// name in an ancestor's row is reported, and one that an ancestor's row names, or a generated tree,
// is not.
func TestArchitectureTableNegativeControl(t *testing.T) {
	rows := architectureTable("| `internal/driver` | Drives; `entry` is the entry, `sub/deep` more |\n| `internal/a` | x |\n")
	got := undescribedPackages(rows, []string{"internal/driver", "internal/driver/entry", "internal/driver/sub/deep", "internal/driver/other", "internal/a", "internal/b", "internal/bench/parsers/json"})
	if want := []string{"internal/driver/other", "internal/b"}; !slices.Equal(got, want) {
		t.Errorf("undescribed %v, want %v", got, want)
	}
}
