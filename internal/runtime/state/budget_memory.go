package state

import (
	"unsafe"

	"syntaxir.dev/sxir/internal/runtime/limits"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// Memory is counted in bytes of logical capacity: the element count of a buffer times the size of
// its element type, for the storage the parser takes into use for a parse. It is counted in four
// ways, all of which cost nothing until a finite Memory limit asks for a check:
//
//   - Chunk refills and memo slot growth add to plain counters, Parser.MemUsed and
//     MemoTable.MemBytes, in the branch that takes a chunk or new slots. They are additions to
//     fields, so the inlined allocators stay inlined, and they run on every parse, controlled or
//     not.
//   - Tables that grow an element at a time keep a byte counter beside their data: the context
//     tables, the hash indexes of memo chains, and the expectations of skip-run records.
//   - Storage that only the controlled path allocates is added where it is allocated: values
//     (strings that actions concatenate, text copied out of the input and rebound variable
//     environments) to BudgetControl.values, which a stream returns with its node chunks, syntax
//     errors to BudgetControl.errs, which a stream moves to memoHeld when its diagnostic tail
//     takes the recovered errors, and the keys of memo entries and the lists of recovered errors
//     to BudgetControl.memoHeld, which a stream recounts at each memo prune.
//   - Buffers that grow by appending, the decoded input, the line table and the working stacks,
//     are measured when the limit is checked (heldBytes). A parse with a finite limit starts them
//     empty, so their capacities do not depend on an earlier parse.
//
// The check runs at the compute polls of the control and at its explicit checkpoints, so the count
// can pass the limit by what one poll interval allocates. Storage dropped by backtracking is never
// returned; a stream returns what its commits let go of, except what memo entries and skip
// records may still hold, which it returns or recounts when it prunes the memo: the nodes, text and
// strings of delivered elements, memo keys, error lists and errors.

// Sizes of the elements that the counters charge, of the types that this package defines for
// itself; the others are in budget_sizes.go.
const (
	nodeBytes     = uint64(unsafe.Sizeof(value.Node{}))
	frameBytes    = uint64(unsafe.Sizeof(Frame{}))
	memoBytes     = uint64(unsafe.Sizeof(MemoEntry{}))
	memoSlotBytes = uint64(unsafe.Sizeof((*MemoEntry)(nil)))
	sdepBytes     = uint64(unsafe.Sizeof(sdep{}))
	growKeyBytes  = uint64(unsafe.Sizeof(growKey{}))
	runStepBytes  = uint64(unsafe.Sizeof(runStep{}))
	// The storage of a context table: the table and its map of heads, a head (its key, the
	// pointer in the map and the slice it points at), a set, a result slot with its key in the
	// set's map, a canonical seed with its key, and a jump.
	ctxTableBytes = uint64(unsafe.Sizeof(ctxTable{})) + mapBytes
	ctxHeadBytes  = uint64(unsafe.Sizeof(ctxHeadKey{})) + 2*ptrBytes + uint64(unsafe.Sizeof([]*ctxSet{}))
	ctxSetBytes   = ptrBytes + uint64(unsafe.Sizeof(ctxSet{})) + mapBytes
	ctxSlotBytes  = uint64(unsafe.Sizeof(valsKey{})) + ptrBytes + uint64(unsafe.Sizeof(ctxSlot{}))
	canonBytes    = uint64(unsafe.Sizeof(canonKey{})) + uint64(unsafe.Sizeof(canonRec{}))
	// wideKeyBytes is what a key of a set of more than maxCtxDeps dependencies takes beyond a
	// valsKey, besides the bytes of its values.
	wideKeyBytes = uint64(unsafe.Sizeof(wideKey{})) - uint64(unsafe.Sizeof(valsKey{}))
	jumpBytes    = uint64(unsafe.Sizeof(jumpKey{})) + uint64(unsafe.Sizeof(jumpRec{}))
	// envNodeBytes bounds a node of a variable environment that a rebind copies: the node and its
	// values.
	envNodeBytes = uint64(unsafe.Sizeof(Env{})) + envWidth*anyBytes
)

// AttachMemory makes the memory check of c read the counters and buffers of p. The memo table's
// counter is read whole: a pooled table starts at zero (MemoTable.Reset), and a Document's table
// carries the bytes it retains from earlier parses. With a finite limit, p takes no partially used
// chunk or stack from an earlier parse, so the count does not depend on what a pooled parser was
// used for. It is safe on a nil receiver.
func (c *BudgetControl) AttachMemory(p *Parser) {
	if c == nil {
		return
	}
	c.hotP = p // whatever the memory limit, the profile of p names the hotspot of a compute error
	if c.Limits.Memory == 0 {
		return
	}
	c.memP = p
	p.MemUsed, p.MemKept = 0, 0
	p.NodeSlab, p.PtrSlab, p.fieldSlab, p.FrameSlab = nil, nil, nil, nil
	if p.Scratch != nil {
		p.KidStack = nil
	}
}

// checkMemory fails the parse when the memory count exceeds the limit.
func (c *BudgetControl) checkMemory() {
	if used := c.MemoryUsed(); used > c.Limits.Memory {
		panic(Fatal{Err: &runtime.LimitError{Kind: runtime.LimitMemory, Limit: c.Limits.Memory, Used: used}})
	}
}

// MemoryUsed returns the memory count of the parse, in bytes.
func (c *BudgetControl) MemoryUsed() uint64 {
	used := c.counted()
	if p := c.memP; p != nil {
		used = limits.SaturatingAdd(used, p.MemUsed+p.MemKept)
		used = limits.SaturatingAdd(used, uint64(max(p.NodeChunks, 0))*NodeChunk*nodeBytes)
		if p.Memo != nil {
			used = limits.SaturatingAdd(used, p.Memo.ChargedBytes())
		}
		used = limits.SaturatingAdd(used, p.heldBytes())
	}
	return used
}

// heldBytes returns the bytes of the storage measured from its state: the decoded or buffered input
// and the table of line starts by their lengths, a stream's read buffer, the working stacks by
// their capacity, the context tables and skip-run records by the counts they keep, and the state
// of missing-token insertion, and the maps of moved nodes and shifts of a Document parse.
func (p *Parser) heldBytes() uint64 {
	n := uint64(len(p.In))*4 + uint64(len(p.Offs))*4 + uint64(len(p.Bs)) + uint64(cap(p.Lines))*intBytes
	if p.Reader != nil {
		n += uint64(p.Reader.Size())
	}
	n += uint64(cap(p.KidStack))*ptrBytes + p.actionExprs.bytes()
	n += uint64(cap(p.Trail))*uint64(unsafe.Sizeof(Undo{})) + uint64(cap(p.Recovered))*recEntryBytes
	n += uint64(cap(p.Created))*ptrBytes + uint64(cap(p.Exp))*expIDBytes
	n += uint64(cap(p.gamma))*uint64(unsafe.Sizeof(growRec{})) + uint64(cap(p.seedReadLog))*intBytes
	n += uint64(cap(p.frames))*uint64(unsafe.Sizeof(depFrame{})) + uint64(cap(p.deps))*uint64(unsafe.Sizeof(dep{}))
	n += uint64(cap(p.looks))*uint64(unsafe.Sizeof(lookRec{})) + uint64(cap(p.obsGuard))*4
	n += uint64(cap(p.keptSeedResults)) * uint64(unsafe.Sizeof(keptSeedResult{}))
	n += uint64(cap(p.ctxs)) * ptrBytes
	n += uint64(len(p.Moved))*2*ptrBytes + uint64(len(p.Shifts))*(4+intBytes)
	if p.Env != p.initEnv {
		// The environment in use, which a stream keeps after it returns the copies made before.
		n += uint64(p.envLevels+1) * envNodeBytes
	}
	if sl := p.streamLines; sl != nil {
		n += uint64(unsafe.Sizeof(streamLines{})) + uint64(cap(sl.starts))*intBytes
	}
	n += uint64(cap(p.DiagnosticTail.Errors))*ptrBytes + p.DiagnosticTail.bytes
	for _, t := range p.ctxs {
		n += t.bytes
	}
	n += uint64(cap(p.skipTables))*ptrBytes + uint64(cap(p.skips))*uint64(unsafe.Sizeof(skipWalk{}))
	n += p.skipBytes
	for i := range p.skips {
		n += uint64(cap(p.skips[i].visits))*uint64(unsafe.Sizeof(skipVisit{})) + uint64(p.skips[i].valCap())*ptrBytes
	}
	for _, s := range p.skipTables {
		if s != nil {
			n += uint64(unsafe.Sizeof(skipSlots{})) + uint64(cap(s.marks))*8 + s.expBytes
			n += uint64(len(s.recs)) * (intBytes + uint64(unsafe.Sizeof(skipRecord{})))
		}
	}
	if m := p.Missing; m != nil {
		n += uint64(cap(m.Path))*uint64(unsafe.Sizeof(MissingTokenEvent{})) + m.seedBytes
		n += uint64(len(m.installed)) * uint64(unsafe.Sizeof(MissingTokenSite{})+1)
		n += uint64(len(m.Seeds)) * (ptrBytes + uint64(unsafe.Sizeof([]MissingTokenEvent{})))
	}
	if p.Backend != nil {
		n += p.Backend.StackBytes()
	}
	return n
}

// chargeValueBytes charges n bytes of a value that only the controlled path builds, for the input
// that holds c (BudgetControl.values).
func (c *BudgetControl) chargeValueBytes(n int) {
	if c.Limits.Memory != 0 {
		c.values = limits.SaturatingAdd(c.values, uint64(n))
	}
}

// memoryLimit returns the memory limit of the parse, 0 for none.
func (c *BudgetControl) memoryLimit() uint64 { return c.Limits.Memory }

// diagnosticLimited reports that the parse keeps its recovered errors under a diagnostic limit
// (DiagnosticTail), so that the memo holds the errors themselves.
func (p *Parser) diagnosticLimited() bool { return p.DiagnosticTail.Limit > 0 }

// eachMemoHolder calls f with the recovered errors and the key values of each memo entry.
func (p *Parser) eachMemoHolder(f func(errs recEntry, env []any)) {
	for _, head := range p.Memo.Slots {
		for e := head; e != nil; e = e.Next {
			f(e.Errs, e.Env)
		}
	}
}
