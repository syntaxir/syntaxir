package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gzipped(t testing.TB, data []byte) []byte {
	t.Helper()
	gz, err := gzipWasm(data)
	if err != nil {
		t.Fatal(err)
	}
	return gz
}

// compressedAssets is testAssets with the binary stored gzip-compressed, as the site publishes it.
func compressedAssets(t testing.TB, label string) *assetBytes {
	raw := testAssets(label)
	gz := gzipped(t, raw.wasm)
	return &assetBytes{describeAssets(gz, raw.runtime), gz, raw.runtime}
}

// TestCheckedInWasmPin checks that the pin of the playground release is well formed, so that a
// mistake in it fails here and not first in a deploy.
func TestCheckedInWasmPin(t *testing.T) {
	pin, err := readWasmPin("..")
	if err != nil {
		t.Fatal(err)
	}
	if pin.Repository != "https://github.com/zeroparse/zp" {
		t.Errorf("the playground release is pinned in %s", pin.Repository)
	}
	if got := pin.downloadURL(); got != pin.Repository+"/releases/download/"+pin.Tag+"/" {
		t.Errorf("download URL %s", got)
	}
}

func TestWasmPinValidation(t *testing.T) {
	good := wasmPin{Repository: "https://github.com/zeroparse/zp", Tag: "playground-wasm-v0.0.1", WasmSHA256: strings.Repeat("a", 64), RuntimeSHA256: strings.Repeat("b", 64)}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*wasmPin){
		"release tag":       func(p *wasmPin) { p.Tag = "v0.1.0" },
		"tag suffix":        func(p *wasmPin) { p.Tag = "playground-wasm-v1/../x" },
		"repository host":   func(p *wasmPin) { p.Repository = "https://example.com/zeroparse/zp" },
		"repository path":   func(p *wasmPin) { p.Repository = "https://github.com/zeroparse/zp/releases" },
		"short digest":      func(p *wasmPin) { p.WasmSHA256 = "abc" },
		"uppercase digest":  func(p *wasmPin) { p.RuntimeSHA256 = strings.Repeat("A", 64) },
		"missing all":       func(p *wasmPin) { *p = wasmPin{} },
		"http repository":   func(p *wasmPin) { p.Repository = "http://github.com/zeroparse/zp" },
		"prerelease suffix": func(p *wasmPin) { p.Tag = "playground-wasm-v0.0.1-rc 1" },
	} {
		p := good
		mutate(&p)
		if err := p.validate(); err == nil {
			t.Errorf("%s: accepted %+v", name, p)
		}
	}
	repo := t.TempDir()
	pinPath := filepath.Join(repo, filepath.FromSlash(wasmPinFile))
	data, _ := json.Marshal(good)
	for name, body := range map[string]string{
		"unknown field": strings.TrimSuffix(string(data), "}") + `,"extra":1}`,
		"trailing data": string(data) + "{}",
	} {
		if err := writeFile(pinPath, []byte(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := readWasmPin(repo); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// releaseServer serves the files of a playground release, with replace changing the body of a
// file (nil: not found).
func releaseServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := strings.CutPrefix(r.URL.Path, "/releases/download/playground-wasm-v0.0.1/")
		data, found := files[name]
		if !ok || !found || data == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestFetchReleasePair(t *testing.T) {
	a := compressedAssets(t, "release")
	pin := wasmPin{Repository: "https://github.com/zeroparse/zp", Tag: "playground-wasm-v0.0.1", WasmSHA256: hashBytes(a.wasm), RuntimeSHA256: hashBytes(a.runtime)}
	files := func() map[string][]byte {
		return map[string][]byte{releaseWasmFile: a.wasm, releaseRuntimeFile: a.runtime}
	}
	base := func(s *httptest.Server) string { return s.URL + "/releases/download/" + pin.Tag + "/" }
	s := releaseServer(t, files())
	got, err := fetchReleasePair(pin, base(s))
	if err != nil {
		t.Fatal(err)
	}
	if got.asset != a.asset || !bytes.Equal(got.wasm, a.wasm) || !bytes.Equal(got.runtime, a.runtime) {
		t.Fatalf("fetched %+v, want %+v", got.asset, a.asset)
	}
	if !strings.HasSuffix(got.asset.Wasm, ".wasm.gz") {
		t.Fatalf("compressed binary named %s", got.asset.Wasm)
	}

	raw := testAssets("uncompressed")
	for name, change := range map[string]func(map[string][]byte, *wasmPin){
		"wasm digest":     func(f map[string][]byte, _ *wasmPin) { f[releaseWasmFile] = append(bytes.Clone(a.wasm), 0) },
		"runtime digest":  func(f map[string][]byte, _ *wasmPin) { f[releaseRuntimeFile] = []byte("// other runtime") },
		"missing wasm":    func(f map[string][]byte, _ *wasmPin) { f[releaseWasmFile] = nil },
		"missing runtime": func(f map[string][]byte, _ *wasmPin) { f[releaseRuntimeFile] = nil },
		"not compressed": func(f map[string][]byte, p *wasmPin) {
			f[releaseWasmFile] = raw.wasm
			p.WasmSHA256 = hashBytes(raw.wasm)
		},
		"not a module": func(f map[string][]byte, p *wasmPin) {
			gz := gzipped(t, []byte("not WebAssembly"))
			f[releaseWasmFile] = gz
			p.WasmSHA256 = hashBytes(gz)
		},
		"empty runtime": func(f map[string][]byte, p *wasmPin) {
			f[releaseRuntimeFile] = []byte{}
			p.RuntimeSHA256 = hashBytes(nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, p := files(), pin
			change(f, &p)
			if _, err := fetchReleasePair(p, base(releaseServer(t, f))); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestCheckWasm(t *testing.T) {
	module := testAssets("module").wasm
	for _, tc := range []struct {
		name string
		data []byte
		ok   bool
	}{
		{"zp.wasm", module, true},
		{"zp.wasm", gzipped(t, module), false},
		{"zp.wasm.gz", gzipped(t, module), true},
		{"zp.wasm.gz", module, false},
		{"zp.wasm.gz", gzipped(t, []byte("text")), false},
		{"zp.wasm.gz", gzipped(t, module)[:20], false}, // truncated: the checksum is missing
	} {
		if err := checkWasm(tc.name, tc.data); (err == nil) != tc.ok {
			t.Errorf("%s (%d bytes): %v", tc.name, len(tc.data), err)
		}
	}
}

func TestOutputFileSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		size int64
		ok   bool
	}{{maxOutputFile - 1, true}, {maxOutputFile, false}} {
		dir := t.TempDir()
		f, err := os.Create(filepath.Join(dir, "large.wasm"))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(tc.size); err != nil {
			t.Fatal(err)
		}
		f.Close()
		_, err = stats(dir, 0)
		if (err == nil) != tc.ok {
			t.Errorf("a file of %d bytes: %v", tc.size, err)
		}
		if err != nil && !strings.Contains(err.Error(), "large.wasm") {
			t.Errorf("the error does not name the file: %v", err)
		}
	}
}

func TestSelectWasmSource(t *testing.T) {
	for in, want := range map[string]string{"": "release", "release": "release", "build": "build"} {
		if got, err := selectWasmSource(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	if _, err := selectWasmSource("go"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if _, err := Build(Config{Out: t.TempDir(), Wasm: true, WasmMode: "dual"}); err == nil || !strings.Contains(err.Error(), "-wasm-source=build") {
		t.Fatalf("dual mode from a release: %v", err)
	}
}
