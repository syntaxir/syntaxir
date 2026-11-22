package state

import "syntaxir.dev/sxir/pkg/runtime/value"

// sortExpected sorts a newly constructed, parser-owned diagnostic in the order of
// compareExpected. Building the error of a failed parse is one event, which counts a unit
// (SyntaxError); sorting its expectations counts nothing (spec/runtime/parsing.md#compute-units).
// Published or memoized errors must never be passed to this helper.
func (p *Parser) sortExpected(values []string) { sortExpectedEntries(values) }

// RecoveryErrorText is the text of the error node of a recovered error. Making it counts nothing:
// the syntax error that a recovery or an insertion builds is the event that covers it.
func (p *Parser) RecoveryErrorText(e *value.SyntaxError) string { return e.Error() }
