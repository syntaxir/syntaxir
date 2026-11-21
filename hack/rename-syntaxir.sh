#!/usr/bin/env bash
# Rewrite the tracked files of a checkout from the former project names to SyntaxIR, and stage the
# result. keep-old-name: this whole script spells the former names; it is removed once every branch
# that predates the rename has been rewritten with it.
#
#	bash hack/rename-syntaxir.sh
#
# Run it at the root of a checkout whose working tree matches its index (the index may differ from
# HEAD, as when a branch is replayed commit by commit). The rewrite is deterministic and idempotent:
# on a tree it has already rewritten it changes nothing. It renames paths with git mv and rewrites
# file contents with one ordered, case-sensitive token map, the same for both:
#
#	zeroparse/zp            -> syntaxir/syntaxir   repository (also inside regular expressions)
#	ZeroParse, ZEROPARSE,
#	zeroparse               -> SyntaxIR, SYNTAXIR, syntaxir   (so zeroparse.dev/zp -> syntaxir.dev/sxir)
#	ZPC\x00                 -> SXC\x00             magic of compiled grammars
#	EncodingZP, BuildZP, buildZP, classifyZPTokens, naiveZPOffset, TestTextIndexZPPositions
#	                        -> ...SXIR...          the identifiers that hold the short name mid-word
#	ZP at a token start     -> SXIR                ZP_*, ZPHooks, ZPNodeTypes, ZPSchema_, ZPClient
#	zp at a token start     -> sxir                zp, .zp, .zpc, cmd/zp, zp_*, zp-*, zpls, zpvalue
#
# A token starts where the preceding byte is not an ASCII letter or digit, or right after the
# escape \n, \t or \r inside a string literal ("\nzp gen", "\tzpGrammar"). So FuzzParse and words
# that merely contain the letters stay as they are.
#
# Not rewritten: lines that hold the marker keep-old-name:, the integrity digests of package locks
# ("integrity": "sha...), whose base64 text holds the letters by chance, the saved raw benchmark results
# (bench/results*.txt), which are never edited by hand, the recovery corpora
# (testdata/recovery/*.txt), whose inputs are random text in which the letters occur by chance, and
# wrangler.jsonc, whose Worker keeps its dashboard name. Paths are still renamed when they hold a
# name.
set -u
cd "$(git rev-parse --show-toplevel)" || exit 1
git diff --quiet || { echo "rename-syntaxir: the working tree differs from the index; stage or discard first" >&2; exit 1; }

rules='
s{zeroparse/zp(?![A-Za-z0-9_])}{syntaxir/syntaxir}g;
s{ZeroParse}{SyntaxIR}g;
s{ZEROPARSE}{SYNTAXIR}g;
s{zeroparse}{syntaxir}g;
s{(?:(?<![A-Za-z0-9])|(?<=\\[ntr]))ZPC(?=\\x00)}{SXC}g;
s{(?<![A-Za-z0-9])(Encoding|Build|build|classify|naive|TestTextIndex)ZP(?![a-z0-9])}{${1}SXIR}g;
s{(?:(?<![A-Za-z0-9])|(?<=\\[ntr]))ZP}{SXIR}g;
s{(?:(?<![A-Za-z0-9])|(?<=\\[ntr]))zp}{sxir}g;
'

skipped() {
	case "$1" in
	# The saved raw results and the random corpora; this script and the guard spell the former names;
	# the Worker keeps the name it has in the Cloudflare dashboard.
	bench/results*.txt | testdata/recovery/*.txt | hack/rename-syntaxir.sh | \
		internal/testing/archtest/names_test.go | wrangler.jsonc) return 0 ;;
	esac
	return 1
}

# Contents. git grep -I leaves out binary files; the prefilter is a superset of what the rules match.
# A line that holds the marker keep-old-name: keeps its former names, and an integrity digest keeps its
# bytes.
list=${TMPDIR:-/tmp}/rename-syntaxir.$$
trap 'rm -f "$list" "$list.go"' EXIT
git grep -I -l -z -E 'zp|ZP|zeroparse|ZeroParse|ZEROPARSE' -- . | while IFS= read -r -d '' f; do
	skipped "$f" || [ -L "$f" ] || printf '%s\0' "$f"
done >"$list" || exit 1
if [ -s "$list" ]; then
	xargs -0 perl -pi -e "next LINE if /keep-old-name:|\"integrity\": \"sha/; $rules" -- <"$list" || exit 1
fi
# Longer names move the alignment of comments and fields, and gofmt wraps lines that grow too long.
tr '\0' '\n' <"$list" | grep '\.go$' | tr '\n' '\0' >"$list.go"
if [ -s "$list.go" ]; then
	xargs -0 "$(go env GOROOT)/bin/gofmt" -w <"$list.go" || exit 1
fi

# Paths. Each tracked path is mapped with the same rules; a path that changes is moved.
git ls-files -z | perl -0 -ne "chomp; my \$o = \$_; $rules print \"\$o\\0\$_\\0\" if \$_ ne \$o;" |
	while IFS= read -r -d '' from && IFS= read -r -d '' to; do
		mkdir -p "$(dirname "$to")" || exit 1
		git mv -- "$from" "$to" || exit 1
		[ -e "$to" ] || { echo "rename-syntaxir: moving $from to $to failed" >&2; exit 1; }
	done || exit 1

git add -u || exit 1
