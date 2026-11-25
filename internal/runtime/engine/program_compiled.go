package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"slices"

	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/vm"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// The compiled grammar format (.sxirc), specified in the File format section of
// spec/bytecode/README.md. A file holds one execution-only compile result: the module of one mode,
// and for a grammar with #missing the module of that mode that the parses inserting missing tokens
// run (insertionProgram).
//
//	magic    "SXC\x00"
//	version  1 byte (compiledVersion)
//	mode     1 byte (CompiledMode)
//	payload  instruction set version, string table, start rule, module, insertion part
//	crc32    CRC-32 of magic through payload (IEEE, 4 bytes little-endian)
//
// The insertion part is a boolean, then, if it is true, a string table of its own and the
// insertion module. Integers are variable-length (signed ones zigzag-encoded), and strings are
// referenced by their index in the string table that precedes them, which begins with the string
// table of the module that follows it. Loading skips parsing, static analysis, type checking, and
// compilation to bytecode.

const (
	compiledMagic   = bytecode.FileMagic
	compiledVersion = 9
)

// CompiledMode is the mode of a compiled grammar file: the one kind of parse its module serves.
type CompiledMode uint8

const (
	// CompiledParse serves parses that build values without tracing or profiling: Parse,
	// ParsePrefix, ParseStream and Iterate.
	CompiledParse CompiledMode = iota
	// CompiledRecognize serves recognition (RecognizeOnly) without tracing or profiling.
	CompiledRecognize
	// CompiledDocument serves Documents and traced or profiled Parse, ParsePrefix, ParseStream
	// and Iterate.
	CompiledDocument
)

var (
	compiledModeNames = [...]string{CompiledParse: "parse", CompiledRecognize: "recognize", CompiledDocument: "document"}
	// compiledModeAPI names the modes in the Go API, for error messages.
	compiledModeAPI = [...]string{CompiledParse: "engine.CompiledParse", CompiledRecognize: "engine.CompiledRecognize", CompiledDocument: "engine.CompiledDocument"}
)

// String returns the name of the mode: parse, recognize or document.
func (m CompiledMode) String() string {
	if int(m) < len(compiledModeNames) {
		return compiledModeNames[m]
	}
	return fmt.Sprintf("CompiledMode(%d)", uint8(m))
}

// ParseCompiledMode returns the mode named s: "parse", "recognize" or "document".
func ParseCompiledMode(s string) (CompiledMode, error) {
	for m, name := range compiledModeNames {
		if name == s {
			return CompiledMode(m), nil
		}
	}
	return 0, fmt.Errorf("unknown compiled grammar mode %q (want parse, recognize or document)", s)
}

// unsupportedISA is the error for a module of instruction set isa.
func unsupportedISA(isa int) error {
	if isa < bytecode.ISAVersion {
		return fmt.Errorf("unsupported instruction set version %d: the file was compiled by an older sxir; compile the grammar again", isa)
	}
	return fmt.Errorf("unsupported instruction set version %d", isa)
}

// errClosureNeedsSource is the error of the closure backend on a program loaded from a compiled
// grammar.
var errClosureNeedsSource = errors.New("the closure backend needs the grammar source; a compiled grammar runs on the bytecode backends")

// ErrNotCompiled reports that the data is not a compiled grammar.
var ErrNotCompiled = errors.New("not a compiled SyntaxIR grammar")

// IsCompiled reports whether the data is in the compiled grammar format.
func IsCompiled(data []byte) bool { return bytecode.IsCompiled(data) }

// Mode returns the mode of a program loaded from a compiled grammar, and false for a program
// compiled from a grammar.
func (prog *Program) Mode() (CompiledMode, bool) { return prog.mode, prog.loaded }

// checkMode reports an error when prog is loaded from a compiled grammar whose mode does not serve
// a parse with options o; document selects a Document. A compiled grammar runs exactly the parses
// of its mode, without falling back to another.
func (prog *Program) checkMode(o ParseOptions, document bool) error {
	if !prog.loaded {
		return nil
	}
	var need CompiledMode
	var what string
	switch {
	case document:
		need, what = CompiledDocument, "documents need"
	case o.Recognize && o.Traced():
		return fmt.Errorf("the compiled grammar is in %s mode; traced or profiled recognition needs the grammar source: compile the grammar from source", prog.mode)
	case o.Recognize:
		need, what = CompiledRecognize, "recognition needs"
	case o.Traced():
		need, what = CompiledDocument, "tracing and profiling need"
	default:
		need, what = CompiledParse, "parsing needs"
	}
	if need == prog.mode {
		return nil
	}
	return modeError(prog.mode, need, what)
}

// modeError is the error of an operation that needs mode need, on a compiled grammar in mode have;
// what names the operation with its verb, such as "documents need".
func modeError(have, need CompiledMode, what string) error {
	return fmt.Errorf("the compiled grammar is in %s mode; %s %s mode: compile it with -mode %s (%s), or compile the grammar from source", have, what, need, need, compiledModeAPI[need])
}

