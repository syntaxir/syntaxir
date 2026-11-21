package state

// The hash indexes of the long chains of a MemoTable, over the shared memoIndex (memo_index.go).

// indexAt returns the index of the entries at pos, whose chain starts at head, building it when the
// position has none yet.
func (t *MemoTable) indexAt(pos int, head *MemoEntry) memoIndex[MemoEntry] {
	return indexAt(&t.Index, &t.IndexBytes, pos, head, func(e *MemoEntry) *MemoEntry { return e.Next }, entryHash)
}

// entryHash returns the hash of the key of the entry e.
func entryHash(e *MemoEntry) uint64 {
	h, _ := memoKeyHash(e.Rule, e.Min, e.missingSilent, e.Env)
	return h
}

// dropIndex drops the index of pos.
func (t *MemoTable) dropIndex(pos int) {
	t.IndexBytes -= min(t.IndexBytes, indexBytes(t.Index[pos]))
	delete(t.Index, pos)
}

// reindex makes the index of pos, if it has one, hold e where it held x, which has the key of e and
// is replaced by it in the chain.
func (t *MemoTable) reindex(pos int, x, e *MemoEntry) {
	ix := t.Index[pos]
	if ix == nil {
		return
	}
	slot, _ := ix.probe(entryHash(x), func(y *MemoEntry) bool { return y == x })
	ix[slot] = e
}
