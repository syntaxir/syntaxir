package main

// The playground's WebAssembly binary is built apart from the site and published as an asset of a
// GitHub pre-release (tags playground-wasm-v*). The site build downloads the release that
// site/playground-wasm.json pins and checks every file against the SHA-256 digest the pin records,
// so a deploy neither compiles the binary nor publishes bytes other than the reviewed ones.
// hack/playground-release.sh builds a release with the playground-release command of this program.

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Files of a playground release.
const (
	releaseWasmFile     = "sxir.wasm.gz"  // the binary, gzip-compressed
	releaseRuntimeFile  = "wasm_exec.js"  // the runtime support of the toolchain that built it
	releaseManifestFile = "manifest.json" // how the release was built (releaseManifest)
)

// wasmPinFile is the repository path of the pin of the playground release.
const wasmPinFile = "site/playground-wasm.json"

// wasmPin names the playground release a site build publishes and the digests of its files.
type wasmPin struct {
	Repository    string `json:"repository"`    // GitHub repository URL, https://github.com/<owner>/<name>
	Tag           string `json:"tag"`           // release tag, playground-wasm-v<version>
	WasmSHA256    string `json:"wasmSHA256"`    // SHA-256 of sxir.wasm.gz
	RuntimeSHA256 string `json:"runtimeSHA256"` // SHA-256 of wasm_exec.js
}

