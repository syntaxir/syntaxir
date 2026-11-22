package libprint

import (
	"os"
	"testing"
)

// TestLibGoCurrent checks that the printed compositions in the Go library are those the
// compositions print now.
func TestLibGoCurrent(t *testing.T) {
	want, err := printLibGo()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(libGoFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is stale; run go generate ./internal/backend/codegen/golang/libprint", libGoFile)
	}
}

// TestGoActionsCurrent checks that the printed action expression compositions in the Go library are those the
// compositions print now. The generated library's copy is held equal to this file by the library
// copy test of the generator.
func TestGoActionsCurrent(t *testing.T) {
	want, err := printGoActions()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(goActionsFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is stale; run go generate ./internal/backend/codegen/golang/libprint", goActionsFile)
	}
}

// TestGoStreamCurrent checks that the printed stream compositions in the Go library are those the
// compositions print now.
func TestGoStreamCurrent(t *testing.T) {
	want, err := printGoStream()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(goStreamFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is stale; run go generate ./internal/backend/codegen/golang/libprint", goStreamFile)
	}
}

// TestEngineCurrent checks that the printed compositions in the engine are those the compositions
// print now.
func TestEngineCurrent(t *testing.T) {
	for _, f := range []struct {
		file string
		gen  func() (string, error)
	}{
		{engineFile, printEngine},
		{engineActionsFile, printEngineActions},
		{engineOperatorsFile, printEngineOperators},
		{engineStreamFile, printEngineStream},
		{engineDocumentFile, printEngineDocument},
		{engineClosureActionFile, printEngineClosureActions},
		{engineVMActionFile, printEngineVMActions},
	} {
		want, err := f.gen()
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(f.file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s is stale; run go generate ./internal/backend/codegen/golang/libprint", f.file)
		}
	}
}

// TestSpecializationsVerify checks that the functions verify for every specialization the Go
// library and the engine print.
func TestSpecializationsVerify(t *testing.T) {
	if _, err := printLibGo(); err != nil {
		t.Fatal(err)
	}
	if _, err := printGoStream(); err != nil {
		t.Fatal(err)
	}
	if _, err := printEngine(); err != nil {
		t.Fatal(err)
	}
	if _, err := printEngineOperators(); err != nil {
		t.Fatal(err)
	}
	if _, err := printEngineDocument(); err != nil {
		t.Fatal(err)
	}
}
