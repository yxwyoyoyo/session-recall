# session-try

Find and resume AI coding sessions by what you remember, not by an opaque
session ID.

`session-try` discovers sessions created by Claude Code, Codex, OpenCode, and
Kiro CLI, indexes their titles, original working directories, and user prompts
locally, then presents one searchable terminal picker. Selecting a result
starts the original harness in the original directory.

```text
$ session-try "pane status"

  opencode  Research Zellij plugin API       ~/Tools/zellij-vertical-tab · 2d
> codex     Persist status across reattach   ~/Tools/zellij-vertical-tab · 3d
    …pane status disappears after reattaching the session…
```

## Status

This is an early MVP. It has been exercised against current local stores from:

- Claude Code (`~/.claude/history.jsonl`)
- Codex (`~/.codex/session_index.jsonl` and `~/.codex/sessions`)
- OpenCode (`~/.local/share/opencode/opencode.db`)

Kiro CLI is not installed on the development machine. Its adapter is verified
against captured-format fixtures and the official resume contract:

- Kiro CLI (`$KIRO_HOME/sessions/cli`, defaulting to
  `~/.kiro/sessions/cli`)

The Kiro adapter targets the stable v2 session triplet (`.json`, `.jsonl`, and
optional `.history`). The incompatible Kiro CLI v3 early-access format is not
yet supported.

Provider stores are opened read-only. The derived search index lives at the
platform user-cache location (`~/Library/Caches/session-try/index.db` on
macOS). Only user prompts are indexed; assistant responses and tool output are
not copied into the index.

## Build

The project pins Go through [mise](https://mise.jdx.dev/):

```sh
mise install
mise run check
mise run build
mise run bench
```

The binary is written to `bin/session-try`.

## Usage

```text
session-try                         browse recent sessions
session-try "permission hook"       search session content
session-try --provider codex        show only Codex sessions
session-try --provider kiro         show only Kiro CLI sessions
session-try --cwd                   show sessions for the current directory
session-try --json "query"          return machine-readable results
session-try index                   incrementally update the index
session-try index --rebuild         rebuild all derived index data
session-try doctor                  show provider and index status
```

The index refreshes automatically when the picker starts. JSONL files that
have not changed and OpenCode sessions whose update timestamp has not changed
are skipped.

## Performance

The project includes repeatable Go benchmarks for FTS search at 1,000 and
10,000 sessions, recent-session lookup, transactional indexing, and Codex
JSONL parsing:

```sh
mise run bench
```

CPU and memory profiles for indexed search can be captured with:

```sh
mise run profile-search
mise run profile-picker
```

Performance targets, methodology, and the current live baseline procedure are
documented in [`spec/performance.md`](spec/performance.md). Benchmarks use
synthetic data and never read private harness sessions.

### Search behavior

Search uses SQLite FTS5 with Unicode tokenization and prefix matching. It
matches session titles, directories, and user-prompt content. Results are
ranked by textual relevance and then recency.

### Resume commands

The provider adapters execute the installed harness directly:

```text
claude --resume SESSION_ID
codex resume SESSION_ID -C DIRECTORY
opencode DIRECTORY --session SESSION_ID
kiro-cli chat --resume-id SESSION_ID
```

The opaque identifier remains internal to normal interactive use.

## Development principles

- Never modify a provider's own session data.
- Let one unavailable or broken provider fail independently.
- Keep provider-specific storage formats behind small adapters.
- Test parsers with fixtures rather than a developer's private session data.
- Keep indexed content local and explicit.

## Inspiration

The interaction model is inspired by
[`tobi/try`](https://github.com/tobi/try): fuzzy search, recency-aware results,
and a fast path back into forgotten work. This project does not copy `try`'s
source; it applies that interaction pattern to AI harness sessions distributed
across many working directories.

## License

MIT
