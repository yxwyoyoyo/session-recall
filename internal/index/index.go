package index

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

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
		);`)
	return err
}

func (s *Store) Rebuild() error {
	_, err := s.db.Exec(`DELETE FROM session_fts; DELETE FROM sessions; VACUUM;`)
	return err
}

func (s *Store) Known(provider string) (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT source, MAX(stamp) FROM sessions WHERE provider = ? GROUP BY source`, provider)
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
			INSERT INTO sessions(provider, id, title, directory, updated_at, source, stamp)
			VALUES(?, ?, ?, ?, ?, ?, ?)
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

var searchToken = regexp.MustCompile(`[\p{L}\p{N}_-]+`)

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
		conditions = append(conditions, "s.provider = ?")
		args = append(args, filters.Provider)
	}
	if filters.CWD != "" {
		conditions = append(conditions, "s.directory = ?")
		args = append(args, filters.CWD)
	}
	args = append(args, filters.Limit)
	querySQL := `
		SELECT s.provider, s.id, s.title, s.directory, s.updated_at, s.source,
		       snippet(session_fts, 4, '[', ']', ' … ', 18), bm25(session_fts)
		FROM session_fts
		JOIN sessions s ON s.provider = session_fts.provider AND s.id = session_fts.session_id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY bm25(session_fts), s.updated_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMatches(rows, true)
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
