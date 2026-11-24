package state_test

import "syntaxir.dev/sxir/internal/runtime/engine"

// vmBackends are the bytecode backends; the tests that compare them with the closure backend range
// over them in every build.
var vmBackends = []engine.Backend{engine.Bytecode, engine.BytecodeIterative}