// MarshalBinary encodes the program in parse mode; see Marshal.
func (prog *Program) MarshalBinary(start string) ([]byte, error) {
	return prog.Marshal(start, CompiledParse)
}

// Marshal encodes the module of mode as a compiled grammar. start is the start rule a loaded parser
// uses: a start rule of the grammar, or empty for its default start rule. A program loaded from a
// compiled grammar is saved only in its own mode, and then reproduces the bytes it was loaded from.
func (prog *Program) Marshal(start string, mode CompiledMode) ([]byte, error) {
	if int(mode) >= len(compiledModeNames) {
		return nil, fmt.Errorf("unknown compiled grammar mode %d", mode)
	}
	if start != "" {
		if err := prog.CheckStart(start); err != nil {
			return nil, err
		}
	}
	if prog.loaded && mode != prog.mode {
		return nil, fmt.Errorf("the compiled grammar is in %s mode and is saved only in that mode; compile the grammar from source to save it in %s mode", prog.mode, mode)
	}
	m, err := prog.modeModule(mode)
	if err != nil {
		return nil, err
	}
	var ins *bytecode.Module
	if prog.HasMissing {
		p, err := prog.insertionProgram()
		if err != nil {
			return nil, err
		}
		if ins, err = p.modeModule(mode); err != nil {
			return nil, err
		}
	}
	return encodeCompiled(start, mode, m, ins), nil
}

// encodeCompiled returns the compiled grammar of mode with the start rule start, the module m and
// the insertion module ins, or none if ins is nil.
func encodeCompiled(start string, mode CompiledMode, m, ins *bytecode.Module) []byte {
	w := moduleEncoder(m)
	w.Str(start)
	w.WriteModule(m)
	w.Bool(ins != nil)
	if ins != nil {
		iw := moduleEncoder(ins)
		iw.WriteModule(ins)
		w.Part(iw)
	}
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(compiledVersion)
	out.WriteByte(byte(mode))
	out.Write(binary.AppendUvarint(nil, uint64(m.InstructionSetVersion())))
	w.Finish(&out)
	return out.Bytes()
}

// modeModule returns the module of prog that serves the parses of mode: its own for a loaded
// program, which is in that mode (Marshal), and its recognizer's for recognize mode.
func (prog *Program) modeModule(mode CompiledMode) (*bytecode.Module, error) {
	if prog.loaded || mode != CompiledRecognize {
		return prog.Module(), nil
	}
	rec, err := prog.Recognizer()
	if err != nil {
		return nil, err
	}
	return rec.Module(), nil
}

// moduleEncoder returns an encoder whose string table begins with the string table of m, as a
// module's strings must.
func moduleEncoder(m *bytecode.Module) *bytecode.Encoder {
	w := &bytecode.Encoder{Strs: map[string]int{}}
	for i, s := range m.Strings {
		w.Strs[s] = i
		w.List = append(w.List, s)
	}
	return w
}

