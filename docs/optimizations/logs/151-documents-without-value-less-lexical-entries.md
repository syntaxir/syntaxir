# 151. Documents Without Value-less Lexical Entries

- **Status**: Rejected
- **Date**: 2026-11-28
- **Mechanisms**: none
- **Result**: A Minilang Document retains 31.7% less, but its edits take 1.1 to 5 times as long on every backend: each reparse matches again the blanks before every statement of the program, where it looked their results up before
- **Revisit when**: A Document reparse no longer visits each element of a long repetition whose elements it reuses, or a narrower class of rules keeps the memory saving with no more units per edit than the memo

The candidate is kept as the signed tag
[`attempt/document-valueless-lexical`](https://github.com/syntaxir/syntaxir/tree/attempt/document-valueless-lexical).

## Problem and Mechanism

A Document stores the result of every call of a memoized or transient rule, so that a later parse can reuse it
([transient rule memo policy](../../design/implementation/transient-rule-memo-policy.md)). Many of those results
belong to rules that only match characters and whose value the caller does not use: blanks, keywords, the characters
of a name. Each costs an entry, its place in the position index, its shift after an edit and a lookup before every
call.

The candidate defined a value-less lexical rule: the value-free twin of a transient rule whose body is made only of
literals, character classes, `.`, anchors, `_`, `_|_`, sequences, choices, options, repetitions, discards,
lookaheads, `#atomic`, captures and calls of such rules, with no action and on no cycle of calls. Documents of every
backend stopped storing and looking up its results and evaluated it at every call, as a parse from scratch does. The
documents specification and the linear-cost proofs were amended for it (the bound carried over unchanged, since such
a call counts what a parse from scratch counts for it), and compiled modules recorded the property, which took a new
instruction set version.

## Reproduction

Timed with the `go` part of `cmd/timed` of the bench repository (`e00bdde`) in a measurement window on an Apple M3 Max
on AC power (100% before and after), 20 interleaved rounds with an A/A arm, the candidate against its parent:

```sh
go run ./cmd/timed -main <checkout> -parts go -rounds 20 -head <candidate> -base <parent> -bench 'DocumentEdit/(CSV|Minilang)/'
```

The counts come from the Document statistics and the compute report of the first parse and of the first 200 edits
of each Minilang trace of the edit-latency benchmarks (100,000 lines), on the closure engine and on the generated Go
Document; the deterministic metrics from `go run ./bench/metrics collect` and `compare`, and the retained memory from
`bench/document-retained` (`GOGC=off GOMAXPROCS=1`, 100,000 lines).

## Results

Candidate against its parent. The A/A geomean is 0.9998 for the mean (accepted), 0.9990 for the median (1 noisy row
of 18) and 0.9759 for the 99th percentile (8 noisy rows of 18, judged alone):

| Workload | Target | Mean | Median | p99 |
|:--|:--|--:|--:|--:|
| CSV, same length | closure, bytecode, iterative | no difference | no difference | noisy or no difference |
| CSV, same length | generated | 0.995 | no difference | noisy |
| CSV, length-changing | closure | 1.009 | 1.012 | no difference |
| CSV, length-changing | bytecode, iterative | no difference | no difference | no difference or noisy |
| CSV, length-changing | generated | 0.979 | 0.964 | 0.964 |
| Minilang, same length | closure | 4.57 | 4.70 | 2.75 |
| Minilang, same length | bytecode | 4.99 | 5.19 | noisy (3.18) |
| Minilang, same length | iterative | 4.41 | 4.53 | noisy (3.27) |
| Minilang, same length | generated | 2.28 | 2.43 | noisy (1.77) |
| Minilang, same length | generated typed | 1.53 | 1.64 | 1.30 |
| Minilang, length-changing | closure | 1.68 | 1.80 | noisy (1.40) |
| Minilang, length-changing | bytecode | 2.05 | noisy (2.18) | 1.72 |
| Minilang, length-changing | iterative | 1.98 | 2.09 | 1.64 |
| Minilang, length-changing | generated | 1.18 | 1.22 | 1.09 |
| Minilang, length-changing | generated typed | 1.06 | no difference | 0.972 |

Every resolved Minilang ratio but one is worse, by up to five times.

The counts name the cause. A Minilang edit of the benchmarks, the same on the engine and on the generated Document:

| Per edit, Minilang | Parent | Candidate |
|:--|--:|--:|
| Same length: units | 31,157 | 168,560 |
| Same length: rule bodies evaluated | 197 | 7,918 |
| Same length: memo hits | 15,430 | 7,711 |
| Length-changing: units | 31,011 | 168,416 |
| Length-changing: rule bodies evaluated | 104 | 7,827 |
| Length-changing: memo hits | 15,435 | 7,714 |

No repetition element is resumed in either. The program is a list of 7,693 functions, matched by
`(-ws s:stmt)*`, and every reparse walks that list again: the parent answers each element with two memo hits, the
blanks `ws` and the statement, about 15,400 per edit. The candidate answers the statement from the memo and matches
the blanks again: about 7,720 more evaluations and 7,720 fewer hits per edit, each evaluation of `ws` costing about 18 units
more than its lookup, which is the 137,400 units, 5.4 times the parent's, that an edit adds. The first parse evaluates
4,195,629 bodies instead of 4,049,461 and counts 7,342,232 units instead of 7,096,055 (3.5% more). CSV edits change by
less than 4% either way.

A Document over 100,000 lines retains 243.3 MB instead of 254.0 MB on CSV (4.2% less; 2.04 times its tree instead of
2.13) and 448.5 MB instead of 656.4 MB on Minilang (31.7% less; 2.45 times its tree instead of 3.58), with 4.1% and
27.1% fewer objects; the trees are unchanged. The generated Go library was 1.2 KB smaller and the playground's
WebAssembly 786 bytes larger compressed.

## Correctness and Tradeoffs

The candidate passed its tests on every backend, with the same units and statistics on the engine and on the
generated Documents. The trade it made is visible in the counts: an entry saved is a lookup replaced by a match, and a
reparse makes that match for every call it does not answer from a reused caller. That is cheap only where the caller
is reused; at the top of a long list, the list rule itself runs again on every edit and visits every element, so the
matches cost on the order of the length of the document per edit.

## Decision and Limits

Rejected: the edit latency of the Documents, whose 99th percentile was to stay no worse, is worse by up to 2.75 times.
The memory saving is real, but it is bought on every edit of a long list.

Revisit when a Document reparse no longer visits each element of a long repetition whose elements it reuses (the
statistics show no resumed element in these traces), since the candidate's matches would then be limited to the
region an edit touches; or with a narrower class of rules that keeps the memory saving while the units per edit stay
at the parent's.
