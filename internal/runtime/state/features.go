package state

// feature is a mechanism of the parsing runtime that the shared files name when they use it (use).
// The library of generated parsers witnesses there that a generated parser uses only the features its
// generator found in the grammar; the engine runs every grammar with every feature, so it has nothing
// to witness.
type feature uint32

// The features that the shared files use.
const (
	featureRecovery feature = 1 << iota
)

// use records that the parse uses the feature f. It is the library's feature witness, and does
// nothing in the engine.
func (p *Parser) use(feature) {}
