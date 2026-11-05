// Package triage contains deterministic risk signals and verdict merging.
package triage

import (
	"strconv"
	"strings"

	"github.com/deptriage/deptriage/internal/github"
)

// Signal is an explainable deterministic risk adjustment.
type Signal struct {
	Name   string `json:"name"`
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// Verdict is the stable classification returned by an LLM provider.
type Verdict struct {
	Verdict                 string   `json:"verdict"`
	Confidence              float64  `json:"confidence"`
	Summary                 string   `json:"summary"`
	Reasons                 []string `json:"reasons"`
	BreakingChangeSuspected bool     `json:"breaking_change_suspected"`
	SecurityRelevant        bool     `json:"security_relevant"`
}

// EvaluateSignals evaluates version, CI, security, dependency type, and diff signals.
func EvaluateSignals(pr github.PR, ctx github.PRContext) []Signal {
	var signals []Signal
	add := func(name string, score int, reason string) {
		signals = append(signals, Signal{Name: name, Score: score, Reason: reason})
	}
	if ctx.FromVersion != "" && ctx.ToVersion != "" {
		oldMajor, oldMinor, oldPatch := versionParts(ctx.FromVersion)
		newMajor, newMinor, newPatch := versionParts(ctx.ToVersion)
		switch {
		case newMajor > oldMajor:
			add("major_version_bump", 2, "major version increased")
		case newMinor > oldMinor:
			add("minor_version_bump", 1, "minor version increased")
		case newPatch > oldPatch:
			add("patch_version_bump", -1, "patch version increased")
		}
	}
	switch ctx.CIStatus {
	case github.CIFailing:
		add("ci_failing", 2, "CI has failing checks")
	case github.CIPassing:
		add("ci_passing", -1, "CI checks are passing")
	case github.CIUnknown:
		add("ci_unknown", 0, "CI status is unknown")
	}
	security := false
	for _, label := range pr.Labels {
		if strings.Contains(strings.ToLower(label), "security") {
			security = true
			break
		}
	}
	bodyUpper := strings.ToUpper(pr.Body)
	if strings.Contains(bodyUpper, "CVE-") || strings.Contains(bodyUpper, "GHSA-") {
		security = true
	}
	if security {
		add("security_label", 3, "security label or advisory reference detected")
	}
	if strings.Contains(strings.ToLower(pr.Body), "dev dependenc") || strings.Contains(strings.ToLower(pr.Body), "development dependency") || developmentPath(ctx.ChangedFiles) {
		add("dev_dependency", -1, "dependency appears to be development-only")
	}
	if len(ctx.ChangedFiles) > 0 {
		lockOnly := true
		for _, file := range ctx.ChangedFiles {
			if !github.IsLockfile(file.Path) {
				lockOnly = false
				break
			}
		}
		if lockOnly {
			add("lockfile_only", -1, "only lockfiles changed")
		}
	}
	if ctx.Additions+ctx.Deletions > 400 {
		add("large_diff", 1, "non-lockfile diff exceeds 400 lines")
	}
	return signals
}

// Merge adjusts an LLM verdict using deterministic risk signals.
func Merge(llmVerdict Verdict, signals []Signal) Verdict {
	score := 0
	forceRisk := false
	for _, signal := range signals {
		score += signal.Score
		if signal.Score >= 3 {
			forceRisk = true
		}
	}
	if forceRisk {
		llmVerdict.Verdict = "risk"
	} else if llmVerdict.Verdict == "safe" && score >= 1 {
		llmVerdict.Verdict = "review"
	}
	return llmVerdict
}

func versionParts(version string) (int, int, int) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	version = strings.SplitN(version, "-", 2)[0]
	version = strings.SplitN(version, "+", 2)[0]
	var major, minor, patch int
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return major, minor, patch
}

func developmentPath(files []github.FileSummary) bool {
	for _, file := range files {
		path := strings.ToLower(file.Path)
		if strings.Contains(path, "dev-dependencies") || strings.Contains(path, "devdependencies") || strings.Contains(path, "dev_dependencies") {
			return true
		}
	}
	return false
}
