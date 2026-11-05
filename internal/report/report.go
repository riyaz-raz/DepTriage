// Package report renders triage results for terminals and machine consumers.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/deptriage/deptriage/internal/github"
	"github.com/deptriage/deptriage/internal/triage"
)

// Result combines pull request context, signals, and its final classification.
type Result struct {
	PR       github.PR
	Context  github.PRContext
	Signals  []triage.Signal
	Verdict  triage.Verdict
	Degraded bool
}

// Document is the stable JSON report schema.
type Document struct {
	SchemaVersion int     `json:"schema_version"`
	Repo          string  `json:"repo"`
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	GeneratedAt   string  `json:"generated_at"`
	Summary       Summary `json:"summary"`
	Items         []Item  `json:"items"`
}

// Summary contains aggregate verdict counts.
type Summary struct {
	Total    int `json:"total"`
	Safe     int `json:"safe"`
	Review   int `json:"review"`
	Risk     int `json:"risk"`
	Degraded int `json:"degraded"`
}

// Item is one PR entry in the JSON report.
type Item struct {
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	URL        string   `json:"url"`
	Dependency string   `json:"dependency"`
	From       string   `json:"from"`
	To         string   `json:"to"`
	CI         string   `json:"ci"`
	Verdict    string   `json:"verdict"`
	Confidence float64  `json:"confidence"`
	Summary    string   `json:"summary"`
	Reasons    []string `json:"reasons"`
	Signals    []string `json:"signals"`
	Degraded   bool     `json:"degraded"`
}

// RenderHuman renders a concise table and summary.
func RenderHuman(w io.Writer, results []Result, provider, model string, color bool) error {
	useColor := color && os.Getenv("NO_COLOR") == "" && writerIsTTY(w)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "PR\tDEPENDENCY\tUPDATE\tCI\tVERDICT\tCONFIDENCE\tSIGNALS"); err != nil {
		return fmt.Errorf("write report header: %w", err)
	}
	counts := Summary{Total: len(results)}
	for _, result := range results {
		verdict := result.Verdict.Verdict
		switch verdict {
		case "safe":
			counts.Safe++
		case "review":
			counts.Review++
		case "risk":
			counts.Risk++
		}
		if result.Degraded {
			counts.Degraded++
		}
		if useColor {
			verdict = colorVerdict(verdict)
		}
		update := ""
		if result.Context.FromVersion != "" || result.Context.ToVersion != "" {
			update = result.Context.FromVersion + " -> " + result.Context.ToVersion
		}
		signalNames := make([]string, 0, len(result.Signals))
		for _, signal := range result.Signals {
			signalNames = append(signalNames, signal.Name)
		}
		if _, err := fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\t%.0f%%\t%s\n", result.PR.Number, result.Context.DependencyName, update, result.Context.CIStatus, verdict, result.Verdict.Confidence*100, strings.Join(signalNames, ",")); err != nil {
			return fmt.Errorf("write report row: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush report table: %w", err)
	}
	_, err := fmt.Fprintf(w, "%d PRs: %d safe, %d review, %d risk (provider: %s/%s)\n", counts.Total, counts.Safe, counts.Review, counts.Risk, provider, model)
	if err != nil {
		return fmt.Errorf("write report summary: %w", err)
	}
	return nil
}

// RenderJSON writes the stable machine-readable report schema.
func RenderJSON(w io.Writer, results []Result, repo, provider, model string) error {
	doc := buildDocument(results, repo, provider, model)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}
	return nil
}

// BuildDocument constructs the stable JSON report data.
func BuildDocument(results []Result, repo, provider, model string, generated time.Time) Document {
	doc := buildDocument(results, repo, provider, model)
	doc.GeneratedAt = generated.UTC().Format(time.RFC3339)
	return doc
}

func buildDocument(results []Result, repo, provider, model string) Document {
	doc := Document{SchemaVersion: 1, Repo: repo, Provider: provider, Model: model, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Items: make([]Item, 0, len(results))}
	doc.Summary.Total = len(results)
	for _, result := range results {
		verdict := result.Verdict.Verdict
		switch verdict {
		case "safe":
			doc.Summary.Safe++
		case "review":
			doc.Summary.Review++
		case "risk":
			doc.Summary.Risk++
		}
		if result.Degraded {
			doc.Summary.Degraded++
		}
		signals := make([]string, 0, len(result.Signals))
		for _, signal := range result.Signals {
			signals = append(signals, signal.Name)
		}
		doc.Items = append(doc.Items, Item{Number: result.PR.Number, Title: result.PR.Title, Author: result.PR.Author, URL: result.PR.URL, Dependency: result.Context.DependencyName, From: result.Context.FromVersion, To: result.Context.ToVersion, CI: string(result.Context.CIStatus), Verdict: verdict, Confidence: result.Verdict.Confidence, Summary: result.Verdict.Summary, Reasons: result.Verdict.Reasons, Signals: signals, Degraded: result.Degraded})
	}
	return doc
}

func writerIsTTY(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func colorVerdict(verdict string) string {
	code := "33"
	switch verdict {
	case "safe":
		code = "32"
	case "risk":
		code = "31"
	}
	return "\x1b[" + code + "m" + verdict + "\x1b[0m"
}
