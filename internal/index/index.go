package index

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

type Store struct {
	db *sql.DB
}

type Filters struct {
	Provider string
	CWD      string
	Limit    int
}

type RefreshStats struct {
	Scanned        int
	Changed        int
	Unchanged      int
	SkippedRecords int
	FailedSources  int
	LastError      string
}

type ProviderState struct {
	ParserRevision int
	LastRefreshAt  time.Time
	RefreshStats
}

func Open(db *sql.DB) (*Store, error) {
	store := &Store{db: db}
	if err := store.createSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) createSchema() error {
	_, err := s.db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA busy_timeout=5000;
		CREATE TABLE IF NOT EXISTS sessions (
			provider TEXT NOT NULL,
			id TEXT NOT NULL,
			title TEXT NOT NULL,
			directory TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			source TEXT NOT NULL,
			stamp INTEGER NOT NULL,
			parser_revision INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (provider, id)
		);
		CREATE INDEX IF NOT EXISTS sessions_updated_idx ON sessions(updated_at DESC);
		CREATE INDEX IF NOT EXISTS sessions_source_idx ON sessions(provider, source);
		CREATE VIRTUAL TABLE IF NOT EXISTS session_fts USING fts5(
			provider UNINDEXED,
			session_id UNINDEXED,
			title,
			directory,
			content,
			tokenize='unicode61'
		);
		CREATE TABLE IF NOT EXISTS provider_state (
			provider TEXT PRIMARY KEY,
			parser_revision INTEGER NOT NULL,
			last_refresh_at INTEGER NOT NULL,
			scanned INTEGER NOT NULL,
			changed INTEGER NOT NULL,
			unchanged INTEGER NOT NULL,
			skipped_records INTEGER NOT NULL,
			failed_sources INTEGER NOT NULL,
			last_error TEXT NOT NULL
		);`)
	if err != nil {
		return err
	}
	return s.ensureParserRevisionColumn()
}

func (s *Store) ensureParserRevisionColumn() error {
	rows, err := s.db.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "parser_revision" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE sessions ADD COLUMN parser_revision INTEGER NOT NULL DEFAULT 0`)
	return err
}

func (s *Store) Rebuild() error {
	_, err := s.db.Exec(`DELETE FROM session_fts; DELETE FROM sessions; DELETE FROM provider_state; VACUUM;`)
	return err
}

func (s *Store) Known(provider string, parserRevision int) (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT source, MAX(stamp) FROM sessions WHERE provider = ? AND parser_revision = ? GROUP BY source`, provider, parserRevision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int64{}
	for rows.Next() {
		var source string
		var stamp int64
		if err := rows.Scan(&source, &stamp); err != nil {
			return nil, err
		}
		result[source] = stamp
	}
	return result, rows.Err()
}

func (s *Store) Upsert(ctx context.Context, sessions []session.Session) error {
	if len(sessions) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range sessions {
		if item.ID == "" || item.Provider == "" {
			continue
		}
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = time.Now()
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO sessions(provider, id, title, directory, updated_at, source, stamp, parser_revision)
			VALUES(?, ?, ?, ?, ?, ?, ?, 0)
			ON CONFLICT(provider, id) DO UPDATE SET
				title=excluded.title, directory=excluded.directory,
				updated_at=excluded.updated_at, source=excluded.source, stamp=excluded.stamp`,
			item.Provider, item.ID, item.Title, item.Directory, item.UpdatedAt.UnixMilli(), item.Source, item.Stamp)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM session_fts WHERE provider = ? AND session_id = ?`, item.Provider, item.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO session_fts(provider, session_id, title, directory, content) VALUES(?, ?, ?, ?, ?)`,
			item.Provider, item.ID, item.Title, item.Directory, item.Content); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ApplyRefresh replaces only sources that decoded successfully. Sources that
