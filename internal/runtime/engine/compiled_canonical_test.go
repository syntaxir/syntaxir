package engine

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// withChecksum returns body, a compiled grammar without its checksum, with the checksum appended.
func withChecksum(body []byte) []byte {
	return binary.LittleEndian.AppendUint32(append([]byte(nil), body...), crc32.ChecksumIEEE(body))
}

// encodeTables is encodeCompiled for parse mode, with edit and editIns applied to the string tables
// of the file and of the insertion part before they are written: what an encoder with other string
// tables would write.
func encodeTables(m, ins *bytecode.Module, start string, edit, editIns func(*bytecode.Encoder)) []byte {
	w := moduleEncoder(m)
	w.Str(start)
	w.WriteModule(m)
	w.Bool(ins != nil)
	if ins != nil {
		iw := moduleEncoder(ins)
		iw.WriteModule(ins)
		editIns(iw)
		w.Part(iw)
	}
	edit(w)
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(compiledVersion)
	out.WriteByte(byte(CompiledParse))
	out.Write(binary.AppendUvarint(nil, uint64(m.InstructionSetVersion())))
	w.Finish(&out)
	return out.Bytes()
}

// TestCompiledCanonicalRejections checks that a compiled grammar that the encoder would not write
// does not load, though it decodes to a valid program: one with an integer longer than needed, or
// with a string that no part refers to or a string twice in the string table of the file or of the
// insertion part. TestFlowBodyEncoding checks the order of the Flow bodies.
func TestCompiledCanonicalRejections(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, "@start\nrule r = \"a\"+")
	data, err := prog.MarshalBinary("")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(data); err != nil {
		t.Fatal(err)
	}
	isa := len(compiledMagic) + 2
	if data[isa] != bytecode.ISAVersion || bytecode.ISAVersion >= 0x80 {
		t.Fatalf("instruction set version byte %#x, want %#x in one byte", data[isa], bytecode.ISAVersion)
	}
	body := data[:len(data)-4]
	// padded returns body with its one-byte integer at offset i encoded in two bytes.
	padded := func(i int) []byte {
		return withChecksum(slices3(body[:i], []byte{body[i] | 0x80, 0}, body[i+1:]))
	}
	m := prog.Module()
	keep := func(*bytecode.Encoder) {}
	unused := func(w *bytecode.Encoder) { w.List = append(w.List, "unused") }
	if again := encodeTables(m, nil, "", keep, nil); !bytes.Equal(again, data) {
		t.Fatalf("encodeTables without an edit differs from byte %d", firstDiff(again, data))
	}
	// A grammar with an insertion module, for the string table of the insertion part.
	insProg := mustCompile(t, "@start\nrule r = \"a\" #missing")
	insData, err := insProg.MarshalBinary("")
	if err != nil {
		t.Fatal(err)
	}
	insProgram, err := insProg.insertionProgram()
	if err != nil {
		t.Fatal(err)
	}
	im, ins := insProg.Module(), insProgram.Module()
	if again := encodeTables(im, ins, "", keep, keep); !bytes.Equal(again, insData) {
		t.Fatalf("encodeTables of the insertion grammar without an edit differs from byte %d", firstDiff(again, insData))
	}
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"padded instruction set version", padded(isa), "not in its shortest encoding"},
		{"padded string count", padded(isa + 1), "not in its shortest encoding"},
		{"unused string", encodeTables(m, nil, "", unused, nil), "strings that the file does not refer to"},
		{"unused string past the start rule", encodeTables(m, nil, "r", unused, nil), "strings that the file does not refer to"},
		{"duplicate string", encodeTables(m, nil, "", func(w *bytecode.Encoder) { w.List = append(w.List, w.List[0]) }, nil), "twice in the string table"},
		{"unused string in the insertion part", encodeTables(im, ins, "", keep, unused), "strings that it does not refer to"},
		{"duplicate string in the insertion part", encodeTables(im, ins, "", keep, func(w *bytecode.Encoder) { w.List = append(w.List, w.List[0]) }), "twice in the string table"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if bytes.Equal(c.data, data) || bytes.Equal(c.data, insData) {
				t.Fatal("the case encodes the program as compiled")
			}
			_, _, err := LoadProgram(c.data)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("LoadProgram error %v, want one with %q", err, c.want)
			}
		})
	}
}

