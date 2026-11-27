package compiler_test

import (
	"bytes"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	igen "syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
)

func TestMain(m *testing.M) {
	// The tests build generated programs; a build cache private to the test process keeps their
	// entries out of the shared cache.
	removeGoCache := codegentest.SetGoCache()
	code := m.Run()
	removeGoCache()
	os.Exit(code)
}

// parserProtocol matches the reference of a parser to the protocol constant of the library it imports.
var parserProtocol = regexp.MustCompile(`(?m)^const _ = rt\.Protocol([1-9][0-9]*)$`)

// protocolName matches the declaration of a protocol constant of the library or of value/impl.
var protocolName = regexp.MustCompile(`\bconst Protocol([1-9][0-9]*) = ([1-9][0-9]*)\b`)

// protocolVersions returns the protocol versions that the sources declare, a constant ProtocolN for each.
func protocolVersions(srcs ...[]byte) []int {
	var out []int
	for _, src := range srcs {
		for _, m := range protocolName.FindAllSubmatch(src, -1) {
			n, _ := strconv.Atoi(string(m[1]))
			out = append(out, n)
		}
	}
	return out
}

// shiftProtocol replaces each protocol constant that src declares with that of the version by places after
// it (by > 0) or before it (by < 0). Shifting every source of a set of declarations by the span of the
// versions they declare, the highest minus the lowest plus one, gives a set that declares only versions
// past the highest of the set, or below its lowest: a set that implements none of the versions it did,
// whichever of them a parser refers to.
func shiftProtocol(src []byte, by int) []byte {
	return protocolName.ReplaceAllFunc(src, func(m []byte) []byte {
		n, _ := strconv.Atoi(string(protocolName.FindSubmatch(m)[1]))
		return []byte("const Protocol" + strconv.Itoa(n+by) + " = " + strconv.Itoa(n+by))
	})
}

// protocolSpan returns the span of the versions, the highest minus the lowest plus one.
func protocolSpan(versions []int) int {
	return slices.Max(versions) - slices.Min(versions) + 1
}

// protocolBuild writes a module syntaxir.dev/sxir in dir/sxir that holds this module's runtime
// packages, with the source of value/impl passed through valueimpl, and the files libFiles at their paths
// below lib (the directory of the library in it, or "" to leave it out), and the parser files in
// dir/p, a module that requires it; then it builds the parser.
func protocolBuild(t *testing.T, files, libFiles []compiler.File, lib string, valueimpl func([]byte) []byte) (string, error) {
	t.Helper()
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		write("p/"+f.Path, f.Content)
	}
	write("sxir/go.mod", []byte("module syntaxir.dev/sxir\n\ngo 1.27.1\n"))
	for _, pkg := range []string{"pkg/runtime", "pkg/runtime/input", "pkg/runtime/value", "pkg/runtime/value/impl", "pkg/runtime/instrument", "pkg/runtime/schema"} {
		src := filepath.Join(codegentest.ModuleRoot(), filepath.FromSlash(pkg))
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if pkg == "pkg/runtime/value/impl" && valueimpl != nil {
				data = valueimpl(data)
			}
			write("sxir/"+pkg+"/"+e.Name(), data)
		}
	}
	for _, f := range libFiles {
		write(lib+"/"+path.Base(f.Path), f.Content)
	}
	write("p/go.mod", []byte("module example.com/proto\n\ngo 1.27.1\n\nrequire syntaxir.dev/sxir v0.0.0\n\nreplace syntaxir.dev/sxir => ../sxir\n"))
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = filepath.Join(dir, "p")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", codegentest.GoFlagsEnv, "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// libVersions returns the protocol versions that the library files declare.
func libVersions(t *testing.T, libFiles []compiler.File) []int {
	t.Helper()
	var srcs [][]byte
	for _, f := range libFiles {
		srcs = append(srcs, f.Content)
	}
	versions := protocolVersions(srcs...)
	if len(versions) == 0 {
		t.Fatal("the library declares no protocol constant")
	}
	return versions
}

// TestImportProtocol checks that a parser generated with LibImport refers to the newest protocol
// version of the library that the generator carries, which declares it, and builds against that
// library; and that it fails to build, on the protocol constant it refers to, against the same library
// declaring only versions past all of those it declares, as a later library that implements none of
// them would. The library may declare older versions besides the newest, which parsers of those
// versions build against.
func TestImportProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated parser")
	}
	g := grammar.FromString("proto.sxir", `@start
rule main = [a-z]+ "=" [0-9]+`)
	files, err := compiler.GenerateGo(g, compiler.GoConfig{Module: "example.com/proto", Lib: compiler.LibImport}, compiler.WithStart("main"))
	if err != nil {
		t.Fatal(err)
	}
	libFiles, err := compiler.GenerateLibGo("example.com/proto")
	if err != nil {
		t.Fatal(err)
	}
	versions := libVersions(t, libFiles)
	var parser []byte
	for _, f := range files {
		if f.Path == "parser.go" {
			parser = f.Content
		}
	}
	m := parserProtocol.FindSubmatch(parser)
	if m == nil {
		t.Fatal("parser.go refers to no protocol constant")
	}
	own, _ := strconv.Atoi(string(m[1]))
	protocol := "Protocol" + strconv.Itoa(own)
	if !slices.Contains(versions, own) {
		t.Fatalf("the library declares the protocol versions %v, not %d, which the parser refers to", versions, own)
	}
	if own != slices.Max(versions) {
		t.Fatalf("the parser refers to %s, not to the newest protocol version %d that the library declares", protocol, slices.Max(versions))
	}
	const lib = "sxir/pkg/runtime/libgo"
	if out, err := protocolBuild(t, files, libFiles, lib, nil); err != nil {
		t.Fatalf("the parser does not build against the library of its generator: %v\n%s", err, out)
	}
	span := protocolSpan(versions)
	bumped := make([]compiler.File, len(libFiles))
	for i, f := range libFiles {
		bumped[i] = compiler.File{Path: f.Path, Content: shiftProtocol(f.Content, span)}
	}
	if later := libVersions(t, bumped); slices.Min(later) <= slices.Max(versions) {
		t.Fatalf("the later library declares the versions %v, not only versions past %v", later, versions)
	}
	out, err := protocolBuild(t, files, bumped, lib, nil)
	if err == nil {
		t.Fatal("the parser builds against a library that does not implement its protocol")
	}
	if !strings.Contains(out, "undefined: rt."+protocol) {
		t.Errorf("the build against another protocol fails for another reason than the protocol constant:\n%s", out)
	}
}

