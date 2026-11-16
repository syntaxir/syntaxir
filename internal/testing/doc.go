// Package testing documents the naming convention of the repository's test support, which lives
// under internal/testing. The package holds no code, and nothing imports it.
//
// A package named <subject>test serves the tests of <subject>: it provides the fixtures, corpora
// or drivers that those tests share, so that the subject's own package does not carry them. For
// example, enginetest compiles and runs grammars for the tests that compare with the engine.
//
// A test-support package that is not of that form is a measurement package: it measures a
// property of the code for the tests and benchmarks to report, and it is test-only. For example,
// recoveryquality measures how well #recover repairs invalid input on two ready-made languages.
//
// Non-test code does not import the test-support packages.
package testing
