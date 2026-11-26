//go:build sxir_long

package print_test

import (
	"strings"
	"testing"
)

// TestGeneratedStreamMemoryWindowMutants is the negative control of TestGeneratedStreamMemoryWindow:
// a library whose streams keep counting what the records they drop held, or that drop nothing, fails
// it on the cases that hold such records.
func TestGeneratedStreamMemoryWindowMutants(t *testing.T) {
	t.Parallel()
	for _, m := range []struct{ name, file, old, new, want string }{
		{"the expectations of dropped skip records stay counted", "skip_records.go", "\t\t\ts.expBytes -= r.countedBytes()\n", "\t\t\t_ = r\n", "skip records limited:"},
		{"the buffers of dropped run-site records stay counted", "skip_walks.go", "\tp.recountSkipBytes()\n", "", "run sites:"},
		{"a stream never drops its records", "stream_exec.go", "\t\tp.streamDropRecords(pos)\n", "", "skip records limited:"},
		{"a stream prunes after two windows", "stream_exec.go", "pos-p.Stream.memoBase >= 1024", "pos-p.Stream.memoBase >= 2048", "memo:"},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			bad := streamWindowGo(t, func(name, src string) string {
				if name != m.file {
					return src
				}
				if strings.Count(src, m.old) != 1 {
					t.Fatalf("%s has %d copies of %q, not one", m.file, strings.Count(src, m.old), m.old)
				}
				return strings.Replace(src, m.old, m.new, 1)
			})
			if !strings.Contains(strings.Join(bad, "\n"), m.want) {
				t.Fatalf("the mutant breaks no %q case:\n%s", m.want, strings.Join(bad, "\n"))
			}
		})
	}
}