// firstDiff returns the offset of the first byte where a and b differ, or the length of the shorter.
func firstDiff(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// slices3 returns the concatenation of a, b and c in a new slice.
func slices3(a, b, c []byte) []byte {
	return append(append(append([]byte(nil), a...), b...), c...)
}

// compiledCorpus returns the compiled grammars of the generator corpus and of the grammars with
// insertion modules, in every mode, in a fixed order: all of them in the long tier and their
// representative cases (codegentest.Representative) in the default tier.
func compiledCorpus(t *testing.T) [][]byte {
	t.Helper()
	var srcs []string
	for _, c := range codegentest.Representative(codegentest.GenCorpus(t)) {
		srcs = append(srcs, c.Src)
	}
	for _, c := range codegentest.Representative(codegentest.InsertionCorpus()) {
		srcs = append(srcs, c.Src)
	}
	var out [][]byte
	for _, src := range srcs {
		prog := mustCompile(t, src)
		for _, mode := range allModes {
			data, err := prog.Marshal("main", mode)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, data)
		}
	}
	return out
}

// TestCompiledCanonicalRoundTrip checks over the corpus that decoding and encoding again is the
// identity: whenever a byte-level variant of a compiled grammar (a byte changed, padded, removed or
// doubled, with the checksum made right) loads, saving the loaded program gives back the variant's
// bytes.
func TestCompiledCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	loads := 0
	for _, data := range compiledCorpus(t) {
		mode := CompiledMode(data[len(compiledMagic)+1])
		body := data[:len(data)-4]
		variants := [][]byte{data}
		// Every byte past the version and mode bytes, which the loader checks first, in at most
		// 97 evenly spread positions.
		from := len(compiledMagic) + 2
		step := max(1, (len(body)-from)/97)
		for i := from; i < len(body); i += step {
			b := body[i]
			variants = append(variants,
				withChecksum(slices3(body[:i], []byte{b + 1}, body[i+1:])),
				withChecksum(slices3(body[:i], []byte{b - 1}, body[i+1:])),
				withChecksum(slices3(body[:i], nil, body[i+1:])),
				withChecksum(slices3(body[:i], []byte{b, b}, body[i+1:])))
			if b < 0x80 {
				variants = append(variants, withChecksum(slices3(body[:i], []byte{b | 0x80, 0}, body[i+1:])))
			}
		}
		for _, v := range variants {
			prog, start, err := LoadProgram(v)
			if err != nil {
				continue
			}
			loads++
			again, err := prog.Marshal(start, mode)
			if err != nil {
				t.Fatalf("saving a loaded grammar: %v", err)
			}
			if !bytes.Equal(again, v) {
				t.Fatalf("a loaded grammar saves to other bytes from byte %d", firstDiff(again, v))
			}
		}
	}
	if loads == 0 {
		t.Fatal("no variant loaded")
	}
}

// compiledDumpEnv names the file that TestCompiledDeterministic writes the compiled corpus to when
// it runs as the child process of itself.
const compiledDumpEnv = "SXIR_TEST_COMPILED_CORPUS_DUMP"

// TestCompiledDeterministic checks that compiling a grammar gives the same bytes every time: within
// a process, where every compile iterates its maps in another order, and in another process, whose
// map hashes are seeded differently.
func TestCompiledDeterministic(t *testing.T) {
	if path := os.Getenv(compiledDumpEnv); path != "" {
		var out bytes.Buffer
		for _, data := range compiledCorpus(t) {
			out.Write(binary.AppendUvarint(nil, uint64(len(data))))
			out.Write(data)
		}
		if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Parallel()
	want := compiledCorpus(t)
	for range 2 {
		for i, data := range compiledCorpus(t) {
			if !bytes.Equal(data, want[i]) {
				t.Fatalf("compiled grammar %d differs between compiles from byte %d", i, firstDiff(data, want[i]))
			}
		}
	}
	path := filepath.Join(t.TempDir(), "corpus")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCompiledDeterministic$", "-test.count=1")
	cmd.Env = append(os.Environ(), compiledDumpEnv+"="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process: %v\n%s", err, out)
	}
	dump, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		n, k := binary.Uvarint(dump)
		if k <= 0 || uint64(len(dump)-k) < n {
			t.Fatalf("child process wrote %d of %d compiled grammars", i, len(want))
		}
		if data := dump[k : k+int(n)]; !bytes.Equal(data, want[i]) {
			t.Fatalf("compiled grammar %d differs between processes from byte %d", i, firstDiff(data, want[i]))
		}
		dump = dump[k+int(n):]
	}
	if len(dump) != 0 {
		t.Fatal("child process wrote more compiled grammars")
	}
}
