package engine

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"reflect"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/vm"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// cloneActions returns a copy of the action code ts that shares no slice with it.
func cloneActions(ts []bytecode.Action) []bytecode.Action {
	out := make([]bytecode.Action, len(ts))
	for i, t := range ts {
		out[i] = bytecode.Action{
			Regs:      t.Regs,
			Code:      append([]bytecode.ActionInstr(nil), t.Code...),
			Consts:    append([]bytecode.ActionConst(nil), t.Consts...),
			Names:     append([]int(nil), t.Names...),
			Members:   append([]bytecode.ActionMember(nil), t.Members...),
			NodeTypes: append([]int(nil), t.NodeTypes...),
			Casts:     append([]bytecode.ActionCast(nil), t.Casts...),
			Structs:   append([]bytecode.ActionStruct(nil), t.Structs...),
		}
	}
	return out
}

// corpusProgram compiles a corpus grammar, or reports false for a grammar that does not compile;
// enginetest.CorpusProgram, which this package cannot import.
func corpusProgram(c codegentest.Case) (*Program, bool) {
	g, err := parseTest(c.Src)
	if err != nil {
		return nil, false
	}
	prog, err := Compile(g, checkpkg.Options{})
	if err != nil {
		return nil, false
	}
	return prog, true
}

// TestActionCodeRoundTrip checks, for every grammar of the corpus and of testdata/grammars, that the
// action code of its module survives a module file: the action code that a loaded program holds equals
// the compiled program's, and loading the action code and exporting it again gives it back. A
// corpus with no action code would make the test vacuous, so it requires action expressions of every pool.
func TestActionCodeRoundTrip(t *testing.T) {
	t.Parallel()
	cases := codegentest.CorpusWithParsers(t)
	type result struct {
		actionExprs int
		pools       [6]bool
		err         string
	}
	results := codegentest.MapParallel(len(cases), func(i int) (r result) {
		prog, ok := corpusProgram(cases[i])
		if !ok {
			return
		}
		m := prog.Module()
		data, err := prog.MarshalBinary("")
		if err != nil {
			r.err = err.Error()
			return
		}
		loaded, _, err := LoadProgram(data)
		if err != nil {
			r.err = err.Error()
			return
		}
		lm := loaded.Module()
		if !reflect.DeepEqual(normalActions(m.Actions), normalActions(lm.Actions)) {
			r.err = cases[i].Name + ": loaded action code differs"
			return
		}
		progs, err := vm.LoadActions(m)
		if err != nil {
			r.err = err.Error()
			return
		}
		for k, tp := range progs {
			strs := map[string]int{}
			for i, s := range m.Strings {
				if _, ok := strs[s]; !ok {
					strs[s] = i
				}
			}
			x, err := tp.Export(func(s string) int { return strs[s] })
			if err != nil {
				r.err = err.Error()
				return
			}
			if !reflect.DeepEqual(normalAction(x), normalAction(canonicalStrings(m, m.Actions[k]))) {
				r.err = cases[i].Name + ": exported action code differs"
				return
			}
			t := &m.Actions[k]
			for j, n := range []int{len(t.Consts), len(t.Names), len(t.Members), len(t.NodeTypes), len(t.Casts), len(t.Structs)} {
				r.pools[j] = r.pools[j] || n > 0
			}
		}
		r.actionExprs = len(m.Actions)
		return
	})
	actionExprs := 0
	var pools [6]bool
	for _, r := range results {
		if r.err != "" {
			t.Error(r.err)
		}
		actionExprs += r.actionExprs
		for j := range pools {
			pools[j] = pools[j] || r.pools[j]
		}
	}
	if actionExprs < 100 {
		t.Fatalf("%d action expressions in the corpus", actionExprs)
	}
	for j, ok := range pools {
		if !ok {
			t.Errorf("no action expression of the corpus has pool %d", j)
		}
	}
}

