package triage

import (
	"testing"

	"github.com/deptriage/deptriage/internal/github"
)

func TestEvaluateSignals(t *testing.T) {
	cases := []struct {
		name   string
		pr     github.PR
		ctx    github.PRContext
		want   string
		weight int
	}{
		{"major bump", github.PR{}, github.PRContext{FromVersion: "1.0.0", ToVersion: "2.0.0", CIStatus: github.CIUnknown}, "major_version_bump", 2},
		{"minor bump", github.PR{}, github.PRContext{FromVersion: "1.0.0", ToVersion: "1.1.0", CIStatus: github.CIUnknown}, "minor_version_bump", 1},
		{"patch bump", github.PR{}, github.PRContext{FromVersion: "1.0.0", ToVersion: "1.0.1", CIStatus: github.CIUnknown}, "patch_version_bump", -1},
		{"CI failing", github.PR{}, github.PRContext{CIStatus: github.CIFailing}, "ci_failing", 2},
		{"CI passing", github.PR{}, github.PRContext{CIStatus: github.CIPassing}, "ci_passing", -1},
		{"CI unknown", github.PR{}, github.PRContext{CIStatus: github.CIUnknown}, "ci_unknown", 0},
		{"security label", github.PR{Labels: []string{"Security Update"}}, github.PRContext{CIStatus: github.CIUnknown}, "security_label", 3},
		{"security advisory", github.PR{Body: "Addresses GHSA-1234"}, github.PRContext{CIStatus: github.CIUnknown}, "security_label", 3},
		{"development dependency", github.PR{Body: "development dependency"}, github.PRContext{CIStatus: github.CIUnknown}, "dev_dependency", -1},
		{"lockfiles only", github.PR{}, github.PRContext{CIStatus: github.CIUnknown, ChangedFiles: []github.FileSummary{{Path: "go.sum"}}}, "lockfile_only", -1},
		{"large diff", github.PR{}, github.PRContext{CIStatus: github.CIUnknown, Additions: 401}, "large_diff", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signals := EvaluateSignals(tc.pr, tc.ctx)
			found := false
			for _, signal := range signals {
				if signal.Name == tc.want {
					found = true
					if signal.Score != tc.weight {
						t.Fatalf("weight %d, want %d", signal.Score, tc.weight)
					}
				}
			}
			if !found {
				t.Fatalf("missing signal %q in %#v", tc.want, signals)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		confidence float64
		signals    []Signal
		want       string
		wantReason string
	}{
		{"signal forces risk", "safe", 0.9, []Signal{{Score: 3}}, "risk", ""},
		{"positive score downgrades safe", "safe", 0.9, []Signal{{Score: 2}}, "review", ""},
		{"risk is not downgraded", "risk", 0.9, []Signal{{Score: -5}}, "risk", ""},
		{"review remains review", "review", 0.9, []Signal{{Score: -3}}, "review", ""},
		{"low confidence safe becomes review", "safe", 0.69, nil, "review", "low_confidence"},
		{"safe at 0.9 stays safe", "safe", 0.9, nil, "safe", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Merge(Verdict{Verdict: tc.input, Confidence: tc.confidence}, tc.signals)
			if got.Verdict != tc.want {
				t.Fatalf("Merge() = %q, want %q", got.Verdict, tc.want)
			}
			if tc.wantReason != "" && (len(got.Reasons) == 0 || got.Reasons[len(got.Reasons)-1] != tc.wantReason) {
				t.Fatalf("Merge() reasons = %#v, want final reason %q", got.Reasons, tc.wantReason)
			}
		})
	}
}
