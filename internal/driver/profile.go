// Package driver runs the compiler pipeline under one optimization setting (design 063): it holds the
// optimization profiles, parses the values that a request gives the optimizations, names the steps
// that -print-after selects, and drives the Core, Flow and Exec pass pipelines with their dumps and
// verification. Every backend that lowers from the IR runs its pipelines through a Driver, so a
// profile or a flag means the same passes whatever the backend.
//
// The optimizations themselves are flags of the registry in internal/pass, which each part of the
// pipeline fills with its own. A profile is a preset: a name for fixed values of flags, with no logic
// of its own.
package driver

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	"syntaxir.dev/sxir/internal/middleend/actionlower"
	"syntaxir.dev/sxir/internal/middleend/actionpass"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/pass"
)

// ParseValues parses a comma-separated list of name or name=count items, as -opt takes it, into the
// values by name: a name alone is 1. Spaces around an item are ignored. A repeated name, a count
// that is not a decimal number or an empty item is an error; whether a name exists, and whether it
// takes the value, is for Settings.With.
func ParseValues(list string) (map[string]int, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	out := map[string]int{}
	for _, item := range strings.Split(list, ",") {
		name, num, hasValue := strings.Cut(strings.TrimSpace(item), "=")
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("empty optimization name in %q", list)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("optimization %s is given twice", name)
		}
		v := 1
		if hasValue {
			n, err := strconv.Atoi(strings.TrimSpace(num))
			if err != nil {
				return nil, fmt.Errorf("optimization %s: %q is not a count", name, strings.TrimSpace(num))
			}
			v = n
		}
		out[name] = v
	}
	return out, nil
}

// A profileDef is a preset: the values that differ from the speed values of the flags, and what the
// preset is for. It has no logic of its own.
type profileDef struct {
	Name   string
	Doc    string
	Values map[string]int
}

// profileDefs lists the profiles; the first is the default. The values were chosen by the rule of
// record 136 (docs/optimizations/136-measured-effect-of-the-generation-flags.md): a profile is kept
// if it improves the metric it is named for by at least 1% beyond the noise of an identical copy, on
// at least six of the ten language grammars, costs at most 3% of parse time as a geometric mean, and
// slows no grammar by more than 6%.
var profileDefs = []profileDef{
	{Name: "speed", Doc: "the fastest parse that does not cost generated code or memory out of proportion; the default"},
	{Name: "memory", Doc: "typed values are allocated one by one: fewer bytes allocated and less live heap, with many more allocations",
		Values: map[string]int{"typed-object-slabs": 0, "typed-list-slabs": 0}},
	{Name: "size", Doc: "smaller generated Go source and binaries, for a little parse time",
		Values: map[string]int{"inline-literals": 0, "class-tables": 5}},
}

// checkProfiles reports a profile that breaks the rules: a repeated or undocumented one, a value of
// a flag that flags does not hold or one the flag does not take, and a value for a flag that changes
// the output.
func checkProfiles(defs []profileDef, flags []pass.Flag) error {
	seen := map[string]bool{}
	for _, d := range defs {
		if d.Name == "" || d.Doc == "" || seen[d.Name] {
			return fmt.Errorf("profile %q has no name or no description, or is declared twice", d.Name)
		}
		seen[d.Name] = true
		for _, name := range slices.Sorted(maps.Keys(d.Values)) {
			v := d.Values[name]
			i := slices.IndexFunc(flags, func(f pass.Flag) bool { return f.Name == name })
			switch {
			case i < 0:
				return fmt.Errorf("profile %s sets %q, which is not a registered flag: the part of the pipeline that registers it is not linked", d.Name, name)
			case flags[i].ChangesOutput:
				return fmt.Errorf("profile %s sets %s, which changes the output", d.Name, name)
			case v < 0 || !flags[i].Int && v > 1:
				return fmt.Errorf("profile %s gives %s the value %d", d.Name, name, v)
			}
		}
	}
	return nil
}

