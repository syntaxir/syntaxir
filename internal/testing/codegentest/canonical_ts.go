package codegentest

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TSTypedValueJSON is the JavaScript function of the harness that writes a typed TypeScript value as
// the JSON that canonicalTS reads: a node of the runtime, which the conversion of an open value
// keeps (TypedValue), is {"$node": its JSON (marshal)}, a bigint is {"$bigint": its decimal digits},
// and in a string each lone surrogate that stands for an invalid byte becomes U+FFFD, as it does in
// the JSON of the engine's value. Other values are written as they are.
const TSTypedValueJSON = `(m, v) => JSON.stringify(v, function (key, x) {
  const raw = this[key];
  if (raw instanceof m.Node) return { $node: JSON.parse(m.marshal(raw)) };
  if (typeof raw === "bigint") return { $bigint: raw.toString() };
  if (typeof x === "string") return x.replace(/[\udc80-\udcff]/gu, "�");
  return x;
})`

// tsKinds are the types that a TypeScript node of an unnamed node type of one kind has (StringNode,
// TupleNode, ListNode): the kind, whatever the named node type of the engine's node.
var tsKinds = map[string]bool{valueimpl.TypeString: true, valueimpl.TypeTuple: true, valueimpl.TypeList: true}

// canonicalTS returns the canonical form of a typed TypeScript value (CanonicalEngine), from the JSON
// TSTypedValueJSON writes. structs holds the names of the grammar's struct types. It maps the
// TypeScript shapes to the engine's:
//   - a struct, an object whose "type" names a struct type, is {"type", "start", "end", "fields"},
//     with its other keys, the grammar names of its fields, as the fields;
//   - a recovered error ("type" error, "message") is the struct Error with the field Message, and a
//     missing value ("type" missing, "expected", "message") the struct Missing with the fields
//     Expected and Message;
//   - any other object with "value" is a node {"start", "end", "value", "type"}, where a kind as
//     "type" is TypedGenericNode: the value of an unnamed node type of one kind has its kind as its
//     type;
//   - a tuple is an array, as in the engine;
//   - a node of the runtime ($node) is the canonical form of its JSON, and a bigint ($bigint) the
//     number it is, as JSON decoding gives the engine's.
//
// Keys other than the listed ones are errors.
func canonicalTS(v any, structs map[string]bool) (any, error) {
	switch v := v.(type) {
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			c, err := canonicalTS(x, structs)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	case map[string]any:
		if n, ok := v["$node"]; ok {
			if err := onlyKeys(v, "$node"); err != nil {
				return nil, err
			}
			return CanonicalEngine(n)
		}
		if b, ok := v["$bigint"].(string); ok {
			if err := onlyKeys(v, "$bigint"); err != nil {
				return nil, err
			}
			f, err := strconv.ParseFloat(b, 64)
			if err != nil {
				return nil, err
			}
			return f, nil
		}
		typ, ok := v["type"].(string)
		if !ok {
			return nil, fmt.Errorf("typed object with keys %v has no type", keysOf(v))
		}
		span := func(name string, fields map[string]any) map[string]any {
			return map[string]any{"type": name, "start": v["start"], "end": v["end"], "fields": fields}
		}
		switch {
		case typ == valueimpl.TypeError && !structs[typ]:
			if err := onlyKeys(v, "type", "start", "end", "message"); err != nil {
				return nil, err
			}
			return span("Error", map[string]any{"Message": v["message"]}), nil
		case typ == valueimpl.TypeMissing:
			if err := onlyKeys(v, "type", "start", "end", "expected", "message"); err != nil {
				return nil, err
			}
			return span("Missing", map[string]any{"Expected": v["expected"], "Message": v["message"]}), nil
		case structs[typ]:
			fields := map[string]any{}
			for k, x := range v {
				if k == "type" || k == "start" || k == "end" {
					continue
				}
				c, err := canonicalTS(x, structs)
				if err != nil {
					return nil, err
				}
				fields[k] = c
			}
			return span(typ, fields), nil
		}
		if err := onlyKeys(v, "type", "start", "end", "value"); err != nil {
			return nil, err
		}
		value, err := canonicalTS(v["value"], structs)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"start": v["start"], "end": v["end"], "value": value, "type": typ}
		if tsKinds[typ] {
			out["type"] = TypedGenericNode
		}
		return out, nil
	}
	return v, nil
}

// CanonicalTSMatches reports whether the typed TypeScript value encoded in raw, as TSTypedValueJSON
// writes it, has the canonical form want of the engine's value, for a grammar whose struct types
// are structs.
func CanonicalTSMatches(raw string, want any, structs map[string]bool) error {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return err
	}
	got, err := canonicalTS(v, structs)
	if err != nil {
		return err
	}
	var diffs []string
	CanonicalDifferences(got, want, "$", false, &diffs)
	if len(diffs) > 0 {
		return fmt.Errorf("canonical values differ: %s", strings.Join(diffs, "; "))
	}
	return nil
}
