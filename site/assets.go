package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Only the published current pair is inherited. A previous pair never becomes
// current, so successive builds cannot accumulate historical binaries.
type assetManifest struct {
	Version        int              `json:"version"`
	Source         string           `json:"source,omitempty"`
	Current        playgroundAsset  `json:"current"`
	Previous       *playgroundAsset `json:"previous,omitempty"`
	TinyGoCurrent  *playgroundAsset `json:"tinygoCurrent,omitempty"`
	TinyGoPrevious *playgroundAsset `json:"tinygoPrevious,omitempty"`
}

// A playgroundAsset names a WASM binary and its runtime. The binary is stored either as is
// (wasm/sxir-<hash>.wasm) or gzip-compressed (wasm/sxir-<hash>.wasm.gz), which the browser decompresses
// before compiling; the hash is that of the stored bytes in both cases.
type playgroundAsset struct {
	Wasm          string `json:"wasm"`
	WasmSHA256    string `json:"wasmSHA256"`
	Runtime       string `json:"runtime"`
	RuntimeSHA256 string `json:"runtimeSHA256"`
}

type assetBytes struct {
	asset         playgroundAsset
	wasm, runtime []byte
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var legacyWasmPattern = regexp.MustCompile(`data-wasm="(?:\.\./)?playground/(wasm/sxir-[0-9a-f]{16}\.wasm)"`)

// productionSite is the canonical origin of the published site; site/config.json names the same
// origin as siteURL.
const productionSite = "https://syntaxir.dev"

// productionBranch is the branch that Workers Builds deploys to the production site.
const productionBranch = "main"

// deploymentURLs returns the defaults of -previous-site and -deploy-url. Only a Workers Builds run
// of the production branch retains the published playground assets, from the production site.
// Workers Builds knows no immutable URL for a deploy before it is published, so there is no
// default deploy URL. Local builds and builds of other branches default to neither.
func deploymentURLs(getenv func(string) string) (previous, deploy string) {
	if getenv("WORKERS_CI") == "1" && getenv("WORKERS_CI_BRANCH") == productionBranch {
		return productionSite, ""
	}
	return "", ""
}

func siteURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid published site URL %q", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	return u, nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// maxDecompressedWasm bounds the size of a decompressed binary that the builder checks.
const maxDecompressedWasm = 256 << 20

var (
	wasmMagic = []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	gzipMagic = []byte{0x1f, 0x8b}
)

// describeAssets names a stored binary and its runtime after their contents. A gzip stream is named
// .wasm.gz, anything else .wasm.
func describeAssets(wasm, runtime []byte) playgroundAsset {
	w, r := hashBytes(wasm), hashBytes(runtime)
	ext := ".wasm"
	if bytes.HasPrefix(wasm, gzipMagic) {
		ext = ".wasm.gz"
	}
	return playgroundAsset{Wasm: "wasm/sxir-" + w[:16] + ext, WasmSHA256: w,
		Runtime: "runtime/wasm_exec-" + r[:16] + ".js", RuntimeSHA256: r}
}

func (a playgroundAsset) validate() error {
	if !digestPattern.MatchString(a.WasmSHA256) || !digestPattern.MatchString(a.RuntimeSHA256) ||
		(a.Wasm != "wasm/sxir-"+a.WasmSHA256[:16]+".wasm" && a.Wasm != "wasm/sxir-"+a.WasmSHA256[:16]+".wasm.gz") ||
		a.Runtime != "runtime/wasm_exec-"+a.RuntimeSHA256[:16]+".js" {
		return errors.New("invalid playground asset paths or hashes")
	}
	return nil
}

// checkWasm reports whether data is what a binary stored under name must be: a version-one WASM
// module for .wasm, and a gzip stream of one for .wasm.gz.
func checkWasm(name string, data []byte) error {
	if !strings.HasSuffix(name, ".gz") {
		if !bytes.HasPrefix(data, wasmMagic) {
			return fmt.Errorf("%s is not a WebAssembly module", name)
		}
		return nil
	}
	if !bytes.HasPrefix(data, gzipMagic) {
		return fmt.Errorf("%s is not gzip-compressed", name)
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	head := make([]byte, len(wasmMagic))
	if _, err := io.ReadFull(r, head); err != nil || !bytes.Equal(head, wasmMagic) {
		return fmt.Errorf("%s does not hold a WebAssembly module", name)
	}
	// Read to the end so that the gzip checksum is verified, but no further than a binary may be.
	n, err := io.Copy(io.Discard, io.LimitReader(r, maxDecompressedWasm))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if n == maxDecompressedWasm {
		return fmt.Errorf("%s decompresses to %d bytes or more", name, maxDecompressedWasm)
	}
	return nil
}

// looksLikeJSONObject reports whether b starts, after white space, with the
// opening brace of a JSON object. A malformed manifest still fails to decode;
// a body that is not JSON at all is not a manifest.
func looksLikeJSONObject(b []byte) bool {
	b = bytes.TrimLeft(b, " \t\r\n")
	return len(b) > 0 && b[0] == '{'
}

func fetchAsset(client *http.Client, base *url.URL, path string, limit int64) ([]byte, int, error) {
	u := base.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("fetching %s: HTTP %d", u, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(b)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("%s exceeds %d bytes", u, limit)
	}
	return b, resp.StatusCode, nil
}

func readPreviousAssets(raw string) (*assetBytes, error) {
	return readPreviousAssetsWithTinyGo(raw, nil)
}

// readPreviousAssetsWithTinyGo retrieves both current compiler pairs from one
// published manifest and its immutable source URL.
func readPreviousAssetsWithTinyGo(raw string, tinygoOut **assetBytes) (*assetBytes, error) {
	if tinygoOut != nil {
		*tinygoOut = nil
	}
	if raw == "" {
		return nil, nil
	}
	base, err := siteURL(raw)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	b, status, err := fetchAsset(client, base, "playground/assets.json", 64<<10)
	if err != nil {
		return nil, err
	}
	if status == http.StatusOK && !looksLikeJSONObject(b) {
		// The domain serves something other than a SyntaxIR site, such as the
		// placeholder of a newly created Worker: there is nothing to retain.
		fmt.Fprintf(os.Stderr, "site: %s serves no playground asset manifest; building as a first deployment\n", base)
		return nil, nil
	}
	var a playgroundAsset
	var m assetManifest
	legacy := status == http.StatusNotFound
	if legacy {
		// Bootstrap sites built before manifests existed, without archiving their
		// whole output or requiring a cache from a previous build machine.
		page, code, err := fetchAsset(client, base, "playground/", 1<<20)
		if err != nil {
			return nil, err
		}
		if code == http.StatusNotFound {
			return nil, nil
		} // first deployment
		m := legacyWasmPattern.FindSubmatch(page)
		if m == nil {
			return nil, errors.New("published playground has no recognized WASM reference")
		}
		a.Wasm, a.Runtime = string(m[1]), "wasm_exec.js"
	} else {
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if err := d.Decode(&m); err != nil {
			return nil, fmt.Errorf("published asset manifest: %w", err)
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return nil, errors.New("trailing published manifest data")
		}
		if m.Version != 1 {
			return nil, fmt.Errorf("unsupported asset manifest version %d", m.Version)
		}
		if err := m.Current.validate(); err != nil {
			return nil, err
		}
		a = m.Current
		if m.Source != "" {
			base, err = siteURL(m.Source)
			if err != nil {
				return nil, err
			}
		}
	}
	playground := base.ResolveReference(&url.URL{Path: "playground/"})
	wasm, code, err := fetchAsset(client, playground, a.Wasm, 64<<20)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK || checkWasm(a.Wasm, wasm) != nil {
		return nil, errors.New("published WASM is missing or invalid")
	}
	runtime, code, err := fetchAsset(client, playground, a.Runtime, 1<<20)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK || len(runtime) == 0 {
		return nil, errors.New("published Go runtime is missing or empty")
	}
	actual := describeAssets(wasm, runtime)
	if legacy {
		if actual.Wasm != a.Wasm {
			return nil, errors.New("published WASM hash does not match its filename")
		}
	} else if actual != a {
		return nil, errors.New("published playground asset hashes do not match the manifest")
	}
	goPair := &assetBytes{asset: actual, wasm: wasm, runtime: runtime}
	if tinygoOut == nil || legacy || m.TinyGoCurrent == nil {
		return goPair, nil
	}
	tiny := *m.TinyGoCurrent
	if err := tiny.validate(); err != nil {
		return nil, err
	}
	wasm, code, err = fetchAsset(client, playground, tiny.Wasm, 64<<20)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK || checkWasm(tiny.Wasm, wasm) != nil {
		return nil, errors.New("published TinyGo WASM is missing or invalid")
	}
	runtime, code, err = fetchAsset(client, playground, tiny.Runtime, 1<<20)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK || len(runtime) == 0 {
		return nil, errors.New("published TinyGo runtime is missing or empty")
	}
	if describeAssets(wasm, runtime) != tiny {
		return nil, errors.New("published TinyGo asset hashes do not match the manifest")
	}
	*tinygoOut = &assetBytes{asset: tiny, wasm: wasm, runtime: runtime}
	return goPair, nil
}

// publishTinyGoAssets adds the optional TinyGo current and preceding pairs to
// the Go manifest. Go current/previous remain unchanged for older workers.
func publishTinyGoAssets(dir string, current, previous *assetBytes) error {
	if current == nil && previous == nil {
		return nil
	}
	path := filepath.Join(dir, "assets.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m assetManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	known := []playgroundAsset{m.Current}
	if m.Previous != nil {
		known = append(known, *m.Previous)
	}
	for _, pair := range []*assetBytes{current, previous} {
		if pair == nil || (pair == previous && current != nil && pair.asset == current.asset) {
			continue
		}
		if err := pair.asset.validate(); err != nil {
			return err
		}
		if describeAssets(pair.wasm, pair.runtime) != pair.asset {
			return errors.New("TinyGo asset bytes do not match their hashes")
		}
		for _, a := range known {
			if (a.Wasm == pair.asset.Wasm && a.WasmSHA256 != pair.asset.WasmSHA256) ||
				(a.Runtime == pair.asset.Runtime && a.RuntimeSHA256 != pair.asset.RuntimeSHA256) ||
				(a.Wasm == pair.asset.Wasm && a.Runtime != pair.asset.Runtime) {
				return errors.New("conflicting playground compiler assets")
			}
		}
		known = append(known, pair.asset)
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(pair.asset.Wasm)), pair.wasm); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(pair.asset.Runtime)), pair.runtime); err != nil {
			return err
		}
	}
	if current != nil {
		m.TinyGoCurrent = &current.asset
	}
	if previous != nil && (current == nil || previous.asset != current.asset) {
		m.TinyGoPrevious = &previous.asset
	}
	b, err = json.Marshal(m)
	if err != nil {
		return err
	}
	return writeFile(path, b)
}

