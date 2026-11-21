package archtest

import (
	"path/filepath"
	"testing"
)

// TestModuleRootHoldsNoPackage checks that the root of the module holds no Go file: the module's
// packages are its subdirectories (core, engine, codegen and the others), and a package at the root
// would be imported as syntaxir.dev/sxir beside them.
func TestModuleRootHoldsNoPackage(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	files, err := filepath.Glob(filepath.Join(root, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		t.Errorf("%s: the module root holds a Go file; put it in the package it belongs to", filepath.Base(name))
	}
}
