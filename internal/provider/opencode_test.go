package provider

import (
	"context"
	"database/sql"
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
