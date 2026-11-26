#!/usr/bin/env bash
# Run the validation levels of docs/development.md with one command, so that every contributor checks the same things.
#
#	hack/gate.sh [--base REV] [--keep-going] [--summary FILE] [--dry-run] LEVEL...
#
# Levels (several may be given; a step that two levels share runs once):
#   auto    the levels that the change since REV needs, as AGENTS.md and docs/development.md choose them: engine when
#           it touches internal/runtime/, pkg/runtime/, internal/backend/ or internal/testing/codegentest/, or when
#           hack/affected-tests.sh reports go generate or alpha-parsers as affected (a change of the generator or of a
#           package it imports); otherwise code when any package is affected; docs beside either when docs/, spec/,
#           site/ or README.md changed, and docs alone when nothing else applies. It prints the levels and the reason,
#           and chooses docs and engine when the changes or the affected packages cannot be computed.
#   docs    go vet ./..., the tests of the site module, and a site build with its link check (without the WebAssembly
#           binary, which the playground checks cover).
#   code    go build ./..., go vet ./..., and go test of the packages that changed since REV and of every package that
#           depends on them (hack/affected-tests.sh, which uses go list), the longest packages first.
#   engine  code, with the default-tier tests replaced by the same packages in the long tier (-tags sxir_long, which
#           includes the default tier) and, at the same time as those, hack/test-alpha-parsers.sh and
#           hack/test-reference-tags.sh, the last only when the affected packages include internal/runtime/engine,
#           internal/runtime/state or internal/runtime/document (the reference tags build the engine; it also fails
#           closed: it is planned when the affected packages cannot be computed); then go generate
#           ./internal/backend/... ./internal/runtime/state/sharedsrc ./bench ./internal/frontend/parser ./spec/language/... with a check that it changed
#           nothing.
#   full    only the long tier of the affected packages (the test-long step that engine also plans; a step that two
#           levels share runs once).
#   all     go test -tags sxir_long ./... (the whole tree, in the long tier), with the longest packages named first,
#           for a release; it replaces the affected-package test steps that engine and full plan.
#
# The gate runs what the change can affect and nothing more: the generate step only when the affected computation
# reports a generator, a generator input or a generated file, alpha-parsers only when it reports the generator, a grammar or a runtime
# library, the site steps only when docs/, spec/, site/ or README.md changed since REV. vet and build always run. A
# step that is left out is planned and recorded as "NAME skipped (REASON)", never silently. Every condition fails
# closed: when the affected packages or the changed files cannot be computed, the step runs, and the test steps then
# run every package.
#
# REV is the base of the change (default origin/main); the changed packages are those of REV..HEAD, so commit before
# running. Steps run in order and the gate stops at the first failure, so that a heavy step does not run after a
# failed one; --keep-going runs them all. The long-tier tests, alpha-parsers and reference-tags are independent and
# none of them changes the tree, so they run as one group at the same time: the tests in the foreground with their
# output as it comes, the others with their output printed under a heading when the group has ended. Every step of the
# group runs to its end, and a failure stops the gate after the group; each records its own seconds, so the seconds
# of a group overlap. GATE_PARALLEL=0 runs every step on its own. One line per step (name, exit code, seconds) goes to the summary file
# (default: gate-summary.<pid>.txt in $TMPDIR) and is printed at the end. The exit status is non-zero if a step failed.
# --dry-run prints the steps and runs nothing.
#
# For callers (scripts, lock wrappers): the gate never prompts and reads no standard input. Exit status 0: every step
# passed; 1: a step failed (the summary names it); 2: a usage error, before any step ran. The summary file has one
# tab-separated line per step, "NAME<TAB>exit N<TAB>Ss" for a step that ran and "NAME<TAB>skipped (REASON)" for one that
# was left out, in plan order; with a fail-fast stop the failed step is in the last step or group that ran.
#
# The gate exports SXIR_MUTANTS_BASE (the base revision) for the tests that run mutants; see docs/development.md.
# GATE_PARALLEL (default 1; 0 disables) runs the group above at the same time.
# GATE_SHARDS (default 2; 1 disables) is the number of concurrent shards of the tests of internal/backend/codegen/golang/print.
# Environment: GATE_NICE (default 0) is the nice(1) increment of every step, and GOMAXPROCS, GOFLAGS and the other Go
# variables pass through. GATE_SELFTEST_FAIL=1 adds a step that fails first, to show that the gate stops.
# GATE_AFFECTED_CMD replaces hack/affected-tests.sh (GATE_AFFECTED_CMD=false shows that a failed computation runs everything).
# The step commands are single-quoted on purpose: they expand when the step runs.
# shellcheck disable=SC2016,SC2028
set -euo pipefail

