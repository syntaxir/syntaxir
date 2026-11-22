//go:build sxir_charge_log

package engine

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// TestChargeLog prints the reservations of each engine backend for SXIR_CL_GRAMMAR on SXIR_CL_INPUT.
func TestChargeLog(t *testing.T) {
	src, in := os.Getenv("SXIR_CL_GRAMMAR"), os.Getenv("SXIR_CL_INPUT")
	if src == "" {
		t.Skip()
	}
	var tune state.Tuning
	if os.Getenv("SXIR_CL_HOT") != "" {
		tune = state.Tuning{RunHot: 1, ScanHot: 1}
	}
	unit := input.CodePoints
	if os.Getenv("SXIR_CL_BYTES") != "" {
		unit = input.Bytes
	}
	old := mustCompile(t, src)
	for _, c := range []struct {
		name string
		p    *Program
		b    Backend
	}{{"closure", old, Closure}, {"bytecode", old, Bytecode}, {"bytecode-iterative", old, BytecodeIterative}} {
		var lines []string
		var total uint64
		state.ChargeLog = func(n uint64, where string) {
			total += n
			lines = append(lines, fmt.Sprintf("%3d +%d %s", total, n, where))
		}
		if os.Getenv("SXIR_CL_DOC") != "" {
			d, err := c.p.NewDocumentWith("", in, ParseOptions{Backend: c.b, Unit: unit, Limits: sxirruntime.Limits{Compute: 1 << 40}, Tune: tune})
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Parse()
			_ = err
		} else {
			_, _ = c.p.ParseWith("", in, ParseOptions{Backend: c.b, Unit: unit, Limits: sxirruntime.Limits{Compute: 1 << 40}, Tune: tune})
		}
		state.ChargeLog = nil
		fmt.Printf("== %s total %d\n%s\n", c.name, total, strings.Join(lines, "\n"))
	}
}
