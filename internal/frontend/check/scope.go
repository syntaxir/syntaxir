package check

// Scope maps capture names to slots. Each rule body, repetition element, and Operators Rule line
// has its own scope.
type Scope struct {
	Names []string
	Slots map[string]int
	// outer is the scope of the rule body that contains a repetition element's scope, for error
	// messages: its captures are not visible in the element.
	outer *Scope
}

// NewScope returns an empty scope.
func NewScope() *Scope { return &Scope{Slots: map[string]int{}} }

// NewElementScope returns an empty scope of a repetition element inside the scope outer, whose
// captures the element does not see.
func NewElementScope(outer *Scope) *Scope {
	s := NewScope()
	s.outer = outer
	return s
}

// Slot returns the slot of the capture name, adding one if the scope has none.
func (s *Scope) Slot(name string) int {
	if i, ok := s.Slots[name]; ok {
		return i
	}
	s.Slots[name] = len(s.Names)
	s.Names = append(s.Names, name)
	return s.Slots[name]
}

// Has reports whether the scope has the capture name.
func (s *Scope) Has(name string) bool { _, ok := s.Slots[name]; return ok }

// InEnclosing reports whether a scope that encloses s has the capture name.
func (s *Scope) InEnclosing(name string) bool {
	for o := s.outer; o != nil; o = o.outer {
		if _, ok := o.Slots[name]; ok {
			return true
		}
	}
	return false
}
