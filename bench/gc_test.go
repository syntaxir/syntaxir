package bench

import (
	"flag"
	"os"
	"runtime/debug"
	"testing"
)

// The tests allocate many short-lived values over a small live heap, so with the default
// collection target the collector runs continuously and takes about half of the processor time of
// a test process. A larger target (GOGC=400), bounded by a soft memory limit (4 GiB), removes that
// cost for the tests. The benchmarks measure what a user of the parsers gets, so a run that selects
// benchmarks (-test.bench) keeps the user defaults: GOGC=100 and no memory limit. GOGC and GOMEMLIMIT
// in the environment decide either way when they are set; the headers of the saved results record
// the values that applied (go run ./bench/report).
func TestMain(m *testing.M) {
	flag.Parse()
	if f := flag.Lookup("test.bench"); f == nil || f.Value.String() == "" {
		if os.Getenv("GOGC") == "" {
			debug.SetGCPercent(400)
		}
		if os.Getenv("GOMEMLIMIT") == "" {
			debug.SetMemoryLimit(4 << 30)
		}
	}
	os.Exit(m.Run())
}
