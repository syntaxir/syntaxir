package state

// rawValueEqual preserves the interface equality of memo keys and bindings.
// In particular, a typed nil pointer differs from a nil interface. Action
// ValueEqual has different nil semantics and must not be used for these keys.
func (p *Parser) rawValueEqual(l, r any) bool {
	if s, ok := l.(string); ok {
		t, ok := r.(string)
		return ok && s == t
	}
	return l == r
}

// identEqual compares two key values for a lookup of a rule that only a Document parse memoizes
// (memoBindings): two strings are equal if the same assignment bound them, the stamps of the
// assignments being equal, which costs a constant, or if no assignment bound either and they are equal,
// as the initial values of a variable are, which the grammar bounds. A string that another assignment
// bound, even an equal one, is a miss, and a miss is sound, since the rule is then evaluated and that
// work counts.
func (p *Parser) identEqual(l, r any) bool {
	switch l := l.(type) {
	case assigned:
		m, ok := r.(assigned)
		return ok && l.stamp == m.stamp
	case string:
		m, ok := r.(string)
		return ok && l == m
	}
	return l == r
}

// memoKeyIdentical is keysEqual under identEqual.
func (p *Parser) memoKeyIdentical(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		if !p.identEqual(v, r[i]) {
			return false
		}
	}
	return true
}

// keyMatches compares a memo key under the lookup that ident selects. The comparison charges nothing:
// the call paid for the lookup of its key (ChargeKey), which the size of the key bounds. A lookup of a
// rule that only a Document memoizes compares the assignments of strings (identEqual), which costs a
// constant per variable.
func (p *Parser) keyMatches(l, r []any, ident bool) bool {
	if ident {
		return p.memoKeyIdentical(l, r)
	}
	return keysEqual(l, r)
}

// keysEqual reports whether two memo keys are equal under the interface equality of rawValueEqual,
// without charges.
func keysEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		if s, ok := v.(string); ok {
			if t, ok := r[i].(string); !ok || s != t {
				return false
			}
		} else if v != r[i] {
			return false
		}
	}
	return true
}

// current reports whether e was stored or answered a lookup in this parse: in a Document, an entry that
// an earlier parse stored and this one has not touched is not.
func (p *Parser) current(e *MemoEntry) bool {
	return e.Seen == p.Gen
}

// lagged reports whether a lookup treats e as a miss because it lags behind more than PendingEditLimit
// edits (AdvanceEntry). A Document drops such an entry where a walk meets it (unlinkLagged): the entries of
// earlier texts that it retains, and so the work of a walk that compares them, stay bounded by that many
// edits.
func (p *Parser) lagged(e *MemoEntry) bool {
	return !e.Growing && p.Gen-e.Vgen > PendingEditLimit
}

// unlinkLagged removes from the chain at pos, starting at the link, the entries that lag behind (lagged)
// among the next limit entries that do not, or among all of them if limit is negative, and reports where the
// walk ends: the link before the first entry it did not visit. Removing an entry charges nothing, as the
// walk does not (memo.lookup): each entry is removed once, after the counted call that stored it. The
// index of the position, if there is one, no longer matches the chain and is dropped.
func (p *Parser) unlinkLagged(pos int, link **MemoEntry, limit int) **MemoEntry {
	t := p.Memo
	for kept := 0; *link != nil && kept != limit; {
		e := *link
		if !p.lagged(e) {
			link, kept = &e.Next, kept+1
			continue
		}
		*link = e.Next
		if t.Index[pos] != nil {
			t.dropIndex(pos)
		}
		t.Retire(e)
	}
	return link
}

func (p *Parser) memoKeyEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		if !p.rawValueEqual(v, r[i]) {
			return false
		}
	}
	return true
}

// bindControlled is Bind under budget controls, which counts the memory of the levels+1 nodes that a
// rebind copies on the path to the slot. A binding is no event: it counts no compute unit.
func (p *Parser) bindControlled(slot int, val any) {
	if p.rawValueEqual(p.Env.Get(slot, p.envLevels), val) {
		return
	}
	// Saved environments are immutable; only publish a completed path.
	p.Env = p.Env.Set(slot, p.envLevels, p.binding(val))
	p.chargeValueBytes(int(uint64(p.envLevels+1) * envNodeBytes))
}

// MemoGet looks up the entry for k. The lookup charges nothing: the call paid for the lookup of its key
// (ChargeKey). The walk of the chain at the position compares at most memoScan entries and then probes the
// index of the position, so it costs at most a constant plus the size of the key, and building the index
// hashes entries that counted calls stored. The walk drops the entries that lag behind too many edits
// (lagged) before it compares anything with them.
func (p *Parser) MemoGet(k MemoKey) (*MemoEntry, bool) {
	if !BudgetControlsEnabled || p.Control == nil {
		return p.Memo.Get(k)
	}
	ident := k.DocOnly // see MemoKey.DocOnly
	if p.MemoAll {
		defer p.FreeEnd(p.FreeBegin(true))
	}
	t := p.Memo
	i := t.slot(k.Pos - t.Base)
	if i < 0 || i >= len(t.Slots) {
		return nil, false
	}
	scan := memoScan
	if ident {
		scan = docOnlyScan
	}
	link := p.unlinkLagged(k.Pos, &t.Slots[i], scan)
	for e := t.Slots[i]; e != nil && e != *link; e = e.Next {
		if e.Rule == int32(k.Rule) && e.Min == int32(k.Min) && e.missingSilent == k.MissingSilent && p.keyMatches(e.Env, k.Env, ident) {
			e.Seen = p.Gen
			return e, true
		}
	}
	if *link == nil || ident {
		return nil, false
	}
	p.unlinkLagged(k.Pos, link, -1)
	if *link == nil {
		return nil, false
	}
	ix := t.indexAt(k.Pos, t.Slots[i])
	h, _ := memoKeyHash(int32(k.Rule), int32(k.Min), k.MissingSilent, k.Env)
	_, x := ix.probe(h, func(x *MemoEntry) bool {
		return x.Rule == int32(k.Rule) && x.Min == int32(k.Min) && x.missingSilent == k.MissingSilent && keysEqual(x.Env, k.Env)
	})
	if x != nil {
		x.Seen = p.Gen
	}
	return x, x != nil
}

