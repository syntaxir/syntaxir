package engine

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"maps"
	"math/rand"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestCompiledRoundTrip checks that a program loaded from a compiled grammar returns the same
// results as the original program.
func TestCompiledRoundTrip(t *testing.T) {
	t.Parallel()
	for _, c := range codegentest.GenCorpus(t) {
		t.Run(c.Name, func(t *testing.T) {
			checkCompiledRoundTrip(t, mustCompile(t, c.Src), c.Inputs)
		})
	}
}

// Text struct constructors whose field value fails a cast round-trip through compiled files.
func TestCompiledRoundTripTextStructConstructors(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"value error":     "type Int = { Text string }\nvar value int\nrule main = {value := 1} \"x\" => Int{Text: list(value, \"a\")[0].(string)}",
		"predicate error": "type Int = { Text string }\nvar value int\nrule main = {value := 1} {Int{Text: list(value, \"a\")[0].(string)} != nil} \"x\" / \"x\"",
	} {
		t.Run(name, func(t *testing.T) {
			checkCompiledRoundTrip(t, mustCompile(t, src), []string{"x", ""})
		})
	}
}

func checkCompiledRoundTrip(t *testing.T, prog *Program, inputs []string) {
	for _, mode := range allModes {
		data, err := prog.Marshal("main", mode)
		if err != nil {
			t.Fatal(err)
		}
		if data[len(compiledMagic)] != compiledVersion || CompiledMode(data[len(compiledMagic)+1]) != mode {
			t.Fatalf("%s mode: compiled version %d, mode %d", mode, data[len(compiledMagic)], data[len(compiledMagic)+1])
		}
		loaded, start, err := LoadProgram(data)
		if err != nil {
			t.Fatal(err)
		}
		if start != "main" {
			t.Errorf("%s mode: start %q", mode, start)
		}
		if got, ok := loaded.Mode(); !ok || got != mode {
			t.Errorf("loaded mode %v %v, want %s", got, ok, mode)
		}
		if loaded.Static() != nil || strings.Join(loaded.Rules(), ",") != strings.Join(prog.Rules(), ",") {
			t.Errorf("%s mode: checked program %v, rules %v", mode, loaded.Static() != nil, loaded.Rules())
		}
		// Saving the loaded program again produces the same bytes.
		again, err := loaded.Marshal("main", mode)
		if err != nil || !bytes.Equal(data, again) {
			t.Errorf("%s mode: re-marshaled data differs (%v)", mode, err)
		}
		_, err = loaded.ParseWith("main", "", servedBy(mode, ParseOptions{Backend: Closure}))
		wantClosureRefused(t, err)
		for _, in := range inputs {
			for _, b := range []Backend{Default, Bytecode, BytecodeIterative} {
				want := codegentest.ResultJSON(prog.ParseWith("main", in, servedBy(mode, ParseOptions{Backend: b})))
				if got := codegentest.ResultJSON(loaded.ParseWith(start, in, servedBy(mode, ParseOptions{Backend: b}))); got != want {
					t.Errorf("%s mode %v: input %q\n original %s\n loaded   %s", mode, b, in, want, got)
				}
			}
		}
	}
}

// TestCompiledStreamAndDocument checks that stream parsing works with a program loaded in parse
// mode and incremental parsing with one loaded in document mode.
func TestCompiledStreamAndDocument(t *testing.T) {
	t.Parallel()
	data, _ := mustCompile(t, codegentest.Records).MarshalBinary("")
	prog, start, err := LoadProgram(data)
	if err != nil || start != "" {
		t.Fatalf("%v %q", err, start)
	}
	var got []string
	err = prog.ParseStream("main", strings.NewReader("#records\na=1\nb=2\n"), func(n *value.Node) error {
		got = append(got, nodes.D(n.Field("k").(*value.Node)).Text)
		return nil
	})
	if err != nil || strings.Join(got, ",") != "a,b" {
		t.Errorf("got %v, %v", got, err)
	}

	data, _ = mustCompile(t, codegentest.IncrementalGrammar).MarshalBinary("main")
	prog, _, _ = LoadProgram(data)
	if doc, err := prog.NewDocument("main", "x = 1+2\n"); doc != nil {
		t.Errorf("document on a parse-mode program: %v", err)
	} else {
		wantModeRefused(t, err, CompiledParse, CompiledDocument)
	}
	data, _ = mustCompile(t, codegentest.IncrementalGrammar).Marshal("main", CompiledDocument)
	prog, _, _ = LoadProgram(data)
	fresh := mustCompile(t, codegentest.IncrementalGrammar)
	doc, err := prog.NewDocument("main", "x = 1+2\nab,cd\n")
	if err != nil {
		t.Fatal(err)
	}
	doc.Parse()
	doc.Edit(4, 5, "(7)")
	n, err := doc.Parse()
	fn, ferr := fresh.Parse("main", doc.Text())
	if codegentest.ResultJSON(n, err) != codegentest.ResultJSON(fn, ferr) || doc.Stats().Reused == 0 {
		t.Errorf("document on loaded program: %v %v %+v", n, err, doc.Stats())
	}
}

