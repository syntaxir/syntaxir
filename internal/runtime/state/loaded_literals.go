//go:build !sxir_reference_literals

package state

// loadedLiterals compares a literal with loaded input in one loop, updating the position and the
// examined range once. The reference build (sxir_reference_literals) matches it one character at a
// time through the path of lazily read input, which records the examined range and fills the input
// for each character.
const loadedLiterals = true
