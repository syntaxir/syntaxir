package probe

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// WriteSizes writes, for each parser of Grammars, one line "size <parser> <kind> <bytes>" for the
// generated Go parser.go (go-source) and the one that -optimize size generates
// (go-source-size-profile), a binary that calls its ParseValue, built with -trimpath and stripped of
// symbols and DWARF (go-binary), the copy of the library that the parser generated with -lib embed
// carries (go-lib) and the binary built from that parser (go-binary-embed), the generated TypeScript
// parser.ts (ts-raw, ts-gzip) and the lib.ts copy of the library generated with it (ts-lib-raw,
// ts-lib-gzip), compressed with gzip -9. The whole library, which -no-opt used-features copies, is
// recorded once as parser "runtime" (go-lib, ts-lib-raw, ts-lib-gzip). The files are generated into
// the work directory with sxirBin, the sxir command of the repository at root.
func WriteSizes(w io.Writer, root, sxirBin, work string) error {
	for _, name := range Grammars {
		genDir := filepath.Join(work, "gen-"+name)
		if err := GenerateGo(root, sxirBin, genDir, name); err != nil {
			return err
		}
		st, err := os.Stat(filepath.Join(genDir, "parser.go"))
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "size %s go-source %d\n", name, st.Size())
		// The same parser in the size profile, which go-source (the default speed profile) is judged against.
		sizeDir := filepath.Join(work, "gen-size-"+name)
		if err := GenerateGo(root, sxirBin, sizeDir, name, "-optimize", "size"); err != nil {
			return err
		}
		if st, err = os.Stat(filepath.Join(sizeDir, "parser.go")); err != nil {
			return err
		}
		fmt.Fprintf(w, "size %s go-source-size-profile %d\n", name, st.Size())
		_, bin, err := BuildProgram(root, filepath.Join(work, "bin-"+name), genDir, SizeMain)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "size %s go-binary %d\n", name, bin)
		// The same parser with its own copy of the library, narrowed to its features.
		embedDir := filepath.Join(work, "gen-embed-"+name)
		if err := GenerateGo(root, sxirBin, embedDir, name, "-lib", "embed"); err != nil {
			return err
		}
		lib, err := dirGoSize(filepath.Join(embedDir, "internal", "libgo"))
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "size %s go-lib %d\n", name, lib)
		_, bin, err = BuildProgram(root, filepath.Join(work, "bin-embed-"+name), embedDir, SizeMain)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "size %s go-binary-embed %d\n", name, bin)
		if name == Grammars[0] {
			// The whole library, which every copy had before it was narrowed.
			fullDir := filepath.Join(work, "gen-full-"+name)
			if err := GenerateGo(root, sxirBin, fullDir, name, "-lib", "embed", "-no-opt", "used-features"); err != nil {
				return err
			}
			full, err := dirGoSize(filepath.Join(fullDir, "internal", "libgo"))
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "size runtime go-lib %d\n", full)
		}
		out := filepath.Join(work, "size-ts", name)
		if err := GenerateTS(root, sxirBin, out, name); err != nil {
			return err
		}
		raw, gz, err := SizeOfFile(filepath.Join(out, "parser.ts"))
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "size %s ts-raw %d\nsize %s ts-gzip %d\n", name, raw, name, gz)
		raw, gz, err = SizeOfFile(filepath.Join(out, "lib.ts"))
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "size %s ts-lib-raw %d\nsize %s ts-lib-gzip %d\n", name, raw, name, gz)
		if name == Grammars[0] {
			// The whole library, which every copy had before it was narrowed.
			fullOut := filepath.Join(work, "size-ts-full", name)
			if err := GenerateTS(root, sxirBin, fullOut, name, "-no-opt", "used-features"); err != nil {
				return err
			}
			raw, gz, err := SizeOfFile(filepath.Join(fullOut, "lib.ts"))
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "size runtime ts-lib-raw %d\nsize runtime ts-lib-gzip %d\n", raw, gz)
		}
	}
	return nil
}

// dirGoSize returns the total size of the Go files of a directory.
func dirGoSize(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			info, err := e.Info()
			if err != nil {
				return 0, err
			}
			n += int(info.Size())
		}
	}
	return n, nil
}
