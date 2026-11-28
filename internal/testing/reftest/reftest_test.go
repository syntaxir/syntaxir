package reftest_test

import (
	"os"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/reftest"
)

func init() { enginetest.UseEngine() }

func TestMain(m *testing.M) { enginetest.Main(m) }

// The environment of TestReferenceTranscript, which hack/test-reference-tags.sh sets: the tag of the
// build (none for the default build), the file the transcript is written to, and the transcript of
// the default build that the build with the tag is compared with.
const (
	tagEnv  = "SXIR_REFTEST_TAG"
	outEnv  = "SXIR_REFTEST_OUT"
	baseEnv = "SXIR_REFTEST_BASE"
)

// TestReferenceTranscript writes the transcript of this build, and in a build with a reference tag
// compares it with that of the default build: the configurations must differ and the results of the
// tag's control must not. hack/test-reference-tags.sh runs it once in the default build, which writes
// the results of every control, and once with each tag, through GOFLAGS so that the builds of the
// generated parsers take the tag too. Run on its own, it takes the transcript of every control in
// the default build in the long tier, so that it keeps working, and is skipped otherwise.
func TestReferenceTranscript(t *testing.T) {
	codegentest.NeedsBudgetControls(t)
	tag, out, base := os.Getenv(tagEnv), os.Getenv(outEnv), os.Getenv(baseEnv)
	if tag == "" && out == "" && !codegentest.Long() {
		t.Skip("compares two builds (hack/test-reference-tags.sh); takes the transcript in the long tier")
	}
	rcs := pass.ReferenceControls()
	if tag != "" {
		rc, ok := reftest.Control(tag)
		if !ok {
			t.Fatalf("%s=%s is no reference control of the registry", tagEnv, tag)
		}
		rcs = []pass.ReferenceControl{rc}
	}
	got := reftest.Take(t, rcs)
	if out != "" {
		if err := os.WriteFile(out, []byte(got.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if tag == "" {
		return
	}
	if base == "" {
		t.Fatalf("%s=%s needs the transcript of the default build in %s", tagEnv, tag, baseEnv)
	}
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	want, err := reftest.ParseTranscript(string(data))
	if err != nil {
		t.Fatal(err)
	}
	// Compare the transcript as it is read back, as the default build's is.
	got, err = reftest.ParseTranscript(got.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reftest.Compare(tag, want, got) {
		t.Error(p)
	}
}

// Every witness of every reference control is a case of the corpus, a stream or a Document session,
// and a stream witness has a #stream repetition.
func TestWitnesses(t *testing.T) {
	for _, rc := range pass.ReferenceControls() {
		for _, name := range rc.Witnesses {
			w, ok := reftest.Lookup(name)
			switch {
			case !ok:
				t.Errorf("reference control %s: the witness %q is not a case of the corpus, a stream or a Document session", rc.Tag, name)
			case w.Stream && !strings.Contains(w.Case.Src, "#stream"):
				t.Errorf("reference control %s: the stream witness %q has no #stream repetition", rc.Tag, name)
			case w.Document == nil && len(w.Case.Inputs) == 0:
				t.Errorf("reference control %s: the witness %q has no inputs", rc.Tag, name)
			}
		}
	}
	for _, name := range []string{"stream nothing", "document nothing", "nothing"} {
		if _, ok := reftest.Lookup(name); ok {
			t.Errorf("%q is a witness", name)
		}
	}
}

// The comparison fails where the tag selects no file and where the results of its control differ,
// and names the first line that differs; it passes where the configurations differ and the results
// do not. A transcript reads back as written.
func TestCompare(t *testing.T) {
	base := reftest.Transcript{Configuration: "a f.go\n", Results: map[string]string{"sxir_reference_sparse_memo": "x\ny\n"}}
	data := base.String()
	back, err := reftest.ParseTranscript(data)
	if err != nil || back.String() != data {
		t.Fatalf("read back:\n%s\nwant\n%s (%v)", back.String(), data, err)
	}
	same := reftest.Transcript{Configuration: "a f_reference.go\n", Results: map[string]string{"sxir_reference_sparse_memo": "x\ny\n"}}
	if p := reftest.Compare("sxir_reference_sparse_memo", base, same); len(p) != 0 {
		t.Errorf("equal results: %q", p)
	}
	differs := reftest.Transcript{Configuration: "a f_reference.go\n", Results: map[string]string{"sxir_reference_sparse_memo": "x\nz\n"}}
	if p := reftest.Compare("sxir_reference_sparse_memo", base, differs); len(p) != 1 || !strings.Contains(p[0], "line 2:") || !strings.Contains(p[0], "reference z") {
		t.Errorf("differing results: %q", p)
	}
	unselected := reftest.Transcript{Configuration: "a f.go\n", Results: base.Results}
	if p := reftest.Compare("sxir_reference_sparse_memo", base, unselected); len(p) != 1 || !strings.Contains(p[0], "selects no file") {
		t.Errorf("unselected: %q", p)
	}
	if p := reftest.Compare("sxir_reference_stream_text", base, same); len(p) != 1 || !strings.Contains(p[0], "has no results") {
		t.Errorf("no base results: %q", p)
	}
	if _, err := reftest.ParseTranscript("== configuration\n== stray\n"); err == nil {
		t.Error("a stray heading reads")
	}
}
