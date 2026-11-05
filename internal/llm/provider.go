// Package llm defines the provider contract used by triage.
package llm

import (
	"context"

	"github.com/deptriage/deptriage/internal/github"
	"github.com/deptriage/deptriage/internal/triage"
)

// Request contains the pull request context for classification.
type Request struct {
	PR      github.PR
	Context github.PRContext
}

// Provider classifies pull requests and returns a structured verdict.
type Provider interface {
	Name() string
	Classify(ctx context.Context, req Request) (triage.Verdict, error)
}
