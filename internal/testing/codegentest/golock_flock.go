//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd

package codegentest

import (
	"os"
	"syscall"
)

// locksWork tells whether tryLock excludes other processes.
const locksWork = true

// openLock opens the lock file at path, creating it if create is set.
func openLock(path string, create bool) (*os.File, error) {
	flag := os.O_RDWR
	if create {
		flag |= os.O_CREATE
	}
	return os.OpenFile(path, flag, 0o644)
}

// tryLock takes an exclusive lock on f that lasts until f is closed or the process ends. It fails if
// another process holds the lock.
func tryLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
