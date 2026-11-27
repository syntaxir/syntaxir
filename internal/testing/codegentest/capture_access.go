package codegentest

// CaptureAccessCases returns the grammars and inputs of the capture access tests, which every backend runs.
func CaptureAccessCases() []Case {
	return []Case{
		{Name: "map elements", Src: `rule main = xs:(y:[a-z] [0-9])+ $$ => map($xs, (i) => i.$y)`, Inputs: []string{"a1b2", "a1", "", "a"}},
		{Name: "unmatched alternative", Src: `rule main = xs:(y:"a" / z:"b")+ $$ => filter($xs, (i) => i.$y != nil)`, Inputs: []string{"abab", "bb", ""}},
		{Name: "both captures", Src: `rule main = xs:(y:"a" z:"b")+ $$ => list($xs[0].$z, $xs[0].$y)`, Inputs: []string{"ab", "abab", "a", ""}},
		{Name: "predicate", Src: `rule main = xs:(y:"a"? "b")+ ({$xs[0].$y != nil} "!" / "?") $$`, Inputs: []string{"ab!", "b?", "b!", "ab?"}},
		{Name: "chained", Src: `rule main = xs:(ys:(z:"a" "b")+ "c")+ $$ => $xs[0].$ys[0].$z`, Inputs: []string{"abc", "ababc", "c", ""}},
		{Name: "indexed", Src: `rule main = xs:(y:[a-z] [0-9])+ $$ => $xs[len($xs) - 1].$y`, Inputs: []string{"a1b2", ""}},
		{Name: "struct field", Src: "type Out = { Y *node[string]; Z *node[string] }\nrule main = xs:(y:\"a\"? z:\"b\")+ $$ => Out{Y: $xs[0].$y, Z: $xs[0].$z}", Inputs: []string{"ab", "b", ""}},
		{Name: "sequence capture", Src: `rule main = x:(y:"a" "b") $$ => $x.$y`, Inputs: []string{"ab", "a", ""}},
		{Name: "nested sequence capture", Src: `rule main = x:(y:(z:"a" "b") "c") $$ => list($x.$y, $x.$z, $x.$y.$z)`, Inputs: []string{"abc", "ab"}},
		{Name: "sequence capture after a capture of the same name", Src: `rule main = y:"q" x:((y:"a" / "b") "c") $$ => list($y, $x.$y)`, Inputs: []string{"qac", "qbc", "qc"}},
		{Name: "sequence capture before a capture of the same name", Src: `rule main = x:(y:"a" "b") y:"c" $$ => list($x.$y, $y)`, Inputs: []string{"abc", "ab"}},
		{Name: "struct list", Src: "type Out = { Ys []node[string] }\nrule main = xs:(-\",\" y:[a-z])* $$ => Out{Ys: map($xs, (i) => i.$y)}", Inputs: []string{",a,b", "", ",a,"}},
	}
}

