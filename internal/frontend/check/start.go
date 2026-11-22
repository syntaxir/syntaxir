package check

// CheckStart reports why name cannot begin a parse of m: it is empty and there is no default
// start rule, it is hidden, it is not defined, or it is not a start rule (CheckStartName).
func (m *Model) CheckStart(name string) error {
	return CheckStartName(m.StartRules, m.DefaultStartRule, m.definesRule, name)
}

// StartRule returns the start rule name selects.
func (m *Model) StartRule(name string) (*Rule, error) {
	if err := m.CheckStart(name); err != nil {
		return nil, err
	}
	return m.ByName[m.ResolveStart(name)], nil
}

// definesRule reports whether the grammar defines the rule name.
func (m *Model) definesRule(name string) bool { return m.ByName[name] != nil }
