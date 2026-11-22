package document

import (
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// copyDocumentDiagnostics publishes owned recovered errors without exposing
// memoized error objects or their mutable slice containers. Ordinary failures
// are already freshly constructed by Parser.SyntaxError. The caller runs it in a
// region of Document work (Parser.FreeBegin): it reserves a unit for each error
// and for each string it copies, which no Compute limit counts and which keep
// the polls for cancellation and memory going over a long list.
func copyDocumentDiagnostics(p *state.Parser, src value.SyntaxErrors) value.SyntaxErrors {
	if src == nil {
		return nil
	}
	out := make(value.SyntaxErrors, len(src))
	for i, e := range src {
		p.Spend()
		if e == nil {
			continue
		}
		cloned := new(value.SyntaxError)
		*cloned = *e
		cloned.Expected = copyStrings(p, e.Expected)
		cloned.Messages = copyStrings(p, e.Messages)
		out[i] = cloned
	}
	return out
}

// copyStrings returns a copy of src, nil for nil, after reserving a unit for each string. A nil p
// copies without reserving.
func copyStrings(p *state.Parser, src []string) []string {
	if src == nil {
		return nil
	}
	if p != nil {
		p.SpendN(uint64(len(src)))
	}
	return append(make([]string, 0, len(src)), src...)
}
