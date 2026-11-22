//go:build ignore

// print_lib runs the pipeline of the Go library (driver.GoLibrary) and writes its files: the printed
// compositions of the Go library to pkg/runtime/libgo/call_exec.go, action_exec.go, operators_exec.go
// and stream_exec.go, those of the engine to internal/runtime/state/call_exec.go, action_exec.go,
// operators_exec.go, stream_exec.go and document_exec.go, the action expression compositions of the
// engine's backends to internal/backend/closure/action_exec.go and internal/runtime/vm/action_exec.go,
// and the engine's conformance bindings to the *_bind_exec_test.go files of internal/runtime/state.
// It runs from go generate in the directory of package libprint.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
)

// root is the root of the module, relative to the directory of package libprint.
const root = "../../../../.."

func main() {
	settings, err := driver.GenSettings(common.GenOptions{})
	if err != nil {
		log.Fatal(err)
	}
	out, err := driver.GoLibrary(context.Background(), driver.Options{Settings: settings})
	if err != nil {
		log.Fatal(err)
	}
	files, ok := out.(*result.Files)
	if !ok {
		log.Fatalf("the Go library printed %T, not files", out)
	}
	for _, f := range files.Files {
		if err := os.WriteFile(filepath.Join(root, f.Path), f.Content, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
