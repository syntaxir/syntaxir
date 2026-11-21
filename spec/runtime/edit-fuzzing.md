# Edit Fuzzing

An incremental [Document](documents.md) reuses the work of earlier parses, and it MUST give the result that a parse of
its current text from scratch gives. The `editfuzz` package and the `sxir edit-fuzz` command test that for a grammar: they
apply sequences of edits to a Document, compare it with a fresh parse, and save a failing sequence as a file that
replays the failure exactly. This chapter defines the checks, the operations and the Go API. The file that holds a
sequence is defined in [Edit Sequences](test-formats.md#edit-sequences).

> The Go API of this chapter and the sequence format are experimental and MAY change incompatibly. Which sequences a
> seed generates MAY change between versions.

## Scope and Terminology

A **sequence** is a replayable edit history for one grammar, start rule, backend and position unit: the initial text,
then **operations**. An operation is an `edit`, which replaces a range of the current text; an `undo`, which restores the
text before the latest edit that is not undone; a `redo`, which applies the latest undone edit again; or a
`checkpoint`. A **checkpoint** parses the Document and compares the result with a parse of the Document's current text
from scratch, the **fresh parse**. The initial text is checked as a checkpoint too. A **backend** is the parser that the
Documents and the fresh parses run on: `closure`, the reference parser; `bytecode`, the bytecode virtual machine that
uses Go recursion for rule calls; and `bytecode-iterative`, the one that keeps rule calls on its own stack (see
[Parsing](parsing.md)).

## Contract

### Entry Points

| | Command line | Go (`syntaxir.dev/sxir/pkg/editfuzz`) |
|:--|:--|:--|
| Generate sequences and check them | `sxir edit-fuzz -g g.sxir` | `editfuzz.Fuzz(src, opts)` |
| Replay a saved sequence | `-replay file` | `editfuzz.Load(path)` and `editfuzz.Replay(src, seq)` |
| Minimize a failing sequence | `-minimize` | `editfuzz.Minimize(src, seq, maxAttempts)` or `Options.Minimize` |
| Save a sequence | `-save-failures dir` | `editfuzz.Save(dir, seq)` or `Options.SaveFailures` |

`src` is the grammar source, in grammar syntax, as bytes. A sequence records the SHA-256 hash of those exact bytes, which
`editfuzz.GrammarHash(src)` returns as 64 lowercase hexadecimal digits.

### Sequences

An `editfuzz.Sequence` has these members; the file format is that of [Edit Sequences](test-formats.md#edit-sequences),
whose version `editfuzz.Version` is.

| Member | Meaning |
|:--|:--|
| `GrammarSHA256` | The hash of the exact bytes of the grammar source. A replay refuses other sources |
| `Start` | The start rule; required |
| `Backend` | `editfuzz.Closure`, `editfuzz.Bytecode` or `editfuzz.BytecodeIterative`, with the values `closure`, `bytecode` and `bytecode-iterative`. The zero value is invalid in a sequence. `ParseBackend(name)` returns the backend of a name and an error for another |
| `Unit` | The position unit of the Documents, the fresh parses and the edits: `input.CodePoints` (the zero value) or `input.Bytes` |
| `Limits` | The `runtime.Limits` of the Documents and the fresh parses of a replay. The file does not record them, so a replay supplies them again; the zero value selects the defaults |
| `Seed` | The seed that generated the sequence. It is provenance only: a replay does not use it |
| `Initial` | The initial text, as bytes, which MAY be invalid UTF-8 |
| `Operations` | The operations, in order |

An `editfuzz.Operation` has a `Kind` (`editfuzz.Edit`, `Undo`, `Redo` or `Checkpoint`, with the values `edit`, `undo`,
`redo` and `checkpoint`) and, for an edit, `Start`, `End` and `Text`: the range `[Start, End)` of the text just before the
operation, in the unit of the sequence, and the replacement as bytes. The other kinds have no range and no text. The
JSON of an operation has exactly the members of its kind, and a reader rejects the others.

An edit has the effect that `Document.Edit` defines (see [Incremental Documents](documents.md)). In the unit
`codepoints` a Document holds decoded text, in which each invalid byte is U+FFFD, and the text that an edit makes is
the one that this decoding gives. An undo restores the text before the latest edit that is not undone, whatever
characters the edit joined or split. A new edit discards the edits that can be redone.

### Checks

A replay compares the Document with the fresh parse of its current text at the initial text and at each checkpoint, in
the values with their spans and in the errors with their positions, expectations and messages. After each edit, undo and
redo it also compares the Document's text with the text that the operation should make.

If a Document parse or its fresh parse exhausts its compute limit, the check compares nothing: a Document reuses earlier work
and spends less than a fresh parse, so an exhausted compute limit says nothing about the results. A check in which both complete
is made as usual.

### Failures

A check that fails is an `editfuzz.Mismatch`. It has these members.

| Member | Meaning |
|:--|:--|
| `Backend`, `Unit` | Those of the sequence |
| `Event` | The index of the operation, or -1 for the initial text |
| `Text` | The text of the Document at a checkpoint, or before the operation that changed it wrongly or panicked |
| `Path` | The first differing location of the compared results, such as `value.value[1].end` or `error.errors[0].expected`, or `value_invalid_utf8[i]` for strings that are not valid UTF-8; `text` for a wrong text, or for an undo or redo that the Document rejected; `panic.edit`, `panic.document`, `panic.fresh` or `panic.both` for a panic in an operation, in the Document parse, in the fresh parse or in both |
| `Document`, `Fresh` | The differing values at `Path` as JSON: those of the Document and of the fresh parse; the text of the Document and the expected text; or the panic values, empty for none |

`Mismatch.Category()` is the first element of `Path`: `value` (also for `value_invalid_utf8`), `error`, `text` or
`panic`. `Mismatch.Fingerprint()` identifies the kind of failure independently of the input: the category, the backend,
the unit and `Path` with its list indexes removed. `Mismatch` implements `error`.

An `editfuzz.InvalidError` reports a sequence that cannot be applied: its `Event` is the index of the operation, or -1
for the sequence as a whole, and `Err` is the reason. The reasons are a grammar that does not match the recorded hash or
does not compile, a Document that cannot be made of the initial text, an unknown backend or unit, an edit outside the text or off a character boundary, and an undo or redo
without an edit to undo or redo. An `InvalidError` is not a `Mismatch`: it is a problem of the sequence, and not of the
grammar.

### Generating Sequences

`editfuzz.Fuzz(src, opts)` runs generated sequences. For each of `opts.Sequences` sequences it chooses an initial text,
generates `opts.Operations` operations, the last of which is a checkpoint, and applies them to a Document, checking as it
goes. It stops at the first failure. It returns nil if every check passed, a `*editfuzz.Failure` for the first sequence
that failed, and another error for a grammar that does not compile, an unknown backend or unit, or a failure directory
that is unusable.

| Option | Meaning |
|:--|:--|
| `Start` | The start rule. Empty selects the default start rule of the grammar |
| `Backend` | The backend. The zero value selects `closure` (unlike a `Sequence`, where it is invalid) |
| `Unit` | The position unit. The zero value is `codepoints` |
| `Limits` | The limits of the Documents and the fresh parses |
| `Seed` | The seed of the generated sequences. The same seed, options and version, with the same Go release, generate the same sequences; the saved sequence, not the seed, is the reproducer |
| `Corpus` | The initial texts, which the seed selects from in order. Without a corpus the initial texts are the empty text and inputs that [Input Generation](sampling.md) generates from the grammar with the seed. In some sequences several texts are joined into a text of a few kilobytes |
| `Sequences` | The number of sequences; zero or less selects `editfuzz.DefaultSequences`, 100 |
| `Operations` | The number of operations of each sequence; zero or less selects `editfuzz.DefaultOperations`, 32 |
| `SaveFailures` | A directory where a failing sequence is saved, created if it does not exist |
| `Minimize`, `MaxAttempts` | Minimize a sequence that fails with a `Mismatch`, after saving the original, with at most `MaxAttempts` replays (`editfuzz.DefaultMaxAttempts`, 2000, if zero or less), and save the result too |

The generated edits include insertions, deletions and replacements at the start, the end and elsewhere, with
replacements that join, split and complete characters with their neighbors and that are invalid UTF-8. The edits that
the generator makes are on character boundaries, so a `Document` that rejects one is a failure of the Document.

A `Failure` has these members: `Err`, a `*Mismatch`, or an `*InvalidError` for an edit that the Document rejected
although its range was on character boundaries; `Sequence`; `Path`, the file where the sequence was saved, or empty;
and `Minimized` and `MinimizedPath` for the minimized sequence and its file, if `Options.Minimize` was set and `Err` is
a `Mismatch`. Its `Error` message is that of `Err`, the paths of the files and, when the sequence was not saved, the
sequence as JSON. It unwraps to `Err`.

### Replaying and Minimizing

`editfuzz.Replay(src, seq)` applies the operations of `seq` to a Document of the grammar `src` and checks at the
initial text and at each checkpoint. It returns nil if all checks pass, a `*Mismatch` for the first that does not, and an
`*InvalidError` if the sequence cannot be applied. It does not use `seq.Seed`.

`editfuzz.Minimize(src, seq, maxAttempts)` shortens a sequence that fails with a `Mismatch` on `src`, while it fails with
the same fingerprint. It removes operations, then characters of the initial text, then bytes of replacements, and
turns edits into insertions, also at the start of the text, or deletions. A candidate that cannot be applied is
discarded. It replays at most `maxAttempts` candidates (`editfuzz.DefaultMaxAttempts` if zero or less) and returns the
shortest failing sequence it found with its mismatch. `seq` is not modified. It returns an error if the sequence
does not fail, and the error of the replay, such as an `InvalidError`, if it cannot be applied.

### Saving and Loading

`editfuzz.Save(dir, seq)` writes the sequence as a file in `dir`, which it creates if needed, named
`editfuzz-` followed by the first 16 digits of the hash of the contents and `.json`, and returns its path. The file appears
complete or not at all (unless the file system lacks hard links) and an existing file is never replaced: if the file
exists with the same contents `Save` returns its path, and otherwise an error. `editfuzz.Load(path)` reads a sequence
that was saved; its errors name the file.

### The Command

`sxir edit-fuzz -g g.sxir` fuzzes the grammar `g.sxir` (SyntaxIR source) on each backend of `-backend` (default `closure`,
`bytecode` and `bytecode-iterative`) and each unit of `-unit` (default `codepoints` and `bytes`), with `-sequences` and
`-operations`, `-seed` and `-s`. `-corpus` names a file, or a directory whose files are read in the order of their
names, with the initial texts; hidden files (names that start with a dot) and files that are not regular are skipped. A negative `-sequences`, `-operations` or `-max-attempts` is an error. `-save-failures` names the directory for failing sequences, and is required with
`-minimize`; `-max-attempts` bounds the replays of minimizing. `-replay file` replays a saved sequence instead of
generating sequences; with `-minimize` it minimizes the sequence, and saves the result in `-save-failures` or by default
in the directory of the file. With `-replay`, the flags that give limits (`-max-compute`, `-max-memory`, `-max-input` and `-max-depth`) and `-max-attempts` apply. A failure ends the command with the message of the `Failure`.

## Validation and Errors

| Condition | Result |
|:--|:--|
| The Document's result differs from a fresh parse at a check | A `*Mismatch` |
| An edit that gives another text than it should, or a panic | A `*Mismatch` with the category `text` or `panic` |
| A replay with a source whose hash differs from the sequence | An `*InvalidError` with `Event` -1 |
| A sequence with an unknown backend or unit, an edit outside the text or off a character boundary, an undo or redo with nothing to undo or redo | An `*InvalidError` |
| A sequence file with a missing, unknown or mistyped member, or an unknown version, backend or unit | An error from `Load`, before any operation runs |
| `Save` where the file exists with other contents | An error |
| `Minimize` of a sequence that does not fail | An error |

## Compatibility and Support

The package and the command need the grammar source, and a compiled grammar cannot be fuzzed. Both are part of the Go
API and the `sxir` command, and not of generated parsers. The sequence format is at version 1 and a reader supports only
the versions that it names.

## Conformance

The tests of the `editfuzz` package establish the checks, the mismatch categories and paths on injected faults, the
determinism of generated sequences, minimizing, the round trip of sequence files and the atomicity of saving them.
