// Command gen writes the generated tables of the optimization catalog and the code-generation guide
// (package optcatalog). go generate runs it from internal/optcatalog.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"syntaxir.dev/sxir/internal/optcatalog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "optcatalog:", err)
		os.Exit(1)
	}
}

func run() error {
	root := filepath.Join("..", "..")
	files, err := optcatalog.Generate(root)
	if err != nil {
		return err
	}
	for path, data := range files {
		full := filepath.Join(root, path)
		if old, err := os.ReadFile(full); err == nil && bytes.Equal(old, data) {
			continue
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
