package libgo

import "testing"

// sparseMemoRecord records a call in the sparse form for more than 64 rules and in the dense form
// otherwise, and reports whether it is the first.
func sparseMemoRecord(memo *firstCalls, pos, r int) bool {
	if memo.stride > 64 {
		return memo.markSparseSeen(pos, r)
	}
	return memo.markSeen(pos, r)
}

func TestSparseMemoBoundariesAndReset(t *testing.T) {
	memo := firstCalls{}
	for _, stride := range []int{63, 64, 65, 128, 175, 65, 64} {
		memo.stride = stride
		positions := []int{0, 1, 63, 64, 1023, 1024, 1025, 65535}
		for _, pos := range positions {
			for _, r := range []int{0, stride - 1} {
				if !sparseMemoRecord(&memo, pos, r) || sparseMemoRecord(&memo, pos, r) {
					t.Fatalf("stride=%d pos=%d rule=%d: first/repeated mismatch", stride, pos, r)
				}
			}
		}
		for i := len(positions) - 1; i >= 0; i-- {
			if sparseMemoRecord(&memo, positions[i], stride-1) {
				t.Fatal("returning to an earlier page lost its bit")
			}
		}
		if stride > 64 && len(memo.seen) != 0 {
			t.Fatal("sparse path allocated the dense bitset")
		}
		memo.reset(1 << 20)
		for _, dir := range memo.pages[:cap(memo.pages)] {
			if dir.last != nil || len(dir.pages) != 0 {
				t.Fatal("cached page pointer or directory length survived reset")
			}
		}
		if memo.bitPages.n != 0 {
			t.Fatal("allocated page count survived reset")
		}
		memo.stride = stride
		for _, pos := range positions {
			for _, r := range []int{0, stride - 1} {
				if !sparseMemoRecord(&memo, pos, r) {
					t.Fatalf("stride=%d pos=%d rule=%d: stale bit survived reset", stride, pos, r)
				}
			}
		}
		memo.reset(1 << 20)
	}
}

func TestSparseMemoStrideChanges(t *testing.T) {
	memo := firstCalls{}
	for _, stride := range []int{175, 65, 64, 128, 175} {
		memo.stride = stride
		if !sparseMemoRecord(&memo, 1024, stride-1) || sparseMemoRecord(&memo, 1024, stride-1) {
			t.Fatal("stride change reused a stale bit")
		}
		memo.reset(1 << 20)
	}
}

// The pages of one rule beyond the bound are dropped by reset, as its directories are.
func TestSparseMemoPagesBound(t *testing.T) {
	const limit = 1 << 20
	memo := firstCalls{stride: 65}
	for i := 0; i <= limit/(64*16)*64; i++ {
		memo.markSparseSeen(i*1024, 0)
	}
	memo.reset(limit)
	if memo.pages != nil || memo.bitPages.chunks != nil {
		t.Fatal("oversized pages retained")
	}
}

func TestSparseMemoPoolReuse(t *testing.T) {
	needsScratchPools(t)
	for _, typed := range []bool{false, true} {
		p := &Parser{memo: memoTable{firstCalls: firstCalls{stride: 65, sparse: true}}, deferMemo: true}
		r := &Rule{Seen: 64}
		p.Pos = 1024
		if !p.FirstCall(r) || p.FirstCall(r) {
			t.Fatal("first/repeated mismatch")
		}
		if typed {
			q := &TParser{Parser: p}
			q.recycle()
		} else {
			p.release()
			for _, dir := range p.memo.pages {
				if dir.last != nil {
					t.Fatal("released parser retains cached page")
				}
				for _, page := range dir.pages {
					if page != nil {
						t.Fatal("released parser retains page pointers")
					}
				}
			}
			for _, c := range p.memo.calls {
				if c != (seenCalls{}) {
					t.Fatal("released parser retains eager decisions")
				}
			}
			continue
		}
		p.memo.stride, p.memo.sparse, p.Pos, p.deferMemo = 65, true, 1024, true
		if !p.FirstCall(r) {
			t.Fatal("page bit or eager decision survived pool reset")
		}
	}
}