func TestCompiledErrors(t *testing.T) {
	t.Parallel()
	data, err := mustCompile(t, codegentest.Arith).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	resum := func(b []byte) []byte {
		b = append([]byte(nil), b[:len(b)-4]...)
		return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	}
	version := append([]byte(nil), data...)
	version[len(compiledMagic)] = 99
	isa := append([]byte(nil), data...)
	isa[isaOffset] = bytecode.ISAVersion + 1
	isa = resum(isa)
	flipped := append([]byte(nil), data...)
	flipped[20] ^= 0xff
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{"not compiled", []byte("rule main = x"), "not a compiled SyntaxIR grammar"},
		{"too short", []byte(compiledMagic), "too short"},
		{"version", version, "unsupported compiled grammar version 99"},
		{"isa version", isa, fmt.Sprintf("unsupported instruction set version %d", bytecode.ISAVersion+1)},
		{"checksum", flipped, "checksum mismatch"},
		{"truncated", resum(data[:len(data)-10]), "invalid compiled grammar"},
		{"trailing", resum(append(append([]byte(nil), data[:len(data)-4]...), 0, 0, 0, 0, 0)), "trailing bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := LoadProgram(tc.data); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := mustCompile(t, codegentest.Arith).MarshalBinary("nope"); err == nil {
		t.Error("expected an error for an undefined start rule")
	}
}

