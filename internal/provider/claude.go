package provider

import (
	"bufio"
	"context"
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

type Claude struct {
	Home       string
	Executable string
}

func NewClaude(home string) *Claude {
	return &Claude{Home: home, Executable: "claude"}
}

func (p *Claude) Name() string { return "claude" }

func (p *Claude) ParserRevision() int { return 1 }

func (p *Claude) historyPath() string {
	return filepath.Join(p.Home, ".claude", "history.jsonl")
}

func (p *Claude) Available() bool {
	_, commandErr := exec.LookPath(p.Executable)
	_, historyErr := os.Stat(p.historyPath())
	return commandErr == nil && historyErr == nil
}

type claudeHistory struct {
	Display   string `json:"display"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
	Timestamp int64  `json:"timestamp"`
}

func (p *Claude) Discover(ctx context.Context, known map[string]int64) (Discovery, error) {
	report := Discovery{Scanned: 1}
	path := p.historyPath()
	info, err := os.Stat(path)
	if err != nil {
		return report, err
	}
	stamp := info.ModTime().UnixNano() ^ info.Size()
	if known[path] == stamp {
		report.Unchanged = 1
		return report, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return report, err
	}
	defer file.Close()

	byID := map[string]*session.Session{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		var row claudeHistory
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil || row.SessionID == "" {
			report.SkippedRecords++
			continue
		}
		current := byID[row.SessionID]
		if current == nil {
			current = &session.Session{
				Provider: "claude", ID: row.SessionID, Directory: row.Project,
				Source: path, Stamp: stamp,
			}
			byID[row.SessionID] = current
		}
		text := strings.TrimSpace(row.Display)
		if text != "" && !strings.HasPrefix(text, "/") {
			if current.Title == "" {
				current.Title = oneLine(text, 100)
			}
			current.Content += text + "\n"
		}
		updated := time.UnixMilli(row.Timestamp)
		if updated.After(current.UpdatedAt) {
			current.UpdatedAt = updated
		}
	}
	if err := scanner.Err(); err != nil {
		report.Failures = append(report.Failures, SourceFailure{Source: path, Err: fmt.Errorf("read Claude history: %w", err)})
		return report, nil
	}
	if info.Size() > 0 && len(byID) == 0 {
		report.Failures = append(report.Failures, SourceFailure{Source: path, Err: fmt.Errorf("no recognizable Claude session records")})
		return report, nil
	}

	result := make([]session.Session, 0, len(byID))
	for _, item := range byID {
		if item.Title == "" {
			item.Title = "Claude session"
		}
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	report.Sessions = result
	return report, nil
}

func (p *Claude) ResumeCommand(s session.Session) (*exec.Cmd, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("Claude session has no ID")
	}
	cmd := exec.Command(p.Executable, "--resume", s.ID)
	cmd.Dir = s.Directory
	return cmd, nil
}

func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}
