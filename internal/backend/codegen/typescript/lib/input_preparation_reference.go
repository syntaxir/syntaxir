//go:build sxir_reference_input_preparation

package lib

import _ "embed"

//go:embed reference/input-preparation.region
var inputPreparationBody string

// The reference build prepares code-point input from a string in three passes.
var inputPreparation = region{name: "input-preparation", body: inputPreparationBody}