// CheckRegistry reports a registry that breaks the rules that only the driver knows: a flag with the name of
// a step that -print-after selects, and a profile that is not valid over the registered flags. A
// program checks it once all the parts it links have registered their flags.
func CheckRegistry() error {
	flags := pass.Flags()
	if err := checkStepNames(flags); err != nil {
		return err
	}
	return checkProfiles(profileDefs, flags)
}

// checkStepNames reports a flag that has the name of a required step or "all", which -print-after
// would select both by.
func checkStepNames(flags []pass.Flag) error {
	for _, f := range flags {
		if slices.Contains(requiredSteps(), f.Name) || f.Name == PrintAll {
			return fmt.Errorf("flag %s has the name of a step that -print-after selects", f.Name)
		}
	}
	return nil
}

// Profile returns the settings of the named profile; the empty name is the default, speed. A profile
// that sets a flag that no linked part registers is an error that names the flag, never settings
// without it.
func Profile(name string) (pass.Settings, error) {
	if name == "" {
		name = profileDefs[0].Name
	}
	for _, d := range profileDefs {
		if d.Name != name {
			continue
		}
		if err := checkProfiles([]profileDef{d}, pass.Flags()); err != nil {
			return pass.Settings{}, err
		}
		s := pass.Settings{}
		for _, flag := range slices.Sorted(maps.Keys(d.Values)) {
			var err error
			if s, err = s.With(flag, d.Values[flag]); err != nil {
				return pass.Settings{}, err
			}
		}
		return s, nil
	}
	return pass.Settings{}, fmt.Errorf("unknown optimization profile %q (valid: %s)", name, strings.Join(Profiles(), ", "))
}

// Profiles returns the names of the profiles; the first is the default.
func Profiles() []string {
	out := make([]string, len(profileDefs))
	for i, d := range profileDefs {
		out[i] = d.Name
	}
	return out
}

// ProfileDoc returns the one line that says what the profile is for, or "" for an unknown name.
func ProfileDoc(name string) string {
	for _, d := range profileDefs {
		if d.Name == name {
			return d.Doc
		}
	}
	return ""
}

// ProfileValues returns the flags that the profile sets to other than their speed values, by name.
func ProfileValues(name string) map[string]int {
	for _, d := range profileDefs {
		if d.Name == name {
			return maps.Clone(d.Values)
		}
	}
	return nil
}

// PrintAll selects every step and every pass for printing.
const PrintAll = "all"

// requiredSteps returns the names of the required steps of the pipeline in the order they run:
// elaborate, flowlower, flow-finish and execlower.
func requiredSteps() []string {
	return []string{Elaborate, flowlower.Name, flowpass.FinishName, execlower.Name}
}

// CheckOptimizable reports a name in names that is a required step, which runs whatever the settings
// say: a setting that turns one on or off (-opt, -no-opt, the overrides of the Go setting) is an
// error that names it, rather than an unknown optimization. Spaces around a name are ignored.
func CheckOptimizable(names ...string) error {
	for _, n := range names {
		if n = strings.TrimSpace(n); slices.Contains(requiredSteps(), n) {
			return fmt.Errorf("%s is a required step and always runs", n)
		}
	}
	return nil
}

