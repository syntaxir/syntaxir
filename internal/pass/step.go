package pass

// Step names a step of the pipeline of a compilation in the failure of a required step.
type Step struct {
	// Name names the step: the name of a pass, or of a required step such as a lowering.
	Name string
	// Required reports that the step always runs: it has no flag, and no setting skips it.
	Required bool
}

// Label names the unit of a step in the dumps: the table it belongs to and, for a unit that is one
// function, the function.
type Label struct {
	Table, Func string
}

// String returns the label as the dumps and the errors print it: the table, and the function after a
// slash if there is one.
func (l Label) String() string {
	if l.Func == "" {
		return l.Table
	}
	return l.Table + "/" + l.Func
}

// StepError is the failure of a required step, which it names.
type StepError struct {
	Step Step
	Err  error
}

// Error is the message of the failure itself: the step adds no words to it.
func (e *StepError) Error() string { return e.Err.Error() }

func (e *StepError) Unwrap() error { return e.Err }
