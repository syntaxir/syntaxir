package print

import "fmt"

// generatedDocumentParseGuard begins a generated Document.ParseValue with a
// result of type typ.
func generatedDocumentParseGuard(typ string) string {
	return fmt.Sprintf(" if err := d.doc.BeginParse(); err != nil { var zero %s; return zero, err }\n defer d.doc.EndParse()\n", typ)
}
