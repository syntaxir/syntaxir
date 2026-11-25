package libgo

// Chunk owns unused values and the next allocation size for one type. Generated parsers keep one
// per type of a typed value and take the values from it (Alloc, AllocSlice). Handed-out values
// are never reused, even when a parse discards them.
//
// Chunk is for generated parsers; not a stable API.
type Chunk[T any] struct {
	free      []T
	next      int
	bootstrap uint8
	// allocated counts the values allocated (MemoryBytes).
	allocated int
}

// chunkRatio bounds a chunk by what its allocator has handed out: a chunk is at most the larger of its fixed
// limit and 1/chunkRatio of the values allocated before it, so the chunks of a parse grow geometrically and
// their number is logarithmic in the output. A value that escapes pins at most such a chunk, and the unused
// tail of the last chunk is at most 1/(chunkRatio+1) of the allocator's total.
//
// The ratio is 32 because a smaller one leaves a larger tail: with 8, the bytes allocated for 128 to 512 KiB of
// JSON or XML were about 7% above those of fixed chunks, with 32 they are within about 2% (JSON -0.4% to +1.1%,
// XML -0.5% to +2.0%) while the allocations of a 512 KiB parse still fall by a third.
const chunkRatio = 32

// chunkLimit returns the size that a chunk may reach after allocated values: the fixed limit, or more once
// allocated/chunkRatio exceeds it.
func chunkLimit(limit, allocated int) int { return max(limit, allocated/chunkRatio) }

// grow starts small and doubles up to the limit of chunkLimit. The request is always satisfied.
func (s *Chunk[T]) grow(n, initial, limit int) {
	size := s.next
	if size == 0 {
		size = initial
	}
	for size < n {
		size *= 2
	}
	s.free = make([]T, size)
	s.allocated += size
	s.next = min(size*2, chunkLimit(limit, s.allocated))
}

// Alloc returns a new zero value from the chunk, which grows from 8 values to at least 256 (chunkRatio). It allocates the
// first three values independently, so a later published value cannot retain discarded early
// siblings through a chunk.
//
// Alloc is for generated parsers; not a stable API.
func Alloc[T any](s *Chunk[T]) *T {
	if len(s.free) == 0 {
		if s.bootstrap < 3 {
			s.bootstrap++
			s.allocated++
			return new(T)
		}
		s.grow(1, 8, 256)
	}
	v := &s.free[0]
	s.free = s.free[1:]
	return v
}

// AllocSlice returns n zero values with capacity n, so appending cannot overwrite another returned
// slice. Small-list chunks grow from 64 to at least 1,024 elements (chunkRatio); a list of more than 256 elements is
// allocated on its own, and so is the first nonempty small list, to keep its backing values
// separate from later published lists of the same type. An empty list is not nil.
//
// AllocSlice is for generated parsers; not a stable API.
func AllocSlice[T any](s *Chunk[T], n int) []T {
	if n == 0 {
		return []T{} // an empty list, not nil
	}
	if n > 256 {
		s.allocated += n
		return make([]T, n)
	}
	if len(s.free) < n {
		if s.bootstrap == 0 {
			s.bootstrap = 1
			s.allocated += n
			return make([]T, n)
		}
		s.grow(n, 64, 1024)
	}
	v := s.free[:n:n]
	s.free = s.free[n:]
	return v
}
