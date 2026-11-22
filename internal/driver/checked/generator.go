package checked

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
)

// Generator returns the view of the checked program that the generators read (driver.View).
func (s *Source) Generator() *common.Program { return driver.View(s.prog) }
