//go:build !sxir_reference_input_preparation

package lib

// inputPreparation is the region of lib.ts that prepares code-point input from a string in one pass.
// The reference build (sxir_reference_input_preparation) replaces it with three passes.
var inputPreparation = region{name: "input-preparation"}
