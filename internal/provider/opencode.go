package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

type OpenCode struct {
	Home       string
	Executable string
	OpenDB     func(string) (*sql.DB, error)
}

func NewOpenCode(home string, openDB func(string) (*sql.DB, error)) *OpenCode {
	return &OpenCode{Home: home, Executable: "opencode", OpenDB: openDB}
}

func (p *OpenCode) Name() string { return "opencode" }

func (p *OpenCode) ParserRevision() int { return 1 }

func (p *OpenCode) dbPath() string {
	return filepath.Join(p.Home, ".local", "share", "opencode", "opencode.db")
}

func (p *OpenCode) Available() bool {
	_, commandErr := exec.LookPath(p.Executable)
	_, dbErr := os.Stat(p.dbPath())
	return commandErr == nil && dbErr == nil && p.OpenDB != nil
}

func (p *OpenCode) Discover(ctx context.Context, known map[string]int64) (Discovery, error) {
	report := Discovery{}
	db, err := p.OpenDB(p.dbPath())
	if err != nil {
		return report, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.directory, s.title, s.time_updated,
		       COALESCE(group_concat(json_extract(p.data, '$.text'), char(10)), '')
		FROM session s
		LEFT JOIN message m ON m.session_id = s.id
		LEFT JOIN part p ON p.message_id = m.id
		  AND json_extract(p.data, '$.type') = 'text'
		  AND json_extract(m.data, '$.role') = 'user'
		WHERE s.time_archived IS NULL
		GROUP BY s.id
		ORDER BY s.time_updated DESC`)
	if err != nil {
		return report, fmt.Errorf("query OpenCode sessions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		report.Scanned++
		var item session.Session
		var updated int64
		if err := rows.Scan(&item.ID, &item.Directory, &item.Title, &updated, &item.Content); err != nil {
			return report, err
		}
		if known[item.ID] == updated {
			report.Unchanged++
			continue
		}
		item.Provider = "opencode"
		item.Source = item.ID
		item.Stamp = updated
		item.UpdatedAt = time.UnixMilli(updated)
		report.Sessions = append(report.Sessions, item)
	}
	return report, rows.Err()
}

func (p *OpenCode) ResumeCommand(s session.Session) (*exec.Cmd, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("OpenCode session has no ID")
	}
	cmd := exec.Command(p.Executable, s.Directory, "--session", s.ID)
	cmd.Dir = s.Directory
	return cmd, nil
}

// Kept here so fixtures can share OpenCode's current JSON record shape without
// coupling the index package to it.
func openCodeText(raw string) string {
	var value struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil || value.Type != "text" {
		return ""
	}
	return strings.TrimSpace(value.Text)
}
