//go:build ignore

// copy writes the engine's copies of the shared files (sharedsrc.Files) and removes copies of files
// that are no longer shared. It runs in internal/runtime/state/sharedsrc.
package main

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/runtime/state/sharedsrc"
)

func main() {
	root := filepath.Join("..", "..", "..", "..")
	state := filepath.Join(root, sharedsrc.StateDir)
	for _, name := range sharedsrc.Files {
		src, err := os.ReadFile(filepath.Join(root, sharedsrc.LibDir, name))
		if err != nil {
			log.Fatal(err)
		}
		out, err := sharedsrc.Copy(name, src)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, name), out, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || slices.Contains(sharedsrc.Files, n) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(state, n))
		if err != nil {
			log.Fatal(err)
		}
		if sharedsrc.IsCopy(data) {
			if err := os.Remove(filepath.Join(state, n)); err != nil {
				log.Fatal(err)
			}
		}
	}
}
