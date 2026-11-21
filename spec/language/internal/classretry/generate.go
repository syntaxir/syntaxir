// Package classretry is the generated parser of a grammar whose repetition tries a character class
// again from every "[" of a line that does not close it. It is the negative control of the scaling
// tests of the generated grammar-source parser.

package classretry

//go:generate go run ../../../../cmd/sxir gen -g classretry.sxir -module syntaxir.dev/sxir/spec/language/internal/classretry -lib import -limits -o .