// TestCompiledEarlierInstructionSet checks that a file of an earlier instruction set, which built
// the previous syntax trees, does not load and asks for the grammar to be compiled again, that a
// file of a later version does not load either, and that one of the current version does.
func TestCompiledEarlierInstructionSet(t *testing.T) {
	t.Parallel()
	src := `rule main = n:[0-9]+ #atomic => $n`
	data, err := mustCompile(t, src).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	resum := func(b []byte) []byte {
		b = append([]byte(nil), b[:len(b)-4]...)
		return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	}
	for _, isa := range []byte{1, 8, 12, bytecode.ISAVersion, bytecode.ISAVersion + 1} {
		d := append([]byte(nil), data...)
		d[isaOffset] = isa
		prog, _, err := LoadProgram(resum(d))
		switch {
		case isa < bytecode.ISAVersion:
			if err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
				t.Errorf("isa %d: got %v", isa, err)
			}
			continue
		case isa > bytecode.ISAVersion:
			if err == nil || !strings.Contains(err.Error(), "unsupported instruction set version") {
				t.Errorf("isa %d: got %v", isa, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("isa %d: %v", isa, err)
		}
		if got := resultWith(prog, "12", ParseOptions{Backend: Bytecode}); got != `"12"` {
			t.Errorf("isa %d: got %s", isa, got)
		}
	}
}

// TestCompiledCorruptionDoesNotPanic checks that loading corrupted data (with a valid checksum)
// does not panic, that what loads saves to the bytes it was loaded from (the format is canonical),
// and that a parse of what loads ends. It corrupts with many random sources: a Flow
// body with a cycle that charged no step, which ran forever, came from the seventh source and from
// none of the corruptions of the first.
func TestCompiledCorruptionDoesNotPanic(t *testing.T) {
	t.Parallel()
	var seeds [][]byte
	for _, c := range codegentest.GenCorpus(t) {
		prog := mustCompile(t, c.Src)
		for _, mode := range allModes {
			data, err := prog.Marshal("main", mode)
			if err != nil {
				t.Fatal(err)
			}
			seeds = append(seeds, data)
		}
	}
	// The grammars with #missing add their insertion modules, which the parses below run.
	for _, c := range codegentest.InsertionCorpus() {
		prog := mustCompile(t, c.Src)
		for _, mode := range allModes {
			data, err := prog.Marshal("main", mode)
			if err != nil {
				t.Fatal(err)
			}
			seeds = append(seeds, data)
		}
	}
	var srcs []string
	for _, c := range codegentest.GenCorpus(t) {
		srcs = append(srcs, c.Src)
	}
	seeds = append(seeds, flowSeeds(t, srcs)...)
	for source := int64(1); source <= corruptionSources; source++ {
		corruptSeeds(t, seeds, rand.New(rand.NewSource(source)))
	}
	corruptActionJumps(t)
	corruptScans(t, srcs)
}

// corruptScans checks that a module does not load when a repetition of a class in one instruction
// (SCANCLASS, SCANCLASSCOUNT or SCANCLASSAT) of a Flow body of the grammars srcs is corrupted: made a
// JUMP to itself, a cycle that no step bounds and that moves past no character, or given a class, a
// count register or a scan site out of range. The instruction moves past a character on every pass
// but the last, so it ends by itself; what the loader must keep out is what is no longer such an
// instruction. Each operation must occur; the modules as compiled load.
func corruptScans(t *testing.T, srcs []string) {
	t.Helper()
	seen := map[bytecode.FlowOp]bool{}
	for _, src := range srcs {
		_, prog, _ := flowProgramsOf(t, src, func(string) bool { return true }, nil)
		load := func() error {
			_, _, err := LoadProgram(marshalWithTrailing(prog, "main", nil))
			return err
		}
		if err := load(); err != nil {
			t.Fatal(err)
		}
		m := prog.Module()
		for _, f := range m.Flows {
			for pc := range f.Code {
				in := f.Code[pc]
				if in.Op != bytecode.FlowScanClass && in.Op != bytecode.FlowScanClassCount && in.Op != bytecode.FlowScanClassAt {
					continue
				}
				seen[in.Op] = true
				reject := func(what, want string, bad bytecode.FlowInstr) {
					f.Code[pc] = bad
					err := load()
					f.Code[pc] = in
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("%v at %d %s: got %v, want an error with %q", in.Op, pc, what, err, want)
					}
				}
				reject("made a JUMP to itself", "no step bounds", bytecode.FlowInstr{Op: bytecode.FlowJump, Target: pc})
				bad := in
				bad.Dst = append([]int(nil), in.Dst...)
				bad.Dst[len(bad.Dst)-1] = f.Regs
				reject("given a register past the frame", "register", bad)
				bad = in
				if x, ok := in.Imm.(bytecode.FlowScanClassOf); ok {
					x.Test.Class = len(m.Classes)
					bad.Imm = x
					reject("given a class out of range", "out of range", bad)
					x = in.Imm.(bytecode.FlowScanClassOf)
					x.Site = 0
					bad.Imm = x
					reject("given scan site 0", "invalid scan site", bad)
				} else {
					x := in.Imm.(bytecode.FlowClassTestOf)
					x.Class = len(m.Classes)
					bad.Imm = x
					reject("given a class out of range", "out of range", bad)
				}
			}
		}
	}
	for _, op := range []bytecode.FlowOp{bytecode.FlowScanClass, bytecode.FlowScanClassCount, bytecode.FlowScanClassAt} {
		if !seen[op] {
			t.Errorf("no Flow body of the grammars has %v", op)
		}
	}
}

