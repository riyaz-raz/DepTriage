package llm

import (
	"fmt"
	"strings"

	"github.com/deptriage/deptriage/internal/triage"
)

// ValidateVerdict validates fields required by the shared verdict contract.
func ValidateVerdict(verdict triage.Verdict) error {
	switch verdict.Verdict {
	case "safe", "review", "risk":
	default:
		return fmt.Errorf("verdict must be safe, review, or risk")
	}
	if verdict.Confidence < 0 || verdict.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if summaryLength := len([]rune(verdict.Summary)); summaryLength < 1 || summaryLength > 200 {
		return fmt.Errorf("summary must contain between 1 and 200 characters")
	}
	if verdict.Reasons == nil {
		return fmt.Errorf("reasons must be a non-nil array")
	}
	return nil
}

// DegradedVerdict returns the safe fallback used when model output cannot be repaired.
func DegradedVerdict() triage.Verdict {
	return triage.Verdict{Verdict: "review", Confidence: 0, Summary: "LLM output invalid; manual review required", Reasons: []string{"parse_failure"}, Degraded: true}
}

// RepairUserMessage builds the one-shot repair prompt for invalid model output.
func RepairUserMessage(user, priorOutput string, validationErr error) string {
	return strings.TrimSpace(user) + "\n\nPrevious model output:\n" + priorOutput + "\n\nYour previous output was invalid: " + validationErr.Error() + ". Return ONLY valid JSON matching this schema: " + string(VerdictSchema) + ". No prose. No markdown fences."
}
