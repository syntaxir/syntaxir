package generate

import (
	"context"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TargetsObserved is Targets under ctx with the edits and observers of x.
func TargetsObserved(ctx context.Context, g *ast.Grammar, x driver.Options, targets ...driver.Target) ([][]common.GeneratedFile, error) {
	for i := range targets {
		targets[i].Gen.LibGo, targets[i].Gen.TSLib = libGo, tsLib
	}
	return TargetsContext(ctx, driver.CheckForGenerator, g, x, targets...)
}