// corruptActionJumps checks that a module does not load when an instruction of its action code is
// made a jump back to the instruction before it. Such a corruption of the action of main of the
// integer boundary grammar, its BINARY at 15 made a JUMP to 14, came from the thirteenth random
// source; it loaded, and the evaluation of the action ran forever, since action code charges no
// step. Each instruction of each action, but the first, is corrupted so in turn; the module as
// compiled loads.
func corruptActionJumps(t *testing.T) {
	t.Helper()
	prog := mustCompile(t, codegentest.IntegerBoundaryGrammar)
	m := prog.Module()
	if _, _, err := LoadProgram(marshalWithTrailing(prog, "main", nil)); err != nil {
		t.Fatal(err)
	}
	if len(m.Actions) == 0 {
		t.Fatal("the integer boundary grammar has no action code")
	}
	for i := range m.Actions {
		code := m.Actions[i].Code
		for pc := 1; pc < len(code); pc++ {
			in := code[pc]
			code[pc] = bytecode.ActionInstr{Op: uint8(state.TJump), Dst: int32(pc - 1)}
			_, _, err := LoadProgram(marshalWithTrailing(prog, "main", nil))
			code[pc] = in
			want := "back to"
			if pc == len(code)-1 {
				want = "does not end with a return"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("action %d: instruction %d made a JUMP to %d: got %v, want a rejection", i, pc, pc-1, err)
			}
		}
	}
}

// corruptionSources is the number of random sources that TestCompiledCorruptionDoesNotPanic
// corrupts the compiled grammars with, 3000 corruptions each.
const corruptionSources = 64

// corruptSeeds loads 3000 corruptions of the compiled grammars seeds, drawn from rng, saves each one
// that loads to check that it gives back its bytes, and parses with it when the build has budget
// controls.
func corruptSeeds(t *testing.T, seeds [][]byte, rng *rand.Rand) {
	t.Helper()
	for i := 0; i < 3000; i++ {
		seed := seeds[rng.Intn(len(seeds))]
		b := append([]byte(nil), seed[:len(seed)-4]...)
		for k := rng.Intn(4) + 1; k > 0; k-- {
			p := len(compiledMagic) + 1 + rng.Intn(len(b)-len(compiledMagic)-1)
			b[p] = byte(rng.Intn(256))
		}
		if rng.Intn(4) == 0 {
			b = b[:len(compiledMagic)+1+rng.Intn(len(b)-len(compiledMagic)-1)]
		}
		b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
		func() {
			defer func() {
				if x := recover(); x != nil {
					t.Fatalf("panic on corrupted data: %v\n%s", x, debug.Stack())
				}
			}()
			prog, start, err := LoadProgram(b)
			// The format is canonical: what loads saves to the bytes it was loaded from.
			if err == nil {
				if again, err := prog.Marshal(start, CompiledMode(b[len(compiledMagic)+1])); err != nil || !bytes.Equal(again, b) {
					t.Fatalf("a corrupted grammar that loads saves to other bytes (%v)", err)
				}
			}
			// Non-terminating recursion, for example from corrupted left-recursion marks, fails at the
			// nesting depth limit, and a loop of a Flow body at its step, which requires progress.
			// Action code has no such bound and may loop until the default compute limit; a build
			// without budget controls has none, so there a corrupted module only loads.
			if err == nil && start != "" && state.BudgetControlsEnabled {
				for _, b := range []Backend{Default, Bytecode, BytecodeIterative} {
					for _, mode := range allModes {
						prog.ParseWith(start, "1+2", servedBy(mode, ParseOptions{Backend: b, MissingTokens: 2, Limits: runtime.Limits{Depth: 1000}}))
					}
				}
			}
		}()
	}
}

