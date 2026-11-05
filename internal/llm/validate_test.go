package llm

import (
	"strings"
	"testing"

	"github.com/deptriage/deptriage/internal/triage"
)

func TestValidateVerdict(t *testing.T) {
	cases := []struct {
		name    string
		verdict triage.Verdict
		wantErr bool
	}{
		{"valid verdict", triage.Verdict{Verdict: "safe", Confidence: 0.7, Summary: "Looks safe", Reasons: []string{}}, false},
		{"bad enum", triage.Verdict{Verdict: "unknown", Confidence: 0.7, Summary: "Summary", Reasons: []string{}}, true},
		{"confidence over one", triage.Verdict{Verdict: "safe", Confidence: 1.1, Summary: "Summary", Reasons: []string{}}, true},
		{"confidence below zero", triage.Verdict{Verdict: "safe", Confidence: -0.1, Summary: "Summary", Reasons: []string{}}, true},
		{"empty summary", triage.Verdict{Verdict: "safe", Confidence: 0.7, Reasons: []string{}}, true},
		{"summary over 200 chars", triage.Verdict{Verdict: "safe", Confidence: 0.7, Summary: strings.Repeat("x", 201), Reasons: []string{}}, true},
		{"nil reasons", triage.Verdict{Verdict: "safe", Confidence: 0.7, Summary: "Summary"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateVerdict(tc.verdict)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateVerdict() error = %v, want error %t", err, tc.wantErr)
			}
		})
	}
}
