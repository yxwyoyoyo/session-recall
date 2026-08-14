package index

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yxwyoyoyo/session-try/internal/session"
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
