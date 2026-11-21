#!/bin/sh
# Fail fast when a CI job is not running the exact runtime its test harness expects.
# Usage: require-toolchain.sh TOOL...
# TOOL is one of: node, tsc, python, typescript-corpus. Each one named is checked against its
# pinned version; an unknown name fails. The Go tests need node and tsc whatever this script
# checks, and fail when one is missing unless SKIP_NODE=1 (internal/testing/tooltest).
# SXIR_PYTHON, SXIR_TYPESCRIPT and SXIR_TYPESCRIPT_TESTS locate the inputs of python and
# typescript-corpus.
set -eu

require_version() {
	name=$1
	actual=$2
	expected=$3
	if [ "$actual" != "$expected" ]; then
		echo "$name version mismatch: expected $expected, got $actual" >&2
		exit 1
	fi
}

if [ "$#" -eq 0 ]; then
	echo "usage: require-toolchain.sh node|tsc|python|typescript-corpus..." >&2
	exit 2
fi

for tool in "$@"; do
	case $tool in
	node)
		require_version node "$(node --version)" "v22.18.0"
		;;
	tsc)
		require_version tsc "$(tsc --version)" "Version 5.9.3"
		;;
	python)
		python=${SXIR_PYTHON:-python3}
		require_version Python "$("$python" --version 2>&1)" "Python 3.14.0"
		"$python" -c 'import sys; assert sys.version_info[:3] == (3, 14, 0), sys.version'
		;;
	typescript-corpus)
		: "${SXIR_TYPESCRIPT:?SXIR_TYPESCRIPT must name the TypeScript 5.9.3 package}"
		: "${SXIR_TYPESCRIPT_TESTS:?SXIR_TYPESCRIPT_TESTS must name the v5.9.3 tests/cases corpus}"
		node -e 'const p=require(process.argv[1]+"/package.json"); if (p.version !== "5.9.3") throw Error(`expected TypeScript 5.9.3, got ${p.version}`)' "$SXIR_TYPESCRIPT"
		test -d "$SXIR_TYPESCRIPT_TESTS"
		;;
	*)
		echo "unknown toolchain requirement: $tool" >&2
		exit 2
		;;
	esac
done
