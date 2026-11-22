package print_test

import (
	"bytes"
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// statsGrammar is the fixture whose statistics the test pins.
const statsGrammar = `rule main = (word / num / -" ")* $$
rule word = "if" / "else" / [a-z]+
rule num = [0-9]+
`

// generateWithStats generates the Go parser of statsGrammar with opts, watched by st if it is not
// nil.
func generateWithStats(t *testing.T, opts common.GenOptions, st *pipe.Stats) []common.GeneratedFile {
	t.Helper()
	g, err := parseTest(statsGrammar)
	if err != nil {
		t.Fatal(err)
	}
	var hooks generate.Hooks
	if st != nil {
		hooks.Observers = []pipe.Observer{st}
	}
	files, err := print.GenerateParser(g, codegentest.Compile, generate.Passes(opts, hooks), codegentest.WithLib(opts), nil)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sameFiles(a, b []common.GeneratedFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || !bytes.Equal(a[i].Content, b[i].Content) {
			return false
		}
	}
	return true
}

// Watching a generation with pipe.Stats leaves every generated file as it is, and the statistics of
// the same generation are the same every time. The comparison is not blind: turning off an
// optimization that the statistics show changing the fixture changes the files.
func TestStatsLeaveTheGeneratedCodeAsItIs(t *testing.T) {
	opts := common.GenOptions{Module: "example.com/p", Lib: common.LibImport}
	plain := generateWithStats(t, opts, nil)
	st, again := pipe.NewStats(), pipe.NewStats()
	if !sameFiles(plain, generateWithStats(t, opts, st)) {
		t.Fatal("the generated files differ with Stats")
	}
	generateWithStats(t, opts, again)
	if fmt.Sprint(st.Instances()) != fmt.Sprint(again.Instances()) {
		t.Errorf("the statistics differ between two runs:\n%v\n%v", st.Instances(), again.Instances())
	}
	var changing string
	for _, s := range st.Instances() {
		if s.Changed > 0 && !isRequired(s.Instance) {
			changing = s.Instance
			break
		}
	}
	if changing == "" {
		t.Fatalf("no optional pass changed the fixture: %v", st.Instances())
	}
	opts.NoOpt = []string{changing}
	if sameFiles(plain, generateWithStats(t, opts, nil)) {
		t.Errorf("turning off %s, which changed the fixture, left the files as they were", changing)
	}
}

// The statistics of one pass on the fixture are pinned, so that an optimization that stops firing is
// noticed: guards runs once, on the Core module of the Node table, the one table of the parser, which
// the numbering of the expectation table reuses, and gives 3 alternatives a guard.
func TestStatsPinTheGuardsPassOnTheFixture(t *testing.T) {
	st := pipe.NewStats()
	generateWithStats(t, common.GenOptions{Module: "example.com/p", Lib: common.LibImport}, st)
	got, ok := st.Instance("", "guards")
	if !ok {
		t.Fatalf("guards did not run: %v", st.Instances())
	}
	if want := (pipe.InstanceStats{Instance: "guards", Runs: 1, Changed: 1, Counters: []pipe.Counter{{Name: corepass.CounterGuarded, Value: 3}}}); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("guards: %+v, want %+v", got, want)
	}
}

// isRequired reports whether the instance is of a step that no optimization flag names, such as a
// required step: -no-opt rejects its name.
func isRequired(instance string) bool {
	return generate.Passes(common.GenOptions{NoOpt: []string{instance}}, generate.Hooks{}).Validate() != nil
}
