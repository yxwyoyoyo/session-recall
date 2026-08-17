package index

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSearchesContentAndFiltersProvider(t *testing.T) {
	store := testStore(t)
	items := []session.Session{
		{Provider: "codex", ID: "one", Title: "Fix pane status", Directory: "/repo", UpdatedAt: time.Unix(20, 0), Content: "persist across reattach", Source: "one.jsonl", Stamp: 1},
		{Provider: "claude", ID: "two", Title: "Other work", Directory: "/elsewhere", UpdatedAt: time.Unix(10, 0), Content: "unrelated", Source: "history.jsonl", Stamp: 1},
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	matches, err := store.Search(context.Background(), "reatt", Filters{Provider: "codex", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].ID != "one" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if matches[0].Snippet == "" {
		t.Fatal("expected content snippet")
	}
	if matches[0].Source != "one.jsonl" {
		t.Fatalf("source = %q, want one.jsonl", matches[0].Source)
	}
}

func TestEmptySearchReturnsRecentAndEmptySlice(t *testing.T) {
	store := testStore(t)
	matches, err := store.Search(context.Background(), "", Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if matches == nil || len(matches) != 0 {
		t.Fatalf("expected non-nil empty slice, got %#v", matches)
	}
}

func TestParserRevisionRetriesFailuresAndPreservesLastGoodSource(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	old := session.Session{
		Provider: "pi", ID: "one", Title: "Old title", Directory: "/repo",
		UpdatedAt: time.Unix(10, 0), Content: "last good prompt", Source: "session.jsonl", Stamp: 10,
	}
	other := session.Session{
		Provider: "pi", ID: "two", Title: "Other title", Directory: "/repo",
		UpdatedAt: time.Unix(10, 0), Content: "other prompt", Source: "other.jsonl", Stamp: 10,
	}
	if err := store.ApplyRefresh(ctx, "pi", 1, []session.Session{old, other}, RefreshStats{Scanned: 2, Changed: 2}); err != nil {
		t.Fatal(err)
	}
	known, err := store.Known("pi", 1)
	if err != nil || known[old.Source] != old.Stamp {
		t.Fatalf("known v1 = %#v, err=%v", known, err)
	}
	known, err = store.Known("pi", 2)
	if err != nil || len(known) != 0 {
		t.Fatalf("parser revision should force reparse: %#v, err=%v", known, err)
	}

	other.Content = "other prompt updated"
	other.Stamp = 20
	if err := store.ApplyRefresh(ctx, "pi", 2, []session.Session{other}, RefreshStats{
		Scanned: 2, Changed: 1, FailedSources: 1, LastError: "session.jsonl: unknown schema",
	}); err != nil {
		t.Fatal(err)
	}
	matches, err := store.Search(ctx, "last good", Filters{Provider: "pi", Limit: 10})
	if err != nil || len(matches) != 1 || matches[0].Title != "Old title" {
		t.Fatalf("last-good data was not preserved: %#v, err=%v", matches, err)
	}
	known, err = store.Known("pi", 2)
	if err != nil || len(known) != 1 || known[other.Source] != other.Stamp {
		t.Fatalf("failed source should remain eligible for retry: %#v, err=%v", known, err)
	}

	updated := old
	updated.Title = "New title"
	updated.Content = "new compatible prompt"
	updated.Stamp = 20
	if err := store.ApplyRefresh(ctx, "pi", 2, []session.Session{updated}, RefreshStats{Scanned: 1, Changed: 1}); err != nil {
		t.Fatal(err)
	}
	known, err = store.Known("pi", 2)
	if err != nil || len(known) != 2 || known[updated.Source] != updated.Stamp || known[other.Source] != other.Stamp {
		t.Fatalf("known v2 = %#v, err=%v", known, err)
	}
	matches, err = store.Search(ctx, "last good", Filters{Provider: "pi", Limit: 10})
	if err != nil || len(matches) != 0 {
		t.Fatalf("old source content should be replaced: %#v, err=%v", matches, err)
	}
}

func TestOpenMigratesExistingSessionsTableWithParserRevision(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sessions (
		provider TEXT NOT NULL, id TEXT NOT NULL, title TEXT NOT NULL,
		directory TEXT NOT NULL, updated_at INTEGER NOT NULL, source TEXT NOT NULL,
		stamp INTEGER NOT NULL, PRIMARY KEY (provider, id));`); err != nil {
		t.Fatal(err)
	}
	store, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := db.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		found = found || name == "parser_revision"
	}
	if !found {
		t.Fatal("parser_revision column was not migrated")
	}
}
