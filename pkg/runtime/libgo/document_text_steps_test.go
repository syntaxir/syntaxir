//go:build !sxir_long

package libgo

// documentEditSteps is the number of edits of TestDocumentEditSplicesText in the default tier. The edits
// alternate every 30 steps between growing and shrinking the text, so 60 steps run both phases; the long
// tier (sxir_long) runs 300 edits, a longer walk over the same phases, from document_text_steps_long_test.go.
const documentEditSteps = 60
