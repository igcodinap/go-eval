package compare

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"sort"
	"strings"
	"time"
)

// StaticReport is the renderable local/CI report payload.
type StaticReport struct {
	GeneratedAt  string
	ResultsPath  string
	BaselinePath string
	CurrentPath  string
	Summary      ResultsSummary
	Comparison   *Report
}

// NewResultsReport builds a static report payload for one result set.
func NewResultsReport(path string, summary ResultsSummary) StaticReport {
	return StaticReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ResultsPath: path,
		Summary:     summary,
	}
}

// NewComparisonReport builds a static report payload for a baseline/current comparison.
func NewComparisonReport(baselinePath string, currentPath string, summary ResultsSummary, report Report) StaticReport {
	return StaticReport{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		BaselinePath: baselinePath,
		CurrentPath:  currentPath,
		Summary:      summary,
		Comparison:   &report,
	}
}

// ReportJSON renders a StaticReport as indented JSON.
func ReportJSON(report StaticReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

// ReportMarkdown renders a StaticReport as Markdown.
func ReportMarkdown(report StaticReport) ([]byte, error) {
	var b strings.Builder
	b.WriteString("# go-eval report\n\n")
	if report.ResultsPath != "" {
		fmt.Fprintf(&b, "- results: `%s`\n", report.ResultsPath)
	}
	if report.BaselinePath != "" || report.CurrentPath != "" {
		fmt.Fprintf(&b, "- baseline: `%s`\n- current: `%s`\n", report.BaselinePath, report.CurrentPath)
	}
	fmt.Fprintf(&b, "- generated_at: `%s`\n\n", report.GeneratedAt)

	writeMarkdownSummary(&b, report.Summary)
	if report.Comparison != nil {
		writeMarkdownComparison(&b, *report.Comparison)
	}
	return []byte(b.String()), nil
}

// ReportHTML renders a StaticReport as standalone HTML.
func ReportHTML(report StaticReport) ([]byte, error) {
	var b bytes.Buffer
	err := reportHTMLTemplate.Execute(&b, struct {
		Report     StaticReport
		MetricRows []reportMetricRow
		EntryRows  []Entry
		HasCompare bool
	}{
		Report:     report,
		MetricRows: sortedReportMetricRows(report.Summary.ByMetric),
		EntryRows:  changedEntries(report.Comparison),
		HasCompare: report.Comparison != nil,
	})
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

type reportMetricRow struct {
	Name    string
	Summary MetricSummary
}

var reportHTMLTemplate = htmltemplate.Must(htmltemplate.New("report").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>go-eval report</title>
<style>
body { font-family: system-ui, sans-serif; margin: 2rem; color: #1f2933; }
h1, h2 { margin-bottom: .4rem; }
table { border-collapse: collapse; width: 100%; margin: 1rem 0 2rem; }
th, td { border: 1px solid #d9e2ec; padding: .45rem .55rem; text-align: left; }
th { background: #f0f4f8; }
code { background: #f0f4f8; padding: .1rem .25rem; border-radius: 3px; }
.summary { display: flex; gap: 1rem; flex-wrap: wrap; margin: 1rem 0; }
.pill { border: 1px solid #d9e2ec; padding: .4rem .6rem; border-radius: 4px; }
</style>
</head>
<body>
<h1>go-eval report</h1>
<p>Generated at <code>{{.Report.GeneratedAt}}</code></p>
{{if .Report.ResultsPath}}<p>Results: <code>{{.Report.ResultsPath}}</code></p>{{end}}
{{if .HasCompare}}<p>Baseline: <code>{{.Report.BaselinePath}}</code><br>Current: <code>{{.Report.CurrentPath}}</code></p>{{end}}
<div class="summary">
  <div class="pill">total {{.Report.Summary.Total}}</div>
  <div class="pill">passed {{.Report.Summary.Passed}}</div>
  <div class="pill">failed {{.Report.Summary.Failed}}</div>
  <div class="pill">pass rate {{printf "%.3f" .Report.Summary.PassRate}}</div>
</div>
<h2>Metrics</h2>
<table>
<thead><tr><th>metric</th><th>count</th><th>passed</th><th>failed</th><th>mean score</th><th>stddev</th><th>p95 tokens</th><th>p95 latency ns</th></tr></thead>
<tbody>
{{range .MetricRows}}<tr><td>{{.Name}}</td><td>{{.Summary.Count}}</td><td>{{.Summary.Passed}}</td><td>{{.Summary.Failed}}</td><td>{{printf "%.3f" .Summary.MeanScore}}</td><td>{{printf "%.3f" .Summary.StdDev}}</td><td>{{printf "%.1f" .Summary.P95Tokens}}</td><td>{{.Summary.P95Latency.Nanoseconds}}</td></tr>{{end}}
</tbody>
</table>
{{if .HasCompare}}
<h2>Comparison</h2>
<div class="summary">
  <div class="pill">added {{.Report.Comparison.Summary.Added}}</div>
  <div class="pill">missing {{.Report.Comparison.Summary.Missing}}</div>
  <div class="pill">improved {{.Report.Comparison.Summary.Improved}}</div>
  <div class="pill">regressed {{.Report.Comparison.Summary.Regressed}}</div>
  <div class="pill">policy failures {{.Report.Comparison.Summary.PolicyFailures}}</div>
</div>
<table>
<thead><tr><th>status</th><th>test</th><th>case</th><th>metric</th><th>score delta</th><th>pass</th></tr></thead>
<tbody>
{{range .EntryRows}}<tr><td>{{.Status}}</td><td>{{.Identity.TestName}}</td><td>{{.Identity.CaseName}}</td><td>{{.Identity.Metric}}</td><td>{{printf "%+.3f" .Delta.Score}}</td><td>{{.Delta.Pass}}</td></tr>{{end}}
</tbody>
</table>
{{end}}
</body>
</html>
`))

func writeMarkdownSummary(b *strings.Builder, summary ResultsSummary) {
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(
		b,
		"total=%d passed=%d failed=%d pass_rate=%.3f scenarios=%d scenario_runs=%d scenario_pass_runs=%d\n\n",
		summary.Total,
		summary.Passed,
		summary.Failed,
		summary.PassRate,
		summary.ScenarioTotal,
		summary.ScenarioRuns,
		summary.ScenarioPassRuns,
	)
	b.WriteString("## Metrics\n\n")
	b.WriteString("| metric | count | passed | failed | mean_score | stddev | p95_tokens | p95_latency_ns |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, row := range sortedReportMetricRows(summary.ByMetric) {
		fmt.Fprintf(
			b,
			"| %s | %d | %d | %d | %.3f | %.3f | %.1f | %d |\n",
			row.Name,
			row.Summary.Count,
			row.Summary.Passed,
			row.Summary.Failed,
			row.Summary.MeanScore,
			row.Summary.StdDev,
			row.Summary.P95Tokens,
			int64(row.Summary.P95Latency),
		)
	}
	b.WriteString("\n")
}

func writeMarkdownComparison(b *strings.Builder, report Report) {
	b.WriteString("## Comparison\n\n")
	fmt.Fprintf(
		b,
		"total=%d added=%d missing=%d improved=%d regressed=%d unchanged=%d policy_failures=%d\n\n",
		report.Summary.Total,
		report.Summary.Added,
		report.Summary.Missing,
		report.Summary.Improved,
		report.Summary.Regressed,
		report.Summary.Unchanged,
		report.Summary.PolicyFailures,
	)
	b.WriteString("| status | test | case | metric | score_delta | pass |\n")
	b.WriteString("|---|---|---|---|---:|---|\n")
	for _, entry := range changedEntries(&report) {
		fmt.Fprintf(
			b,
			"| %s | %s | %s | %s | %+.3f | %s |\n",
			entry.Status,
			entry.Identity.TestName,
			entry.Identity.CaseName,
			entry.Identity.Metric,
			entry.Delta.Score,
			entry.Delta.Pass,
		)
	}
	b.WriteString("\n")
}

func sortedReportMetricRows(groups map[string]MetricSummary) []reportMetricRow {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]reportMetricRow, len(keys))
	for i, key := range keys {
		rows[i] = reportMetricRow{Name: key, Summary: groups[key]}
	}
	return rows
}

func changedEntries(report *Report) []Entry {
	if report == nil {
		return nil
	}
	var entries []Entry
	for _, entry := range report.Entries {
		if entry.Status != StatusUnchanged {
			entries = append(entries, entry)
		}
	}
	return entries
}
