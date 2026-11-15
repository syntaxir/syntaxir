//go:build zp_long

package gogen_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// engineObservationRun is a test that an engine mutant runs, and what the test must then report (want),
// or "" if it must pass.
type engineObservationRun struct{ pkg, test, want string }

// engineObservationMutant changes a file of the engine (file, relative to the module root) by replacing
// old with new, and runs tests with the change through the overlay of the go command.
type engineObservationMutant struct {
	name, file, old, new string
	runs                 []engineObservationRun
}

var (
	agreementRun = engineObservationRun{"./internal/codegen/go", "TestEngineObservationsAgreeWithGenerated", ""}
	discardRun   = engineObservationRun{"./internal/engine", "TestProfileStreamMemory", ""}
)

func expecting(r engineObservationRun, want string) engineObservationRun { r.want = want; return r }

var engineObservationMutants = []engineObservationMutant{
	{name: "a memo hit records nothing", file: "internal/engine/state/call_exec.go",
		old: "\t\tp.touch(e.Examined)\n\t\tif e.From < e.Pos {", new: "\t\tif e.From < e.Pos {",
		runs: []engineObservationRun{expecting(agreementRun, "examined end of")}},
	{name: "lookahead flipped", file: "internal/engine/state/trace.go",
		old: "Pos: p.Pos, Lookahead: p.Silent > 0}", new: "Pos: p.Pos, Lookahead: p.Silent == 0}",
		runs: []engineObservationRun{expecting(agreementRun, "lookahead of the call of")}},
	{name: "profiles not told of discarded input", file: "internal/engine/state/stream_ops.go",
		old: "\t\tp.Tr.discard(pos)\n", new: "",
		runs: []engineObservationRun{expecting(discardRun, "heap")}},
	// The control changes nothing that the tests observe: they must pass with it, so that what makes the
	// other mutants fail is their change, not the overlay.
	{name: "control", file: "internal/engine/state/trace.go",
		old: "\tt.exp = nil\n", new: "\tt.exp = nil\n\t_ = t\n",
		runs: []engineObservationRun{agreementRun, discardRun}},
}

// TestEngineObservationMutants checks that the agreement of the engine's observations with the generated
// parsers' and the bound on the profile of a stream notice an engine that leaves out the examined end of a
// memo hit, inverts Lookahead or keeps the profile's records of discarded input.
func TestEngineObservationMutants(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds the engine again for each mutant")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range engineObservationMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(root, filepath.FromSlash(m.file))
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(src), m.old) != 1 {
				t.Fatalf("%q occurs %d times in %s", m.old, strings.Count(string(src), m.old), m.file)
			}
			dir := t.TempDir()
			mutated := filepath.Join(dir, filepath.Base(path))
			if err := os.WriteFile(mutated, []byte(strings.Replace(string(src), m.old, m.new, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			overlay, _ := json.Marshal(map[string]map[string]string{"Replace": {path: mutated}})
			overlayFile := filepath.Join(dir, "overlay.json")
			if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
				t.Fatal(err)
			}
			for _, r := range m.runs {
				cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-count=1", "-overlay="+overlayFile, "-run", "^"+r.test+"$", r.pkg)
				cmd.Dir = root
				// The tests run in the module itself, as go test runs them here.
				cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=")
				out, err := cmd.CombinedOutput()
				switch {
				case r.want == "" && err != nil:
					t.Errorf("%s fails with the mutant: %v\n%s", r.test, err, out)
				case r.want != "" && err == nil:
					t.Errorf("%s passes with the mutant", r.test)
				case r.want != "" && !strings.Contains(string(out), r.want):
					t.Errorf("%s fails with the mutant without reporting %q:\n%s", r.test, r.want, out)
				}
			}
		})
	}
}
