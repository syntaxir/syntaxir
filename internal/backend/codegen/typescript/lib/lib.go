// Package lib holds the TypeScript library of generated parsers (lib.ts and the files it
// assembles), which the TypeScript generator reads and the -lib modes copy.
package lib

import (
	"embed"
	"io/fs"
)

//go:embed *.ts
var files embed.FS

// FS holds the files of the library, with the reference regions of lib.ts resolved for the build
// (regions).
var FS fs.FS = mustResolve(files, regions)
