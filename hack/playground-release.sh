#!/bin/sh
# Build the playground's WebAssembly release and, with -publish, publish it as a GitHub pre-release.
#
# Usage: sh hack/playground-release.sh [-commit REV] [-publish] TAG OUT_DIR
#
# The binary is built from a clean, detached worktree of REV (default HEAD) by the site program's
# playground-release command, which writes into OUT_DIR (created; must not exist):
#   sxir.wasm.gz     the binary, gzip-compressed
#   wasm_exec.js   the runtime support of the toolchain that built it
#   manifest.json  toolchain, build arguments, source commit, sizes and SHA-256 digests
#   pin.json       the site/playground-wasm.json that publishes this release
# With -publish, it creates the pre-release TAG (playground-wasm-v<version>) at REV in the GitHub
# repository with gh, uploads the first three files, and copies pin.json to site/playground-wasm.json.
# REV must already be on GitHub. An existing tag or release is never changed. See docs/site.md.
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd -P)
repository=https://github.com/syntaxir/syntaxir
commit=HEAD
publish=false
while [ $# -gt 0 ]; do
  case "$1" in
    -commit) [ $# -ge 2 ] || { echo '-commit needs a revision' >&2; exit 2; }; commit=$2; shift 2 ;;
    -publish) publish=true; shift ;;
    -*) echo "unknown flag $1" >&2; exit 2 ;;
    *) break ;;
  esac
done
[ $# -eq 2 ] || { echo 'usage: sh hack/playground-release.sh [-commit REV] [-publish] TAG OUT_DIR' >&2; exit 2; }
tag=$1
out=$2
case "$tag" in playground-wasm-v[0-9]*) ;; *) echo "tag $tag is not playground-wasm-v<version>" >&2; exit 2 ;; esac
[ ! -e "$out" ] || { echo "$out already exists" >&2; exit 1; }
mkdir -p -- "$(dirname -- "$out")"
out=$(CDPATH= cd -- "$(dirname -- "$out")" && pwd -P)/$(basename -- "$out")
rev=$(git -C "$repo_dir" rev-parse --verify "$commit^{commit}")

if [ "$publish" = true ]; then
  command -v gh >/dev/null || { echo 'publishing needs the gh command' >&2; exit 1; }
  slug=${repository#https://github.com/}
  gh api "repos/$slug/commits/$rev" --silent || { echo "commit $rev is not on GitHub; push it first" >&2; exit 1; }
  # ls-remote exits with 2 when no ref matches; any other failure stops the release.
  status=0
  git ls-remote --exit-code --tags "$repository" "refs/tags/$tag" >/dev/null || status=$?
  if [ "$status" -ne 2 ] || gh release view "$tag" --repo "$slug" >/dev/null 2>&1; then
    echo "tag or release $tag already exists (or the remote cannot be read); choose a new version" >&2
    exit 1
  fi
fi

# The physical path: go resolves the workspace of the build against it.
work=$(CDPATH= cd -- "$(mktemp -d "${TMPDIR:-/tmp}/sxir-playground-release.XXXXXX")" && pwd -P)
cleanup() {
  git -C "$repo_dir" worktree remove --force "$work/src" >/dev/null 2>&1 || true
  rm -rf -- "$work"
}
trap cleanup EXIT HUP INT TERM
git -C "$repo_dir" worktree add --detach --quiet "$work/src" "$rev"
(cd "$repo_dir/site" && GOWORK=off go run . playground-release -repo "$work/src" -out "$out" -tag "$tag" -repository "$repository")

if [ "$publish" = true ]; then
  gh release create "$tag" --repo "$slug" --prerelease --target "$rev" \
    --title "Playground WebAssembly $tag" \
    --notes "The WebAssembly binary of the SyntaxIR playground, built from $rev and published on syntaxir.dev. manifest.json records the toolchain, the build arguments and the SHA-256 digest of each file; sxir.wasm.gz is gzip-compressed. This is not a release of the SyntaxIR module." \
    "$out/sxir.wasm.gz" "$out/wasm_exec.js" "$out/manifest.json"
  cp -- "$out/pin.json" "$repo_dir/site/playground-wasm.json"
  echo "published $tag; site/playground-wasm.json now pins it" >&2
else
  echo "built $tag in $out (not published; pass -publish to create the pre-release)" >&2
fi