base=origin/main
keep=0
dry=0
summary=
levels=()
while [ $# -gt 0 ]; do
	case $1 in
	--base) [ $# -ge 2 ] || { echo "gate: --base needs a revision" >&2; exit 2; }; base=$2; shift 2 ;;
	--summary) [ $# -ge 2 ] || { echo "gate: --summary needs a file" >&2; exit 2; }; summary=$2; shift 2 ;;
	--keep-going) keep=1; shift ;;
	--dry-run) dry=1; shift ;;
	-h | --help) sed -n '2,/^set -/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit 0 ;;
	auto | docs | code | engine | full | all) levels+=("$1"); shift ;;
	*) echo "gate: unknown argument $1 (levels: auto docs code engine full all)" >&2; exit 2 ;;
	esac
done
[ ${#levels[@]} -gt 0 ] || { echo "usage: hack/gate.sh [--base REV] [--keep-going] [--summary FILE] [--dry-run] auto|docs|code|engine|full|all..." >&2; exit 2; }

[ -z "$summary" ] || case $summary in /*) ;; *) summary=$PWD/$summary ;; esac
root=$(git rev-parse --show-toplevel)
cd "$root"
nice_n=${GATE_NICE:-0}
[ -n "$summary" ] || summary=${TMPDIR:-/tmp}/gate-summary.$$.txt

names=()
descs=()
cmds=()
besides=()
# step NAME DESCRIPTION COMMAND [beside]: append a step unless one of that name is already planned. COMMAND runs through
# bash -c in the root of the tree, so it may be a shell fragment. A step marked beside runs concurrently with the
# planned steps next to it that are marked too (see the run loop below).
step() {
	local n
	for n in ${names[@]+"${names[@]}"}; do [ "$n" = "$1" ] && return 0; done
	names+=("$1")
	descs+=("$2")
	cmds+=("$3")
	besides+=("$([ "${4:-}" = beside ] && echo 1 || echo 0)")
}

# The packages whose tests take longest, longest first. go test starts the test runs of its packages in the order of
# its arguments, at most -p (GOMAXPROCS) at a time, so a long package named late starts after the short ones and ends
# the run alone; naming these first lets them overlap with each other and with the rest. Only the order changes: the
# same packages run with the same flags. A package that no longer exists makes go test fail, so the list cannot go
# stale silently.
export GATE_HEAVY='syntaxir.dev/sxir/internal/backend/codegen/golang/print syntaxir.dev/sxir/internal/backend/codegen/common syntaxir.dev/sxir/internal/runtime/engine syntaxir.dev/sxir/internal/backend/codegen/typescript/print syntaxir.dev/sxir/spec/language syntaxir.dev/sxir/pkg/runtime/libgo syntaxir.dev/sxir/internal/testing/complexitytest'

# affected_cmd GO-TEST-ARGS: go test over the packages of the changes since $GATE_BASE and their reverse dependencies,
# those of $GATE_HEAVY first. internal/backend/codegen/golang/print, the longest package, runs as GATE_SHARDS (default 2) concurrent
# invocations, each with the top-level tests (Test, Fuzz and Example functions, from go test -list) of one shard, beside a
# third invocation for the other packages; only when it is affected. The sharding fails closed: when the list fails, is
# empty or the shards do not add up to it, the package runs unsharded with the others. A failed shard fails the step.
affected_cmd() {
	local tpl
	IFS= read -r -d '' tpl <<'EOS' || true
args='@ARGS@'
raw=$("${GATE_AFFECTED_CMD:-hack/affected-tests.sh}" "$GATE_BASE" HEAD) || { echo "gate: the affected packages could not be computed; testing every package" >&2; exec go test -count=1 $args $GATE_HEAVY ./...; }
pkgs=$(printf "%s\n" "$raw" | grep -v "^#" || true)
[ -n "$pkgs" ] || { echo "gate: no package affected since $GATE_BASE"; exit 0; }
echo "$pkgs" | sed "s/^/gate: affected /"
first=$(for h in $GATE_HEAVY; do printf "%s\n" "$pkgs" | grep -Fx "$h" || true; done)
rest=$(printf "%s\n" "$pkgs" | grep -Fvx -f <(printf "%s\n" $GATE_HEAVY) || true)
shardpkg=syntaxir.dev/sxir/internal/backend/codegen/golang/print
shards=${GATE_SHARDS:-2}
if [ "$shards" -gt 1 ] && printf "%s\n" "$pkgs" | grep -Fxq "$shardpkg"; then
	list=$(go test $args -list . "$shardpkg" 2>/dev/null | grep -E "^(Test|Fuzz|Example)" | sort || true)
	n=$(printf "%s\n" "$list" | grep -c . || true)
	if [ "$n" -ge "$shards" ]; then
		d=$(mktemp -d "${TMPDIR:-/tmp}/gate-shards.XXXXXX")
		k=0; joined=
		while [ "$k" -lt "$shards" ]; do
			part=$(printf "%s\n" "$list" | awk -v k="$k" -v s="$shards" "NR % s == k")
			printf "%s\n" "$part" > "$d/part.$k"
			joined="$joined$part"$'\n'
			k=$((k + 1))
		done
		if [ "$(printf "%s" "$joined" | sort)" = "$list" ]; then
			echo "gate: $shardpkg: $n tests in $shards shards"
			others=$(printf "%s\n" $first $rest | grep -Fvx "$shardpkg" || true)
			k=0
			while [ "$k" -lt "$shards" ]; do
				re="^($(paste -sd"|" "$d/part.$k"))\$"
				( go test -count=1 $args -run "$re" "$shardpkg" >"$d/out.$k" 2>&1; echo $? >"$d/rc.$k" ) &
				k=$((k + 1))
			done
			if [ -n "$others" ]; then ( go test -count=1 $args $others >"$d/out.o" 2>&1; echo $? >"$d/rc.o" ) & else echo 0 >"$d/rc.o"; : >"$d/out.o"; fi
			wait
			bad=0
			for t in $(seq 0 $((shards - 1))) o; do
				echo "gate: == shard $t (exit $(cat "$d/rc.$t" 2>/dev/null || echo 1))"
				cat "$d/out.$t" 2>/dev/null
				[ "$(cat "$d/rc.$t" 2>/dev/null || echo 1)" = 0 ] || bad=1
			done
			rm -rf "$d"
			exit $bad
		fi
		echo "gate: the shards of $shardpkg do not add up to its test list; running it unsharded" >&2
		rm -rf "$d"
	else
		echo "gate: no test list for $shardpkg; running it unsharded" >&2
	fi
fi
go test -count=1 $args $first $rest
EOS
	printf '%s' "${tpl//@ARGS@/$1}"
}
# snap hashes the tracked changes and the untracked files, to see that a step left the tree as it found it.
snap_def='snap() { { git diff HEAD --binary; git ls-files -o --exclude-standard -z | xargs -0 shasum 2>/dev/null || true; } | shasum; }'

[ "${GATE_SELFTEST_FAIL:-0}" != 1 ] || step selftest 'a step that fails (GATE_SELFTEST_FAIL)' false

# What a change affects, computed once and only when a conditional step asks. Every answer fails closed: when the
# affected packages or the changed files cannot be computed, a step is planned. aff_has_pkg PKG..., aff_has_marker NAME
# (a "# NAME" line of hack/affected-tests.sh) and docs_changed succeed when the step has to run.
# has_line TEXT GREP-ARGS...: grep -q over TEXT, fed from a here-string rather than a pipe. Under pipefail a pipe into
# grep -q fails with SIGPIPE when grep stops reading before the writer is done (a long change list), which would read
# as "no match" and skip a step; and grep's own errors (status 2 and up) count as a match, so detection fails closed.
has_line() { local text=$1 rc=0; shift; grep -q "$@" <<<"$text" || rc=$?; [ "$rc" != 1 ]; }
aff_state=
aff_raw=
chg_state=
chg=
affected_load() {
	[ -z "$aff_state" ] || return 0
	if aff_raw=$("${GATE_AFFECTED_CMD:-hack/affected-tests.sh}" "$base" HEAD 2>/dev/null); then aff_state=ok; else aff_state=failed; fi
	if chg=$(git diff --name-only "$base" HEAD 2>/dev/null); then chg_state=ok; else chg_state=failed; fi
}
aff_has_pkg() { # aff_has_pkg PATH...: one of the module-relative packages is affected
	affected_load
	[ "$aff_state" = ok ] || return 0
	local p
	for p in "$@"; do has_line "$aff_raw" -x "syntaxir.dev/sxir/$p" && return 0; done
	return 1
}
aff_has_marker() { affected_load; [ "$aff_state" = ok ] || return 0; has_line "$aff_raw" -x "# $1"; }
docs_changed() { affected_load; [ "$chg_state" = ok ] || return 0; has_line "$chg" -E '^(docs/|spec/|site/|README\.md$)'; }
# skipstep NAME REASON: a step that the gate leaves out, listed in the plan and the summary as "NAME skipped (REASON)".
skipstep() { step "$1" "skipped ($2)" ":skip:$2"; }

want() { local l; for l in "${levels[@]}"; do [ "$l" = "$1" ] && return 0; done; return 1; }

# auto (see the header): it replaces itself in the levels by the levels it chooses.
GATE_ENGINE_PATHS='^(internal/runtime/|pkg/runtime/|internal/backend/|internal/testing/codegentest/)'
if want auto; then
	affected_load
	chosen=()
	if [ "$aff_state" != ok ] || [ "$chg_state" != ok ]; then
		chosen=(docs engine)
		why='the changes or the affected packages could not be computed'
	else
		docs_changed && chosen+=(docs)
		if has_line "$chg" -E "$GATE_ENGINE_PATHS"; then
			chosen+=(engine)
			why='a change under internal/runtime/, pkg/runtime/, internal/backend/ or internal/testing/codegentest/'
		elif has_line "$aff_raw" -x -e '# generate' -e '# alpha'; then
			chosen+=(engine)
			why='generation or alpha-parsers is affected'
		elif has_line "$aff_raw" '^[^#]'; then
			chosen+=(code)
			why='packages are affected'
		else
			why='no package is affected'
		fi
		[ ${#chosen[@]} -gt 0 ] || chosen=(docs)
	fi
	rest=()
	for l in "${levels[@]}"; do [ "$l" = auto ] || rest+=("$l"); done
	levels=(${rest[@]+"${rest[@]}"} "${chosen[@]}")
	echo "gate: auto chose ${chosen[*]} ($why)" >&2
fi

if want docs; then
	step vet 'go vet ./...' 'go vet ./...'
	if docs_changed; then
		step site-test 'cd site && go test ./...' 'cd site && go test -count=1 ./...'
		step site-build 'site/build.sh -wasm=false (builds into a temporary directory, checks links)' 'out=$(cd -P "$(mktemp -d "${TMPDIR:-/tmp}/gate-site.XXXXXX")" && pwd) && trap "rm -rf \"$out\"" EXIT && site/build.sh -out "$out/dist" -wasm=false'
	else
		skipstep site-test "no change under docs/, spec/, site/ or README.md since $base"
		skipstep site-build "no change under docs/, spec/, site/ or README.md since $base"
	fi
fi
if want code || want engine; then
	step fmt 'gofmt -l over every tracked Go file (none may be listed)' 'files=$(git ls-files -z "*.go" | xargs -0 -r gofmt -l); [ -z "$files" ] || { printf "%s\n" "$files"; false; }'
	step build 'go build ./...' 'go build ./...'
	step vet 'go vet ./...' 'go vet ./...'
	if ! want engine && ! want full && ! want all; then
		step test 'go test of the affected packages (internal/backend/codegen/golang/print in shards when affected)' "$(affected_cmd '-timeout 45m')"
	fi
fi
if want engine || want full || want all; then
	if want all; then
		step test-all 'go test -tags sxir_long ./... (the whole tree, the longest packages first)' 'go test -count=1 -tags sxir_long -timeout 90m $GATE_HEAVY ./...' beside
	else
		step test-long 'go test -tags sxir_long of the affected packages (internal/backend/codegen/golang/print in shards when affected)' "$(affected_cmd '-tags sxir_long -timeout 90m')" beside
	fi
fi
if want engine || want all; then
	if aff_has_marker alpha; then
		step alpha-parsers 'hack/test-alpha-parsers.sh' hack/test-alpha-parsers.sh beside
	else
		skipstep alpha-parsers "neither the generator, a grammar nor a runtime library is affected since $base"
	fi
	if aff_has_pkg internal/runtime/engine internal/runtime/state internal/runtime/document; then
		step reference-tags 'hack/test-reference-tags.sh (tags 4 at a time)' hack/test-reference-tags.sh beside
	else
		skipstep reference-tags "internal/runtime/engine, internal/runtime/state and internal/runtime/document are not affected since $base"
	fi
	if aff_has_marker generate; then
		step generate 'go generate ./internal/backend/... ./internal/runtime/state/sharedsrc ./bench ./internal/frontend/parser ./spec/language/...; the tree must not change' "$snap_def"'
before=$(snap)
go generate ./internal/backend/... ./internal/runtime/state/sharedsrc ./bench ./internal/frontend/parser ./spec/language/...
[ "$(snap)" = "$before" ] || { echo "gate: go generate changed the tree; commit the regenerated files" >&2; git status --short >&2; exit 1; }'
	else
		skipstep generate "no generator, generator input or generated file is affected since $base"
	fi
fi

isskip() { [ "${cmds[$1]#:skip:}" != "${cmds[$1]}" ]; }
# group_end I: the index after the group that starts at step I. The steps marked beside that are next to each other in
# the plan (steps left out in between do not separate them) form one group; GATE_PARALLEL=0 makes every step its own.
group_end() {
	local j=$(($1 + 1))
	if [ "${GATE_PARALLEL:-1}" != 0 ] && [ "${besides[$1]}" = 1 ]; then
		while [ "$j" -lt "${#names[@]}" ] && { [ "${besides[$j]}" = 1 ] || isskip "$j"; }; do j=$((j + 1)); done
	fi
	echo "$j"
}

if [ "$dry" = 1 ]; then
	echo "gate: base $base; steps:"
	i=0
	for n in "${names[@]}"; do printf '  %-15s %s\n' "$n" "${descs[$i]}"; i=$((i + 1)); done
	i=0
	while [ "$i" -lt "${#names[@]}" ]; do
		j=$(group_end "$i")
		run=
		k=$i
		while [ "$k" -lt "$j" ]; do isskip "$k" || run="$run ${names[$k]}"; k=$((k + 1)); done
		case $run in *" "*" "*) echo "gate: these steps run at the same time:$run" ;; esac
		i=$j
	done
	case " ${names[*]} " in *" test"* | *" test-long "* | *" test-all "*) echo "gate: affected packages since $base:"; "${GATE_AFFECTED_CMD:-hack/affected-tests.sh}" "$base" HEAD | sed 's/^/  /' ;; esac
	if [ "${GATE_SHARDS:-2}" -gt 1 ] && case " ${names[*]} " in *" test"* | *" test-long "*) true ;; *) false ;; esac && affected_load && [ "$aff_state" = ok ] && has_line "$aff_raw" -x syntaxir.dev/sxir/internal/backend/codegen/golang/print; then
		echo "gate: internal/backend/codegen/golang/print is affected: its tests run in ${GATE_SHARDS:-2} concurrent shards (go test -list), beside the other packages"
	fi
	exit 0
fi

git rev-parse --verify --quiet "$base^{commit}" >/dev/null || { echo "gate: base $base is not a revision" >&2; exit 2; }
if [ -n "$(git status --porcelain)" ]; then echo "gate: warning: the tree has uncommitted changes; the changed packages are those of $base..HEAD" >&2; fi
export GATE_BASE=$base
# Mutant runners skip when what they depend on is unchanged since the base (internal/testing/mutanttest).
export SXIR_MUTANTS_BASE=$base
: >"$summary"
# A group of steps (group_end) runs the first step in the foreground, with its output as it comes, the others at the same time with their
# output kept in a file and printed under a heading when the group has ended. Each step of a group records its own
# exit code and seconds, so the seconds of a group overlap. Every step of a group runs to its end; a failure stops the
# gate after the group.
gdir=$(mktemp -d "${TMPDIR:-/tmp}/gate-steps.XXXXXX")
# shellcheck disable=SC2329 # killtree runs from the trap.
# On an interrupt, stop the steps that run in the background (and their children, which ignore SIGINT as background
# commands of a non-interactive shell do) before exiting.
killtree() { local c; for c in $(pgrep -P "$1" 2>/dev/null); do killtree "$c"; done; kill -TERM "$1" 2>/dev/null || true; }
bg_pids=()
trap 'for p in ${bg_pids[@]+"${bg_pids[@]}"}; do killtree "$p"; done; rm -rf "$gdir"; exit 130' INT TERM
trap 'rm -rf "$gdir"' EXIT
failed=0
total=${#names[@]}
i=0
while [ "$i" -lt "$total" ]; do
	j=$(group_end "$i")
	lead=
	bg_pids=()
	others=
	k=$i
	while [ "$k" -lt "$j" ]; do
		if isskip "$k"; then
			echo "gate: ${names[$k]} skipped (${cmds[$k]#:skip:})" >&2
		elif [ -z "$lead" ]; then
			lead=$k
		else
			others="$others ${names[$k]}"
			( s=$SECONDS; r=0; nice -n "$nice_n" bash -c "${cmds[$k]}" >"$gdir/$k.out" 2>&1 || r=$?; echo "$r $((SECONDS - s))" >"$gdir/$k.rc" ) &
			bg_pids+=("$!")
		fi
		k=$((k + 1))
	done
	if [ -n "$lead" ]; then
		if [ -n "$others" ]; then echo "gate: == ${names[$lead]} (beside:$others)" >&2; else echo "gate: == ${names[$lead]}" >&2; fi
		start=$SECONDS
		rc=0
		nice -n "$nice_n" bash -c "${cmds[$lead]}" || rc=$?
		echo "$rc $((SECONDS - start))" >"$gdir/$lead.rc"
		for p in ${bg_pids[@]+"${bg_pids[@]}"}; do wait "$p" || true; done
		bg_pids=()
	fi
	stop=0
	k=$i
	while [ "$k" -lt "$j" ]; do
		n=${names[$k]}
		if isskip "$k"; then
			printf '%s\tskipped (%s)\n' "$n" "${cmds[$k]#:skip:}" >>"$summary"
			k=$((k + 1))
			continue
		fi
		if [ "$k" != "$lead" ]; then
			echo "gate: == $n (output; it ran beside ${names[$lead]})" >&2
			cat "$gdir/$k.out" 2>/dev/null || true
		fi
		read -r rc secs <"$gdir/$k.rc" 2>/dev/null || { rc=1; secs=0; echo "gate: $n left no exit status" >&2; }
		printf '%s\texit %d\t%ds\n' "$n" "$rc" "$secs" >>"$summary"
		if [ "$rc" -ne 0 ]; then
			failed=1
			[ "$keep" = 1 ] || { echo "gate: $n failed (exit $rc); stopping" >&2; stop=1; }
		fi
		k=$((k + 1))
	done
	[ "$stop" = 0 ] || break
	i=$j
done
echo "gate: summary ($summary):"
cat "$summary"
if [ "$failed" = 0 ]; then echo "gate: ok"; else echo "gate: FAILED"; fi
exit "$failed"
