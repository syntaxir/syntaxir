package probe

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
)

// SettingVariants sets each registered flag once against each profile: off where the profile turns
// it on, and otherwise on, a count flag at the largest count that its speed value or a profile gives.
func TestSettingVariants(t *testing.T) {
	vs, err := SettingVariants()
	if err != nil {
		t.Fatal(err)
	}
	if want := len(driver.Profiles()) * len(pass.Flags()); len(vs) != want {
		t.Fatalf("%d variants, want one for each profile and flag, %d", len(vs), want)
	}
	byName := map[string]SettingVariant{}
	for _, v := range vs {
		byName[v.Profile+" "+v.Flag.Name] = v
	}
	for _, c := range []struct {
		profile, flag string
		value         int
		args          []string
	}{
		{"speed", "guards", 0, []string{"-optimize", "speed", "-no-opt", "guards"}},
		{"speed", "class-tables", 5, []string{"-optimize", "speed", "-opt", "class-tables=5"}},
		{"size", "class-tables", 0, []string{"-optimize", "size", "-no-opt", "class-tables"}},
		{"size", "inline-literals", 4, []string{"-optimize", "size", "-opt", "inline-literals=4"}},
		{"memory", "typed-object-slabs", 1, []string{"-optimize", "memory", "-opt", "typed-object-slabs=1"}},
	} {
		v, ok := byName[c.profile+" "+c.flag]
		if !ok {
			t.Errorf("no variant of %s in %s", c.flag, c.profile)
			continue
		}
		if v.Value != c.value || !slices.Equal(v.Args(), c.args) {
			t.Errorf("%s in %s: value %d, args %v; want %d, %v", c.flag, c.profile, v.Value, v.Args(), c.value, c.args)
		}
		if (v.Opt() == nil) != (c.value == 0) || (v.NoOpt() == nil) != (c.value != 0) {
			t.Errorf("%s in %s: Opt %v, NoOpt %v for value %d", c.flag, c.profile, v.Opt(), v.NoOpt(), c.value)
		}
	}
}
