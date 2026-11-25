package libgo

import "testing"

// The chunks of an allocator grow with what it has handed out (chunkRatio): the number of chunks is
// logarithmic in the values, no chunk is larger than the larger of its fixed limit and 1/chunkRatio of the
// values before it, and the count of allocated values (the memory the parse is charged) is the sum of the
// real chunk sizes.
func TestChunkGrowthIsGeometric(t *testing.T) {
	var c Chunk[[4]uint64]
	const n = 200000
	chunks, largest, last := 0, 0, 0
	for i := 0; i < n; i++ {
		before := len(c.free)
		Alloc(&c)
		if len(c.free) > before { // a new chunk was made
			chunks++
			size := len(c.free) + 1
			if limit := chunkLimit(256, last); size > limit && size > 8 {
				t.Fatalf("chunk of %d after %d values, limit %d", size, last, limit)
			}
			largest = max(largest, size)
			last = c.allocated
		}
	}
	if chunks > 200 {
		t.Errorf("%d chunks for %d values, want a logarithmic count (the fixed 256 would make %d)", chunks, n, n/256)
	}
	if largest <= 256 {
		t.Errorf("the largest chunk is %d, want growth past the fixed limit", largest)
	}
	if c.allocated < n || c.allocated > n+n/chunkRatio+256 {
		t.Errorf("allocated %d values for %d requests, want at most the tail of one chunk more", c.allocated, n)
	}
}

// A match chunk grows the same way, and the memory it charges is its real size.
func TestMatchChunksGrow(t *testing.T) {
	p := &TParser{Parser: &Parser{}}
	const n = 100000
	chunks := 0
	for i := 0; i < n; i++ {
		if len(p.matches) == 0 {
			chunks++
		}
		p.NewMatch(0, 0, "", false)
	}
	if chunks > 150 {
		t.Errorf("%d match chunks for %d matches, want a logarithmic count", chunks, n)
	}
	if p.matchTotal < n || uint64(p.matchTotal)*matchBytes != p.memUsed {
		t.Errorf("matchTotal %d, memUsed %d bytes: the memory charged is not the size of the chunks", p.matchTotal, p.memUsed)
	}
}

// The growth of a chunk depends on what its allocator has handed out (Chunk.allocated), so every way to
// hand out values counts them: the first values of Alloc and the first nonempty list, which are allocated
// on their own, and the lists above the small-list limit.
func TestChunkAllocatedCountsEveryAllocation(t *testing.T) {
	t.Run("the first values of Alloc", func(t *testing.T) {
		var c Chunk[[4]uint64]
		for i := 1; i <= 3; i++ {
			Alloc(&c)
			if c.allocated != i {
				t.Fatalf("after %d independent values the chunk counts %d", i, c.allocated)
			}
		}
	})
	t.Run("the first nonempty list", func(t *testing.T) {
		var c Chunk[[4]uint64]
		AllocSlice(&c, 5)
		if c.allocated != 5 {
			t.Fatalf("after a first list of 5 the chunk counts %d, want 5", c.allocated)
		}
	})
	t.Run("a list above the small-list limit", func(t *testing.T) {
		var c Chunk[[4]uint64]
		AllocSlice(&c, 5) // the first list is separate
		before := c.allocated
		AllocSlice(&c, 300)
		if c.allocated != before+300 {
			t.Fatalf("after a list of 300 the chunk counts %d, want %d", c.allocated, before+300)
		}
	})
}
