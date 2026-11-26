// Command site builds the SyntaxIR web site: a landing page, the documentation rendered from the
// repository's Markdown, an API reference generated from the Go source, and the playground.
//
// Usage (from the site directory):
//
//	go run . [-repo ..] [-out dist] [-wasm=true] [-wasm-source=release] [-wasm-mode=go] [-benchmarks=true] [-check=true] [-serve localhost:8080]
//	go run . playground-release -out DIR -tag playground-wasm-vX.Y.Z [-repo DIR]
//
// The first form builds the site. The second builds the files of a playground release (see
// hack/playground-release.sh), which the site build downloads as site/playground-wasm.json pins.
//
// Everything is generated from the repository at build time; nothing in the output is meant to be
// edited or committed. See docs/guide/playground.md and docs/design/records/016-web-site-and-playground.md.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "playground-release" {
		if err := playgroundRelease(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "readme" {
		if err := readmeCmd(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	var cfg Config
	previous, deploy := deploymentURLs(os.Getenv)
	flag.StringVar(&cfg.Repo, "repo", "..", "root of the SyntaxIR repository")
	flag.StringVar(&cfg.Out, "out", "dist", "output directory (replaced by each build)")
	flag.BoolVar(&cfg.Wasm, "wasm", true, "include the playground's WebAssembly binary")
	flag.StringVar(&cfg.WasmSource, "wasm-source", "release", "where the playground's binary comes from: release (the release pinned by site/playground-wasm.json) or build (from source)")
	flag.StringVar(&cfg.WasmMode, "wasm-mode", "go", "playground WASM binaries: go or dual (Go and TinyGo; needs -wasm-source=build)")
	flag.BoolVar(&cfg.Benchmarks, "benchmarks", true, "render the benchmark results of the release pinned by site/benchmarks.json (false: link to the release)")
	flag.BoolVar(&cfg.Check, "check", true, "fail if a generated page links to a missing page or anchor")
	flag.StringVar(&cfg.GitHub, "github", "https://github.com/syntaxir/syntaxir", "repository URL for links to source files")
	flag.StringVar(&cfg.Ref, "ref", "main", "branch or tag for links to source files")
	flag.StringVar(&cfg.PreviousSite, "previous-site", previous, "retain the current WASM/runtime pair from this published site URL (automatic for Workers Builds of the production branch)")
	flag.StringVar(&cfg.DeployURL, "deploy-url", deploy, "immutable URL of this deploy, for fetching its assets on the next production build")
	serve := flag.String("serve", "", "after building, serve the site at this address (for example localhost:8080)")
	flag.Parse()

	st, err := Build(cfg)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "site: %d pages, %d files, %.1f MB in %s\n", st.Pages, st.Files, float64(st.Bytes)/1e6, cfg.Out)
	if *serve != "" {
		fmt.Fprintf(os.Stderr, "site: serving %s at http://%s/\n", cfg.Out, *serve)
		log.Fatal(http.ListenAndServe(*serve, http.FileServer(http.Dir(cfg.Out))))
	}
}
