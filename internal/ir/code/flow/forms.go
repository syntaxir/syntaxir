package flow

import (
	"maps"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// Forms record where the lowering from Core IR wrote the forms that a Flow pass may replace, with
// the IDs of the Core nodes they come from (Attrs.Forms). The lowering writes one form of each
// construct: a class as a peek, a test and an advance, a choice with guards that only peek, and a
// terminal region as its literals and classes. The passes of internal/middleend/flowpass choose
// another form where the target and the function allow it, and use the record to find the
// statements and the facts that the choice depends on, which the lowering recorded with each form.
// The record is provenance: the statements of the function stay what the verifier checks and the
// printers print.
//
// A form holds its statements in a block of its own, labeled form<N> as every Flow block has a label
// and left by no exit, which flowpass.Finishing splices into the enclosing statement list after the
// passes, so that a form leaves no trace in what is printed.
type Forms struct {
	// Classes are the forms of the classes, in the order the lowering wrote them.
	Classes []*Form
	// Regions are the forms of the terminal regions (corepass.TerminalRegions) of a local function.
	Regions []*Form
	// Seqs are the value-free sequences with at least two classes among their items.
	Seqs []*SeqForm
	// Choices are the choices with an alternative that begins with a class.
	Choices []*ChoiceForm
	// Events record, in the order of the lowering, each use of a variable shared by the whole function
	// (ch, size, ok) and each choice: Finish places the shared variables, and a pass places the
	// variables it makes, where the lowering would have made them had it written the forms the passes
	// chose.
	Events []*Event
	// N is the counter of the numbered names (x1, L2, ...) when the lowering finished; a pass names
	// the variables it makes above it.
	N int
	// Named holds the variables that the statements of the function named when the lowering
	// finished; Order is Attrs.Vars then.
	Named map[string]bool
	Order []string
	// Placed holds the event at which each variable a pass made belongs.
	Placed map[string]*Event
}

// FormKind says what a form holds.
type FormKind uint8

const (
	// FormClass is a class: peek(ch, size, ok); if !ok || !inClass(ch) { expect; fail }; then, when it
	// builds its value, start = pos; advance(size); and value = match(start, text(start)), or else
	// advance(size).
	FormClass FormKind = iota
	// FormRegion is a terminal region: a value-free expression of literals and classes with a choice,
	// which skipTerminal can match as one program.
	FormRegion
)

// Form is one form the lowering wrote.
type Form struct {
	Kind FormKind
	// Block is the block of its own that holds the statements of the form.
	Block *code.Block
	// ID is the ID of the Core node of the form (core.Attrs.ID), 0 if it has none: the class of a
	// class, the root of a region. It is provenance only: no pass reads it.
	ID int32
	// Class is the class of a class form (prim.Tables).
	Class prim.ClassID
	// Terminal is the expression of a region, which skipTerminal can match.
	Terminal prim.Terminal
	// Build reports that the form builds the node's value.
	Build bool
	// Fail is the label the form leaves on failure, and Loop reports that it is a loop's (left by
	// a break).
	Fail string
	Loop bool
	// Guard is, for a class that begins a guarded alternative (core.Alt.GuardedClass), the choice
	// whose guard peeked the class's character.
	Guard *ChoiceForm
	// Outer are the forms around this one, outermost first.
	Outer []*Form
	// By is the name of the pass that replaced the statements the lowering wrote, or "" if none did.
	By string
}

// Live reports whether the statements the lowering wrote for s are still in the function: neither
// s nor a form around it was replaced.
func (s *Form) Live() bool {
	if s.By != "" {
		return false
	}
	for _, o := range s.Outer {
		if o.By != "" {
			return false
		}
	}
	return true
}

// FailJump returns the statement that leaves the form on failure.
func (s *Form) FailJump() code.Stmt {
	if s.Loop {
		return &code.Break{Label: s.Fail}
	}
	return &code.Exit{Label: s.Fail}
}

// SeqForm is a value-free sequence (core.Seq) with at least two classes among its items.
type SeqForm struct {
	// ID is the ID of the Core sequence (Form.ID).
	ID int32
	// Items holds the form of each item that is a class, in the order of the items, and nil for
	// the others.
	Items []*Form
	Outer []*Form
}

// Live reports whether the statements of the sequence are still in the function.
func (s *SeqForm) Live() bool {
	for _, o := range s.Outer {
		if o.By != "" {
			return false
		}
	}
	return true
}

// ChoiceForm is a choice (core.Choice) with an alternative that begins with a class.
type ChoiceForm struct {
	// ID is the ID of the Core choice (Form.ID).
	ID int32
	// Peeks are the peeks of the choice's guards: one before the first alternative with the guard of
	// a class; none if no alternative has one.
	Peeks []*code.Set
	// Event is the event of the choice, at the point where the lowering would make a variable of the
	// choice before its alternatives.
	Event *Event
	Outer []*Form
}

// Live reports whether the statements of the choice are still in the function.
func (s *ChoiceForm) Live() bool {
	for _, o := range s.Outer {
		if o.By != "" {
			return false
		}
	}
	return true
}

// Event is a point of the lowering: the use of a shared variable, or a choice.
type Event struct {
	// Name is the shared variable used, or empty at a choice.
	Name string
	// Vars is the number of variables (Attrs.Vars) the lowering had made at the event.
	Vars  int
	Outer []*Form
}

// Live reports whether the statements written at the event are still in the function.
func (e *Event) Live() bool {
	for _, o := range e.Outer {
		if o.By != "" {
			return false
		}
	}
	return true
}

// NamedVars returns the locals that the statements of b read or assign.
func NamedVars(b *code.Block) map[string]bool {
	out := map[string]bool{}
	code.Walk(b, func(s code.Stmt) {
		switch s := s.(type) {
		case *code.Set:
			for _, d := range s.Dst {
				if d.Name != "" && !d.Reg {
					out[d.Name] = true
				}
			}
		case *code.Let:
			for _, n := range s.Names {
				out[n] = true
			}
		}
	}, func(e code.Expr) {
		if l, ok := e.(*code.Local); ok {
			out[l.Name] = true
		}
	})
	return out
}

// CloneAttrs returns a copy of a for the copy of its function that c makes: the record of the forms
// is copied, each form once, with the statements it refers to followed to their copies. The Core
// nodes and the rule are shared.
func (a *Attrs) CloneAttrs(c *code.Cloner) any {
	out := *a
	out.Vars = slices.Clone(a.Vars)
	if a.Forms != nil {
		out.Forms = a.Forms.clone(c)
	}
	return &out
}

// formCloner copies the forms of a record, each once.
type formCloner struct {
	c       *code.Cloner
	forms   map[*Form]*Form
	choices map[*ChoiceForm]*ChoiceForm
	events  map[*Event]*Event
}

func (f *Forms) clone(c *code.Cloner) *Forms {
	k := &formCloner{c: c, forms: map[*Form]*Form{}, choices: map[*ChoiceForm]*ChoiceForm{}, events: map[*Event]*Event{}}
	out := &Forms{N: f.N, Named: maps.Clone(f.Named), Order: slices.Clone(f.Order)}
	out.Classes = k.formList(f.Classes)
	out.Regions = k.formList(f.Regions)
	for _, s := range f.Seqs {
		out.Seqs = append(out.Seqs, &SeqForm{ID: s.ID, Items: k.formList(s.Items), Outer: k.formList(s.Outer)})
	}
	for _, ch := range f.Choices {
		out.Choices = append(out.Choices, k.choice(ch))
	}
	for _, e := range f.Events {
		out.Events = append(out.Events, k.event(e))
	}
	if f.Placed != nil {
		out.Placed = make(map[string]*Event, len(f.Placed))
		for name, e := range f.Placed {
			out.Placed[name] = k.event(e)
		}
	}
	return out
}

func (k *formCloner) form(f *Form) *Form {
	if f == nil {
		return nil
	}
	if x, ok := k.forms[f]; ok {
		return x
	}
	y := *f
	k.forms[f] = &y
	y.Block = k.c.Block(f.Block)
	y.Guard = k.choice(f.Guard)
	y.Outer = k.formList(f.Outer)
	return &y
}

func (k *formCloner) formList(fs []*Form) []*Form {
	if fs == nil {
		return nil
	}
	out := make([]*Form, len(fs))
	for i, f := range fs {
		out[i] = k.form(f)
	}
	return out
}

func (k *formCloner) choice(ch *ChoiceForm) *ChoiceForm {
	if ch == nil {
		return nil
	}
	if x, ok := k.choices[ch]; ok {
		return x
	}
	y := *ch
	k.choices[ch] = &y
	if ch.Peeks != nil {
		y.Peeks = make([]*code.Set, len(ch.Peeks))
		for i, p := range ch.Peeks {
			y.Peeks[i] = k.c.Set(p)
		}
	}
	y.Event = k.event(ch.Event)
	y.Outer = k.formList(ch.Outer)
	return &y
}

func (k *formCloner) event(e *Event) *Event {
	if e == nil {
		return nil
	}
	if x, ok := k.events[e]; ok {
		return x
	}
	y := *e
	k.events[e] = &y
	y.Outer = k.formList(e.Outer)
	return &y
}
