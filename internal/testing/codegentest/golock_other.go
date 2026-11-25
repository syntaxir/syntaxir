//go:build !unix

package codegentest

import "os"

// locksWork tells whether holdLock excludes other processes.
const locksWork = false

// holdLock creates the file at path and returns it; without file locks, caches of ended processes
// are not told from running ones, so reapCaches finds none to remove.
func holdLock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
}
