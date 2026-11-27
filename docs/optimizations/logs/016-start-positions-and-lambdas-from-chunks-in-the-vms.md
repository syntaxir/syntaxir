# 16. Start Positions and Lambdas from Chunks in the VMs

*Note:* the VMs no longer run the match instructions that this change measured (`PUSHPOS`, `EFUNC`, `SEQ`, `ATOMIC`,
`ENDREPEAT`), nor the chunks that served them: every VM body is now a Flow body selected from Code IR, which keeps start
positions in registers. The record stays as the measurement of the change.

- `PUSHPOS` pushed the start position of a sequence, repetition or atomic expression onto the value stack (`[]any`),
  and converting an `int` to an interface allocates for values above 255. It now pushes a `*int` taken from a chunk
  of the machine; the consumers (`SEQ`, `ATOMIC`, `ENDREPEAT`) dereference it.
- `EFUNC` allocated each `vmFunc`; lambdas never outlive the evaluation, and they now come from a chunk of
  the machine too.
- Effect (min of 6 interleaved runs, codepoints, Apple M3 Max): allocations per parse JSON 32k → 1.9k, CSV 41k → 1.2k,
  XML 47k → 19k, minilang 14k → 1.4k, for both VMs. Bytes and time are unchanged (within noise); the gain is fewer
  objects for the GC.
- The remaining XML allocations are `text(...)` results: a string converted to an interface needs a header on the
  heap (comparisons of two texts avoid it since change 36).