// failed parsing are absent from sessions and therefore keep their last-good
// indexed rows.
func (s *Store) ApplyRefresh(ctx context.Context, provider string, parserRevision int, sessions []session.Session, stats RefreshStats) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	bySource := map[string][]session.Session{}
	for _, item := range sessions {
		if item.Provider != provider || item.ID == "" || item.Source == "" {
			continue
		}
		bySource[item.Source] = append(bySource[item.Source], item)
	}
	for source, items := range bySource {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM sessions WHERE provider = ? AND source = ?`, provider, source)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `DELETE FROM session_fts WHERE provider = ? AND session_id = ?`, provider, id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE provider = ? AND source = ?`, provider, source); err != nil {
			return err
		}
		for _, item := range items {
			if item.UpdatedAt.IsZero() {
				item.UpdatedAt = time.Now()
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM session_fts WHERE provider = ? AND session_id = ?`, provider, item.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE provider = ? AND id = ?`, provider, item.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO sessions(provider, id, title, directory, updated_at, source, stamp, parser_revision)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, item.Provider, item.ID, item.Title, item.Directory,
				item.UpdatedAt.UnixMilli(), item.Source, item.Stamp, parserRevision); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO session_fts(provider, session_id, title, directory, content) VALUES(?, ?, ?, ?, ?)`,
				item.Provider, item.ID, item.Title, item.Directory, item.Content); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider_state(provider, parser_revision, last_refresh_at, scanned, changed, unchanged, skipped_records, failed_sources, last_error)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider) DO UPDATE SET
			parser_revision=excluded.parser_revision, last_refresh_at=excluded.last_refresh_at,
			scanned=excluded.scanned, changed=excluded.changed, unchanged=excluded.unchanged,
			skipped_records=excluded.skipped_records, failed_sources=excluded.failed_sources,
			last_error=excluded.last_error`, provider, parserRevision, time.Now().UnixMilli(), stats.Scanned,
		stats.Changed, stats.Unchanged, stats.SkippedRecords, stats.FailedSources, stats.LastError); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ProviderStates() (map[string]ProviderState, error) {
	rows, err := s.db.Query(`SELECT provider, parser_revision, last_refresh_at, scanned, changed, unchanged, skipped_records, failed_sources, last_error FROM provider_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]ProviderState{}
	for rows.Next() {
		var name string
		var refreshed int64
		var state ProviderState
		if err := rows.Scan(&name, &state.ParserRevision, &refreshed, &state.Scanned, &state.Changed, &state.Unchanged,
			&state.SkippedRecords, &state.FailedSources, &state.LastError); err != nil {
			return nil, err
		}
		state.LastRefreshAt = time.UnixMilli(refreshed)
		result[name] = state
	}
	return result, rows.Err()
}

var searchToken = regexp.MustCompile(`[\p{L}\p{N}_-]+`)

// fallbackToken splits on the same characters as FTS5's default unicode61
// tokenizer, which treats '-' and '_' as separators. searchToken keeps them
// inside one token for the ftsQuery phrase, so term-presence fallback checks
// must use this narrower form or hyphenated queries never match.
var fallbackToken = regexp.MustCompile(`[\p{L}\p{N}]+`)

func ftsQuery(query string) string {
	tokens := searchToken.FindAllString(query, -1)
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.ReplaceAll(token, `"`, `""`)
		parts = append(parts, `"`+token+`"*`)
	}
	return strings.Join(parts, " AND ")
}

