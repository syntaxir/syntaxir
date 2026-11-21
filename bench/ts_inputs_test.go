package bench

import (
	"os"
	"path/filepath"
	"testing"

	"syntaxir.dev/sxir/internal/bench/inputs"
)

// TestExportTSInputs writes the inputs of the plain-parse TypeScript throughput script
// (ts-parse.mjs) to the directory named by SXIR_TS_INPUT_DIR, one file per workload. It does
// nothing without the variable.
func TestExportTSInputs(t *testing.T) {
	dir := os.Getenv("SXIR_TS_INPUT_DIR")
	if dir == "" {
		t.Skip("SXIR_TS_INPUT_DIR is not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]string{
		"json":  inputs.JSONInput(256 << 10),
		"csv":   inputs.CSVInput(5000),
		"xml":   inputs.XMLInput(256 << 10),
		"arith": inputs.ArithInput(20000),
	} {
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
