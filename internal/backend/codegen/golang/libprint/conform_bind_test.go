package libprint

import "testing"

// TestEngineExpBindingsCurrent checks that the engine's conformance bindings of the expectation record
// are those the engine's printer prints now.
func TestEngineExpBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineExpBindingsFile)
}

// TestEngineDocBindingsCurrent checks that the engine's conformance bindings of the doc operations are
// those the engine's printer prints now.
func TestEngineDocBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineDocBindingsFile)
}

// TestEngineOprBindingsCurrent checks that the engine's conformance bindings of the Operators Rule
// operations are those the engine's printer prints now.
func TestEngineOprBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineOprBindingsFile)
}

// TestEngineCtxBindingsCurrent checks that the engine's conformance bindings of the dependency memo
// are those the engine's printer prints now.
func TestEngineCtxBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineCtxBindingsFile)
}

// TestEngineFrameBindingsCurrent checks that the engine's conformance bindings of the capture frames
// are those the engine's printer prints now.
func TestEngineFrameBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineFrameBindingsFile)
}

// TestEngineRuleBindingsCurrent checks that the engine's conformance bindings of the operations of the
// rules are those the engine's printer prints now.
func TestEngineRuleBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineRuleBindingsFile)
}

// TestEngineMissingBindingsCurrent checks that the engine's conformance bindings of the missing-token
// mode are those the engine's printer prints now.
func TestEngineMissingBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineMissingBindingsFile)
}

// TestEngineCallBindingsCurrent checks that the engine's conformance bindings of the traced call path
// are those the engine's printer prints now.
func TestEngineCallBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineCallBindingsFile)
}

// TestEngineActionStateBindingsCurrent checks that the engine's conformance bindings of the elements gathered
// and the values made are those the engine's printer prints now.
func TestEngineActionStateBindingsCurrent(t *testing.T) {
	checkCurrent(t, engineActionStateBindingsFile)
}
