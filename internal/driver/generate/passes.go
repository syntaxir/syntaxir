package generate

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
)

// Settings returns the optimizations that o.Optimize, o.Opt and o.NoOpt select, in that order, or an
// error for a profile or an optimization that does not exist, a value that it does not take, or a name
// that Opt gives twice (keys that are the same once their spaces are trimmed), and validates
// o.PrintAfter: every name it lists is a step or a pass (driver.PrintNames), and a list that is not
// empty needs o.PrintTo.
func Settings(o common.GenOptions) (pass.Settings, error) {
	_, s, err := resolve(o)
	return s, err
}

// resolve returns the names of o.PrintAfter and the settings of Settings.
func resolve(o common.GenOptions) ([]string, pass.Settings, error) {
	s, err := driver.Profile(o.Optimize)
	if err != nil {
		return nil, pass.Settings{}, err
	}
	printAfter, err := driver.ParsePrintAfter(o.PrintAfter)
	if err != nil {
		return nil, pass.Settings{}, err
	} else if len(printAfter) > 0 && o.PrintTo == nil {
		return nil, pass.Settings{}, fmt.Errorf("PrintAfter %q needs a PrintTo writer", o.PrintAfter)
	}
	given := map[string]string{}
	for _, n := range slices.Sorted(maps.Keys(o.Opt)) {
		name := strings.TrimSpace(n)
		if prev, dup := given[name]; dup {
			return nil, pass.Settings{}, fmt.Errorf("optimization %s is given twice in Opt (%q and %q)", name, prev, n)
		}
		given[name] = n
		if s, err = s.With(name, o.Opt[n]); err != nil {
			return nil, pass.Settings{}, err
		}
	}
	s, err = s.Without(o.NoOpt...)
	if err != nil {
		return nil, pass.Settings{}, err
	}
	return printAfter, s, nil
}

// Hooks are the test hooks of the pipelines of a generation (driver.Hooks). The settings, the dumps and
// the verification come from the GenOptions, so Hooks has no field for them.
type Hooks = driver.Hooks

// Passes returns the passes of a generation with o (common.Passes): the settings of Settings (read through the Runner), and
// runners that are copies of hooks with those settings, the names of o.PrintAfter and
// o.VerifyEachPass. hooks supplies only the test hooks of the driver; the zero value runs the
// pipelines of the passes.
func Passes(o common.GenOptions, hooks Hooks) common.Passes {
	printAfter, s, err := resolve(o)
	d := driver.Driver{
		Opt: s, PrintAfter: printAfter, VerifyEach: o.VerifyEachPass, Hooks: hooks,
	}
	return passes{driver: d, err: err}
}

// passes is the common.Passes of Passes.
type passes struct {
	driver driver.Driver // the driver of every runner, without PrintTo
	err    error         // the error of resolving the settings
}

var _ common.Runner = (*driver.Driver)(nil)

func (p passes) Validate() error { return p.err }

func (p passes) Runner(w io.Writer) common.Runner {
	d := p.driver
	d.PrintTo = w
	return &d
}
