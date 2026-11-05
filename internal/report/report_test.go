package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/deptriage/deptriage/internal/github"
	"github.com/deptriage/deptriage/internal/triage"
)

var update = flag.Bool("update", false, "update report golden files")

func sampleResult() []Result {
	pr := github.PR{Number: 7, Title: "Bump widget from 1.0.0 to 1.0.1", Author: "dependabot[bot]", URL: "https://github.com/o/r/pull/7"}
	return []Result{{PR: pr, Context: github.PRContext{PR: pr, DependencyName: "widget", FromVersion: "1.0.0", ToVersion: "1.0.1", CIStatus: github.CIPassing}, Signals: []triage.Signal{{Name: "patch_version_bump", Score: -1}}, Verdict: triage.Verdict{Verdict: "safe", Confidence: 0.9, Summary: "fake provider", Reasons: []string{"fake"}}}}
}

func TestRenderGolden(t *testing.T) {
	for _, tc := range []struct {
		name   string
		render func(*bytes.Buffer) error
	}{
		{"report-human.golden", func(b *bytes.Buffer) error { return RenderHuman(b, sampleResult(), "fake", "fake", false) }},
		{"report-json.golden", func(b *bytes.Buffer) error { return RenderJSON(b, sampleResult(), "o/r", "fake", "fake") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := tc.render(&output); err != nil {
				t.Fatal(err)
			}
			actual := output.Bytes()
			if tc.name == "report-json.golden" {
				actual = regexp.MustCompile(`"generated_at": "[^"]+"`).ReplaceAll(actual, []byte(`"generated_at": "<timestamp>"`))
				var document Document
				if err := json.Unmarshal(output.Bytes(), &document); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
			}
			path := filepath.Join("..", "..", "testdata", tc.name)
			if *update {
				if err := os.WriteFile(path, actual, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, want) {
				t.Fatalf("golden mismatch; run go test ./internal/report -update")
			}
		})
	}
}

func TestBuildDocumentTimestamp(t *testing.T) {
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("test", 3600))
	doc := BuildDocument(sampleResult(), "o/r", "fake", "fake", at)
	if doc.GeneratedAt != "2024-01-02T02:04:05Z" {
		t.Fatalf("timestamp = %s", doc.GeneratedAt)
	}
}
