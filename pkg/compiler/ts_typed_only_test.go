package compiler_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
)

// TestTypeScriptTypedOnly checks the TypedOnly field of TSConfig: without it a typed grammar keeps the tree entry
// points beside the typed ones, with it only the typed ones remain, and it needs Types.
func TestTypeScriptTypedOnly(t *testing.T) {
	src := grammar.FromString("typed.sxir", "type A = { Text string }\n@start\nrule main A = t:\"a\" #atomic => A{Text: text($t)}")
	text := func(cfg compiler.TSConfig) string {
		t.Helper()
		files, err := compiler.GenerateTypeScript(src, cfg, compiler.WithStart("main"))
		if err != nil {
			t.Fatal(err)
		}
		var all strings.Builder
		for _, f := range files {
			all.Write(f.Content)
		}
		return all.String()
	}
	tree := []string{"export function parse(", "export function parseRule(", "export function parseWithOptions("}
	typed := []string{"export function parseValue(", "export function parseRuleValue("}
	full := text(compiler.TSConfig{Types: true})
	for _, entry := range append(tree, typed...) {
		if !strings.Contains(full, entry) {
			t.Errorf("a typed grammar without TypedOnly has no %s", entry)
		}
	}
	only := text(compiler.TSConfig{Types: true, TypedOnly: true})
	for _, entry := range tree {
		if strings.Contains(only, entry) {
			t.Errorf("TypedOnly output has %s", entry)
		}
	}
	for _, entry := range typed {
		if !strings.Contains(only, entry) {
			t.Errorf("TypedOnly output has no %s", entry)
		}
	}
	for _, cfg := range []compiler.TSConfig{{TypedOnly: true}, {Types: true, TypedOnly: true, Document: true}, {Types: true, TypedOnly: true, RecognizeOnly: true}} {
		if files, err := compiler.GenerateTypeScript(src, cfg, compiler.WithStart("main")); err == nil || len(files) != 0 {
			t.Errorf("%+v: %d files, error %v, want a refusal", cfg, len(files), err)
		}
	}
}
