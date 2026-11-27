package pass

import (
	"fmt"
	"slices"
	"strings"
)

// The catalog of the optimizations (design record 080). Each flag implements one mechanism of the
// optimization catalog (docs/optimizations), names the record that decided its state and the backends
// whose parser it changes. The mechanisms, the reference controls (build tags that compare a mechanism
// where no setting can select it at no cost), the names of removed optimizations, the mechanisms not
// classified yet and the former IDs are declared here, once; the catalog's tables are generated from
// them.

// Backend is a backend whose parser an optimization can change.
type Backend uint8

const (
	BackendClosure Backend = iota
	BackendRecursiveVM
	BackendIterativeVM
	BackendGoNode
	BackendGoRecognizer
	BackendTypedGo
	BackendTypeScript
	numBackends
)

// AllBackends lists the backends in the order the catalog's columns list them.
func AllBackends() []Backend {
	out := make([]Backend, numBackends)
	for i := range out {
		out[i] = Backend(i)
	}
	return out
}

func (b Backend) String() string {
	switch b {
	case BackendClosure:
		return "closure"
	case BackendRecursiveVM:
		return "recursive VM"
	case BackendIterativeVM:
		return "iterative VM"
	case BackendGoNode:
		return "Go Node"
	case BackendGoRecognizer:
		return "Go recognizer"
	case BackendTypedGo:
		return "typed Go"
	case BackendTypeScript:
		return "TypeScript"
	}
	return fmt.Sprintf("backend(%d)", int(b))
}

// Backends is a set of backends.
type Backends uint8

// On returns the set of the backends.
func On(bs ...Backend) Backends {
	var s Backends
	for _, b := range bs {
		s |= 1 << b
	}
	return s
}

// Engines are the backends of the engine, which build their parsers under the Core passes:
// the closure engine and both bytecode VMs.
var Engines = On(BackendClosure, BackendRecursiveVM, BackendIterativeVM)

// GeneratedGo are the Go parsers that the generator prints.
var GeneratedGo = On(BackendGoNode, BackendGoRecognizer, BackendTypedGo)

// Generated are the parsers that the generator prints: Go and TypeScript.
var Generated = GeneratedGo | On(BackendTypeScript)

// Has reports whether the set holds b.
func (s Backends) Has(b Backend) bool { return s&(1<<b) != 0 }

// List returns the backends of the set in the order of AllBackends.
func (s Backends) List() []Backend {
	var out []Backend
	for _, b := range AllBackends() {
		if s.Has(b) {
			out = append(out, b)
		}
	}
	return out
}

func (s Backends) String() string {
	var names []string
	for _, b := range s.List() {
		names = append(names, b.String())
	}
	return strings.Join(names, ", ")
}

// A Mechanism is an optimization mechanism of the catalog. Its controls, the flags and reference
// controls that name it, turn it off for a comparison.
type Mechanism struct {
	// ID is the stable ID of the catalog: a category letter and three digits, as "M010".
	ID string
	// Title names the mechanism in the catalog's index.
	Title string
	// Page is the file of the mechanism's page in docs/optimizations/mechanisms.
	Page string
	// Scope says where the mechanism applies, as the index's path scope.
	Scope string
	// Full are the backends on whose every parser the mechanism acts, and Partial those where it
	// acts on an eligible subset that the page names. A control lists only backends that the
	// mechanism acts on, and at a release every backend it acts on is listed by one of its controls.
	Full, Partial Backends
}

// Reaches returns the backends the mechanism acts on.
func (m Mechanism) Reaches() Backends { return m.Full | m.Partial }

// A ReferenceControl is a build tag that selects the reference form of a mechanism of the engine or of
// a runtime library, where no setting can choose it when the parser is built or generated at no cost
// on the default path. It is compared across two builds, one with the default tags and one with the
// tag; it is not bisected and is not part of the command line.
type ReferenceControl struct {
	Mechanism string
	Tag       string
	Backends  Backends
	// Record is the optimization record that says why no setting selects the mechanism.
	Record int
}

// A Removed optimization is the name of a flag that was removed, with the record that removed it. A
// request that names it is an error that names the record.
type Removed struct {
	Name   string
	Record int
}

// Mechanisms returns the declared mechanisms, by ID.
func Mechanisms() []Mechanism { return slices.Clone(mechanisms) }

// LookupMechanism returns the declared mechanism of the ID.
func LookupMechanism(id string) (Mechanism, bool) {
	i := slices.IndexFunc(mechanisms, func(m Mechanism) bool { return m.ID == id })
	if i < 0 {
		return Mechanism{}, false
	}
	return mechanisms[i], true
}

// ReferenceControls returns the declared reference controls.
func ReferenceControls() []ReferenceControl { return slices.Clone(referenceControls) }

// RemovedFlags returns the names of the removed optimizations.
func RemovedFlags() []Removed { return slices.Clone(removedFlags) }

// Unclassified returns the IDs of the catalog's mechanisms that are not classified yet as an
// optimization or an implementation design. The list is empty at a release.
func Unclassified() []string { return slices.Clone(unclassified) }

// Former returns the IDs that are no longer mechanisms of the catalog: the implementation designs and
// the retired mechanisms, each of which a design page carries in a marker with the reason, and the
// rejected attempts, each of which an optimization record with status Rejected names. An ID is never
// reused, so a former ID is never declared again.
func Former() []string { return slices.Clone(former) }

// removedNamed returns the removed optimization of the name.
func removedNamed(name string) (Removed, bool) {
	i := slices.IndexFunc(removedFlags, func(r Removed) bool { return r.Name == name })
	if i < 0 {
		return Removed{}, false
	}
	return removedFlags[i], true
}

