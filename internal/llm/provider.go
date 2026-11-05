// Package llm defines the provider contract used by triage.
package llm

import (
	"context"
	"encoding/json"
	"time"

	"github.com/deptriage/deptriage/internal/github"
	"github.com/deptriage/deptriage/internal/triage"
)

// Request contains the pull request context for classification.
type Request struct {
	System  string
	User    string
	Schema  json.RawMessage
	Model   string
	Temp    float64
	Timeout time.Duration
	PR      github.PR
	Context github.PRContext
}

// Provider classifies pull requests and returns a structured verdict.
type Provider interface {
	Name() string
	Classify(ctx context.Context, req Request) (triage.Verdict, error)
}
