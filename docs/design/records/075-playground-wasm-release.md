# 075. The Playground's WebAssembly as a Pinned Release

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-11-20

## Context and Problem

The site build compiled the playground's WebAssembly binary on every deploy and published it as one static file.
Cloudflare Workers static assets serve files of up to 25 MiB, and the stripped Go binary grew to 27.1 MB (25.8 MiB),
so the deploy failed and blocked every site update. The binary also changed with every build of a different commit,
although it depends only on the library and the toolchain, and compiling it made each deploy slower and depend on the
toolchain that the build image happens to provide.

## Decision

The binary is built apart from the site and published as an asset of a GitHub pre-release of the repository, under a
tag of its own namespace, `playground-wasm-v<version>`, which never names a release of the module.
`hack/playground-release.sh` builds a release from a clean checkout of one commit and publishes it with the
`gh` command:

- `zp.wasm.gz`: the binary, gzip-compressed at the highest level with no name or time in the header, so the same
  commit and toolchain give the same bytes;
- `wasm_exec.js`: the runtime support of the toolchain that built it;
- `manifest.json`: the toolchain and its version, the build arguments, the source commit, and the size and SHA-256
  digest of each file, with those of the decompressed binary.

`site/playground-wasm.json` pins the release: the repository, the tag and the SHA-256 digests of `zp.wasm.gz` and
`wasm_exec.js`. The site build downloads the two files from the pinned release and fails unless both match their
digests and the binary decompresses to a WebAssembly module. It then publishes them as the current pair of
[asset retention](023-playground-asset-retention.md), under the content-hashed name
`playground/wasm/zp-<hash>.wasm.gz`, the hash being that of the compressed bytes. A deploy therefore publishes
exactly the reviewed bytes and needs no WebAssembly toolchain; updating the playground means publishing a new
release and changing the pin in a commit.

The browser fetches the compressed file as stored, decompresses it with `DecompressionStream("gzip")`, and compiles
the result. The site serves it with the content type of its extension, not `application/wasm`, so no server or
browser treats the gzip data as a module. If a server decodes the gzip itself and delivers the module, the page
detects the WebAssembly header and compiles the bytes as they are. Compressed binaries are about a quarter of the
size of the module (6.2 MB for the 27.1 MB Go binary).

A site build fails when its output has a file of 25 MiB or more, so a binary or any other file that Workers would
refuse stops the build that produced it instead of its deploy.

Local builds can still compile the binary from the working tree (`-wasm-source=build`), compressed the same way, to
try playground changes before a release; the opt-in dual Go and TinyGo build is available only in this mode.

## Alternatives Considered

- **Compile in the deploy and compress there.** It fixes the size, but each deploy still needs the toolchain and
  publishes a binary nobody reviewed, and a site change must wait for the compilation.
- **Brotli instead of gzip.** Brotli makes the Go binary 4.3 MB instead of 6.2 MB, but browsers expose no Brotli
  decompression to scripts, so the page would need a decompressor of its own.
- **Serve the stored gzip as the `Content-Encoding` of `zp.wasm`.** The browser would decompress it before the
  page sees it, but the server would have to answer one path with another file and set the encoding; the site is
  served as static assets with no Worker script, and adding one is a larger change than decompressing in the page.
- **Commit the binary or store it in another service.** Git history would grow by megabytes per update, and another
  service would add credentials and a second publication path; a release asset is public, immutable once uploaded and
  addressed by the repository and the tag.
- **A smaller binary.** Building the playground with TinyGo and optimizing it with Binaryen's `wasm-opt` is the
  intended next release. It changes only how the release is built: the site downloads and verifies whatever pair the
  pin names.

## Consequences

The playground runs the library of the commit its release was built from, not that of the deploy. A change to the
playground or to the library that it should show needs a new release and pin. The playground shows the source
revision of the binary that is running.

A release with a tag must not change once a pin names it: the pin's digests would reject changed bytes and fail the
build. A new binary is a new tag. Pre-releases are not shown as the repository's latest release.

## Testing

Site tests serve releases locally and check acceptance of the pinned pair and rejection of changed digests, missing
files, an uncompressed or non-WebAssembly binary and an empty runtime; validation of the pin and of the checked-in pin;
the compressed binary check, including a truncated stream; retention from an uncompressed generation to compressed
ones; the output size limit at 25 MiB; and the build flags. The JavaScript tests run the page client and the worker
with compressed binaries, including a server that already decoded the gzip. The full site build test compiles the
binary from source and checks the compressed file.
