package vmlower_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/vmlowertest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// A module whose rule converts its value to a type that is not a named node type is rejected.
func TestNodeTypeModuleValidation(t *testing.T) {
	t.Parallel()
	prog := compiletest.Program(t, `type Name = node[string]
rule main = name ("," name)*
rule name Name = ([a-z]+) #atomic`, check.Options{})
	mp, _, err := vmlowertest.Lower(vmInput(t, prog))
	if err != nil {
		t.Fatal(err)
	}
	m := *mp
	if err := program.ValidateModule(&m); err != nil {
		t.Fatal(err)
	}
	m.Rules = append([]bytecode.RuleInfo(nil), m.Rules...)
	found := false
	for i := range m.Rules {
		if m.Rules[i].NodeType >= 0 {
			m.Rules[i].NodeType = m.Rules[i].Name
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no rule of a named node type")
	}
	if err := program.ValidateModule(&m); err == nil || !strings.Contains(err.Error(), "is not a named node type") {
		t.Fatalf("got %v", err)
	}
}

// TestModuleVariableValidation checks that a module whose code names a variable that its table
// does not declare is rejected, wherever the variable appears: an immediate of a Flow body is a slot
// of the table, and a rule lists names. Action code is checked when the engine loads it.
func TestModuleVariableValidation(t *testing.T) {
	t.Parallel()
	fresh := func() *bytecode.Module {
		m := *compileModule(t, varModuleSource)
		m.Strings = append([]string(nil), m.Strings...)
		m.Rules = append([]bytecode.RuleInfo(nil), m.Rules...)
		return &m
	}
	if err := program.ValidateModule(fresh()); err != nil {
		t.Fatal(err)
	}
	undeclared := func(m *bytecode.Module) int {
		m.Strings = append(m.Strings, "ghost")
		return len(m.Strings) - 1
	}
	for _, tc := range []struct {
		name   string
		want   string
		mutate func(m *bytecode.Module) bool
	}{
		{"binding", "variable 3 out of range", func(m *bytecode.Module) bool {
			for i, f := range m.Flows {
				for j, in := range f.Code {
					if in.Op == bytecode.FlowBind {
						g := &bytecode.Flow{Regs: f.Regs, Code: slices.Clone(f.Code)}
						g.Code[j].Imm = bytecode.FlowBindOf{Var: len(m.Vars), Action: in.Imm.(bytecode.FlowBindOf).Action}
						m.Flows = maps.Clone(m.Flows)
						m.Flows[i] = g
						return true
					}
				}
			}
			return false
		}},
		{"rule variables", `variable "ghost" is not declared`, func(m *bytecode.Module) bool {
			for i := range m.Rules {
				if len(m.Rules[i].Vars) != 0 {
					m.Rules[i].Vars = []int{undeclared(m)}
					return true
				}
			}
			return false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fresh()
			if !tc.mutate(m) {
				t.Fatal("nothing to mutate")
			}
			if err := program.ValidateModule(m); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// TestModuleInitialValueMatchesType checks that a module whose variable holds an initial value of
// another type than its declared one is rejected: an int or bool variable with a string, a string
// variable with an int or without a string, a bool that is neither 0 nor 1, and an unknown type.
func TestModuleInitialValueMatchesType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(m *bytecode.Module)
	}{
		{"string for an int", func(m *bytecode.Module) { m.Vars[0].Str = m.Vars[1].Str }},
		{"int for a string", func(m *bytecode.Module) { m.Vars[1].Int = 5 }},
		{"no string for a string", func(m *bytecode.Module) { m.Vars[1].Str = -1 }},
		{"2 for a bool", func(m *bytecode.Module) { m.Vars[2].Int = 2 }},
		{"a string for a bool", func(m *bytecode.Module) { m.Vars[2].Str = m.Vars[1].Str }},
		{"unknown type", func(m *bytecode.Module) { m.Vars[0].Type = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := *compileModule(t, varModuleSource)
			m.Vars = append([]bytecode.VarInfo(nil), m.Vars...)
			tc.mutate(&m)
			if err := program.ValidateModule(&m); err == nil {
				t.Fatal("a module with a mistyped initial value was accepted")
			}
		})
	}
}
