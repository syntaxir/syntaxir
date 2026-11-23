package engine

import (
	"syntaxir.dev/sxir/internal/runtime/document"
)

// The Document, its snapshots and change notifications are declared in the document package. The
// engine re-exports them under the same names for the package engine.

type (
	ChangeDocument  = document.ChangeDocument
	DocumentVersion = document.DocumentVersion
	DocumentChange  = document.DocumentChange
	Snapshot        = document.Snapshot
	SnapshotNode    = document.SnapshotNode
)
