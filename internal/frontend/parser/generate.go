package parser

// The generated parser of the grammar source, from the grammar of the language (sxir.sxir). It is
// checked in, so that building SyntaxIR needs no generation step; a test of spec/language fails
// when it is stale.
//go:generate go run ../../../cmd/sxir gen -g ../../../sxir.sxir -module syntaxir.dev/sxir/internal/frontend/parser/sxirparser -lib import -types -typed-only -limits -expectations -o sxirparser
