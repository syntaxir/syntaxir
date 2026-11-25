package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDifferingFiles(t *testing.T) {
	write := func(dir string, files map[string]string) string {
		for name, data := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	a := write(t.TempDir(), map[string]string{"parser.go": "x", "lib/lib.go": "y", "only-a.go": "a"})
	for _, c := range []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"same", map[string]string{"parser.go": "x", "lib/lib.go": "y", "only-a.go": "a"}, 0},
		{"one differs", map[string]string{"parser.go": "x", "lib/lib.go": "z", "only-a.go": "a"}, 1},
		{"one missing and one extra", map[string]string{"parser.go": "x", "lib/lib.go": "y", "only-b.go": "b"}, 2},
	} {
		n, err := differingFiles(a, write(t.TempDir(), c.files))
		if err != nil {
			t.Fatal(err)
		}
		if n != c.want {
			t.Errorf("%s: %d differing files, want %d", c.name, n, c.want)
		}
	}
}
