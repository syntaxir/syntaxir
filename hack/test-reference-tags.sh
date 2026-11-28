#!/bin/sh
# Vet and test the tree under each reference build tag, so that a variant cannot rot unnoticed.
#
# A reference tag (sxir_budget_controls_reference, sxir_reference_*, sxir_snapshot_*_reference) selects the simpler or
# the former form of a mechanism that the default build optimizes, for same-source comparisons; a candidate tag
# (sxir_snapshot_position_overlays) selects an opt-in representation that the default build has not adopted. The
# default build does not compile the files of either, so a change to the engine or its tests can break a variant
# without failing the root suite. For each tag the script runs go vet and go test -short over the engine
# packages and every other package with a file constrained by the tag:
#
#	hack/test-reference-tags.sh [tag...]
#
# The tag is set through GOFLAGS, not -tags, so that the go commands that the tests run take it too: the
# generated parsers that they build and run are built with it, and a tag that selects a form of the Go
# library reaches them. For a tag that is a reference control of the registry (internal/pass), the script
# also compares the parsers of its build with those of the default build, which a test cannot do in one
# process: the test TestReferenceTranscript of internal/testing/reftest writes the transcript of the default
# build once, and in the build with the tag compares its own with it. The configurations, the files that a
# tag selects, must differ, and the results of the control's backends on its witnesses must not.
#
# Without arguments it checks every tag of the tree: each sxir_ tag named in a build constraint except sxir_long, the
# long test tier, sxir_feature_witness, which the tests of generated parsers set themselves, sxir_settingsprobe,
# which selects the measuring program of internal/bench/settingsprobe: that program builds only with the overlay that
# supplies its parser, as the test TestSettingsProbe of bench/metrics builds it, and sxir_release, the release gate,
# which is no reference tag and runs on its own (go test -tags sxir_release -run TestReleaseCriterion
# ./internal/optcatalog). It rejects a given tag that no build constraint names, or one of these, so a misspelt tag
# cannot pass as the default build. It exits non-zero when a tag fails to build or pass. A test whose feature a tag removes skips
# under that tag. Tags run 4 at a time (TAG_JOBS), each with its output printed under its own heading once all have ended.
# The run takes tens of minutes.
set -u
cd "$(dirname "$0")/.." || exit 1
constrained() { # constrained [grep flags]: the Go files whose build constraint matches the last argument
	grep -rl --include='*.go' "$@" . | grep -v '^\./\.'
}
if [ $# -gt 0 ]; then
	tags="$*"
else
	tags=$(constrained '^//go:build .*sxir_' | xargs grep -h '^//go:build' | grep -o 'sxir_[a-z0-9_]*' | sort -u | grep -v -e '^sxir_long$' -e '^sxir_feature_witness$' -e '^sxir_settingsprobe$' -e '^sxir_release$')
fi
[ -n "$tags" ] || { echo "test-reference-tags: no reference tags found" >&2; exit 1; }
for t in $tags; do
	case $t in
	sxir_long | sxir_feature_witness | sxir_settingsprobe | sxir_release | *[!a-z0-9_]* | '') ok= ;;
	*) ok=$(constrained -w "^//go:build .*$t") ;;
	esac
	[ -n "$ok" ] || { echo "test-reference-tags: $t: not a reference tag of the tree" >&2; exit 2; }
done
# Each tag runs in its own subshell, at most TAG_JOBS (default 4) at a time, with its output in a log that is
# printed, with a heading, after all tags ended, so that the lines of two tags do not mix. The Go build cache is safe
# under concurrent use; set GOFLAGS=-p=N to bound the compile and test processes of each tag.
jobs=${TAG_JOBS:-4}
dir=$(mktemp -d "${TMPDIR:-/tmp}/reference-tags.XXXXXX") || exit 1
trap 'rm -rf "$dir"' EXIT
flags=${GOFLAGS:+$GOFLAGS }
# control TAG: whether the tag is the build tag of a reference control of the registry.
control() { grep -q "Tag: \"$1\"" internal/pass/mechanisms.go; }
transcript() { # transcript TAG: run TestReferenceTranscript in the build with the tag, or in the default build for ""
	GOFLAGS="$flags${1:+-tags=$1}" SXIR_REFTEST_TAG=$1 SXIR_REFTEST_OUT="$dir/transcript.${1:-default}" SXIR_REFTEST_BASE="$dir/transcript.default" \
		go test -count=1 -run '^TestReferenceTranscript$' ./internal/testing/reftest
}
check() { # check TAG: vet then test one tag, then compare its transcript; the status is the result
	t=$1
	pkgs=$(constrained -w "^//go:build .*$t" | xargs -n1 dirname | sort -u)
	GOFLAGS="$flags-tags=$t" go vet ./internal/runtime/engine/... $pkgs || { echo "test-reference-tags: $t: vet failed"; return 1; }
	GOFLAGS="$flags-tags=$t" go test -count=1 -short ./internal/runtime/engine/... $pkgs || { echo "test-reference-tags: $t: tests failed"; return 1; }
	if control "$t"; then
		transcript "$t" || { echo "test-reference-tags: $t: the transcript differs from the default build's"; return 1; }
	fi
}
any=
for t in $tags; do control "$t" && any=1; done
if [ -n "$any" ]; then
	transcript "" >"$dir/default.log" 2>&1 || { cat "$dir/default.log" >&2; echo "test-reference-tags: the transcript of the default build failed" >&2; exit 1; }
fi
n=0
for t in $tags; do
	( check "$t" >"$dir/$t.log" 2>&1; echo $? >"$dir/$t.rc" ) &
	n=$((n + 1))
	if [ "$n" -ge "$jobs" ]; then wait; n=0; fi
done
wait
bad=0
for t in $tags; do
	rc=$(cat "$dir/$t.rc" 2>/dev/null || echo 1)
	echo "test-reference-tags: == $t (exit $rc)" >&2
	sed "s/^/[$t] /" "$dir/$t.log" >&2
	[ "$rc" = 0 ] || bad=1
done
[ "$bad" = 0 ] && echo "test-reference-tags: all tags pass" >&2
exit "$bad"
