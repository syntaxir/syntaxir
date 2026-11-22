package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// Without limit controls no function of the parser package or the types package charges steps or counts compute
// units, including the typed constructors, the conversions of typed values and the closures of actions, which
// the specialization of the rule functions does not reach. With them, the conversions of the types package
// count nothing outside a Document, since converting the result to typed values is no event of the compute
// units, and charge steps for a list in a Document, which still charges budget.
func TestGeneratedNoStepChargesInTypedCode(t *testing.T) {
	t.Parallel()
	for _, src := range []string{codegentest.OrdinaryOnlyTyped, codegentest.OrdinaryOnlyConverted} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		// generated returns the source of the parser package and of the types package.
		generated := func(opts common.GenOptions) string {
			t.Helper()
			opts.Package, opts.Module, opts.Start, opts.Lib = "p", "example.com/p", "main", common.LibImport
			files, err := codegentest.Generate(t, g, opts)
			if err != nil {
				t.Fatal(err)
			}
			all := ""
			for _, f := range files {
				if f.Path == "parser.go" || strings.HasSuffix(f.Path, "types.go") {
					all += string(f.Content)
				}
			}
			if all == "" {
				t.Fatal("no parser.go")
			}
			return all
		}
		for _, opts := range []common.GenOptions{{Types: true, NoLimitControls: true}, {Types: true, TypedOnly: true, NoLimitControls: true}, {Types: true, Document: true, NoLimitControls: true}, {Types: true, Recognize: true, Document: true, NoLimitControls: true}} {
			if out := generated(opts); strings.Contains(out, ".Spend()") || strings.Contains(out, ".Units") {
				t.Errorf("%+v: a parser without limit controls charges steps or counts units", opts)
			}
		}
		if out := generated(common.GenOptions{Types: true, Document: true}); !strings.Contains(out, ".Spend()") {
			t.Error("a controlled parser does not charge steps: the check is void")
		}
	}
	// The conversions of the types package charge steps only in a Document with limit controls, for a list,
	// and count no units outside one.
	g, err := parseTest(codegentest.OrdinaryOnlyConverted)
	if err != nil {
		t.Fatal(err)
	}
	types := func(document bool) string {
		t.Helper()
		files, err := codegentest.Generate(t, g, common.GenOptions{Package: "p", Module: "example.com/p", Start: "main", Lib: common.LibImport, Types: true, Document: document})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f.Path, "types.go") {
				return string(f.Content)
			}
		}
		t.Fatal("no types.go")
		return ""
	}
	if out := types(false); strings.Contains(out, ".Spend()") || strings.Contains(out, ".Units") {
		t.Error("the types package of a parser with compute units charges or counts its conversions")
	}
	if !strings.Contains(types(true), ".Spend()") {
		t.Error("the types package of a controlled Document parser does not charge steps: the check of the types package is void")
	}
}
