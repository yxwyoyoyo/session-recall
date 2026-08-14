# Performance specification

## Objective

`session-try` should feel immediate during normal search even when its source
harnesses contain gigabytes of transcript data. Provider storage is read only;
all interactive queries operate on the compact derived SQLite FTS index.

This structure follows the useful parts of `tobi/try`'s performance approach:
a written contract, synthetic repeatable workloads, allocation reporting, and
an explicit profiling path. Targets are guidelines rather than pass/fail tests
because CI runners and developer machines vary substantially.

## Data flow requirements

### Provider discovery

- Unchanged Codex JSONL files must not be parsed again.
- Unchanged OpenCode sessions must be excluded by `time_updated` before their
  message content is aggregated.
- Claude history may be scanned as one append-oriented file, but its cost must
  remain visible in provider benchmarks.
- A failure in one provider must not block discovery from the others.

### Indexing

- Searchable content is normalized once during indexing, not per keystroke.
- Upserts run in a transaction and update metadata and FTS atomically.
- The index contains titles, directories, and user prompts by default.
- Index size should remain below 5% of the source transcript footprint for the
  default user-prompt-only policy.

### Interactive search

- Search must not read provider transcript files or the OpenCode database.
- Query work is bounded by the derived index and requested result limit.
- Rendering only handles the visible result window.
- One keystroke must trigger at most one FTS query and one rendered frame.

## Performance targets

Targets assume a warm filesystem cache on contemporary developer hardware.

| Operation | Workload | Target |
|---|---:|---:|
| Indexed content search | 1,000 sessions, 50 results | < 10 ms |
| Selective indexed search | 10,000 sessions, 50 results | < 16 ms |
| Worst-case indexed search | 10,000 matching sessions, 50 results | < 50 ms |
| Recent-session query | 10,000 sessions, 50 results | < 10 ms |
| Picker update and render | 50 visible results | < 16 ms |
| No-change live refresh | About 2 GB source data | < 1 s |
| Clean live rebuild | About 500 sessions / 2 GB | < 5 s |
| Clean rebuild peak RSS | About 500 sessions / 2 GB | < 200 MiB |

## Repeatable benchmarks

Run all synthetic benchmarks with allocation reporting:

```sh
mise run bench
```

The suite measures:

- FTS search over 1,000 sessions plus selective and all-matching searches over
  10,000 synthetic sessions
- recent-session lookup over 10,000 sessions
- transactional upsert of 1,000 sessions
- parsing a Codex JSONL transcript with 1,000 user messages
- parsing a Kiro JSONL journal with 1,000 user prompts
- rendering a picker window with 50 visible results

Benchmarks create isolated in-memory databases and temporary fixtures. They do
not read private local harness data.

## Profiling

Capture CPU and heap profiles for the 1,000-session search workload:

```sh
mise run profile-search
mise run profile-picker
go tool pprof .profiles/search.cpu.pprof
go tool pprof .profiles/search.mem.pprof
```

Profiles are generated under `.profiles/` and are ignored by Git.

## Live baseline

Synthetic benchmarks answer whether a code change regressed a specific hot
path. Before a release, also measure a real clean rebuild and incremental
refresh because provider storage layouts and filesystem behavior are material.

Record at least:

- machine CPU, memory, and OS
- source sizes for each provider
- indexed session counts
- clean rebuild wall time and peak RSS
- no-change refresh wall time and peak RSS
- 100 separate search invocations
- generated index size

Do not commit private titles, prompts, session identifiers, or absolute home
paths in performance reports.

## Current optimization priority

The OpenCode discovery query currently aggregates message content before Go
compares session timestamps with the existing index. Move the timestamp filter
into SQL so unchanged sessions do not pay content aggregation cost.
