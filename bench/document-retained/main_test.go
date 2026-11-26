package main

import "testing"

// Every subject parses its text, and a Document with its tree keeps more than the tree alone.
func TestMeasure(t *testing.T) {
	for _, subject := range []string{"settings", "csv", "minilang"} {
		tree, err := measure(subject, "tree", 2000)
		if err != nil {
			t.Fatalf("%s tree: %v", subject, err)
		}
		doc, err := measure(subject, "document", 2000)
		if err != nil {
			t.Fatalf("%s document: %v", subject, err)
		}
		if tree.RetainedBytes <= 0 || doc.RetainedBytes <= tree.RetainedBytes {
			t.Errorf("%s: tree %d bytes, document %d bytes; want 0 < tree < document", subject, tree.RetainedBytes, doc.RetainedBytes)
		}
		if tree.InputBytes != doc.InputBytes {
			t.Errorf("%s: the tree and the document read different texts (%d and %d bytes)", subject, tree.InputBytes, doc.InputBytes)
		}
	}
	if _, err := measure("nosuch", "tree", 10); err == nil {
		t.Error("an unknown subject: no error")
	}
	if _, err := measure("csv", "nosuch", 10); err == nil {
		t.Error("an unknown -what: no error")
	}
}
