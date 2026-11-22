package libgo

import sxirruntime "syntaxir.dev/sxir/pkg/runtime"

// The library Document is the support type behind the typed Document of a generated parser, which
// implements runtime.Document through it. Embedded copies of the library prune methods nothing
// names, so the assertion lives here and not in the library source.
var _ sxirruntime.Document = (*Document)(nil)
