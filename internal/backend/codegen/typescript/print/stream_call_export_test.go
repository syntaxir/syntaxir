package print

// StreamCallPanics returns, for each rule of the printed generator g, the panic of an admitted
// construct that printing its call in a function of the stream that is not resumable raises, or "",
// with the table of the stream, as streamRules prints it.
func StreamCallPanics(g *Generator) map[string]string {
	g.stream = true
	defer func() { g.stream = false }()
	if !g.startTSTable() {
		return nil
	}
	out := map[string]string{}
	for _, r := range g.Prog.RuleList {
		out[r.Name] = admittedPanic(func() { (&tsPrimPrinter{g: g}).call(r, 0) })
	}
	return out
}
