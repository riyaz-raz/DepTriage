package github

import (
	"strings"
	"testing"
)

func TestParseDependencyInfo(t *testing.T) {
	cases := []struct {
		name       string
		title      string
		dependency string
		from       string
		to         string
		update     string
	}{
		{
			name:       "standard single dependency",
			title:      "build(deps): bump dawidd6/action-download-artifact from 27 to 28",
			dependency: "dawidd6/action-download-artifact", from: "27", to: "28", update: "27 -> 28",
		},
		{
			name:       "scoped npm dependency",
			title:      "chore(deps): bump @types/node from 20.1.0 to 20.2.0",
			dependency: "@types/node", from: "20.1.0", to: "20.2.0", update: "20.1.0 -> 20.2.0",
		},
		{
			name:       "grouped updates",
			title:      "build(deps): bump the github-actions group with 3 updates",
			dependency: "github-actions group", update: "(group)",
		},
		{
			name:  "unknown title fallback",
			title: "Update README", dependency: "Update README",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dependency, from, to, update := ParseDependencyInfo(tc.title)
			if dependency != tc.dependency || from != tc.from || to != tc.to || update != tc.update {
				t.Fatalf("ParseDependencyInfo() = (%q, %q, %q, %q), want (%q, %q, %q, %q)", dependency, from, to, update, tc.dependency, tc.from, tc.to, tc.update)
			}
		})
	}
}

func TestParseDependencyInfoFallbackTruncatesTitle(t *testing.T) {
	title := "Update " + strings.Repeat("x", 80)
	dependency, from, to, update := ParseDependencyInfo(title)
	if len([]rune(dependency)) != 60 || from != "" || to != "" || update != "" {
		t.Fatalf("fallback = (%q, %q, %q, %q)", dependency, from, to, update)
	}
}
