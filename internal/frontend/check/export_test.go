package check

// CountTypeReads makes c count each of its reads of the type declarations in *reads.
func CountTypeReads(c *Checker, reads *int) { c.typeReads = func() { *reads++ } }