// canonicalStrings returns t with every string index replaced by the first index of the same
// string, as an export with a first-index string table makes it.
func canonicalStrings(m *bytecode.Module, t bytecode.Action) bytecode.Action {
	first := map[string]int{}
	for i, s := range m.Strings {
		if _, ok := first[s]; !ok {
			first[s] = i
		}
	}
	c := func(i int) int {
		if i < 0 {
			return i
		}
		return first[m.Strings[i]]
	}
	t = cloneActions([]bytecode.Action{t})[0]
	for i := range t.Consts {
		if t.Consts[i].Kind == bytecode.ConstString {
			t.Consts[i].Int = int64(c(int(t.Consts[i].Int)))
		}
	}
	for i := range t.Names {
		t.Names[i] = c(t.Names[i])
	}
	for i := range t.Members {
		t.Members[i].Name, t.Members[i].Field = c(t.Members[i].Name), c(t.Members[i].Field)
	}
	for i := range t.NodeTypes {
		t.NodeTypes[i] = c(t.NodeTypes[i])
	}
	for i := range t.Casts {
		t.Casts[i].Type = c(t.Casts[i].Type)
	}
	for i := range t.Structs {
		t.Structs[i].Type = c(t.Structs[i].Type)
	}
	return t
}

// normalAction returns t with empty pools as nil, which a decoded module may hold as empty slices.
func normalAction(t bytecode.Action) bytecode.Action {
	if len(t.Code) == 0 {
		t.Code = nil
	}
	if len(t.Consts) == 0 {
		t.Consts = nil
	}
	if len(t.Names) == 0 {
		t.Names = nil
	}
	if len(t.Members) == 0 {
		t.Members = nil
	}
	if len(t.NodeTypes) == 0 {
		t.NodeTypes = nil
	}
	if len(t.Casts) == 0 {
		t.Casts = nil
	}
	if len(t.Structs) == 0 {
		t.Structs = nil
	}
	return t
}

func normalActions(ts []bytecode.Action) []bytecode.Action {
	out := make([]bytecode.Action, len(ts))
	for i, t := range ts {
		out[i] = normalAction(t)
	}
	return out
}

// actionCodeSource has action expressions with every pool: constants, names, members, conversions, casts and
// struct literals, a variable and a predicate.
const actionCodeSource = `type S = { A node[string]; B int }
type N = node[string]
var n int = 1
rule main S = a:"a" &{ len(text($a)) > 0 } b:"b" => S{A: N{$a}, B: len(map(list($b.Start, n), (x) => x.(int) + 1))}`

