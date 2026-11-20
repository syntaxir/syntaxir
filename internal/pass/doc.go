// Package pass holds what the passes of a compilation share outside the pass manager
// (internal/pass/pipe): the registry of the optimizations that turn passes and other choices of the
// pipeline on and off (design 063, Flag, Settings), the analyses of a unit that its passes and tools
// read (AnalysisManager, PreservedSet), and the failure of a required step that the driver returns
// (StepError).
//
// It imports no other package of the module. It holds no state between compilations; the registry is
// filled once, by the init functions of the parts that register flags, and the analysis keys are
// declared once.
package pass
