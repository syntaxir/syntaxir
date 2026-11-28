package lib

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// A region is a part of lib.ts that a reference build tag replaces: the lines between
//
//	// sxir:reference <name> begin
//	// sxir:reference <name> end
//
// at any indentation. The build with the tag replaces them with the reference body, the file
// reference/<name>.region; every build drops the marker lines, so the default build's lib.ts is the
// library without them. Each region is declared by a pair of files constrained by its tag, which set
// body in the reference build and leave it empty otherwise.
type region struct {
	name string
	body string // the reference body, empty in the default build
}

// regions are the reference regions of lib.ts.
var regions = []region{inputPreparation}

// The marker lines of a region.
const (
	markerPrefix = "// sxir:reference "
	beginSuffix  = " begin"
	endSuffix    = " end"
)

// libName is the file of the library that holds the regions.
const libName = "lib.ts"

// mustResolve returns files with lib.ts resolved by resolve.
func mustResolve(files embed.FS, rs []region) fs.FS {
	data, err := files.ReadFile(libName)
	if err != nil {
		panic(err)
	}
	src, err := resolve(string(data), rs)
	if err != nil {
		panic("internal/backend/codegen/typescript/lib: " + err.Error())
	}
	return libFS{files, src}
}

// resolve returns src with the marker lines of the regions rs dropped, and the lines between them
// replaced by the body of a region that has one. It fails unless the markers of every region appear
// exactly once each, the begin marker first, and every marker names a region of rs.
func resolve(src string, rs []region) (string, error) {
	known := map[string]bool{}
	for _, r := range rs {
		known[r.name] = true
	}
	lines := strings.SplitAfter(src, "\n")
	begins, ends := map[string]int{}, map[string]int{}
	for i, l := range lines {
		t := strings.TrimSpace(l)
		rest, ok := strings.CutPrefix(t, markerPrefix)
		if !ok {
			continue
		}
		var name string
		var at map[string]int
		if n, ok := strings.CutSuffix(rest, beginSuffix); ok {
			name, at = n, begins
		} else if n, ok := strings.CutSuffix(rest, endSuffix); ok {
			name, at = n, ends
		} else {
			return "", fmt.Errorf("%s:%d: the marker %q neither begins nor ends a region", libName, i+1, t)
		}
		if !known[name] {
			return "", fmt.Errorf("%s:%d: the marker names %q, which is no reference region", libName, i+1, name)
		}
		if _, dup := at[name]; dup {
			return "", fmt.Errorf("%s:%d: the region %q is marked twice", libName, i+1, name)
		}
		at[name] = i
	}
	drop := map[int]bool{}
	replace := map[int]string{}
	for _, r := range rs {
		b, okb := begins[r.name]
		e, oke := ends[r.name]
		switch {
		case !okb || !oke:
			return "", fmt.Errorf("%s has no begin and end markers of the region %q", libName, r.name)
		case e < b:
			return "", fmt.Errorf("%s ends the region %q before it begins", libName, r.name)
		}
		drop[b], drop[e] = true, true
		if r.body != "" {
			for i := b + 1; i < e; i++ {
				drop[i] = true
			}
			replace[b] = r.body
		}
	}
	var out strings.Builder
	for i, l := range lines {
		if body, ok := replace[i]; ok {
			out.WriteString(body)
		}
		if !drop[i] {
			out.WriteString(l)
		}
	}
	return out.String(), nil
}

// libFS is the library with lib.ts resolved. It is a comparable value, as the embedded files are.
type libFS struct {
	files embed.FS
	lib   string
}

func (f libFS) Open(name string) (fs.File, error) {
	if name != libName {
		return f.files.Open(name)
	}
	return &libFile{strings.NewReader(f.lib), libInfo{size: int64(len(f.lib))}}, nil
}

func (f libFS) ReadFile(name string) ([]byte, error) {
	if name != libName {
		return f.files.ReadFile(name)
	}
	return []byte(f.lib), nil
}

func (f libFS) ReadDir(name string) ([]fs.DirEntry, error) { return f.files.ReadDir(name) }

// libFile is the resolved lib.ts opened.
type libFile struct {
	*strings.Reader
	info libInfo
}

func (f *libFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *libFile) Close() error               { return nil }

// libInfo describes the resolved lib.ts.
type libInfo struct{ size int64 }

func (i libInfo) Name() string       { return libName }
func (i libInfo) Size() int64        { return i.size }
func (i libInfo) Mode() fs.FileMode  { return 0o444 }
func (i libInfo) ModTime() time.Time { return time.Time{} }
func (i libInfo) IsDir() bool        { return false }
func (i libInfo) Sys() any           { return nil }
