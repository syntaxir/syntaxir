package libgo

import (
	"errors"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// The bounds of these tests are written with the ratio 32 itself, not with chunkRatio, so that another
// ratio fails them: 1 or 8 make fewer and larger chunks and a larger unused tail, 64 or fixed chunks make
// more chunks.

// The chunks of an allocator grow with what it has handed out: 200,000 values take between 100 and 200
// chunks (the ratio 32 takes 140; 8 takes 51, 64 takes 230, fixed chunks of 256 take 786), no chunk is
// larger than the larger of 256 and 1/32 of the values before it, and the values allocated exceed those
// handed out by at most 1/32 of them and one fixed chunk.
func TestChunkGrowthIsGeometric(t *testing.T) {
	var c Chunk[[4]uint64]
	const n = 200000
	chunks, largest, before, sum := 0, 0, 0, 0
	for i := 0; i < n; i++ {
		free := len(c.free)
		Alloc(&c)
		switch {
		case len(c.free) > free: // a new chunk was made
			size := len(c.free) + 1
			if size > max(256, before/32) && size > 8 {
				t.Fatalf("chunk of %d after %d values, want at most %d", size, before, max(256, before/32))
			}
			chunks++
			largest = max(largest, size)
			sum += size
		case free == 0: // one of the first values, allocated on its own
			sum++
		}
		before = c.allocated
	}
	if chunks < 100 || chunks > 200 {
		t.Errorf("%d chunks for %d values, want 100 to 200", chunks, n)
	}
	if largest > max(256, c.allocated/32) {
		t.Errorf("the largest chunk is %d, want at most %d", largest, max(256, c.allocated/32))
	}
	if c.allocated != sum {
		t.Errorf("the chunk counts %d values allocated, its chunks hold %d", c.allocated, sum)
	}
	if c.allocated < n || c.allocated > n+n/32+256 {
		t.Errorf("allocated %d values for %d requests, want at most %d", c.allocated, n, n+n/32+256)
	}
}

// chunkMemory is the memory counter of one chunk, as the generated tslabs is of all of them.
type chunkMemory struct{ c Chunk[[4]uint64] }

func (m *chunkMemory) MemoryBytes() uint64 { return m.c.MemoryBytes() }

// The memory a chunk charges is the size of the values it allocated, grown chunks included, and the
// memory limit fails a parse on it.
func TestChunkMemoryIsChargedAndLimited(t *testing.T) {
	var m chunkMemory
	for range 100000 {
		Alloc(&m.c)
	}
	used := m.c.MemoryBytes()
	if want := uint64(m.c.allocated) * 32; used != want || m.c.allocated < 100000 {
		t.Fatalf("MemoryBytes %d for %d values of 32 bytes, want %d", used, m.c.allocated, want)
	}
	check := func(limit uint64) (err error) {
		c := newBudgetControl(sxirruntime.Limits{Memory: limit})
		c.memExt = &m
		defer func() {
			if r := recover(); r != nil {
				f, ok := r.(fatal)
				if !ok {
					panic(r)
				}
				err = f.err
			}
		}()
		c.checkMemory()
		return nil
	}
	if err := check(used); err != nil {
		t.Errorf("a limit of the bytes charged failed: %v", err)
	}
	var le *sxirruntime.LimitError
	if err := check(used - 1); !errors.As(err, &le) || le.Kind != sxirruntime.LimitMemory || le.Used != used {
		t.Errorf("a limit one byte below the bytes charged: %v, want a memory limit error with %d used", err, used)
	}
}

// Match chunks grow the same way unless the grammar keeps them fixed (FixedTypedChunks), and the memory
// they charge is their real size.
func TestMatchChunksGrow(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		p := &TParser{Parser: &Parser{g: &grammar{FixedTypedChunks: fixed}}}
		const n = 100000
		chunks := 0
		for range n {
			if len(p.matches) == 0 {
				chunks++
			}
			p.NewMatch(0, 0, "", false)
		}
		switch {
		case fixed && chunks != (n+nodeChunk-1)/nodeChunk:
			t.Errorf("fixed: %d match chunks for %d matches, want %d of %d", chunks, n, (n+nodeChunk-1)/nodeChunk, nodeChunk)
		case !fixed && (chunks < 50 || chunks > 150):
			t.Errorf("%d match chunks for %d matches, want 50 to 150", chunks, n)
		}
		if p.matchTotal < n || uint64(p.matchTotal)*matchBytes != p.memUsed {
			t.Errorf("fixed %v: matchTotal %d, memUsed %d bytes: the memory charged is not the size of the chunks", fixed, p.matchTotal, p.memUsed)
		}
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
