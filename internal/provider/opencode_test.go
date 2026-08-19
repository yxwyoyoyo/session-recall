package provider

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenCodeDiscoverReadsOnlyUserText(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE session(id TEXT, directory TEXT, title TEXT, time_updated INTEGER, time_archived INTEGER);
		CREATE TABLE message(id TEXT, session_id TEXT, data TEXT);
		CREATE TABLE part(id TEXT, message_id TEXT, data TEXT);
		INSERT INTO session VALUES('ses_1', '/repo', 'Readable title', 2000, NULL);
		INSERT INTO message VALUES('user_1', 'ses_1', '{"role":"user"}');
		INSERT INTO message VALUES('assistant_1', 'ses_1', '{"role":"assistant"}');
		INSERT INTO part VALUES('part_1', 'user_1', '{"type":"text","text":"remember this phrase"}');
		INSERT INTO part VALUES('part_2', 'assistant_1', '{"type":"text","text":"exclude assistant output"}');`)
	if err != nil {
		t.Fatal(err)
	}
	p := NewOpenCode(t.TempDir(), func(string) (*sql.DB, error) { return db, nil })
	discovery, err := p.Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Sessions) != 1 || discovery.Sessions[0].Content != "remember this phrase" {
		t.Fatalf("unexpected sessions: %#v", discovery.Sessions)
	}
}

func TestOpenCodeDiscoverSkipsUnchangedSessions(t *testing.T) {
	newProvider := func() *OpenCode {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		_, err = db.Exec(`
			CREATE TABLE session(id TEXT, directory TEXT, title TEXT, time_updated INTEGER, time_archived INTEGER);
			CREATE TABLE message(id TEXT, session_id TEXT, data TEXT);
			CREATE TABLE part(id TEXT, message_id TEXT, data TEXT);
			INSERT INTO session VALUES('old', '/repo', 'Unchanged', 100, NULL);
			INSERT INTO session VALUES('edited', '/repo', 'Still edited', 200, NULL);
			INSERT INTO session VALUES('fresh', '/repo', 'Brand new', 300, NULL);
			INSERT INTO message VALUES('m1', 'old', '{"role":"user"}');
			INSERT INTO part VALUES('p1', 'm1', '{"type":"text","text":"old content"}');
			INSERT INTO message VALUES('m2', 'edited', '{"role":"user"}');
			INSERT INTO part VALUES('p2', 'm2', '{"type":"text","text":"edited content"}');
			INSERT INTO message VALUES('m3', 'fresh', '{"role":"user"}');
			INSERT INTO part VALUES('p3', 'm3', '{"type":"text","text":"fresh content"}');`)
		if err != nil {
			t.Fatal(err)
		}
		return NewOpenCode(t.TempDir(), func(string) (*sql.DB, error) { return db, nil })
	}

	// Only the newest session matches a current stamp; the edited one carries
	// a stale stamp and the fresh one is unknown.
	discovery, err := newProvider().Discover(context.Background(), map[string]int64{"old": 100, "edited": 150})
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Unchanged != 1 {
		t.Fatalf("unchanged = %d, want 1", discovery.Unchanged)
	}
	if len(discovery.Sessions) != 2 {
		t.Fatalf("got %d sessions: %#v", len(discovery.Sessions), discovery.Sessions)
	}
	contents := map[string]string{}
	for _, item := range discovery.Sessions {
		contents[item.ID] = item.Content
	}
	if contents["edited"] != "edited content" || contents["fresh"] != "fresh content" {
		t.Fatalf("unexpected contents: %#v", contents)
	}

	// All sessions unchanged: no aggregation, nothing to index.
	all, err := newProvider().Discover(context.Background(), map[string]int64{"old": 100, "edited": 200, "fresh": 300})
	if err != nil {
		t.Fatal(err)
	}
	if all.Unchanged != 3 || len(all.Sessions) != 0 {
		t.Fatalf("expected everything unchanged: %#v", all)
	}
}

func TestOpenCodeDiscoverWithProductionReadOnlyDSN(t *testing.T) {
	// The production DSN (main.go) opens the provider database with
	// mode=ro and the query_only pragma. Discovery must still work there,
	// without any writes. 700+ known sessions also guard against the SQL
	// VALUES clause accidentally hitting compound-select limits.
	path := filepath.Join(t.TempDir(), "opencode.db")
	writable, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writable.Exec(`
		CREATE TABLE session(id TEXT, directory TEXT, title TEXT, time_updated INTEGER, time_archived INTEGER);
		CREATE TABLE message(id TEXT, session_id TEXT, data TEXT);
		CREATE TABLE part(id TEXT, message_id TEXT, data TEXT);`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 720; i++ {
		id := fmt.Sprintf("ses_%04d", i)
		if _, err := writable.Exec(`INSERT INTO session VALUES(?, '/repo', 'Title', ?, NULL)`, id, i); err != nil {
			t.Fatal(err)
		}
		if _, err := writable.Exec(`INSERT INTO message VALUES(?, ?, '{"role":"user"}')`, "m"+id, id); err != nil {
			t.Fatal(err)
		}
		if _, err := writable.Exec(`INSERT INTO part VALUES(?, ?, '{"type":"text","text":"prompt text"}')`, "p"+id, "m"+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}

	// Mirror main.go's openReadOnly: a fresh read-only handle per call.
	p := NewOpenCode(t.TempDir(), func(string) (*sql.DB, error) {
		return sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	})

	// All 720 sessions unchanged: everything is skipped with no writes.
	known := map[string]int64{}
	for i := 0; i < 720; i++ {
		known[fmt.Sprintf("ses_%04d", i)] = int64(i)
	}
	discovery, err := p.Discover(context.Background(), known)
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Unchanged != 720 || len(discovery.Sessions) != 0 {
		t.Fatalf("expected all sessions unchanged: %#v", discovery)
	}
	// One session edited, 719 unchanged.
	known["ses_0001"] = 999
	discovery, err = p.Discover(context.Background(), known)
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Unchanged != 719 || len(discovery.Sessions) != 1 || discovery.Sessions[0].Content != "prompt text" {
		t.Fatalf("expected one edited session: %#v", discovery)
	}
}
