package state

// assigned is what the slot of a string variable holds once an assignment of a Document parse bound
// it: the value, and the stamp of the assignment, which no other assignment of the Document's memo
// shares (MemoTable.assign). The key of a rule that only a Document memoizes holds it, so that a lookup
// finds an entry only under the values of the same assignments, at a constant cost per variable
// (identEqual). Reading the variable reads the value (Env.Get); a slot that holds a plain string holds
// the initial value of the variable.
type assigned struct {
	s     string
	stamp uint64
}
