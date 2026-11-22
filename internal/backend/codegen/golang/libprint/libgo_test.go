package libprint

import "testing"

// TestLibGoCurrent checks that the printed compositions in the Go library are those the
// compositions print now.
func TestLibGoCurrent(t *testing.T) {
	checkCurrent(t, libGoFile)
}

// TestGoActionsCurrent checks that the printed action expression compositions in the Go library are those the
// compositions print now. The generated library's copy is held equal to this file by the library
// copy test of the generator.
func TestGoActionsCurrent(t *testing.T) {
	checkCurrent(t, goActionsFile)
}

// TestGoStreamCurrent checks that the printed stream compositions in the Go library are those the
// compositions print now.
func TestGoStreamCurrent(t *testing.T) {
	checkCurrent(t, goStreamFile)
}

// TestEngineCurrent checks that the printed compositions in the engine are those the compositions
// print now.
func TestEngineCurrent(t *testing.T) {
	for _, f := range []string{
		engineFile,
		engineActionsFile,
		engineOperatorsFile,
		engineStreamFile,
		engineDocumentFile,
		engineClosureActionFile,
		engineVMActionFile,
	} {
		checkCurrent(t, f)
	}
}

// TestSpecializationsVerify checks that the functions verify for every specialization the Go
// library and the engine print: the run of testLibrary verifies every unit, and the printers verify
// the functions they print.
func TestSpecializationsVerify(t *testing.T) {
	if _, err := testLibrary(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{libGoFile, goStreamFile, engineFile, engineOperatorsFile, engineDocumentFile} {
		printOutput(t, f)
	}
}