func TestSparseMemoScratchBounds(t *testing.T) {
	const limit = 1 << 20
	dense := &TParser{Parser: &Parser{n: 4 << 20, memo: memoTable{firstCalls: firstCalls{seen: make([]uint64, 0, limit+1)}}}}
	dense.recycle()
	if dense.memo.seen != nil {
		t.Fatal("typed pool retained oversized dense seen scratch")
	}
	for _, memo := range []firstCalls{
		{pages: make([]seenRulePages, 0, limit/5+1)},
		{pages: make([]seenRulePages, 0, limit/5), bitPages: seenPageArena{chunks: []*[64][16]uint64{new([64][16]uint64)}}},
		{pages: []seenRulePages{{pages: make([]*[16]uint64, 0, limit+1)}}},
	} {
		memo.reset(limit)
		if memo.pages != nil || memo.bitPages.chunks != nil {
			t.Fatal("oversized sparse scratch retained")
		}
	}
}

func TestSparseMemoTypedRetentionAndTrimming(t *testing.T) {
	const limit = 2 << 20
	for _, extra := range []int{0, 1} {
		// Count the whole chunk and its directory, including unused capacity.
		chunk := new([64][16]uint64)
		chunk[0][0] = 1
		directory := make([]*[16]uint64, 1, limit-5*65-64*16-1+extra)
		directory[0] = &chunk[0]
		pages := make([]seenRulePages, 65)
		pages[0] = seenRulePages{pages: directory, last: directory[0], at: 1}
		p := &TParser{Parser: &Parser{n: limit, memo: memoTable{firstCalls: firstCalls{
			pages:    pages,
			bitPages: seenPageArena{chunks: []*[64][16]uint64{chunk}, n: 1},
			calls:    []seenCalls{{calls: 1, repeats: 1, eager: true}},
		}}}}
		p.recycle()
		if extra == 1 {
			if p.memo.pages != nil || p.memo.bitPages.chunks != nil {
				t.Fatal("typed pool retained sparse backing above its input spend")
			}
			continue
		}
		if cap(p.memo.pages) != 65 || len(p.memo.bitPages.chunks) != 1 {
			t.Fatal("typed pool discarded sparse backing at its input spend")
		}
		dir := p.memo.pages[:1][0]
		// A directory keeps its storage but starts the next parse empty, so that the charges of growing
		// it are the parse's own.
		if dir.last != nil || dir.at != 0 || len(dir.pages) != 0 || dir.pages[:1][0] != nil || chunk[0][0] != 0 || p.memo.bitPages.n != 0 || p.memo.calls[:1][0] != (seenCalls{}) {
			t.Fatal("retained sparse scratch preserved first-call state")
		}
		p.n, p.memo.stride, p.memo.sparse, p.Pos, p.deferMemo = limit, 65, true, 0, true
		if !p.FirstCall(&Rule{Seen: 0}) || p.FirstCall(&Rule{Seen: 0}) {
			t.Fatal("retained sparse scratch changed first/repeated decisions")
		}
		if p.memo.bitPages.chunks[0] != chunk || p.memo.pages[0].pages[0] != &chunk[0] {
			t.Fatal("retained sparse scratch was not reused")
		}
		p.n = 16 // A small parse trims a cache left by a larger parse.
		p.recycle()
		if p.memo.pages != nil || p.memo.bitPages.chunks != nil {
			t.Fatal("small typed parse retained the preceding large sparse cache")
		}
		p.n, p.memo.stride, p.memo.sparse, p.Pos, p.deferMemo = limit, 65, true, 0, true
		if !p.FirstCall(&Rule{Seen: 0}) || p.FirstCall(&Rule{Seen: 0}) {
			t.Fatal("trimmed sparse scratch changed first/repeated decisions")
		}
	}
}

func TestSparseMemoTypedAbsoluteRetentionBound(t *testing.T) {
	const limit = 4 << 20
	p := &TParser{Parser: &Parser{n: int(^uint(0) >> 1), memo: memoTable{firstCalls: firstCalls{
		pages: []seenRulePages{{pages: make([]*[16]uint64, 0, limit)}},
	}}}}
	p.recycle()
	if p.memo.pages != nil || p.memo.bitPages.chunks != nil {
		t.Fatal("maximum-sized input bypassed the absolute sparse retention bound")
	}
}

// Dropping an oversized dense buffer must preserve the plain parser's
// existing policy of discarding its complete memo scratch table.
func TestSparseMemoDenseReleasePolicy(t *testing.T) {
	needsScratchPools(t)
	p := &Parser{memo: memoTable{slots: make([]*memoEntry, 1), firstCalls: firstCalls{seen: make([]uint64, 0, (1<<20)+1)}}}
	p.release()
	if p.memo.slots != nil || p.memo.seen != nil {
		t.Fatal("oversized dense table changed the plain release policy")
	}
}
