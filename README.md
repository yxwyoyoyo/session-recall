# session-recall

Find and resume AI coding sessions by what you remember, not by an opaque
session ID.

`session-recall` discovers sessions created by Claude Code, Codex, OpenCode,
Kiro CLI, and Pi, indexes their titles, original working directories, and user
prompts locally, then presents one searchable terminal picker. Selecting a
result starts the original harness in the original directory.

```text
$ session-recall "pane status"

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

Pi is installed on the development machine. Its adapter is verified against
official-format v3 fixtures and the installed CLI's exact-file resume contract:

- Pi (`~/.pi/agent/sessions`, respecting global settings and
  `PI_CODING_AGENT_DIR` / `PI_CODING_AGENT_SESSION_DIR` overrides)

Only Pi user messages are indexed; assistant, tool, and extension content is
left in Pi's source JSONL files.

Provider stores are opened read-only. The derived search index lives at the
platform user-cache location (`~/Library/Caches/session-recall/index.db` on
macOS). Only user prompts are indexed; assistant responses and tool output are
not copied into the index. On first launch, `session-recall` automatically
migrates an existing `session-try` index.

## Install

### mise

Install and activate the latest GitHub release:

```sh
mise use -g github:yxwyoyoyo/session-recall
```

To install without changing your active mise configuration:

```sh
mise install github:yxwyoyoyo/session-recall@latest
```

### Homebrew

```sh
brew install yxwyoyoyo/tap/session-recall
```

### GitHub release

Download a macOS, Linux, or Windows archive from the
[GitHub releases page](https://github.com/yxwyoyoyo/session-recall/releases).
Every release includes SHA-256 checksums.

## Build

The project pins Go through [mise](https://mise.jdx.dev/):

```sh
mise install
mise run check
mise run build
mise run bench
mise run release-snapshot
```

The binary is written to `bin/session-recall`.

## Usage

```text
session-recall                         browse recent sessions
session-recall "permission hook"       search session content
session-recall -p codex                show only Codex sessions
session-recall -p kiro                 show only Kiro CLI sessions
session-recall -p pi                   show only Pi sessions
session-recall -c                      show sessions for the current directory
session-recall -j "query"              return machine-readable results
session-recall -n 20                   limit the number of results
session-recall index                   incrementally update the index
session-recall index --rebuild         rebuild all derived index data
session-recall doctor                  show provider and index status
```

The index refreshes automatically when the picker starts. JSONL files that
have not changed and OpenCode sessions whose update timestamp has not changed
are skipped. Parser revisions force only the affected provider sources to be
decoded again. If a changed source no longer matches a recognized format,
session-recall keeps its last successfully indexed content and reports the
source as degraded in `session-recall doctor`.

Provider compatibility policy, parser revision rules, and fixture guidance are
documented in [`spec/compatibility.md`](spec/compatibility.md).

## Performance

The project includes repeatable Go benchmarks for FTS search at 1,000 and
10,000 sessions, recent-session lookup, transactional indexing, and Codex,
Kiro, and Pi JSONL parsing:

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

### Configuration

Optional TOML rc file at `~/.config/session-recall/rc` (override the path
with `SESSION_RECALL_RC`):

```toml
# Use a dedicated index database (e.g. for experiments or per-project
# isolation); defaults to $XDG_CACHE_HOME/session-recall/index.db.
# Leading ~/ is expanded against your home directory.
database = "~/session-recall-experiments/index.db"
```

Everything still works exactly as before without the file. `session-recall
doctor` prints the active rc path and database location.

### Resume commands

The provider adapters execute the installed harness directly:

```text
claude --resume SESSION_ID
codex resume SESSION_ID -C DIRECTORY
opencode DIRECTORY --session SESSION_ID
kiro-cli chat --resume-id SESSION_ID
pi --session SESSION_FILE
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