// Control says how a mechanism can be turned off for a comparison.
type Control int

const (
	// ControlNone: no control yet, a state that the release criterion forbids.
	ControlNone Control = iota
	// ControlReference: only reference controls.
	ControlReference
	// ControlFlag: at least one flag.
	ControlFlag
)

func (c Control) String() string {
	switch c {
	case ControlFlag:
		return "flag"
	case ControlReference:
		return "reference"
	}
	return "none"
}

// ControlOf returns the control of the mechanism among the flags and the declared reference controls.
func ControlOf(id string, flags []Flag) Control {
	if slices.ContainsFunc(flags, func(f Flag) bool { return f.Mechanism == id }) {
		return ControlFlag
	}
	if slices.ContainsFunc(referenceControls, func(r ReferenceControl) bool { return r.Mechanism == id }) {
		return ControlReference
	}
	return ControlNone
}

// Uncontrolled returns the backends that the mechanism acts on and that none of its controls among the
// flags and the declared reference controls lists. A release has none for any mechanism; before it, a
// mechanism may act on backends whose control has not landed yet.
func Uncontrolled(m Mechanism, flags []Flag) Backends {
	return m.Reaches() &^ controlled(m.ID, flags, referenceControls)
}

// controlled returns the backends that the controls of the mechanism list.
func controlled(id string, flags []Flag, refs []ReferenceControl) Backends {
	var s Backends
	for _, f := range flags {
		if f.Mechanism == id {
			s |= f.Backends
		}
	}
	for _, r := range refs {
		if r.Mechanism == id {
			s |= r.Backends
		}
	}
	return s
}

// A catalog holds the declarations that CheckCatalog checks.
type catalog struct {
	mechanisms   []Mechanism
	refs         []ReferenceControl
	removed      []Removed
	unclassified []string
	former       []string
}

// declared is the catalog of the declarations of this package.
func declared() catalog {
	return catalog{mechanisms, referenceControls, removedFlags, unclassified, former}
}

// CheckCatalog reports what breaks the rules of the catalog in the flags, which are the registry of a
// program that links every part: a flag without a mechanism, a record, a trade-off, backends or
// witnesses; a mechanism that is not declared or not classified; an ID declared twice, or in more than
// one of the mechanisms, the unclassified mechanisms and the former IDs; a control that changes a
// backend that its mechanism does not act on; a reference control of an unknown mechanism; and a
// removed name that is registered.
func CheckCatalog(flags []Flag) error { return declared().check(flags) }

func (c catalog) check(flags []Flag) error {
	ids := map[string]string{}
	claim := func(id, list string) error {
		if prev, ok := ids[id]; ok {
			if prev == list {
				return fmt.Errorf("%s %s is declared twice", list, id)
			}
			return fmt.Errorf("%s is both %s and %s", id, prev, list)
		}
		ids[id] = list
		return nil
	}
	for _, m := range c.mechanisms {
		switch {
		case m.ID == "" || m.Title == "" || m.Page == "" || m.Scope == "":
			return fmt.Errorf("mechanism %q has no ID, title, page or scope", m.ID)
		case m.Full&m.Partial != 0:
			return fmt.Errorf("mechanism %s acts both fully and partly on %s", m.ID, m.Full&m.Partial)
		}
		if err := claim(m.ID, "mechanism"); err != nil {
			return err
		}
	}
	for _, id := range c.unclassified {
		if err := claim(id, "unclassified mechanism"); err != nil {
			return err
		}
	}
	for _, id := range c.former {
		if err := claim(id, "former ID"); err != nil {
			return err
		}
	}
	known := func(id string) bool {
		return ids[id] == "mechanism" || ids[id] == "unclassified mechanism"
	}
	for _, f := range flags {
		switch {
		case f.Mechanism == "":
			return fmt.Errorf("flag %s names no mechanism", f.Name)
		case !known(f.Mechanism):
			return fmt.Errorf("flag %s names the mechanism %s, which is neither declared nor unclassified", f.Name, f.Mechanism)
		case f.Record <= 0:
			return fmt.Errorf("flag %s names no record", f.Name)
		case f.Tradeoff == "":
			return fmt.Errorf("flag %s says nothing of what it trades", f.Name)
		case f.Backends == 0:
			return fmt.Errorf("flag %s changes the parser of no backend", f.Name)
		case len(f.Witnesses) == 0:
			return fmt.Errorf("flag %s names no witness of the backends it changes", f.Name)
		}
	}
	for _, r := range c.refs {
		switch {
		case !known(r.Mechanism):
			return fmt.Errorf("reference control %s names the mechanism %s, which is neither declared nor unclassified", r.Tag, r.Mechanism)
		case r.Tag == "" || r.Record <= 0 || r.Backends == 0:
			return fmt.Errorf("reference control of %s has no tag, record or backends", r.Mechanism)
		}
	}
	for _, m := range c.mechanisms {
		if extra := controlled(m.ID, flags, c.refs) &^ m.Reaches(); extra != 0 {
			return fmt.Errorf("mechanism %s acts on %s, but its controls also change %s", m.ID, m.Reaches(), extra)
		}
	}
	for _, r := range c.removed {
		if slices.ContainsFunc(flags, func(f Flag) bool { return f.Name == r.Name }) {
			return fmt.Errorf("the removed optimization %s is registered", r.Name)
		}
		if r.Record <= 0 {
			return fmt.Errorf("the removed optimization %s names no record", r.Name)
		}
	}
	return nil
}
