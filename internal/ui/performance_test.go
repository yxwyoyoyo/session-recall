package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/yxwyoyoyo/session-try/internal/session"
)

var renderedFrame string

func BenchmarkPickerView50(b *testing.B) {
	input := textinput.New()
	input.SetValue("pane lifecycle")
	results := make([]session.Match, 50)
	for i := range results {
		results[i] = session.Match{
			Session: session.Session{
				Provider:  []string{"claude", "codex", "opencode"}[i%3],
				ID:        fmt.Sprintf("session-%03d", i),
				Title:     fmt.Sprintf("Investigate pane lifecycle issue %d", i),
				Directory: fmt.Sprintf("/workspace/project-%03d", i),
				UpdatedAt: time.Unix(int64(1_800_000_000-i), 0),
			},
			Snippet: "The [pane] status should persist across the agent [lifecycle].",
		}
	}
	picker := &Picker{input: input, results: results, width: 120, height: 60}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		renderedFrame = picker.View()
	}
}
