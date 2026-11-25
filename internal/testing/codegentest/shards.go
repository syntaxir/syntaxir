package codegentest

import (
	"fmt"
	"testing"
)

// shardMin is the number of items that a shard of Shards holds at least, and shardMax the number of
// shards at most: a shard may pay for processes of its own (tsc, node), and the shards run together
// on the cores of the machine.
const (
	shardMin = 12
	shardMax = 4
)

// Shards runs run over contiguous parts of items as parallel subtests, each part of its own:
// the items are independent, the order of what run returns is that of the items, and
// what a part reports (a failure of a tool, a mismatch) is what a run over all the items would
// report for its items. Few items run as one part, in t.
//
// With two or more parts the runs execute concurrently, up to shardMax at a time: run, and any
// callback that it passes on (a mutate function, a mismatch check), must be safe for concurrent
// use. A shared counter or flag is an atomic value, and a *testing.T is the part's own t, never
// the parent's: a call to the parent's Fatal or Error from a shard is a misuse.
func Shards[T any](t *testing.T, items []T, run func(t *testing.T, part []T) []string) []string {
	t.Helper()
	n := min(shardMax, len(items)/shardMin)
	if n < 2 {
		return run(t, items)
	}
	per := make([][]string, n)
	t.Run("shards", func(t *testing.T) {
		for i := range n {
			part := items[i*len(items)/n : (i+1)*len(items)/n]
			t.Run(fmt.Sprintf("shard %d", i), func(t *testing.T) {
				t.Parallel()
				per[i] = run(t, part)
			})
		}
	})
	var all []string
	for _, m := range per {
		all = append(all, m...)
	}
	return all
}