// TestCompiledUnmarkedCycleRejected checks that a module where a rule on a cycle of calls is not
// marked Rec does not load: only the calls of marked rules count against the nesting limit, so the
// cycle would recurse past the host stack. Giving a rule the Flow body of main with every call made
// a call of itself makes a direct cycle, giving two rules such bodies that call each other a mutual
// one; with the marks set, the same modules load and the parse fails at the nesting limit.
func TestCompiledUnmarkedCycleRejected(t *testing.T) {
	t.Parallel()
	const src = "rule main = a \"+\" b\nrule a = [0-9]+ \"a\"\nrule b = [0-9]+ \"b\"\n"
	// calls returns the rules that main calls and that are not marked Rec, in increasing order, and
	// the index of main.
	calls := func(m *bytecode.Module) ([]int, int) {
		main := -1
		for i, r := range m.Rules {
			if m.Strings[r.Name] == "main" {
				main = i
			}
		}
		at := map[int]bool{}
		for _, in := range m.Flows[main].Code {
			if c, ok := in.Imm.(bytecode.FlowCallOf); ok && !m.Rules[c.Rule].Rec {
				at[c.Rule] = true
			}
		}
		return slices.Sorted(maps.Keys(at)), main
	}
	// calling returns a copy of the Flow body f whose every call calls the rule to.
	calling := func(f *bytecode.Flow, to int) *bytecode.Flow {
		g := &bytecode.Flow{Regs: f.Regs, Code: slices.Clone(f.Code)}
		for i, in := range g.Code {
			if c, ok := in.Imm.(bytecode.FlowCallOf); ok {
				g.Code[i].Imm = bytecode.FlowCallOf{Rule: to, Level: c.Level}
			}
		}
		return g
	}
	for _, tc := range []struct {
		name  string
		cycle func(m *bytecode.Module, rs []int, main int) []int // patches m, returns the rules of the cycle
	}{
		{"direct", func(m *bytecode.Module, rs []int, main int) []int {
			if len(rs) == 0 {
				return nil
			}
			m.Flows[rs[0]] = calling(m.Flows[main], rs[0])
			return rs[:1]
		}},
		{"mutual", func(m *bytecode.Module, rs []int, main int) []int {
			if len(rs) < 2 {
				return nil
			}
			a, b := rs[0], rs[1]
			m.Flows[a], m.Flows[b] = calling(m.Flows[main], b), calling(m.Flows[main], a)
			return []int{a, b}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mark := range []bool{false, true} {
				prog := mustCompile(t, src)
				m := prog.Module()
				called, main := calls(m)
				rs := tc.cycle(m, called, main)
				if rs == nil {
					t.Fatalf("no calls to patch in\n%s", src)
				}
				for _, r := range rs {
					m.Rules[r].Rec = mark
				}
				loaded, start, err := LoadProgram(marshalWithTrailing(prog, "main", nil))
				if !mark {
					if err == nil || !strings.Contains(err.Error(), "is on a cycle of calls but is not marked recursive") {
						t.Fatalf("unmarked cycle: got %v, want a rejection", err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("marked cycle: %v", err)
				}
				for _, be := range []Backend{Default, Bytecode, BytecodeIterative} {
					if _, err := loaded.ParseWith(start, "1+2", servedBy(CompiledParse, ParseOptions{Backend: be, Limits: runtime.Limits{Depth: 1000}})); err == nil {
						t.Errorf("%v: marked cycle parsed", be)
					}
				}
			}
		})
	}
}

// TestCompiledStepFreeCycleRejected checks that a module whose Flow body has a cycle that no step
// bounds does not load. A corrupted module whose first conditional jump of main went back to the start
// of the body, before its first STEP, made a parse run forever: no limit stops a cycle that neither
// charges nor reaches a call. The same module loads and parses with the jump as compiled.
func TestCompiledStepFreeCycleRejected(t *testing.T) {
	t.Parallel()
	for _, corrupt := range []bool{false, true} {
		prog := mustCompile(t, "rule main = item* $$\nrule item = [a-z]+ \",\"?\n")
		m := prog.Module()
		main := slices.IndexFunc(m.Rules, func(r bytecode.RuleInfo) bool { return m.Strings[r.Name] == "main" })
		f := m.Flows[main]
		if f == nil {
			t.Fatal("main has no Flow body")
		}
		jump := slices.IndexFunc(f.Code, func(in bytecode.FlowInstr) bool { return in.Op == bytecode.FlowJumpFalse })
		step := slices.IndexFunc(f.Code, func(in bytecode.FlowInstr) bool { return in.Op == bytecode.FlowStep })
		if jump < 0 || step >= 0 && step < jump {
			t.Fatalf("main has no JUMPFALSE before its first STEP\n%s", m.Disassemble())
		}
		if corrupt {
			f.Code[jump].Target = 0
		}
		loaded, start, err := LoadProgram(marshalWithTrailing(prog, "main", nil))
		if corrupt {
			if err == nil || !strings.Contains(err.Error(), "is on a cycle that no step bounds") {
				t.Fatalf("JUMPFALSE at %d back to 0: got %v, want a rejection", jump, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := resultWith(loaded, "ab,c", ParseOptions{Backend: Bytecode}); strings.HasPrefix(got, "error") {
			t.Errorf("%s: %s", start, got)
		}
	}
}

// TestCompiledStepWithoutProgressFails checks that a loop of a corrupted module whose step finds the
// position where its last step left it ends the parse with an error of invalid bytecode, under the
// default compute limit and under none: a step counts no compute unit, so only its requirement of
// progress stops the loop. The class test of the loop of item jumps back to the step when it fails
// instead of leaving the loop; the cycle passes through the step, so the module loads.
func TestCompiledStepWithoutProgressFails(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, "rule main = item* $$\nrule item = [a-z]+ \",\"?\n")
	m := prog.Module()
	item := slices.IndexFunc(m.Rules, func(r bytecode.RuleInfo) bool { return m.Strings[r.Name] == "item" })
	f := m.Flows[item]
	if f == nil {
		t.Fatal("item has no Flow body")
	}
	step := slices.IndexFunc(f.Code, func(in bytecode.FlowInstr) bool { return in.Op == bytecode.FlowStep })
	exit := -1
	for pc := step + 1; step >= 0 && pc < len(f.Code) && exit < 0; pc++ {
		if s, _ := f.Code[pc].Op.Shape(); s.Target && f.Code[pc].Op != bytecode.FlowJump && f.Code[pc].Target > pc {
			exit = pc
		}
	}
	if exit < 0 {
		t.Fatalf("item has no conditional jump out of its loop after a STEP\n%s", m.Disassemble())
	}
	f.Code[exit].Target = step
	loaded, _, err := LoadProgram(marshalWithTrailing(prog, "main", nil))
	if err != nil {
		t.Fatalf("%v at %d back to the STEP at %d: %v", f.Code[exit].Op, exit, step, err)
	}
	for _, b := range []Backend{Bytecode, BytecodeIterative} {
		for _, lim := range []runtime.Limits{{}, {Compute: runtime.Unlimited}} {
			got := resultWith(loaded, "ab,c", ParseOptions{Backend: b, Limits: lim})
			if !strings.HasPrefix(got, "error: invalid bytecode: STEP at ") {
				t.Errorf("backend %v, limits %+v: got %s, want the error of a step without progress", b, lim, got)
			}
		}
	}
}

// flowSeeds returns compiled grammars whose rules run Flow bodies, so that corrupting them corrupts
// the encoding of the bodies too: the grammars srcs with every rule that has a Flow body running it,
// saved in each compile mode, for those that have one.
func flowSeeds(t testing.TB, srcs []string) [][]byte {
	t.Helper()
	var seeds [][]byte
	for _, src := range srcs {
		_, prog, _ := flowProgramsOf(t, src, func(string) bool { return true }, nil)
		if len(prog.Module().Flows) == 0 {
			continue
		}
		for _, mode := range allModes {
			data, err := prog.Marshal("main", mode)
			if err != nil {
				t.Fatal(err)
			}
			seeds = append(seeds, data)
		}
	}
	if len(seeds) == 0 {
		t.Fatal("no grammar has a Flow body")
	}
	return seeds
}

func FuzzLoadProgram(f *testing.F) {
	for _, src := range []string{codegentest.Arith, codegentest.AST, codegentest.IncrementalGrammar, codegentest.Records} {
		g := mustParse(src)
		prog, err := Compile(g, checkpkg.Options{})
		if err != nil {
			f.Fatal(err)
		}
		for _, mode := range allModes {
			data, _ := prog.Marshal("main", mode)
			f.Add(data)
		}
	}
	for _, data := range flowSeeds(f, []string{codegentest.Arith, codegentest.AST, codegentest.IncrementalGrammar, codegentest.Records}) {
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// The checksum is made right for the mutated bytes: otherwise almost every mutation fails
		// the checksum and never reaches the decoder, the validation or the canonical round trip.
		if len(data) >= 4 {
			data = withChecksum(data[:len(data)-4])
		}
		prog, start, err := LoadProgram(data)
		if err == nil {
			// The format is canonical: what loads saves to the bytes it was loaded from.
			if again, err := prog.Marshal(start, CompiledMode(data[len(compiledMagic)+1])); err != nil || !bytes.Equal(again, data) {
				t.Fatalf("a grammar that loads saves to other bytes (%v)", err)
			}
		}
		if err == nil && start != "" {
			for _, mode := range allModes {
				prog.ParseWith(start, "a=1\n", servedBy(mode, ParseOptions{Limits: runtime.Limits{Depth: 1000}}))
				prog.ParseWith(start, "a=1\n", servedBy(mode, ParseOptions{Backend: Bytecode, Limits: runtime.Limits{Depth: 1000}}))
			}
		}
	})
}

// BenchmarkLoad compares compiling from source with loading a compiled grammar.
func BenchmarkLoad(b *testing.B) {
	src, err := os.ReadFile("../../../examples/minilang/minilang.sxir")
	if err != nil {
		b.Fatal(err)
	}
	prog, _ := Compile(mustParse(string(src)), checkpkg.Options{})
	data, _ := prog.MarshalBinary("main")
	b.Run("source", func(b *testing.B) {
		for b.Loop() {
			g, _ := parseTest(string(src))
			Compile(g, checkpkg.Options{})
		}
	})
	b.Run("compiled", func(b *testing.B) {
		for b.Loop() {
			LoadProgram(data)
		}
	})
}

// TestCompiledTypeExpressions checks that a grammar with every form of type expression runs the
// same from a compiled grammar, which holds no types: they stay with the grammar source.
func TestCompiledTypeExpressions(t *testing.T) {
	t.Parallel()
	src := `type P = { A node[string]; B *(int); C []node[string] }
type Q ~ P | node[string] & node[_]
rule main P = a:[a-z] => P{A: $a, B: 1}
rule pair node[(string)] = [a-z]`
	prog := mustCompile(t, src)
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := loaded.MarshalBinary("main"); err != nil || !bytes.Equal(data, again) {
		t.Errorf("re-marshaled data differs (%v)", err)
	}
	if loaded.Static() != nil || bytes.Contains(data, []byte("node[")) {
		t.Errorf("compiled grammar keeps the types: checked program %v", loaded.Static() != nil)
	}
	if grammarTypes(prog) == "" {
		t.Error("source grammar has no types")
	}
	for _, rule := range []string{"main", "pair"} {
		for _, in := range []string{"a", "ab", ""} {
			n1, err1 := prog.Parse(rule, in)
			n2, err2 := loaded.Parse(rule, in)
			if a, b := codegentest.ResultJSON(n1, err1), codegentest.ResultJSON(n2, err2); a != b {
				t.Errorf("%s %q\n original %s\n loaded   %s", rule, in, a, b)
			}
		}
	}
}

// grammarTypes renders the type definitions and rule types of a program's grammar, ignoring
// layout, which compiled grammars do not keep.
func grammarTypes(prog *Program) string {
	var b strings.Builder
	for _, s := range prog.Static().Grammar.File.Decls {
		switch s := s.(type) {
		case ast.TypeDecl:
			fmt.Fprintf(&b, "type %s", s.DeclName())
			switch spec := s.(type) {
			case *ast.TypeDef:
				for _, f := range spec.Struct().Fields {
					fmt.Fprintf(&b, " %s %s;", f.Name, syntaxfmt.FormatType(f.Type))
				}
			case *ast.TypeAlias:
				b.WriteString(" ~ " + syntaxfmt.FormatType(spec.Type))
			}
			b.WriteString("\n")
		case *ast.RuleDef:
			if s.Type != nil {
				fmt.Fprintf(&b, "rule %s %s\n", s.Name, syntaxfmt.FormatType(s.Type))
			}
		}
	}
	return b.String()
}

// TestCompiledFormatVersion checks that only the current container version is read: earlier and
// later versions are refused with the version in the message.
func TestCompiledFormatVersion(t *testing.T) {
	t.Parallel()
	data, err := mustCompile(t, `rule main = "a"`).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	resum := func(b []byte) []byte {
		b = append([]byte(nil), b[:len(b)-4]...)
		return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	}
	for _, v := range []byte{1, 2, 3, compiledVersion - 1, compiledVersion + 1} {
		d := append([]byte(nil), data...)
		d[len(compiledMagic)] = v
		_, _, err := LoadProgram(resum(d))
		want := fmt.Sprintf("unsupported compiled grammar version %d (want %d): compile the grammar again", v, compiledVersion)
		if err == nil || err.Error() != want {
			t.Errorf("version %d: got %v, want %q", v, err, want)
		}
	}
}
