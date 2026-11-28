package lib

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Every region of lib.ts has its markers once each and a reference body, every reference body
// belongs to a region, and resolving lib.ts drops the markers and, with a region's body, replaces
// exactly that region. FS serves lib.ts resolved for the build.
func TestRegions(t *testing.T) {
	data, err := os.ReadFile(libName)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	bodies, err := filepath.Glob(filepath.Join("reference", "*.region"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range regions {
		names = append(names, r.name)
	}
	for _, b := range bodies {
		if name := strings.TrimSuffix(filepath.Base(b), ".region"); !slices.Contains(names, name) {
			t.Errorf("%s is the body of no reference region", b)
		}
	}
	plain := make([]region, len(regions))
	for i, r := range regions {
		plain[i] = region{name: r.name}
	}
	def, err := resolve(src, plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(def, markerPrefix) {
		t.Error("the resolved library keeps a marker")
	}
	for i, r := range plain {
		body, err := os.ReadFile(filepath.Join("reference", r.name+".region"))
		if err != nil {
			t.Errorf("the region %q has no reference body: %v", r.name, err)
			continue
		}
		with := slices.Clone(plain)
		with[i].body = string(body)
		got, err := resolve(src, with)
		if err != nil {
			t.Fatal(err)
		}
		begin, end := markerPrefix+r.name+beginSuffix, markerPrefix+r.name+endSuffix
		b, e := strings.Index(src, begin), strings.Index(src, end)
		head := src[:strings.LastIndex(src[:b], "\n")+1]
		tail := src[e:]
		tail = tail[strings.Index(tail, "\n")+1:]
		if got != stripMarkers(head)+string(body)+stripMarkers(tail) {
			t.Errorf("the region %q is not replaced by its body alone", r.name)
		}
	}
	served, err := fs.ReadFile(FS, libName)
	if err != nil {
		t.Fatal(err)
	}
	want, err := resolve(src, regions)
	if err != nil {
		t.Fatal(err)
	}
	if string(served) != want {
		t.Error("FS does not serve lib.ts resolved for the build")
	}
	for _, r := range regions {
		if r.body != "" && !strings.Contains(string(served), r.body) {
			t.Errorf("the reference build serves lib.ts without the body of %q", r.name)
		}
	}
}

// stripMarkers drops the marker lines of src.
func stripMarkers(src string) string {
	var b strings.Builder
	for _, l := range strings.SplitAfter(src, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), markerPrefix) {
			b.WriteString(l)
		}
	}
	return b.String()
}

// resolve fails where a marker of a region is missing, duplicated, out of order, malformed or names
// no region, so that a reference body cannot silently stop replacing its region.
func TestResolveRejects(t *testing.T) {
	rs := []region{{name: "x", body: "  body\n"}}
	ok := "a\n  // sxir:reference x begin\n  old\n  // sxir:reference x end\nb\n"
	if got, err := resolve(ok, rs); err != nil || got != "a\n  body\nb\n" {
		t.Fatalf("resolve = %q, %v", got, err)
	}
	if got, err := resolve(ok, []region{{name: "x"}}); err != nil || got != "a\n  old\nb\n" {
		t.Fatalf("resolve without a body = %q, %v", got, err)
	}
	for _, c := range []struct{ name, src, want string }{
		{"no begin", "a\n  // sxir:reference x end\n", "no begin and end"},
		{"no end", "  // sxir:reference x begin\nold\n", "no begin and end"},
		{"no markers", "a\n", "no begin and end"},
		{"begin twice", "  // sxir:reference x begin\n  // sxir:reference x begin\n  // sxir:reference x end\n", "marked twice"},
		{"end twice", "  // sxir:reference x begin\n  // sxir:reference x end\n  // sxir:reference x end\n", "marked twice"},
		{"end first", "  // sxir:reference x end\n  // sxir:reference x begin\n", "before it begins"},
		{"unknown", ok + "  // sxir:reference y begin\n", "no reference region"},
		{"malformed", ok + "  // sxir:reference x\n", "neither begins nor ends"},
	} {
		if _, err := resolve(c.src, rs); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", c.name, err, c.want)
		}
	}
}