// CaptureAccessResults returns the result of the parse of each input of each case of CaptureAccessCases,
// by the case's name, as ResultJSON writes it for a program that records source information: the
// values that the specification gives (spec/language/actions.md), which every backend must return.
func CaptureAccessResults() map[string][]string {
	return map[string][]string{
		"map elements": {
			`{"node":[{"start":0,"end":1,"value":"a"},{"start":2,"end":3,"value":"b"}]}`,
			`{"node":[{"start":0,"end":1,"value":"a"}]}`,
			`{"err":"1:1: syntax error","node":null}`,
			`{"err":"1:2: syntax error","node":null}`,
		},
		"unmatched alternative": {
			`{"node":[{"start":0,"end":1,"value":[{"start":0,"end":1,"value":"a"}],"captures":{"y":{"path":[0]}}},{"start":2,"end":3,"value":[{"start":2,"end":3,"value":"a"}],"captures":{"y":{"path":[0]}}}]}`,
			`{"node":[]}`,
			`{"err":"1:1: syntax error","node":null}`,
		},
		"both captures": {
			`{"node":[{"start":1,"end":2,"value":"b"},{"start":0,"end":1,"value":"a"}]}`,
			`{"node":[{"start":1,"end":2,"value":"b"},{"start":0,"end":1,"value":"a"}]}`,
			`{"err":"1:2: syntax error","node":null}`,
			`{"err":"1:1: syntax error","node":null}`,
		},
		"predicate": {
			`{"node":{"start":0,"end":3,"value":[{"start":0,"end":2,"value":[{"start":0,"end":2,"value":[{"start":0,"end":1,"value":"a"},{"start":1,"end":2,"value":"b"}],"captures":{"y":{"path":[0]}}}]},{"start":2,"end":3,"value":[{"start":2,"end":3,"value":"!"}]}],"rule":"main","captures":{"xs":{"path":[0]}}}}`,
			`{"node":{"start":0,"end":2,"value":[{"start":0,"end":1,"value":[{"start":0,"end":1,"value":[null,{"start":0,"end":1,"value":"b"}]}]},{"start":1,"end":2,"value":"?"}],"rule":"main","captures":{"xs":{"path":[0]}}}}`,
			`{"err":"1:2: syntax error","node":null}`,
			`{"node":{"start":0,"end":3,"value":[{"start":0,"end":2,"value":[{"start":0,"end":2,"value":[{"start":0,"end":1,"value":"a"},{"start":1,"end":2,"value":"b"}],"captures":{"y":{"path":[0]}}}]},{"start":2,"end":3,"value":"?"}],"rule":"main","captures":{"xs":{"path":[0]}}}}`,
		},
		"chained": {
			`{"node":{"start":0,"end":1,"value":"a"}}`,
			`{"node":{"start":0,"end":1,"value":"a"}}`,
			`{"err":"1:1: syntax error","node":null}`,
			`{"err":"1:1: syntax error","node":null}`,
		},
		"indexed": {
			`{"node":{"start":2,"end":3,"value":"b"}}`,
			`{"err":"1:1: syntax error","node":null}`,
		},
		"struct field": {
			`{"node":{"type":"Out","start":0,"end":2,"fields":{"Y":{"start":0,"end":1,"value":"a"},"Z":{"start":1,"end":2,"value":"b"}}}}`,
			`{"node":{"type":"Out","start":0,"end":1,"fields":{"Y":null,"Z":{"start":0,"end":1,"value":"b"}}}}`,
			`{"err":"1:1: syntax error","node":null}`,
		},
		"sequence capture": {
			`{"node":{"start":0,"end":1,"value":"a"}}`,
			`{"err":"1:2: syntax error","node":null}`,
			`{"err":"1:1: syntax error","node":null}`,
		},
		"nested sequence capture": {
			`{"node":[{"start":0,"end":2,"value":[{"start":0,"end":1,"value":"a"},{"start":1,"end":2,"value":"b"}],"captures":{"z":{"path":[0]}}},{"start":0,"end":1,"value":"a"},{"start":0,"end":1,"value":"a"}]}`,
			`{"err":"1:3: syntax error","node":null}`,
		},
		"sequence capture after a capture of the same name": {
			`{"node":[{"start":1,"end":2,"value":"a"},{"start":1,"end":2,"value":"a"}]}`,
			`{"node":[{"start":0,"end":1,"value":"q"},null]}`,
			`{"err":"1:2: syntax error","node":null}`,
		},
		"sequence capture before a capture of the same name": {
			`{"node":[{"start":0,"end":1,"value":"a"},{"start":2,"end":3,"value":"c"}]}`,
			`{"err":"1:3: syntax error","node":null}`,
		},
		"struct list": {
			`{"node":{"type":"Out","start":0,"end":4,"fields":{"Ys":[{"start":1,"end":2,"value":"a"},{"start":3,"end":4,"value":"b"}]}}}`,
			`{"node":{"type":"Out","start":0,"end":0,"fields":{"Ys":[]}}}`,
			`{"err":"1:4: syntax error","node":null}`,
		},
	}
}
