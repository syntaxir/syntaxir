package enginetest

import (
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
)

// ValueModes are the modes of compiled grammars that build values: parse mode serves plain parses
// and document mode traced ones (ServedBy).
var ValueModes = []engine.CompiledMode{engine.CompiledParse, engine.CompiledDocument}

// ServedBy returns o as a parse that a program loaded in mode serves: a recognize-mode program
// serves recognition, and a document-mode program traced parses, so o gets a trace that does
// nothing unless it has one.
func ServedBy(mode engine.CompiledMode, o engine.ParseOptions) engine.ParseOptions {
	switch mode {
	case engine.CompiledRecognize:
		o.Recognize = true
	case engine.CompiledDocument:
		if o.Trace == nil {
			o.Trace = func(engine.TraceEvent) {}
		}
	}
	return o
}

// SaveLoad saves prog in mode with the start rule start and loads it again.
func SaveLoad(t *testing.T, prog *engine.Program, start string, mode engine.CompiledMode) *engine.Program {
	t.Helper()
	data, err := prog.Marshal(start, mode)
	if err != nil {
		t.Fatalf("save in %s mode: %v", mode, err)
	}
	loaded, _, err := engine.LoadProgram(data)
	if err != nil {
		t.Fatalf("load in %s mode: %v", mode, err)
	}
	return loaded
}
