package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

	// Known stamps ride along as an inlined VALUES table expression, so the
	// provider database stays strictly read-only: no temp tables, no writes.
	// A dummy row keeps the query shape identical when nothing is known.
	var sb strings.Builder
	sb.WriteString("WITH known(id, stamp) AS (VALUES ")
	ids := make([]string, 0, len(known))
	for id := range known {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		sb.WriteString(`('__none__', -1)`)
	}
	for i, id := range ids {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "('%s', %d)", strings.ReplaceAll(id, "'", "''"), known[id])
	}
	sb.WriteString(")")
	// Unchanged sessions keep their last indexed content: gating the message
	// join on the known stamp skips content aggregation for them in SQL.
	rows, err := db.QueryContext(ctx, sb.String()+`
		SELECT s.id, s.directory, s.title, s.time_updated,
		       COALESCE(group_concat(json_extract(p.data, '$.text'), char(10)), '')
		FROM session s
		LEFT JOIN known k ON k.id = s.id
		LEFT JOIN message m ON m.session_id = s.id
		  AND (k.id IS NULL OR k.stamp != s.time_updated)
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
