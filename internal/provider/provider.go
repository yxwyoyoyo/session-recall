package provider

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

// Provider discovers sessions owned by one AI harness and knows how to resume
// them. known maps provider source identifiers to their last indexed stamps.
type Provider interface {
	Name() string
	ParserRevision() int
	Available() bool
	Discover(context.Context, map[string]int64) (Discovery, error)
	ResumeCommand(session.Session) (*exec.Cmd, error)
}

// Discovery describes one provider scan. Parse failures are scoped to a
// source so successfully decoded sources can still be refreshed atomically.
type Discovery struct {
	Sessions       []session.Session
	Scanned        int
	Unchanged      int
	SkippedRecords int
	Failures       []SourceFailure
}

type SourceFailure struct {
	Source string
	Err    error
}

func (f SourceFailure) Error() string {
	return fmt.Sprintf("%s: %v", f.Source, f.Err)
}

func Available(providers []Provider) []Provider {
	result := make([]Provider, 0, len(providers))
	for _, p := range providers {
		if p.Available() {
			result = append(result, p)
		}
	}
	return result
}