// PrintNames returns the names of the steps and passes that select what to print after, in the order
// they run: elaborate, the Core passes, dispatch, outline, plan, flowlower, the Flow passes, flow-finish, execlower, the
// Exec passes and those of the registered targets (golang/pass, typescript/pass), and "all". A pass
// that runs at several stages, such as canonicalize, is named once, at its first place.
func PrintNames() []string {
	out := []string{Elaborate}
	out = append(out, corepass.Names()...)
	out = append(out, actionlower.PassLower, actionpass.PassTextDispatch, gopass.PassFieldCompare, gopass.PassStructCall, gopass.PassInPlace, actionpass.PassFinish)
	out = append(out, flowplan.PassDispatch, flowplan.PassOutline, flowplan.PlanName, flowlower.Name)
	out = append(out, flowpass.Names()...)
	out = append(out, flowpass.FinishName, execlower.Name)
	out = append(out, codepass.Names()...)
	for _, ps := range targets {
		for _, p := range ps {
			out = append(out, p.Name())
		}
	}
	var names []string
	for _, n := range out {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	return append(names, PrintAll)
}

// stageSteps maps the names of the stages, which select no dump, to the step that makes the unit
// of the stage, for the hint of an error.
var stageSteps = map[string]string{"core": Elaborate, "action": actionlower.PassLower, "flow": flowlower.Name, "exec": execlower.Name}

// ParsePrintAfter validates a comma-separated list of step and pass names for printing, as
// -print-after takes it, and returns the names in order, without the spaces around them. The empty
// string selects nothing.
func ParsePrintAfter(list string) ([]string, error) {
	out := splitNames(list)
	if err := CheckPrintAfter(out); err != nil {
		return nil, err
	}
	return out, nil
}

// ParsePrintBefore validates a comma-separated list of pass names for printing, as -print-before
// takes it, and returns the names in order, without the spaces around them. The empty string
// selects nothing.
func ParsePrintBefore(list string) ([]string, error) {
	out := splitNames(list)
	if err := CheckPrintBefore(out); err != nil {
		return nil, err
	}
	return out, nil
}

func splitNames(list string) []string {
	if list == "" {
		return nil
	}
	var out []string
	for _, n := range strings.Split(list, ",") {
		out = append(out, strings.TrimSpace(n))
	}
	return out
}

// CheckPrintAfter reports a name in names that is not a step or a pass to print after (PrintNames).
// A stage name is an error that names the step that makes the IR of the stage.
func CheckPrintAfter(names []string) error {
	valid := PrintNames()
	for _, n := range names {
		if !slices.Contains(valid, n) {
			if step, ok := stageSteps[n]; ok {
				return fmt.Errorf("unknown pass or step %q to print after: %s is a stage; the step that makes its IR is %s (valid: %s)", n, n, step, strings.Join(valid, ", "))
			}
			return fmt.Errorf("unknown pass or step %q to print after (valid: %s)", n, strings.Join(valid, ", "))
		}
	}
	return nil
}

// PrintBeforeNames returns the names of the passes that select what to print before, in the order
// they run, and "all": the names of PrintNames without the required steps, whose input is the dump
// after the step before them.
func PrintBeforeNames() []string {
	return slices.DeleteFunc(PrintNames(), func(n string) bool { return slices.Contains(requiredSteps(), n) })
}

// CheckPrintBefore reports a name in names that is not a pass to print before (PrintBeforeNames). A
// required step is an error that says so; a stage name is an error like any unknown name.
func CheckPrintBefore(names []string) error {
	valid := PrintBeforeNames()
	for _, n := range names {
		switch {
		case slices.Contains(valid, n):
		case slices.Contains(requiredSteps(), n):
			return fmt.Errorf("%s is a required step, and -print-before takes passes; -print-after prints the unit after the step before it", n)
		default:
			return fmt.Errorf("unknown pass %q to print before (valid: %s)", n, strings.Join(valid, ", "))
		}
	}
	return nil
}

// CheckDebug reports what is wrong with the debugging settings d: a name that PrintAfter lists that is
// not a step or a pass (CheckPrintAfter), one that PrintBefore lists that is not a pass
// (CheckPrintBefore), a negative bisection limit, or settings that write something without an Output.
func CheckDebug(d common.Debug) error {
	if err := CheckPrintAfter(d.PrintAfter); err != nil {
		return err
	} else if err := CheckPrintBefore(d.PrintBefore); err != nil {
		return err
	} else if l := d.OptBisectLimit; l != nil && *l < 0 {
		return fmt.Errorf("the bisection limit %d is negative", *l)
	} else if d.Writes() && d.Output == nil {
		return fmt.Errorf("the debugging settings need an Output writer")
	}
	return nil
}
