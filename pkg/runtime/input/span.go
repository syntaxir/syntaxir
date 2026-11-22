package input

// Span is a range [Start, End) of the input, in the position unit of the parse that produced
// it; End is exclusive. The typed values of a generated parser embed it to report the input they
// cover.
type Span struct{ Start, End int }

// Range returns the start and the end (exclusive) of the span.
func (s Span) Range() (start, end int) { return s.Start, s.End }