// TestEmbedCoreProtocol builds a parser generated with LibEmbed, whose copy of the library imports
// the runtime packages from syntaxir.dev/sxir, against this module's runtime packages, which builds, and against
// runtime packages whose value/impl serves the representation of the protocol version after or before the
// library's, which must fail to build on the valueimpl constant the copy refers to.
func TestEmbedCoreProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated parser")
	}
	g := grammar.FromString("proto.sxir", `@start
rule main = [a-z]+ "=" [0-9]+`)
	files, err := compiler.GenerateGo(g, compiler.GoConfig{Module: "example.com/proto"}, compiler.WithStart("main"))
	if err != nil {
		t.Fatal(err)
	}
	var lib []compiler.File
	for _, f := range files {
		if strings.HasPrefix(f.Path, "internal/libgo/") {
			lib = append(lib, f)
		}
	}
	if len(lib) == 0 {
		t.Fatal("embed mode wrote no copy of the library")
	}
	if out, err := protocolBuild(t, files, nil, "", nil); err != nil {
		t.Fatalf("the parser does not build against the runtime packages of its generator: %v\n%s", err, out)
	}
	src, err := os.ReadFile(filepath.Join(codegentest.ModuleRoot(), "pkg", "runtime", "value", "impl", "impl.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := protocolName.FindSubmatch(src)
	if m == nil {
		t.Fatal("value/impl declares no protocol constant")
	}
	want := "undefined: valueimpl.Protocol" + string(m[1])
	span := protocolSpan(protocolVersions(src))
	for _, by := range []int{span, -span} {
		out, err := protocolBuild(t, files, nil, "", func(src []byte) []byte { return shiftProtocol(src, by) })
		if err == nil {
			t.Fatalf("the copy of the library builds against a value/impl of protocol %+d", by)
		}
		if !strings.Contains(out, want) {
			t.Errorf("protocol %+d: the build fails for another reason than the valueimpl constant:\n%s", by, out)
		}
	}
}

// goGenerated matches the line that marks a generated Go file.
var goGenerated = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// TestGeneratedHeader checks that every generated Go and TypeScript file starts with the header that
// names the generator version, and that generating twice gives the same files.
func TestGeneratedHeader(t *testing.T) {
	g := grammar.FromString("proto.sxir", `type Pair = { Key node[string]; Value node[string] }
@start
rule main Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic => Pair{Key: $k, Value: $v}`)
	header := igen.Header()
	if !goGenerated.MatchString(header) || !strings.Contains(header, " sxir "+igen.Version()+".") {
		t.Fatalf("header %q", header)
	}
	goGen := func(c compiler.GoConfig) func() ([]compiler.File, error) {
		return func() ([]compiler.File, error) { return compiler.GenerateGo(g, c, compiler.WithStart("main")) }
	}
	tsGen := func(c compiler.TSConfig) func() ([]compiler.File, error) {
		return func() ([]compiler.File, error) { return compiler.GenerateTypeScript(g, c, compiler.WithStart("main")) }
	}
	for i, generate := range []func() ([]compiler.File, error){
		goGen(compiler.GoConfig{Module: "example.com/h", Types: true}),
		goGen(compiler.GoConfig{Module: "example.com/h", Types: true, Lib: compiler.LibImport}),
		goGen(compiler.GoConfig{Module: "example.com/h", Types: true, TypedOnly: true, Recognize: true}),
		goGen(compiler.GoConfig{Module: "example.com/h", RecognizeOnly: true}),
		tsGen(compiler.TSConfig{Types: true}),
		tsGen(compiler.TSConfig{Types: true, Lib: compiler.LibImport, LibModule: "./rt.ts"}),
	} {
		generator := i // the generator's index in messages
		first, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		second, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		if len(first) != len(second) {
			t.Fatalf("generator %d: %d files, then %d", generator, len(first), len(second))
		}
		for i, f := range first {
			if line, _, _ := strings.Cut(string(f.Content), "\n"); line != header {
				t.Errorf("generator %d: %s starts with %q, want %q", generator, f.Path, line, header)
			}
			if f.Path != second[i].Path || !bytes.Equal(f.Content, second[i].Content) {
				t.Errorf("generator %d: %s differs between two generations", generator, f.Path)
			}
		}
	}
}
