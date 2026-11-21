package compile_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/enginetest"
)

// The tests of this package that generate parsers compile their grammars with the engine, as
// package engine does.
func init() { enginetest.UseEngine() }

func TestMain(m *testing.M) { enginetest.Main(m) }
