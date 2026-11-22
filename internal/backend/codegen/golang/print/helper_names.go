package print

// ordinaryHelperName reports whether the library has an Ordinary form of the method name, which
// a parser without budget controls and Documents calls instead (the library writes them by hand).
func ordinaryHelperName(name string) bool {
	switch name {
	case "NewNode", "NewTerminal", "NewBranch", "NewMatch", "Expect", "Peek", "HasChar", "MatchLiteral", "MatchLiteralFold", "MatchAny",
		"AtEnd", "AtLineStart", "AtEndBatch", "AtLineStartBatch", "AtLineEnd", "SpendRead":
		return true
	}
	return false
}

// tracedHelperName reports whether the library has a Traced form of the read name, which records
// the examined end of a stream read, for the traced Node bodies (the library writes them by hand).
func tracedHelperName(name string) bool {
	switch name {
	case "Peek", "HasChar", "MatchLiteral", "MatchLiteralFold", "AtLineEnd":
		return true
	}
	return false
}

// logicalHelperName reports whether the library has a Logical form of the read name, which counts
// the input it advances over as logical compute units.
func logicalHelperName(name string) bool {
	switch name {
	case "MatchLiteral", "MatchLiteralFold", "MatchAny":
		return true
	}
	return false
}
