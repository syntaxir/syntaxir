package generate

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// CheckForGenerator is checkForGenerator, for the tests of package generate_test, which import
// codegentest (and through it this package).
var CheckForGenerator = checkForGenerator

// TargetsObserved is Targets with the edits and observers of x.
func TargetsObserved(g *ast.Grammar, x driver.Options, targets ...driver.Target) ([][]common.GeneratedFile, error) {
	for i := range targets {
		targets[i].Gen.LibGo, targets[i].Gen.TSLib = libGo, tsLib
	}
	return TargetsWith(checkForGenerator, g, x, targets...)
}
