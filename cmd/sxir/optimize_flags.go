package main

import (
	"flag"
	"fmt"
	"maps"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/pkg/compiler"
)

// optimizeFlags are -optimize, -opt and -no-opt: the optimization setting of a compilation
// (spec/runtime/optimization-settings.md). Every command that compiles a grammar takes them, with
// the same names, meaning and checks as sxir gen.
type optimizeFlags struct {
	fs                   *flag.FlagSet
	optimize, opt, noOpt *string
}

// registerOptimizeFlags defines the optimization flags in fs.
func registerOptimizeFlags(fs *flag.FlagSet) optimizeFlags {
	return optimizeFlags{
		fs:       fs,
		optimize: fs.String("optimize", "", "optimization profile: "+profileHelp()+"; no profile changes what the parser returns"),
		opt:      fs.String("opt", "", "optimizations to give a value, separated by commas, as name or name=count: a name alone turns on an optimization that the profile leaves off, and 0 turns one off; applied after the profile and before -no-opt; the optimizations, with kind and default: "+pass.Summaries()),
		noOpt:    fs.String("no-opt", "", "optimizations to turn off after the profile is applied, separated by commas, for isolating a miscompilation or measuring one optimization; the optimizations, with kind and default: "+pass.Summaries()),
	}
}

// given reports whether the command line sets any of the flags.
func (f optimizeFlags) given() bool {
	return isFlagSet(f.fs, "optimize") || isFlagSet(f.fs, "opt") || isFlagSet(f.fs, "no-opt")
}

// setting returns the optimization setting of the flags. An unknown profile or optimization, a
// value that an optimization does not take and a malformed list are errors that name the flag.
func (f optimizeFlags) setting() (compiler.Optimize, error) {
	var off []string
	if *f.noOpt != "" {
		off = strings.Split(*f.noOpt, ",")
	}
	profile, err := driver.Profile(*f.optimize)
	if err != nil {
		return compiler.Optimize{}, fmt.Errorf("-optimize: %w", err)
	}
	values, err := driver.ParseValues(*f.opt)
	if err == nil {
		err = driver.CheckOptimizable(slices.Sorted(maps.Keys(values))...)
	}
	if err != nil {
		return compiler.Optimize{}, fmt.Errorf("-opt: %w", err)
	}
	if err := driver.CheckOptimizable(off...); err != nil {
		return compiler.Optimize{}, fmt.Errorf("-no-opt: %w", err)
	}
	for _, n := range slices.Sorted(maps.Keys(values)) {
		if profile, err = profile.With(n, values[n]); err != nil {
			return compiler.Optimize{}, fmt.Errorf("-opt: %w", err)
		}
	}
	if _, err := profile.Without(off...); err != nil {
		return compiler.Optimize{}, fmt.Errorf("-no-opt: %w", err)
	}
	return compiler.Optimize{Profile: *f.optimize, Values: values, Off: off}, nil
}
