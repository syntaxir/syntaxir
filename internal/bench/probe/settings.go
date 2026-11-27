package probe

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
)

// A SettingVariant is a profile with one registered flag set against it: the unit of the standing
// per-setting measurements (design record 080, "Re-Measurement"), both of the settings part of
// bench/metrics and of the per-setting timing that the workload manifest exports to the timed
// harness. Value is 0 if the profile turns the flag on, and otherwise the value that turns it on (see
// SettingVariants).
type SettingVariant struct {
	Profile string
	Flag    pass.Flag
	Value   int
}

// Name is the name of the variant in keys and reports: the flag with its value, as "guards=0".
func (v SettingVariant) Name() string { return v.Flag.Name + "=" + strconv.Itoa(v.Value) }

// Args are the options of sxir gen that build the variant: the profile, then the flag's value.
func (v SettingVariant) Args() []string {
	args := []string{"-optimize", v.Profile}
	if v.Value == 0 {
		return append(args, "-no-opt", v.Flag.Name)
	}
	return append(args, "-opt", v.Name())
}

// Opt is the value of the variant's flag, as the generation options take it, if the variant turns it
// on.
func (v SettingVariant) Opt() map[string]int {
	if v.Value == 0 {
		return nil
	}
	return map[string]int{v.Flag.Name: v.Value}
}

// NoOpt is the flag that the variant turns off, if it turns one off.
func (v SettingVariant) NoOpt() []string {
	if v.Value != 0 {
		return nil
	}
	return []string{v.Flag.Name}
}

// SettingVariants returns a variant for each profile and each registered flag, by profile in the
// order of driver.Profiles and by flag name. The value of the flag is 0 if the profile turns it on,
// and otherwise the value that turns it on: 1, or for a flag with a count the largest count that its
// speed value or a profile gives it, so that an opt-in or an experiment is measured at the value that
// a profile adopts or at its proposed one.
func SettingVariants() ([]SettingVariant, error) {
	flags := pass.Flags()
	if len(flags) == 0 {
		return nil, fmt.Errorf("the registry holds no flags")
	}
	slices.SortFunc(flags, func(a, b pass.Flag) int { return strings.Compare(a.Name, b.Name) })
	var out []SettingVariant
	for _, p := range driver.Profiles() {
		s, err := driver.Profile(p)
		if err != nil {
			return nil, err
		}
		for _, f := range flags {
			v := SettingVariant{Profile: p, Flag: f}
			if s.Value(f.Name) == 0 {
				if v.Value, err = onValue(f); err != nil {
					return nil, err
				}
			}
			out = append(out, v)
		}
	}
	return out, nil
}

// onValue is the value that turns f on: 1, or for a flag with a count the largest count that its
// speed value or a profile gives it.
func onValue(f pass.Flag) (int, error) {
	if !f.Int {
		return 1, nil
	}
	v := f.Default
	for _, p := range driver.Profiles() {
		if n, ok := driver.ProfileValues(p)[f.Name]; ok {
			v = max(v, n)
		}
	}
	if v == 0 {
		return 0, fmt.Errorf("flag %s has a count that neither its speed value nor a profile gives", f.Name)
	}
	return v, nil
}
