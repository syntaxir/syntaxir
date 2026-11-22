package state

import (
	"io"
)

// Only controlled streams wrap the caller's Reader. Check actual caller reads,
// including bufio refills, and remember errors returned together with data.
// Ordinary streams retain their original buffer reuse and prefetch path.
type StreamReaderObserver struct {
	Reader  io.Reader
	Result  *error
	Control *BudgetControl
}

func (r *StreamReaderObserver) Read(dst []byte) (int, error) {
	r.Control.Check() // a read of the caller's reader is no event: it counts no unit
	n, err := r.Reader.Read(dst)
	*r.Result = err
	r.Control.checkAfterReader(err)
	return n, err
}

// A non-EOF error from this caller read wins over cancellation first observed
// after the read. Without cancellation, bufio still delivers its buffered data
// before exposing the error, preserving the ordinary delivered prefix.
func (c *BudgetControl) checkAfterReader(returned error) {
	if c == nil || c.Ctx == nil {
		return
	}
	if err := contextErr(c.Ctx); err != nil {
		if returned != nil && returned != io.EOF {
			err = returned
		}
		panic(Fatal{Err: err})
	}
	c.SinceCheck = 0
}

// CopyStreamPrefix compacts src to dst, an earlier address in the same slice, and returns the number
// of elements copied. Compacting the input of a stream is no event: it counts nothing.
func CopyStreamPrefix[T any](dst, src []T) int { return copy(dst, src) }

// PruneStreamMemo drops the memo entries before pos, which a stream has committed, and settles what
// the memo holds under controls (releaseAtPrune). Pruning is no event: it counts nothing.
func (p *Parser) PruneStreamMemo(pos int) {
	if pos <= p.Memo.Base {
		return
	}
	p.Memo.Prune(pos)
	if p.controlled() {
		p.releaseAtPrune(pos)
	}
}
