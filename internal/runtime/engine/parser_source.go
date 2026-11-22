package engine

import (
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
)

func init() { state.ParserSource = parserSource }

// parserSource is state.ParserSource: the Source of the program behind p, a parser of package vm or
// a reference parser, and its start rule.
func parserSource(p runtime.Parser) (state.Source, string, bool) {
	ep := Of(p)
	if ep == nil {
		return nil, "", false
	}
	return ep.prog.Source(), ep.start, true
}
