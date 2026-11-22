package driver

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/pass/pipe"
)

// printingKindErrors returns the registrations of ks that print an output (the kind output) from a
// unit of another kind than one at the Exec stage (a name that ends in @exec), the library or
// vm-input: a printer takes Exec-stage units only, and the library's compositions, which are at no
// stage and hold no Flow operation (record 074, Printers Take Exec-Stage Units). The bytecode
// lowering is no printer: it selects the instructions of the VM from the Flow stage, so it takes its
// table there (vm-input). library and vm-input are accepted by name, since another stage-less kind
// could carry anything.
func printingKindErrors(ks []pipe.PassKind) []string {
	var out []string
	for _, k := range ks {
		if k.Out == "output" && !strings.HasSuffix(k.In, "@exec") && k.In != "library" && k.In != "vm-input" {
			out = append(out, fmt.Sprintf("%s prints the output from %s", k.Name, k.In))
		}
	}
	return out
}

// Every registered pass that returns the output takes an Exec-stage unit or the library.
func TestPrintingPassesTakeExecStageUnits(t *testing.T) {
	ks := pipe.PassKinds()
	var printing []string
	for _, k := range ks {
		if k.Out == "output" {
			printing = append(printing, k.Name)
		}
	}
	// The rule proves nothing without the printing passes registered.
	for _, want := range []string{"print-go", "print-ts", "print-ts-lib", "print-libgo", "vmlower"} {
		if !slices.Contains(printing, want) {
			t.Errorf("the printing pass %s is not registered; registered printing passes: %v", want, printing)
		}
	}
	if errs := printingKindErrors(ks); len(errs) > 0 {
		t.Errorf("printing passes on kinds other than Exec-stage units and the library:\n%s", strings.Join(errs, "\n"))
	}
}

// The negative control of the rule: a printing pass planted on code@flow and one on a stage-less kind
// other than library are reported, and the accepted kinds are not.
func TestPrintingKindRuleReportsPlantedPasses(t *testing.T) {
	ks := []pipe.PassKind{
		{Name: "print-flow", In: "code@flow", Out: "output"},
		{Name: "print-program", In: "program", Out: "output"},
		{Name: "print-exec", In: "tables@exec", Out: "output"},
		{Name: "print-lib", In: "library", Out: "output"},
		{Name: "lower-vm", In: "vm-input", Out: "output"},
		{Name: "lower", In: "code@flow", Out: "code@exec"},
	}
	got := printingKindErrors(ks)
	want := []string{"print-flow prints the output from code@flow", "print-program prints the output from program"}
	if !slices.Equal(got, want) {
		t.Errorf("printingKindErrors = %q, want %q", got, want)
	}
}