func (s *Store) Search(ctx context.Context, query string, filters Filters) ([]session.Match, error) {
	if filters.Limit <= 0 {
		filters.Limit = 50
	}
	if strings.TrimSpace(query) == "" {
		return s.recent(ctx, filters)
	}
	match := ftsQuery(query)
	if match == "" {
		return s.recent(ctx, filters)
	}
	conditions := []string{"session_fts MATCH ?"}
	args := []any{match}
	if filters.Provider != "" {
		conditions = append(conditions, "session_fts.provider = ?")
		args = append(args, filters.Provider)
	}
	if filters.CWD != "" {
		conditions = append(conditions, "session_fts.directory = ?")
		args = append(args, filters.CWD)
	}
	args = append(args, filters.Limit)
	// Rank and snippet in one pass: the auxiliary snippet() must run inside
	// an FTS match context to get match spans, so MATCH stays in this query.
	// The alternative two-level form (ranked subquery, outer MATCH refetch)
	// re-iterated every matching rowid to evaluate snippet() on the limited
	// top rows and measured slower in a best-of-3 benchmark on a synthetic
	// 10k-session DB: 31.5 vs 20.6 ms at ~10k matches per query, 254.7 vs
	// 229.7 ms with 500 matching tokens per document.
	querySQL := `
		SELECT s.provider, s.id, s.title, s.directory, s.updated_at, s.source,
		       snippet(session_fts, 4, '[', ']', ' … ', 18), bm25(session_fts)
		FROM session_fts
		JOIN sessions s ON s.provider = session_fts.provider AND s.id = session_fts.session_id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY bm25(session_fts) ASC, s.updated_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches, err := scanMatches(rows, true)
	if err != nil {
		return nil, err
	}
	// FTS brackets mark only the matched column; the content window can
	// still lack the term when the match landed in the title or directory.
	// Fall back so every snippet displays where the row matched, without
	// trusting bracket presence (literal '[' in content would fake it).
	// Tokens are derived with fallbackToken, mirroring how FTS5's unicode61
	// tokenizer splits the query ("foo/bar" and "foo-bar" both become the
	// terms foo and bar). Note the trade-off: a content window containing a
	// query token (unbracketed, match actually in title/directory) counts as
	// a hit and suppresses the fallback, keeping a still-relevant window.
	tokens := FallbackTokens(query)
	// Strip the FTS markers once so content windows and fallback text (plain
	// titles or directories, which may contain literal '[') display alike.
	for i := range matches {
		matches[i].Snippet = StripBrackets(matches[i].Snippet)
	}
	for i := range matches {
		if containsAny(matches[i].Snippet, tokens) {
			continue
		}
		switch {
		case containsAny(matches[i].Title, tokens):
			matches[i].Snippet = matches[i].Title
		case containsAny(matches[i].Directory, tokens):
			matches[i].Snippet = matches[i].Directory
		}
	}
	return matches, nil
}

// FallbackTokens splits a query into the terms FTS5's unicode61 tokenizer
// would produce for it (hyphen and underscore act as separators, diacritics
// are folded). Shared by the snippet fallback in Search and by the UI's
// query-term highlighting.
func FallbackTokens(query string) []string {
	return fallbackToken.FindAllString(FoldDiacritics(strings.ToLower(query)), -1)
}

// containsAny reports whether any query token occurs in text,
// case-insensitively and with diacritics folded, like FTS matching.
func containsAny(text string, tokens []string) bool {
	lower := FoldDiacritics(strings.ToLower(text))
	for _, tok := range tokens {
		if tok != "" && strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

// FoldDiacritics normalizes text to NFD and drops combining marks, so
// "café" and "cafe" compare equal the way unicode61 remove_diacritics
// matches them. Exported for the UI's query-term highlighting.
func FoldDiacritics(text string) string {
	nf := norm.NFD.String(text)
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, nf)
}

// StripBrackets removes the snippet context markers emitted by snippet().
// Exported so the UI can display snippets without duplicating this.
func StripBrackets(text string) string {
	return strings.NewReplacer("[", "", "]", "").Replace(text)
}

func (s *Store) recent(ctx context.Context, filters Filters) ([]session.Match, error) {
	conditions := []string{"1=1"}
	var args []any
	if filters.Provider != "" {
		conditions = append(conditions, "provider = ?")
		args = append(args, filters.Provider)
	}
	if filters.CWD != "" {
		conditions = append(conditions, "directory = ?")
		args = append(args, filters.CWD)
	}
	args = append(args, filters.Limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, id, title, directory, updated_at, source, '', 0.0
		FROM sessions WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY updated_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMatches(rows, false)
}

func scanMatches(rows *sql.Rows, ranked bool) ([]session.Match, error) {
	result := make([]session.Match, 0)
	for rows.Next() {
		var item session.Match
		var updated int64
		var rank float64
		if err := rows.Scan(&item.Provider, &item.ID, &item.Title, &item.Directory, &updated, &item.Source, &item.Snippet, &rank); err != nil {
			return nil, err
		}
		item.UpdatedAt = time.UnixMilli(updated)
		if ranked {
			item.Score = -rank
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Counts() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT provider, COUNT(*) FROM sessions GROUP BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var provider string
		var count int
		if err := rows.Scan(&provider, &count); err != nil {
			return nil, err
		}
		result[provider] = count
	}
	return result, rows.Err()
}

func FormatAge(updated time.Time, now time.Time) string {
	d := now.Sub(updated)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return updated.Format("2006-01-02")
	}
}

func ProviderNames(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