// hashMemoKey hashes the key of a record of a skip run for the index of its records. Hashing is
// no event: the walk counts the key of the site (walkKeyUnits).
func (p *Parser) hashMemoKey(rule, min int32, silent bool, env []any) uint64 {
	h, _ := memoKeyHash(rule, min, silent, env)
	return h
}

// MemoPut stores e for k, unless the join with the entry stored there keeps that entry (joinStored),
// and returns the entry it keeps. The store charges nothing: it follows the evaluation of a body that
// counted, and its walk of the chain at the position costs what MemoGet's walk does. Growing the table
// charges nothing either.
func (p *Parser) MemoPut(k MemoKey, e *MemoEntry) *MemoEntry {
	if !BudgetControlsEnabled || p.Control == nil {
		return p.Memo.put(k, e, p)
	}
	p.Control.noteStored(k.Pos)
	ident := k.DocOnly
	if p.MemoAll {
		defer p.FreeEnd(p.FreeBegin(true))
	}
	t := p.Memo
	i := k.Pos - t.Base
	if i < 0 {
		return e
	}
	i = t.slot(i)
	slots := t.Slots
	if i >= len(slots) {
		// Growing the slot array charges no unit (it grows to positions of counted calls, by
		// doubling). Its memory is charged by its length, not its capacity, so the count does
		// not depend on the capacity an earlier parse left.
		t.MemBytes += uint64(i+1-len(slots)) * memoSlotBytes
		if i < cap(slots) {
			slots = slots[:i+1]
		} else {
			grown := make([]*MemoEntry, i+1, max(2*cap(slots), i+1, 64))
			copy(grown, slots)
			slots = grown
		}
	}
	link := &slots[i]
	var replaced *MemoEntry
	same := func(x *MemoEntry) bool {
		return x.Rule == int32(k.Rule) && x.Min == int32(k.Min) && x.missingSilent == k.MissingSilent && p.keyMatches(x.Env, k.Env, ident)
	}
	scan := memoScan
	if ident {
		scan = docOnlyScan
	}
	end := p.unlinkLagged(k.Pos, link, scan)
	for x := *link; x != nil && link != end; link, x = &x.Next, x.Next {
		if same(x) {
			replaced = x
			break
		}
	}
	var ixSlot uint64
	var ix memoIndex[MemoEntry]
	if replaced == nil && *link != nil && !ident {
		// A long chain: the index finds the entry to replace.
		p.unlinkLagged(k.Pos, link, -1)
	}
	if replaced == nil && *link != nil && !ident {
		ix = t.indexAt(k.Pos, slots[i])
		h, _ := memoKeyHash(int32(k.Rule), int32(k.Min), k.MissingSilent, k.Env)
		ixSlot, replaced = ix.probe(h, same)
		if replaced != nil {
			// Walk to the link of the entry.
			link = &slots[i]
			for *link != replaced {
				link = &(*link).Next
			}
		}
	}
	if replaced != nil && ix == nil {
		// The entry replaced among the first of a long chain is indexed too.
		if ix = t.Index[k.Pos]; ix != nil {
			h, _ := memoKeyHash(replaced.Rule, replaced.Min, replaced.missingSilent, replaced.Env)
			ixSlot, _ = ix.probe(h, func(y *MemoEntry) bool { return y == replaced })
		}
	}
	if replaced != nil && p.joinStored(replaced, e) == replaced {
		return replaced
	}
	// No checkpoint may separate the link swap from retiring its old entry.
	// In particular, do not publish a partially copied slot array or key.
	t.Slots = slots
	e.Rule, e.Min, e.Pos, e.Env = int32(k.Rule), int32(k.Min), k.Pos, k.Env
	e.Seen = p.Gen
	e.missingSilent = k.MissingSilent
	if ix != nil {
		n := len(ix)
		ix[ixSlot] = e
		t.IndexBytes += uint64(len(ix)-n) * memoIndexEntryBytes
	}
	if replaced != nil {
		e.Next = replaced.Next
		*link = e
		if replaced != e {
			t.Retire(replaced)
		}
		return e
	}
	e.Next = slots[i]
	slots[i] = e
	return e
}

// ChargeKey counts the key of a call of r, a rule that a parse from scratch memoizes, a sized event
// (budget.chargeKey): one unit for each variable r reads and one for each byte of each string value
// among them, whether or not the call looks the key up.
func (p *Parser) ChargeKey(r *Rule) {
	if len(r.VarSlots) == 0 || !BudgetControlsEnabled || p.Control == nil {
		return
	}
	n := uint64(len(r.VarSlots))
	for _, slot := range r.VarSlots {
		if slot < 0 {
			continue
		}
		if s, ok := p.Env.Get(slot, p.envLevels).(string); ok {
			n += uint64(len(s))
		}
	}
	p.SpendN(n)
}

// PendingEditLimit is the number of edits that a memo entry may lag behind when a lookup finds it
// (exec.PendingEditLimit, which the generated parsers use).
const PendingEditLimit = 128