// TestActionCodeValidation checks that a module file whose action code is invalid is rejected, for each
// check of the loader: the pools refer to strings, conversions to named node types, struct literals
// to struct types and casts to valid types; the instructions are known, their operands are in
// range for their kinds, and the code ends with a return or a jump. The unchanged file loads, the
// negative control.
func TestActionCodeValidation(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, actionCodeSource)
	base := prog.Module()
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(data); err != nil {
		t.Fatalf("unchanged module: %v", err)
	}
	// find returns the first action expression with a nonempty pool of the kind.
	find := func(m *bytecode.Module, has func(*bytecode.Action) bool) *bytecode.Action {
		for i := range m.Actions {
			if has(&m.Actions[i]) {
				return &m.Actions[i]
			}
		}
		t.Fatal("no action expression with the pool")
		return nil
	}
	str := func(m *bytecode.Module, s string) int {
		for i, x := range m.Strings {
			if x == s {
				return i
			}
		}
		t.Fatalf("no string %q", s)
		return -1
	}
	for _, tc := range []struct {
		name string
		edit func(m *bytecode.Module)
		want string
	}{
		{"name string", func(m *bytecode.Module) {
			find(m, func(x *bytecode.Action) bool { return len(x.Names) > 0 }).Names[0] = len(m.Strings)
		}, "string"},
		{"const string", func(m *bytecode.Module) {
			x := find(m, func(x *bytecode.Action) bool { return len(x.Consts) > 0 })
			x.Consts[0] = bytecode.ActionConst{Kind: bytecode.ConstString, Int: int64(len(m.Strings))}
		}, "string"},
		{"const kind", func(m *bytecode.Module) {
			find(m, func(x *bytecode.Action) bool { return len(x.Consts) > 0 }).Consts[0].Kind = 9
		}, "invalid constant kind"},
		{"member string", func(m *bytecode.Module) {
			find(m, func(x *bytecode.Action) bool { return len(x.Members) > 0 }).Members[0].Field = -2
		}, "string"},
		{"member kind", func(m *bytecode.Module) {
			find(m, func(x *bytecode.Action) bool { return len(x.Members) > 0 }).Members[0].Kind = 99
		}, "invalid member kind"},
		{"node type", func(m *bytecode.Module) {
			find(m, func(x *bytecode.Action) bool { return len(x.NodeTypes) > 0 }).NodeTypes[0] = str(m, "S")
		}, "is not a named node type"},
		{"struct type", func(m *bytecode.Module) {
			find(m, func(x *bytecode.Action) bool { return len(x.Structs) > 0 }).Structs[0].Type = str(m, "N")
		}, "is not a struct type"},
		{"cast type", func(m *bytecode.Module) {
			x := find(m, func(x *bytecode.Action) bool { return len(x.Casts) > 0 })
			m.Strings = append(m.Strings, "Other")
			x.Casts[0].Type = len(m.Strings) - 1
		}, "invalid field type"},
		{"cast position", func(m *bytecode.Module) {
			find(m, func(x *bytecode.Action) bool { return len(x.Casts) > 0 }).Casts[0].Line = -1
		}, "invalid cast position"},
		{"opcode", func(m *bytecode.Module) {
			m.Actions[0].Code[0].Op = 250
		}, "invalid action instruction"},
		{"register", func(m *bytecode.Module) {
			m.Actions[0].Regs = [4]int{}
		}, "invalid operand"},
		{"jump", func(m *bytecode.Module) {
			x := find(m, func(x *bytecode.Action) bool {
				for _, in := range x.Code {
					if in.Op == m.Actions[0].Code[len(m.Actions[0].Code)-1].Op {
						return true
					}
				}
				return false
			})
			x.Code[len(x.Code)-1] = bytecode.ActionInstr{Op: jumpOp(t, m), Dst: int32(len(x.Code))}
		}, "invalid operand"},
		{"end", func(m *bytecode.Module) {
			x := &m.Actions[0]
			x.Code = append(x.Code, x.Code[0])
			if x.Code[0].Op == x.Code[len(x.Code)-2].Op {
				t.Fatal("first instruction is a return")
			}
		}, "does not end"},
		{"empty", func(m *bytecode.Module) {
			m.Actions[0].Code = nil
		}, "empty action code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := *base
			m.Strings = append([]string(nil), base.Strings...)
			m.Actions = cloneActions(base.Actions)
			tc.edit(&m)
			w := &bytecode.Encoder{Strs: map[string]int{}}
			for i, s := range m.Strings {
				w.Strs[s] = i
				w.List = append(w.List, s)
			}
			w.Str("main")
			w.WriteModule(&m)
			w.Bool(false) // no insertion module
			file := recompose(t, data[:isaOffset+1], w)
			if _, _, err := LoadProgram(file); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// jumpOp returns the opcode of an unconditional jump of action code: that of the instruction of the
// module's action code whose only operand is a jump target, found by loading variants.
func jumpOp(t *testing.T, m *bytecode.Module) uint8 {
	t.Helper()
	ret := m.Actions[0].Code[len(m.Actions[0].Code)-1]
	ret.A = 0
	for op := 0; op < 256; op++ {
		x := bytecode.Action{Regs: [4]int{1, 1, 1, 1}, Code: []bytecode.ActionInstr{{Op: uint8(op), Dst: 1}, ret}}
		y := bytecode.Action{Regs: [4]int{1, 1, 1, 1}, Code: []bytecode.ActionInstr{{Op: uint8(op), Dst: 2}, ret}}
		mx, my := *m, *m
		mx.Actions, my.Actions = []bytecode.Action{x}, []bytecode.Action{y}
		if _, err := vm.LoadActions(&mx); err != nil {
			continue
		}
		if _, err := vm.LoadActions(&my); err != nil {
			return uint8(op) // a jump to the return loads; a jump past it does not
		}
	}
	t.Fatal("no jump instruction")
	return 0
}

// recompose returns the module file whose header (magic, version, mode and instruction-set version)
// is head and whose payload w encodes, with its checksum.
func recompose(t *testing.T, head []byte, w *bytecode.Encoder) []byte {
	t.Helper()
	var out bytes.Buffer
	out.Write(head)
	w.Finish(&out)
	b := out.Bytes()
	if crc32.ChecksumIEEE(b[:len(b)-4]) != binary.LittleEndian.Uint32(b[len(b)-4:]) {
		t.Fatal("checksum")
	}
	return b
}
