//go:build unix

package codegentest

import (
	"os"
	"syscall"
)

// locksWork tells whether holdLock excludes other processes.
const locksWork = true

// holdLock creates the file at path, if needed, and takes an exclusive lock on it that lasts until
// the returned file is closed or the process ends. It fails if another process holds the lock.
func holdLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
