//go:build !(darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd)

package codegentest

import "os"

// locksWork tells whether tryLock excludes other processes. Without file locks, caches of ended
// processes are not told from running ones, so reapCaches removes none.
const locksWork = false

// openLock opens the lock file at path, creating it if create is set.
func openLock(path string, create bool) (*os.File, error) {
	flag := os.O_RDWR
	if create {
		flag |= os.O_CREATE
	}
	return os.OpenFile(path, flag, 0o644)
}

// tryLock does not lock.
func tryLock(*os.File) error { return nil }
