package index

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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

func TestSnippetShowsTitleAndDirectoryMatches(t *testing.T) {
	store := testStore(t)
	items := []session.Session{
		{Provider: "codex", ID: "t1", Title: "Optimize pane layout", Directory: "/repo", UpdatedAt: time.Unix(20, 0), Content: "unrelated filler", Source: "t1.jsonl", Stamp: 1},
		{Provider: "codex", ID: "t2", Title: "Other work", Directory: "/elsewhere", UpdatedAt: time.Unix(10, 0), Content: "unrelated filler", Source: "t2.jsonl", Stamp: 1},
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	titleMatches, err := store.Search(context.Background(), "pane", Filters{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(titleMatches) != 1 || !strings.Contains(strings.ToLower(titleMatches[0].Snippet), "pane") {
		t.Fatalf("title match not surfaced in snippet: %#v", titleMatches)
	}
	if titleMatches[0].Snippet != titleMatches[0].Title {
		t.Fatalf("title-only match should surface the title as snippet: %q", titleMatches[0].Snippet)
	}
	dirMatches, err := store.Search(context.Background(), "elsewhere", Filters{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(dirMatches) != 1 || !strings.Contains(strings.ToLower(dirMatches[0].Snippet), "elsewhere") {
		t.Fatalf("directory match not surfaced in snippet: %#v", dirMatches)
	}
}

func TestSnippetIgnoresLiteralBrackets(t *testing.T) {
	store := testStore(t)
	items := []session.Session{
		{Provider: "codex", ID: "b1", Title: "Deploy the dashboard", Directory: "/repos/session-recall", UpdatedAt: time.Unix(20, 0), Content: "[INFO] starting build step now", Source: "b1.jsonl", Stamp: 1},
		{Provider: "codex", ID: "b2", Title: "Deploy [skip ci] fast", Directory: "/repos/session-recall", UpdatedAt: time.Unix(10, 0), Content: "unrelated words here only", Source: "b2.jsonl", Stamp: 1},
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	// "session-recall" matches only the directory column of both rows.
	matches, err := store.Search(context.Background(), "session-recall", Filters{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	for _, m := range matches {
		if !strings.Contains(strings.ToLower(m.Snippet), "session-recall") {
			t.Fatalf("literal brackets suppressed the directory fallback: %q", m.Snippet)
		}
		if m.Snippet == m.Title {
			t.Fatalf("title with literal brackets picked over matching directory: %q", m.Snippet)
		}
	}
}

func TestContentSnippetPreservesLiteralBrackets(t *testing.T) {
	store := testStore(t)
	item := session.Session{
		Provider: "codex", ID: "brackets", Title: "Inspect output", Directory: "/repo",
		UpdatedAt: time.Unix(20, 0), Content: "check items[0] beside the [INFO] marker",
		Source: "brackets.jsonl", Stamp: 1,
	}
	if err := store.Upsert(context.Background(), []session.Session{item}); err != nil {
		t.Fatal(err)
	}
	matches, err := store.Search(context.Background(), "items", Filters{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if !strings.Contains(matches[0].Snippet, "items[0]") || !strings.Contains(matches[0].Snippet, "[INFO]") {
		t.Fatalf("literal brackets were not preserved: %q", matches[0].Snippet)
	}
}

func TestSnippetFallsBackPerFtsToken(t *testing.T) {
	store := testStore(t)
	items := []session.Session{
		{Provider: "codex", ID: "f1", Title: "foo bar queue", Directory: "/repo",
			UpdatedAt: time.Unix(20, 0), Content: strings.Repeat("filler ", 40), Source: "f1.jsonl", Stamp: 1},
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	// FTS tokenizes "foo/bar" into foo AND bar, so the row matches via the
	// title while the content window shows plain filler. Fallback tokens must
	// come from the same tokenizer, or nothing matches and the snippet stays
	// unusable.
	matches, err := store.Search(context.Background(), "foo/bar", Filters{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if matches[0].Snippet != "foo bar queue" {
		t.Fatalf("fallback did not surface the title for token-split query: %q", matches[0].Snippet)
	}
}

func TestSnippetHyphenatedQueryFallsBackPerSplitToken(t *testing.T) {
	store := testStore(t)
	items := []session.Session{
		{Provider: "codex", ID: "h1", Title: "session recall utils", Directory: "/work/misc",
			UpdatedAt: time.Unix(20, 0), Content: strings.Repeat("filler ", 40), Source: "h1.jsonl", Stamp: 1},
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	// unicode61 splits "session-recall" into session + recall, and the phrase
	// query matches the space-separated title. The fallback tokens must split
	// the same way for the title to surface.
	matches, err := store.Search(context.Background(), "session-recall", Filters{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if matches[0].Snippet != "session recall utils" {
		t.Fatalf("fallback did not surface the title for hyphenated query: %q", matches[0].Snippet)
	}
}

func TestSnippetWindowWithSubstringStays(t *testing.T) {
	store := testStore(t)
	items := []session.Session{
		{Provider: "codex", ID: "w1", Title: "fix release", Directory: "/work/misc",
			UpdatedAt: time.Unix(20, 0), Content: "affix and padding " + strings.Repeat("filler ", 30), Source: "w1.jsonl", Stamp: 1},
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	// The row matches "fix" via the title; the content window shows only
	// "affix" (which FTS does not match, so no bracket). Because the window
	// still contains the term, the fallback intentionally keeps it.
	matches, err := store.Search(context.Background(), "fix", Filters{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if matches[0].Snippet == matches[0].Title {
		t.Fatalf("window with a term substring should suppress the fallback: %q", matches[0].Snippet)
	}
	if !strings.Contains(matches[0].Snippet, "affix") {
		t.Fatalf("window should contain affix: %q", matches[0].Snippet)
	}
}

func TestSearchFiltersCWD(t *testing.T) {
	store := testStore(t)
	items := []session.Session{
		{Provider: "codex", ID: "d1", Title: "pane reattach", Directory: "/work/a",
			UpdatedAt: time.Unix(30, 0), Content: "pane reattach", Source: "d1.jsonl", Stamp: 1},
		{Provider: "claude", ID: "d2", Title: "pane reattach", Directory: "/work/b",
			UpdatedAt: time.Unix(20, 0), Content: "pane reattach", Source: "d2.jsonl", Stamp: 1},
		{Provider: "codex", ID: "d3", Title: "pane reattach", Directory: "/work/a/sub",
			UpdatedAt: time.Unix(10, 0), Content: "pane reattach", Source: "d3.jsonl", Stamp: 1},
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	matches, err := store.Search(context.Background(), "pane", Filters{Provider: "codex", CWD: "/work/a", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if matches[0].ID != "d1" || matches[0].Provider != "codex" || matches[0].Directory != "/work/a" {
		t.Fatalf("filter mismatch: %#v", matches[0])
	}
}

func TestSnippetFallbackFoldsDiacritics(t *testing.T) {
	store := testStore(t)
	items := []session.Session{
		{Provider: "codex", ID: "a1", Title: "cafe ordering", Directory: "/work/misc",
			UpdatedAt: time.Unix(20, 0), Content: strings.Repeat("filler ", 40), Source: "a1.jsonl", Stamp: 1},
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	// unicode61 folds diacritics: the "café" query matches the title, and the
	// fallback must fold the same way to surface it.
	matches, err := store.Search(context.Background(), "café", Filters{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	if matches[0].Snippet != "cafe ordering" {
		t.Fatalf("fallback did not surface the title for accented query: %q", matches[0].Snippet)
	}
}

func TestSearchOrdersByRankThenRecencyAndLimits(t *testing.T) {
	store := testStore(t)
	items := make([]session.Session, 60)
	for i := range items {
		// Extra filler words set each pair apart from the previous one:
		// longer docs rank lower, so scores strictly decrease as i grows.
		// Pairs of documents share identical content to exercise the
		// recency tiebreak within one rank.
		extra := strings.Repeat("filler ", i/2)
		items[i] = session.Session{
			Provider: "codex", ID: fmt.Sprintf("s%02d", i), Title: "Title",
			Directory: "/repo", UpdatedAt: time.Unix(int64(60-i), 0),
			Content: "pane reattach " + extra, Source: "one.jsonl", Stamp: 1,
		}
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	matches, err := store.Search(context.Background(), "pane reattach", Filters{Provider: "codex", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 50 {
		t.Fatalf("got %d matches, want 50", len(matches))
	}
	for i := 1; i < len(matches); i++ {
		if matches[i].Score > matches[i-1].Score {
			t.Fatalf("rank rose at %d: %v > %v", i, matches[i].Score, matches[i-1].Score)
		}
		if matches[i].Score == matches[i-1].Score && matches[i].UpdatedAt.After(matches[i-1].UpdatedAt) {
			t.Fatalf("tie at %d not broken by recency: %v after %v", i, matches[i].UpdatedAt, matches[i-1].UpdatedAt)
		}
	}
	// Snippet must be present for returned rows.
	for i, m := range matches {
		if m.Snippet == "" {
			t.Fatalf("match %d has empty snippet", i)
		}
	}
}

func TestSearchTieAtLimitKeepsMostRecent(t *testing.T) {
	store := testStore(t)
	// All sessions share identical content, so every match ranks equal. With
	// a limit that cuts through the tied group, the most recent sessions must
	// win; the two oldest must be the ones dropped.
	items := make([]session.Session, 52)
	for i := range items {
		items[i] = session.Session{
			Provider: "claude", ID: fmt.Sprintf("s%02d", i), Title: "Title",
			Directory: "/repo", UpdatedAt: time.Unix(int64(1000+i), 0),
			Content: "identical status prompt", Source: "h.jsonl", Stamp: 1,
		}
	}
	if err := store.Upsert(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	matches, err := store.Search(context.Background(), "identical", Filters{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 50 {
		t.Fatalf("got %d matches, want 50", len(matches))
	}
	for i, m := range matches {
		want := fmt.Sprintf("s%02d", 51-i)
		if m.ID != want {
			t.Fatalf("match %d = %s, want %s", i, m.ID, want)
		}
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
