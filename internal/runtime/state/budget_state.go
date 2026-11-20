package state

// UndoCaptures restores only private capture slots, never Node pointees. It charges nothing: captures
// are not an item of the cost (spec/runtime/parsing.md#cost), and each write it undoes was made by a
// step that was counted. Callers truncate the trail after it. Mandatory scratch cleanup does not call
// this.
func (p *Parser) UndoCaptures(from int) {
	for i := len(p.Trail) - 1; i >= from; i-- {
		u := p.Trail[i]
		u.F.Vals[u.Slot] = u.Old
	}
}
