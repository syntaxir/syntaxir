package archtest

import (
	"fmt"
	"go/ast"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The import rules of the printers (record 074, What Makes the Wrong Shape Impossible): a printer takes
// the units of the Exec stage and the library, and receives every result of the pipeline as data. So
// the print packages and the shared part of the generators import no package of the pass manager or
// the driver, no package that declares passes or steps, no lowering, no planning code, no Core builder
// and no Flow dialect; they may import the data packages of the planning and the lowerings, the result
// package of the driver, Code IR and Core IR.

// printPackages are the packages that the import rules of the printers cover.
var printPackages = []string{
	"internal/backend/codegen/common",
	"internal/backend/codegen/golang/libprint",
	"internal/backend/codegen/golang/print",
	"internal/backend/codegen/typescript/print",
}

// printerImportExceptions are the uses of a banned package that the printers still make, as
// "<file>: <package name>.<identifier>": the action lowering, which the printers call for each action
// expression until the action lowering of the pipeline lowers them as a step, and the import ban
// becomes total. Each entry must be used: a use that goes must leave the list.
var printerImportExceptions = []string{
	"internal/backend/codegen/golang/print/action_exec.go: actionlower.Lower",
	"internal/backend/codegen/golang/print/action_exec.go: actionlower.Options",
	"internal/backend/codegen/typescript/print/action_exec.go: actionlower.Lower",
	"internal/backend/codegen/typescript/print/action_exec.go: actionlower.Options",
}

// bannedForPrinters reports why a printer may not import the package at the module path r, or "".
func bannedForPrinters(r string) string {
	under := func(p string) bool { return r == p || strings.HasPrefix(r, p+"/") }
	switch {
	case r == "internal/driver/result", r == "internal/middleend/flowplan/plandata",
		r == "internal/middleend/flowlower/lowerdata", r == "internal/middleend/execlower/execdata":
		return ""
	case under("internal/pass"):
		return "the pass manager"
	case under("internal/driver"):
		return "the driver"
	case r == "internal/backend/codegen", r == "internal/backend/codegen/golang", r == "internal/backend/codegen/typescript":
		return "a backend package, which declares the printing steps"
	case under("internal/middleend/corepass"), under("internal/middleend/flowpass"), under("internal/middleend/codepass"),
		under("internal/backend/codegen/golang/pass"), under("internal/backend/codegen/typescript/pass"):
		return "a pass package"
	case under("internal/middleend/flowlower"), under("internal/middleend/actionlower"), under("internal/middleend/execlower"), under("internal/backend/vmlower"),
		under("internal/backend/actionlower"):
		return "a lowering"
	case under("internal/middleend/flowplan"):
		return "the planning code"
	case under("internal/frontend/elaborate"):
		return "the Core builder"
	case under("internal/ir/code/flow"):
		return "the Flow dialect"
	}
	return ""
}

// printerImportViolations returns the uses of banned packages in the non-test files of the packages
// prints of pkgs, as "<file>: <package name>.<identifier> (<why>)" for each use, and an import that
// no identifier uses as "<file>: imports <path> (<why>)", leaving out the uses that excepted lists
// ("<file>: <package name>.<identifier>"); and the entries of excepted that no use matches.
func printerImportViolations(pkgs map[string]*sourcePackage, module string, prints, excepted []string) (violations, stale []string) {
	used := map[string]bool{}
	for _, pr := range prints {
		sp := pkgs[module+"/"+pr]
		if sp == nil {
			violations = append(violations, fmt.Sprintf("%s: no such package", pr))
			continue
		}
		for i, f := range sp.files {
			file := pr + "/" + sp.fileNames[i]
			banned := map[string]string{} // the local names of the banned imports, with the reason
			for _, s := range f.Imports {
				p, err := strconv.Unquote(s.Path.Value)
				if err != nil || !strings.HasPrefix(p, module+"/") {
					continue
				}
				r := relPath(p, module)
				why := bannedForPrinters(r)
				if why == "" {
					continue
				}
				name := filepath.Base(p)
				if dep, ok := pkgs[p]; ok {
					name = dep.name
				}
				if s.Name != nil {
					name = s.Name.Name
				}
				if name == "_" || name == "." {
					violations = append(violations, fmt.Sprintf("%s: imports %s (%s)", file, r, why))
					continue
				}
				banned[name] = why
			}
			refs := map[string]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok {
					if _, ok := banned[x.Name]; ok {
						refs[x.Name+"."+sel.Sel.Name] = true
					}
				}
				return true
			})
			for _, ref := range slices.Sorted(maps.Keys(refs)) {
				key := file + ": " + ref
				if slices.Contains(excepted, key) {
					used[key] = true
					continue
				}
				pkg, _, _ := strings.Cut(ref, ".")
				violations = append(violations, fmt.Sprintf("%s (%s)", key, banned[pkg]))
			}
		}
	}
	for _, e := range excepted {
		if !used[e] {
			stale = append(stale, e)
		}
	}
	return violations, stale
}

// TestPrintersImportNoPipelineCode checks the import rules of the printers, with the uses that
// printerImportExceptions lists exactly.
func TestPrintersImportNoPipelineCode(t *testing.T) {
	pkgs, err := loadSources(moduleRoot, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	violations, stale := printerImportViolations(pkgs, modulePath, printPackages, printerImportExceptions)
	for _, v := range violations {
		t.Errorf("%s; a printer receives the results of the pipeline as data", v)
	}
	for _, s := range stale {
		t.Errorf("%s is no longer used: remove it from printerImportExceptions", s)
	}
}

// The negative control of the import rules of the printers: testdata/printers, a module example.com/print
// whose print package imports the driver, a pass package, the planning code under another name, and the
// action lowering, which an exception lists one use of and a stale entry another.
func TestPrintersImportRuleNegativeControl(t *testing.T) {
	const module = "example.com/print"
	pkgs, err := loadSources(filepath.Join("testdata", "printers"), module)
	if err != nil {
		t.Fatal(err)
	}
	const p = "internal/backend/codegen/golang/print"
	violations, stale := printerImportViolations(pkgs, module, []string{p, "internal/backend/codegen/common"},
		[]string{p + "/print.go: actionlower.Lower", p + "/print.go: actionlower.Gone"})
	want := []string{
		"internal/backend/codegen/golang/print/print.go: codepass.Specialize (a pass package)",
		"internal/backend/codegen/golang/print/print.go: driver.Compile (the driver)",
		"internal/backend/codegen/golang/print/print.go: flowlower.Term (a lowering)",
		"internal/backend/codegen/golang/print/print.go: plan.Planning (the planning code)",
		"internal/backend/codegen/common: no such package",
	}
	if !slices.Equal(violations, want) {
		t.Errorf("violations:\n%s\nwant:\n%s", strings.Join(violations, "\n"), strings.Join(want, "\n"))
	}
	if want := []string{p + "/print.go: actionlower.Gone"}; !slices.Equal(stale, want) {
		t.Errorf("stale = %q, want %q", stale, want)
	}
}
