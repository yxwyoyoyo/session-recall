package index

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yxwyoyoyo/session-try/internal/session"
)

func benchmarkStore(b *testing.B, count int) *Store {
	b.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatal(err)
	}
	// A SQLite :memory: database belongs to one connection. Keeping a single
	// connection also makes benchmark results deterministic.
	db.SetMaxOpenConns(1)
	store, err := Open(db)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })

	items := syntheticSessions(count)
	tx, err := store.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for _, item := range items {
		result, err := tx.Exec(`
			INSERT INTO sessions(provider, id, title, directory, updated_at, source, stamp)
			VALUES(?, ?, ?, ?, ?, ?, ?)`, item.Provider, item.ID, item.Title, item.Directory,
			item.UpdatedAt.UnixMilli(), item.Source, item.Stamp)
		if err != nil {
			b.Fatal(err)
		}
		rowID, err := result.LastInsertId()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := tx.Exec(`
			INSERT INTO session_fts(rowid, provider, session_id, title, directory, content)
			VALUES(?, ?, ?, ?, ?, ?)`, rowID, item.Provider, item.ID, item.Title, item.Directory, item.Content); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return store
}

func syntheticSessions(count int) []session.Session {
	items := make([]session.Session, count)
	providers := []string{"claude", "codex", "opencode"}
	for i := range items {
		provider := providers[i%len(providers)]
		items[i] = session.Session{
			Provider:  provider,
			ID:        fmt.Sprintf("session-%06d", i),
			Title:     fmt.Sprintf("Investigate pane lifecycle issue %d", i),
			Directory: fmt.Sprintf("/workspace/project-%03d", i%200),
			UpdatedAt: time.Unix(int64(1_700_000_000+i), 0),
			Content:   fmt.Sprintf("The pane status should persist after detach and reattach. Synthetic prompt %d with searchable content.", i),
			Source:    fmt.Sprintf("fixture-%06d.jsonl", i),
			Stamp:     1,
		}
	}
	return items
}

func BenchmarkSearch1000(b *testing.B) {
	store := benchmarkStore(b, 1_000)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		matches, err := store.Search(ctx, "pane reattach", Filters{Limit: 50})
		if err != nil {
			b.Fatal(err)
		}
		if len(matches) != 50 {
			b.Fatalf("got %d matches", len(matches))
		}
	}
}

func BenchmarkSearch10000WorstCase(b *testing.B) {
	store := benchmarkStore(b, 10_000)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		matches, err := store.Search(ctx, "pane reattach", Filters{Limit: 50})
		if err != nil {
			b.Fatal(err)
		}
		if len(matches) != 50 {
			b.Fatalf("got %d matches", len(matches))
		}
	}
}

func BenchmarkSearch10000Selective(b *testing.B) {
	store := benchmarkStore(b, 10_000)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		matches, err := store.Search(ctx, "prompt 9999", Filters{Limit: 50})
		if err != nil {
			b.Fatal(err)
		}
		if len(matches) != 1 {
			b.Fatalf("got %d matches", len(matches))
		}
	}
}

func BenchmarkRecent10000(b *testing.B) {
	store := benchmarkStore(b, 10_000)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		matches, err := store.Search(ctx, "", Filters{Limit: 50})
		if err != nil {
			b.Fatal(err)
		}
		if len(matches) != 50 {
			b.Fatalf("got %d matches", len(matches))
		}
	}
}

func BenchmarkUpsert1000(b *testing.B) {
	store := benchmarkStore(b, 0)
	ctx := context.Background()
	items := syntheticSessions(1_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		for n := range items {
			items[n].Stamp = int64(i + 1)
		}
		if err := store.Upsert(ctx, items); err != nil {
			b.Fatal(err)
		}
	}
}