func publishAssets(dir, name, source string, previous *assetBytes) error {
	wasm, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	runtime, err := os.ReadFile(filepath.Join(dir, "wasm_exec.js"))
	if err != nil {
		return err
	}
	current := describeAssets(wasm, runtime)
	if current.Wasm != name {
		return errors.New("current WASM hash does not match its filename")
	}
	m := assetManifest{Version: 1, Source: source, Current: current}
	if err := writeFile(filepath.Join(dir, filepath.FromSlash(current.Runtime)), runtime); err != nil {
		return err
	}
	if previous != nil {
		p := previous.asset
		// Detect even a truncated filename collision before overwriting new data.
		if (p.Wasm == current.Wasm && p.WasmSHA256 != current.WasmSHA256) || (p.Runtime == current.Runtime && p.RuntimeSHA256 != current.RuntimeSHA256) {
			return errors.New("playground asset filename collision")
		}
		if p.Wasm == current.Wasm && p.Runtime != current.Runtime {
			return errors.New("identical WASM has conflicting Go runtimes")
		}
		if p != current {
			m.Previous = &p
			if err := writeFile(filepath.Join(dir, filepath.FromSlash(p.Wasm)), previous.wasm); err != nil {
				return err
			}
			if err := writeFile(filepath.Join(dir, filepath.FromSlash(p.Runtime)), previous.runtime); err != nil {
				return err
			}
		}
		// Workers from pre-manifest pages still import this stable path. New
		// workers select their matching, content-hashed runtime from the manifest.
		if err := writeFile(filepath.Join(dir, "wasm_exec.js"), previous.runtime); err != nil {
			return err
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "assets.json"), b)
}
