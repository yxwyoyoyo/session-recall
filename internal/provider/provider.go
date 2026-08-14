package provider

import (
	"context"
	"os/exec"

	"github.com/yxwyoyoyo/session-recall/internal/session"
)

// Provider discovers sessions owned by one AI harness and knows how to resume
// them. known maps provider source identifiers to their last indexed stamps.
type Provider interface {
	Name() string
	Available() bool
	Discover(context.Context, map[string]int64) ([]session.Session, error)
	ResumeCommand(session.Session) (*exec.Cmd, error)
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
