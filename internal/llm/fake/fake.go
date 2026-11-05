// Package fake provides a deterministic provider for development and tests.
package fake

import (
	"context"
	"fmt"

	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/triage"
)

// Provider returns a fixed verdict unless an allowed override is configured.
type Provider struct{ verdict string }

// New constructs the fake provider with an optional verdict override.
func New(override string) (*Provider, error) {
	if override == "" {
		override = "safe"
	}
	if override != "safe" && override != "review" && override != "risk" {
		return nil, fmt.Errorf("invalid fake verdict %q: must be safe, review, or risk", override)
	}
	return &Provider{verdict: override}, nil
}

// Name returns the provider name.
func (*Provider) Name() string { return "fake" }

// Classify returns a deterministic verdict for every request.
func (p *Provider) Classify(ctx context.Context, _ llm.Request) (triage.Verdict, error) {
	if err := ctx.Err(); err != nil {
		return triage.Verdict{}, fmt.Errorf("fake classification canceled: %w", err)
	}
	return triage.Verdict{Verdict: p.verdict, Confidence: 0.9, Summary: "fake provider", Reasons: []string{"fake"}}, nil
}
