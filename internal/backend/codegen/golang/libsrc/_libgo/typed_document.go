package libgo

import (
	"fmt"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// The typed Document runtime, appended to generated parsers made with both sxir gen -types and
// -document: Document.ParseValue parses directly into typed values, which a typed Document keeps
// privately (typedDocumentState) and publishes as read-only values that later results may share.
// The generated CloneValue copies a published value.

// TypedDocumentCloneBase is the state of a CloneValue copy of a published typed Document value. The
// generated typedDocumentClone embeds it and adds the copies of the grammar's types.
//
// TypedDocumentCloneBase is for generated parsers; not a stable API.
type TypedDocumentCloneBase struct {
	Control *BudgetControl
	Values  map[any]any
	Slices  map[DocumentSliceKey]any
	Tasks   []TypedDocumentCloneTask
}

// TypedDocumentCloneTask is a value whose copy (target) has yet to be filled from source.
//
// TypedDocumentCloneTask is for generated parsers; not a stable API.
type TypedDocumentCloneTask struct{ Source, Target any }

func (c *TypedDocumentCloneBase) Begin(control *BudgetControl) { c.Control = control }

func (c *TypedDocumentCloneBase) Remember(from, to any) {
	if c.Values == nil {
		c.Values = make(map[any]any)
	}
	c.Values[from] = to
}

func (c *TypedDocumentCloneBase) ReleaseBase() {
	c.Control, c.Values, c.Slices, c.Tasks = nil, nil, nil, nil
}

// TypedDocumentParse parses the Document d with the typed rule root and returns the value that publish
// makes of the parse result. The values of a typed Document parse are write-once: they are published
// as they are, and later parses reuse them, so publish must not write to them. a is the generated
// code's allocation chunks.
//
// TypedDocumentParse is for generated parsers; not a stable API.
func TypedDocumentParse[T any](d *Document, root *TRule, o parseOptions, a budgetControlSetter, publish func(any) T) (res T, err error) {
	defer recoverParse(&res, &err)
	if o.unit != d.State.unit {
		return res, fmt.Errorf("document parse cannot change unit")
	}
	if o.expectations != d.State.expectations {
		return res, fmt.Errorf("document parse cannot change expectations")
	}
	o = prepareText(o, d.State.text)
	cache, ok := d.State.typedMemo.(*typedDocumentState)
	if o.control != nil && o.control.limits.Memory != 0 && d.State.retainedBytes() > o.control.limits.Memory {
		// What the Document retains alone exceeds the limit: the parse starts without it, as after
		// ResetHistory.
		d.State.resetHistory()
		ok = false
	}
	if !ok {
		cache = &typedDocumentState{}
		d.State.typedMemo = cache
	}
	// Admission leaves reusable state untouched. Once execution can begin,
	// discard private memo values on interruption or caller-context panic.
	completed := false
	defer func() {
		if !completed {
			d.State.dropTypedMemo()
		}
	}()
	p := &TParser{Parser: &Parser{Document: d.State}, TypedDocument: cache, memoSlots: cache.slots, memoGap: cache.memoGap}
	p.setControl(o.control)
	p.edits, p.gen = d.State.edits, uint32(len(d.State.edits))
	if o.control != nil && o.control.limits.Memory != 0 {
		o.control.attachTypedMemory(p, a)
		// The parse reads none of what the Document retains as it counts: the Node memo table is not
		// its own, and the typed memo is counted by its estimate.
		o.control.retained = d.State.retainedBytes()
	}
	p.Observation = newObservation(&o.observe, &p.Silent, &d.State.stats.Resumed)
	defer p.releaseObservation()
	// Do not recycle p: memo values and expectations may refer into its arenas.
	a.SetBudgetControl(o.control.memoryControl())
	defer a.SetBudgetControl(nil)
	d.State.stats = documentStats{}
	v, parseErr := p.run(root, d.State.text, o, a)
	// Context implementations may return any public error type. A fatal stop (a limit, the context
	// or the memory count) must not be mistaken for a completed syntax failure.
	if p.stopped || o.control != nil && o.control.interrupted {
		return res, parseErr
	}
	if parseErr != nil {
		switch parseErr.(type) {
		case *sxirvalue.SyntaxError, sxirvalue.SyntaxErrors:
		default:
			return res, parseErr
		}
	}
	// The conversion that publish makes observes the context of the parse too (conversionControl).
	c, done := conversionControl(o.unitsCtx, o.control)
	defer done()
	a.SetBudgetControl(c)
	c.Check()
	if v != nil {
		res = publish(v)
	}
	err = typedDocumentError(parseErr)
	cache.slots, cache.memoGap = p.memoSlots, p.memoGap
	completed = true
	return res, err
}

// typedDocumentError copies the diagnostics of err. Diagnostics are private memo data
// too; publishing their original pointers or Expected/Messages slices would let caller mutation
// alter a later parse.
func typedDocumentError(err error) error {
	clone := func(e *sxirvalue.SyntaxError) *sxirvalue.SyntaxError {
		if e == nil {
			return nil
		}
		out := new(sxirvalue.SyntaxError)
		*out = *e
		if e.Expected != nil {
			out.Expected = make([]string, len(e.Expected))
			for i, s := range e.Expected {
				out.Expected[i] = s
			}
		}
		if e.Messages != nil {
			out.Messages = make([]string, len(e.Messages))
			for i, s := range e.Messages {
				out.Messages[i] = s
			}
		}
		return out
	}
	switch e := err.(type) {
	case *sxirvalue.SyntaxError:
		return clone(e)
	case sxirvalue.SyntaxErrors:
		out := make(sxirvalue.SyntaxErrors, len(e))
		for i, item := range e {
			out[i] = clone(item)
		}
		return out
	}
	return err
}
