package generate

import (
	"maps"
	"slices"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
)

// Settings returns the optimizations that o.Optimize, o.Opt and o.NoOpt select, in that order, or an
// error for a profile or an optimization that does not exist, a value that it does not take, or a name
// that Opt gives twice (keys that are the same once their spaces are trimmed), and validates
// o.Debug: every name that PrintAfter lists is a step or a pass (driver.PrintNames), every name that
// PrintBefore lists is a pass (driver.PrintBeforeNames), a bisection limit is not negative, and a
// Debug that writes anything needs an Output.
func Settings(o common.GenOptions) (pass.Settings, error) {
	return resolve(o)
}

// resolve returns the settings of Settings, after it validated o.Debug.
func resolve(o common.GenOptions) (pass.Settings, error) {
	if _, err := driver.Profile(o.Optimize); err != nil {
		return pass.Settings{}, err
	}
	if err := driver.CheckDebug(o.Debug); err != nil {
		return pass.Settings{}, err
	}
	if err := driver.CheckOptimizable(slices.Sorted(maps.Keys(o.Opt))...); err != nil {
		return pass.Settings{}, err
	}
	if err := driver.CheckOptimizable(o.NoOpt...); err != nil {
		return pass.Settings{}, err
	}
	return driver.GenSettings(o)
}