var (
	releaseTagPattern  = regexp.MustCompile(`^playground-wasm-v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)
	githubRepoPattern  = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	releaseToolchains  = map[string]bool{"go": true}
	releaseWasmLimit   = int64(64 << 20)
	releaseRuntimeSize = int64(1 << 20)
)

func (p wasmPin) validate() error {
	if !githubRepoPattern.MatchString(p.Repository) {
		return fmt.Errorf("%s: repository %q is not a GitHub repository URL", wasmPinFile, p.Repository)
	}
	if !releaseTagPattern.MatchString(p.Tag) {
		return fmt.Errorf("%s: tag %q is not a playground-wasm-v<version> tag", wasmPinFile, p.Tag)
	}
	if !digestPattern.MatchString(p.WasmSHA256) || !digestPattern.MatchString(p.RuntimeSHA256) {
		return fmt.Errorf("%s: digests must be 64 lowercase hexadecimal characters", wasmPinFile)
	}
	return nil
}

// downloadURL returns the URL from which GitHub serves the files of the pinned release.
func (p wasmPin) downloadURL() string {
	return p.Repository + "/releases/download/" + p.Tag + "/"
}

func readWasmPin(repo string) (wasmPin, error) {
	var p wasmPin
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(wasmPinFile)))
	if err != nil {
		return p, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("%s: %w", wasmPinFile, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return p, fmt.Errorf("%s: trailing data", wasmPinFile)
	}
	return p, p.validate()
}

// fetchReleasePair downloads the pinned release from base (the pin's download URL unless a test
// replaces it) and returns its binary and runtime once both match the pinned digests.
func fetchReleasePair(pin wasmPin, base string) (*assetBytes, error) {
	if base == "" {
		base = pin.downloadURL()
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	get := func(name, want string, limit int64) ([]byte, error) {
		data, code, err := fetchAsset(client, u, name, limit)
		if err != nil {
			return nil, err
		}
		if code != http.StatusOK {
			return nil, fmt.Errorf("playground release %s has no %s (HTTP %d)", pin.Tag, name, code)
		}
		if got := hashBytes(data); got != want {
			return nil, fmt.Errorf("playground release %s: %s has SHA-256 %s, but %s pins %s", pin.Tag, name, got, wasmPinFile, want)
		}
		return data, nil
	}
	wasm, err := get(releaseWasmFile, pin.WasmSHA256, releaseWasmLimit)
	if err != nil {
		return nil, err
	}
	if err := checkWasm(releaseWasmFile, wasm); err != nil {
		return nil, err
	}
	runtime, err := get(releaseRuntimeFile, pin.RuntimeSHA256, releaseRuntimeSize)
	if err != nil {
		return nil, err
	}
	if len(runtime) == 0 {
		return nil, fmt.Errorf("playground release %s has an empty %s", pin.Tag, releaseRuntimeFile)
	}
	return &assetBytes{asset: describeAssets(wasm, runtime), wasm: wasm, runtime: runtime}, nil
}

// releaseManifest records how a playground release was built. It is published with the release
// for people and tools that inspect it; the site build trusts only the pinned digests.
type releaseManifest struct {
	Version          int           `json:"version"`
	Toolchain        string        `json:"toolchain"`        // go
	ToolchainVersion string        `json:"toolchainVersion"` // such as go1.27.1
	BuildArgs        []string      `json:"buildArgs"`        // the arguments of the build command
	Source           string        `json:"source"`           // the commit of the repository that was built
	Files            []releaseFile `json:"files"`
}

type releaseFile struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
	// Encoding is gzip for a compressed file; ContentSize and ContentSHA256 then describe the
	// decompressed bytes.
	Encoding      string `json:"encoding,omitempty"`
	ContentSize   int    `json:"contentSize,omitempty"`
	ContentSHA256 string `json:"contentSHA256,omitempty"`
}

// playgroundRelease implements the playground-release command: it builds the playground from the
// clean checkout repo and writes the files of a release, with the pin that would publish it, into
// out, which must not exist yet.
func playgroundRelease(args []string) error {
	fs := flag.NewFlagSet("playground-release", flag.ContinueOnError)
	repo := fs.String("repo", "..", "clean checkout of the commit to build")
	out := fs.String("out", "", "directory to create for the release files")
	tag := fs.String("tag", "", "release tag, playground-wasm-v<version>")
	toolchain := fs.String("toolchain", "go", "toolchain that builds the binary (go)")
	repository := fs.String("repository", "https://github.com/syntaxir/syntaxir", "GitHub repository of the release")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *out == "" {
		return errors.New("usage: playground-release -out DIR -tag playground-wasm-vX.Y.Z [-repo DIR] [-toolchain go]")
	}
	if !releaseToolchains[*toolchain] {
		return fmt.Errorf("unsupported release toolchain %q (want go)", *toolchain)
	}
	git := func(args ...string) (string, error) {
		b, err := exec.Command("git", append([]string{"-C", *repo}, args...)...).Output()
		return strings.TrimSpace(string(b)), err
	}
	commit, err := git("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("%s is not a Git checkout: %w", *repo, err)
	}
	if dirty, err := git("status", "--porcelain"); err != nil || dirty != "" {
		return fmt.Errorf("%s has uncommitted changes; a release is built from a clean checkout of a commit", *repo)
	}
	if _, err := os.Lstat(*out); err == nil {
		return fmt.Errorf("%s already exists", *out)
	}
	raw, runtime, err := compileWasm(*repo, *toolchain)
	if err != nil {
		return err
	}
	gz, err := gzipWasm(raw)
	if err != nil {
		return err
	}
	version, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return err
	}
	spec, _ := selectWasmBuild(*toolchain, "sxir.wasm")
	m := releaseManifest{Version: 1, Toolchain: *toolchain, ToolchainVersion: strings.TrimSpace(string(version)),
		BuildArgs: spec.args, Source: commit, Files: []releaseFile{
			{Name: releaseWasmFile, Size: len(gz), SHA256: hashBytes(gz), Encoding: "gzip", ContentSize: len(raw), ContentSHA256: hashBytes(raw)},
			{Name: releaseRuntimeFile, Size: len(runtime), SHA256: hashBytes(runtime)},
		}}
	pin := wasmPin{Repository: *repository, Tag: *tag, WasmSHA256: hashBytes(gz), RuntimeSHA256: hashBytes(runtime)}
	if err := pin.validate(); err != nil {
		return err
	}
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	pinJSON, err := json.MarshalIndent(pin, "", "  ")
	if err != nil {
		return err
	}
	if err := os.Mkdir(*out, 0o755); err != nil {
		return err
	}
	for name, data := range map[string][]byte{
		releaseWasmFile:     gz,
		releaseRuntimeFile:  runtime,
		releaseManifestFile: append(manifest, '\n'),
		"pin.json":          append(pinJSON, '\n'),
	} {
		if err := os.WriteFile(filepath.Join(*out, name), data, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "playground-release: %s from %s: %s %d bytes (%d decompressed), %s %d bytes\n",
		*tag, commit, releaseWasmFile, len(gz), len(raw), releaseRuntimeFile, len(runtime))
	return nil
}
