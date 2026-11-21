// Command gensyntax generates the simplified grammar reference.
package main

import (
	"bytes"
	"fmt"
	"os"
	"text/template"

	"syntaxir.dev/sxir/internal/frontend/parser"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	full, err := os.ReadFile("../../sxir.sxir")
	if err != nil {
		return err
	}
	g, err := parser.Parse(string(full))
	if err != nil {
		return err
	}
	t, err := template.ParseFiles("syntax.md.tmpl")
	if err != nil {
		return err
	}
	data := struct{ Simple string }{syntaxfmt.FormatSyntax(g)}
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}
	if existing, err := os.ReadFile("syntax.md"); err == nil && bytes.Equal(existing, out.Bytes()) {
		return nil
	}
	return os.WriteFile("syntax.md", out.Bytes(), 0644)
}