// LoadProgram loads a compiled grammar. It also returns the saved start rule (empty for the
// default start rule). It does not perform parsing, static analysis, type checking, or compilation
// to bytecode. The program runs on the bytecode backends and serves the parses of the file's mode
// only (checkMode).
func LoadProgram(data []byte) (*Program, string, error) {
	if !IsCompiled(data) {
		return nil, "", ErrNotCompiled
	}
	if len(data) < len(compiledMagic)+2+4 {
		return nil, "", errors.New("invalid compiled grammar: too short")
	}
	if v := data[len(compiledMagic)]; v != compiledVersion {
		return nil, "", fmt.Errorf("unsupported compiled grammar version %d (want %d): compile the grammar again", v, compiledVersion)
	}
	body, sum := data[:len(data)-4], binary.LittleEndian.Uint32(data[len(data)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return nil, "", errors.New("invalid compiled grammar: checksum mismatch")
	}
	mode := CompiledMode(data[len(compiledMagic)+1])
	if int(mode) >= len(compiledModeNames) {
		return nil, "", fmt.Errorf("invalid compiled grammar: unknown mode %d", mode)
	}
	r := &bytecode.Decoder{Data: body, Pos: len(compiledMagic) + 2}
	isa := r.Uint()
	if r.Err == nil && isa != bytecode.ISAVersion {
		return nil, "", unsupportedISA(isa)
	}
	r.Strings()
	start := r.Str()
	m := r.ReadModule()
	// The string table of the file is the module's, followed by the start rule if the module does
	// not hold it (encodeCompiled), and that of the insertion part is the insertion module's.
	if n := len(m.Strings); r.Err == nil && len(r.Strs) != n && (len(r.Strs) != n+1 || r.Strs[n] != start) {
		r.Fail("the string table holds strings that the file does not refer to")
	}
	var ins *bytecode.Module
	if r.Bool() {
		r.Part(func(p *bytecode.Decoder) {
			if ins = p.ReadModule(); p.Err == nil && len(p.Strs) != len(ins.Strings) {
				p.Fail("the string table of the insertion part holds strings that it does not refer to")
			}
		})
	}
	if r.Err == nil && r.Pos != len(r.Data) {
		r.Fail("%d trailing bytes", len(r.Data)-r.Pos)
	}
	if r.Err != nil {
		return nil, "", r.Err
	}
	prog, err := loadModule(m, mode)
	if err != nil {
		return nil, "", err
	}
	// The file holds an insertion module exactly when a rule of the grammar uses #missing
	// (Program.HasMissing), as Marshal writes it.
	if inserts := moduleInserts(m); inserts && ins == nil {
		return nil, "", errors.New("invalid compiled grammar: a grammar with #missing has no insertion module")
	} else if !inserts && ins != nil {
		return nil, "", errors.New("invalid compiled grammar: a grammar without #missing has an insertion module")
	}
	if ins != nil {
		p, err := loadModule(ins, mode)
		if err != nil {
			return nil, "", err
		}
		if moduleInserts(ins) != moduleInserts(m) || !sameStarts(m, ins) {
			return nil, "", errors.New("invalid compiled grammar: the insertion module does not match the module of the grammar")
		}
		p.insertion, p.HasMissing = true, true
		prog.HasMissing = true
		prog.insOnce.Do(func() { prog.ins = p })
	}
	if start != "" {
		if err := prog.CheckStart(start); err != nil {
			return nil, "", fmt.Errorf("invalid compiled grammar: %w", err)
		}
	}
	return prog, start, nil
}

// loadModule returns the program of the module m, of a compiled grammar in mode, after validating
// it.
func loadModule(m *bytecode.Module, mode CompiledMode) (*Program, error) {
	if err := program.ValidateModule(m); err != nil {
		return nil, fmt.Errorf("invalid compiled grammar: %w", err)
	}
	actionExprs, err := vm.LoadActions(m)
	if err != nil {
		return nil, fmt.Errorf("invalid compiled grammar: %w", err)
	}
	prog := moduleProgram(m)
	prog.vmActionsOnce.Do(func() { prog.vmActions = actionExprs })
	prog.loaded, prog.mode = true, mode
	return prog, nil
}

// moduleInserts reports whether a rule of the validated module m contains #missing.
func moduleInserts(m *bytecode.Module) bool {
	for _, r := range m.Rules {
		if r.Missing {
			return true
		}
	}
	return false
}

// sameStarts reports whether the validated modules a and b have the same start rules and default
// start rule.
func sameStarts(a, b *bytecode.Module) bool {
	return slices.Equal(moduleStarts(a), moduleStarts(b)) && moduleDefaultStart(a) == moduleDefaultStart(b)
}

// moduleNames returns the strings of the string table indices xs of a validated module.
func moduleNames(m *bytecode.Module, xs []int) []string {
	if len(xs) == 0 {
		return nil
	}
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = m.Strings[x]
	}
	return out
}

// moduleStarts returns the names of the start rules of a validated module.
func moduleStarts(m *bytecode.Module) []string {
	names := make([]string, len(m.Starts))
	for i, x := range m.Starts {
		names[i] = m.Strings[m.Rules[x].Name]
	}
	return names
}

// moduleDefaultStart returns the name of the default start rule of a validated module.
func moduleDefaultStart(m *bytecode.Module) string { return m.Strings[m.Rules[m.DefaultStart].Name] }

// moduleProgram builds a program from a module alone, without the grammar; only the bytecode
// backends run it.
func moduleProgram(m *bytecode.Module) *Program {
	rt := &program.Program{
		ByName:            map[string]*program.Rule{},
		RecordsSourceInfo: m.SourceInfo,
		MakesDefaultValue: m.DefaultValue,
		Types:             map[string]*program.Type{},
		Vars:              program.ModuleVarTable(m),
		StartRules:        moduleStarts(m),
		DefaultStartRule:  moduleDefaultStart(m),
		Bytecode:          m,
	}
	zeros := map[string]*program.StructZero{}
	for _, t := range m.Types {
		name := m.Strings[t.Name]
		if t.Node >= 0 {
			// The run time needs only that the type is a node type.
			rt.Types[name] = &program.Type{Name: name}
			continue
		}
		st := &program.Type{Name: name}
		z := &program.StructZero{Zeros: map[string]string{}}
		for i, f := range t.Fields {
			field := m.Strings[f]
			st.Fields = append(st.Fields, field)
			z.Names = append(z.Names, field)
			z.Zeros[field] = m.Strings[t.Zeros[i]]
		}
		zeros[name] = z
		rt.Types[name] = st
	}
	for _, k := range m.RunKeys {
		if rt.RunKeys == nil {
			rt.RunKeys = map[int]program.RunKey{}
		}
		rt.RunKeys[k.Site] = program.RunKey{Vars: moduleNames(m, k.Vars), Bind: moduleNames(m, k.Bind)}
	}
	rt.StructZero = func(name string) *program.StructZero { return zeros[name] }
	return newProgram(rt, nil)
}
