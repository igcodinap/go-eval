package compare

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	eval "github.com/igcodinap/go-eval"
)

const maxJSONLLineSize = 10 * 1024 * 1024

// Status describes how a result changed from baseline to current.
type Status string

const (
	StatusUnchanged Status = "unchanged"
	StatusImproved  Status = "improved"
	StatusRegressed Status = "regressed"
	StatusAdded     Status = "added"
	StatusMissing   Status = "missing"
)

// PassChange describes a pass/fail transition.
type PassChange string

const (
	PassUnchanged    PassChange = "unchanged"
	PassFailedToPass PassChange = "failed_to_passed"
	PassPassedToFail PassChange = "passed_to_failed"
)

// Identity is the key used to match baseline and current rows.
type Identity struct {
	TestName string
	CaseName string
	Metric   string
}

// IdentityFunc builds a comparison identity for a result row.
type IdentityFunc func(eval.RunResult) Identity

// Options configures comparison behavior.
type Options struct {
	// Identity overrides the default test-name and metric identity.
	Identity IdentityFunc

	// ScoreTolerance treats score deltas within this absolute value as
	// unchanged for status classification. The raw delta is still reported.
	ScoreTolerance float64
}

// Report is the deterministic comparison output.
type Report struct {
	Summary Summary
	Entries []Entry
}

// Summary counts entries by status.
type Summary struct {
	Total     int
	Added     int
	Missing   int
	Improved  int
	Regressed int
	Unchanged int
}

// Entry compares one matched result row.
//
// Occurrence is zero unless multiple rows share the same identity in a file, in
// which case rows are compared in file order.
type Entry struct {
	Identity    Identity
	Occurrence  int
	Status      Status
	Baseline    eval.RunResult
	Current     eval.RunResult
	HasBaseline bool
	HasCurrent  bool
	Delta       Delta
	Dimensions  []DimensionEntry
}

// Delta captures numeric and pass/fail changes for matched rows.
type Delta struct {
	Score            float64
	Tokens           int
	PromptTokens     int
	CompletionTokens int
	LatencyNS        int64
	Pass             PassChange
}

// DimensionEntry compares one Compound dimension.
type DimensionEntry struct {
	Name        string
	Status      Status
	Baseline    eval.DimensionResult
	Current     eval.DimensionResult
	HasBaseline bool
	HasCurrent  bool
	Delta       DimensionDelta
}

// DimensionDelta captures numeric and pass/fail changes for a dimension.
type DimensionDelta struct {
	Score     float64
	Threshold float64
	Pass      PassChange
}

// DefaultIdentity matches rows by test name and metric.
func DefaultIdentity(result eval.RunResult) Identity {
	return Identity{
		TestName: result.TestName,
		Metric:   result.Metric,
	}
}

// ReadJSONL reads RunResult rows from a JSONL stream.
func ReadJSONL(r io.Reader) ([]eval.RunResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), maxJSONLLineSize)

	var results []eval.RunResult
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var result eval.RunResult
		if err := json.Unmarshal(line, &result); err != nil {
			return nil, fmt.Errorf("jsonl line %d: %w", lineNo, err)
		}
		results = append(results, result)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read jsonl: %w", err)
	}
	return results, nil
}

// ReadJSONLFile reads RunResult rows from a results.jsonl file.
func ReadJSONLFile(path string) ([]eval.RunResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	results, readErr := ReadJSONL(f)
	closeErr := f.Close()
	if readErr != nil && closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return results, nil
}

// CompareFiles reads and compares two JSONL result files.
func CompareFiles(baselinePath string, currentPath string) (Report, error) {
	baseline, err := ReadJSONLFile(baselinePath)
	if err != nil {
		return Report{}, fmt.Errorf("read baseline %q: %w", baselinePath, err)
	}
	current, err := ReadJSONLFile(currentPath)
	if err != nil {
		return Report{}, fmt.Errorf("read current %q: %w", currentPath, err)
	}
	return Compare(baseline, current), nil
}

// Compare compares baseline and current result rows using default options.
func Compare(baseline []eval.RunResult, current []eval.RunResult) Report {
	return CompareWithOptions(baseline, current, Options{})
}

// CompareWithOptions compares baseline and current result rows.
func CompareWithOptions(baseline []eval.RunResult, current []eval.RunResult, opts Options) Report {
	identify := opts.Identity
	if identify == nil {
		identify = DefaultIdentity
	}
	tolerance := math.Abs(opts.ScoreTolerance)

	baselineByID := indexResults(baseline, identify)
	currentByID := indexResults(current, identify)
	ids := sortedIdentities(baselineByID, currentByID)

	var report Report
	for _, id := range ids {
		baselineRows := baselineByID[id]
		currentRows := currentByID[id]
		maxLen := max(len(baselineRows), len(currentRows))
		for i := 0; i < maxLen; i++ {
			entry := Entry{
				Identity:   id,
				Occurrence: i,
			}

			switch {
			case i >= len(baselineRows):
				entry.Status = StatusAdded
				entry.Current = currentRows[i]
				entry.HasCurrent = true
			case i >= len(currentRows):
				entry.Status = StatusMissing
				entry.Baseline = baselineRows[i]
				entry.HasBaseline = true
			default:
				entry.Baseline = baselineRows[i]
				entry.Current = currentRows[i]
				entry.HasBaseline = true
				entry.HasCurrent = true
				entry.Delta = compareDelta(entry.Baseline, entry.Current)
				entry.Dimensions = compareDimensions(entry.Baseline.Dimensions, entry.Current.Dimensions, tolerance)
				entry.Status = classify(entry.Baseline, entry.Current, entry.Delta, entry.Dimensions, tolerance)
			}

			report.Entries = append(report.Entries, entry)
			report.Summary.add(entry.Status)
		}
	}

	return report
}

func indexResults(results []eval.RunResult, identify IdentityFunc) map[Identity][]eval.RunResult {
	indexed := make(map[Identity][]eval.RunResult)
	for _, result := range results {
		id := identify(result)
		indexed[id] = append(indexed[id], result)
	}
	return indexed
}

func sortedIdentities(left map[Identity][]eval.RunResult, right map[Identity][]eval.RunResult) []Identity {
	seen := make(map[Identity]struct{}, len(left)+len(right))
	for id := range left {
		seen[id] = struct{}{}
	}
	for id := range right {
		seen[id] = struct{}{}
	}

	ids := make([]Identity, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i int, j int) bool {
		return compareIdentity(ids[i], ids[j]) < 0
	})
	return ids
}

func compareIdentity(left Identity, right Identity) int {
	if left.TestName != right.TestName {
		return stringsCompare(left.TestName, right.TestName)
	}
	if left.CaseName != right.CaseName {
		return stringsCompare(left.CaseName, right.CaseName)
	}
	return stringsCompare(left.Metric, right.Metric)
}

func stringsCompare(left string, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareDelta(baseline eval.RunResult, current eval.RunResult) Delta {
	return Delta{
		Score:            current.Score - baseline.Score,
		Tokens:           current.Tokens - baseline.Tokens,
		PromptTokens:     current.PromptTokens - baseline.PromptTokens,
		CompletionTokens: current.CompletionTokens - baseline.CompletionTokens,
		LatencyNS:        current.LatencyNS - baseline.LatencyNS,
		Pass:             comparePass(baseline.Passed, current.Passed),
	}
}

func comparePass(baseline bool, current bool) PassChange {
	switch {
	case !baseline && current:
		return PassFailedToPass
	case baseline && !current:
		return PassPassedToFail
	default:
		return PassUnchanged
	}
}

func compareDimensions(baseline []eval.DimensionResult, current []eval.DimensionResult, tolerance float64) []DimensionEntry {
	baselineByName := indexDimensions(baseline)
	currentByName := indexDimensions(current)
	names := sortedDimensionNames(baselineByName, currentByName)

	entries := make([]DimensionEntry, 0, len(names))
	for _, name := range names {
		entry := DimensionEntry{Name: name}
		baselineDimension, hasBaseline := baselineByName[name]
		currentDimension, hasCurrent := currentByName[name]
		entry.Baseline = baselineDimension
		entry.Current = currentDimension
		entry.HasBaseline = hasBaseline
		entry.HasCurrent = hasCurrent

		switch {
		case !hasBaseline:
			entry.Status = StatusAdded
		case !hasCurrent:
			entry.Status = StatusMissing
		default:
			entry.Delta = DimensionDelta{
				Score:     currentDimension.Score - baselineDimension.Score,
				Threshold: currentDimension.Threshold - baselineDimension.Threshold,
				Pass:      comparePass(baselineDimension.Passed, currentDimension.Passed),
			}
			entry.Status = classifyDimension(baselineDimension, currentDimension, entry.Delta, tolerance)
		}

		entries = append(entries, entry)
	}
	return entries
}

func indexDimensions(dimensions []eval.DimensionResult) map[string]eval.DimensionResult {
	indexed := make(map[string]eval.DimensionResult, len(dimensions))
	for _, dimension := range dimensions {
		indexed[dimension.Name] = dimension
	}
	return indexed
}

func sortedDimensionNames(left map[string]eval.DimensionResult, right map[string]eval.DimensionResult) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for name := range left {
		seen[name] = struct{}{}
	}
	for name := range right {
		seen[name] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func classify(baseline eval.RunResult, current eval.RunResult, delta Delta, dimensions []DimensionEntry, tolerance float64) Status {
	switch {
	case baseline.Passed && !current.Passed:
		return StatusRegressed
	case !baseline.Passed && current.Passed:
		return StatusImproved
	}

	hasImprovement := false
	for _, dimension := range dimensions {
		switch dimension.Status {
		case StatusRegressed, StatusMissing:
			return StatusRegressed
		case StatusImproved:
			hasImprovement = true
		}
	}
	if hasImprovement {
		if delta.Score < -tolerance {
			return StatusRegressed
		}
		return StatusImproved
	}
	switch {
	case delta.Score < -tolerance:
		return StatusRegressed
	case delta.Score > tolerance:
		return StatusImproved
	}
	return StatusUnchanged
}

func classifyDimension(baseline eval.DimensionResult, current eval.DimensionResult, delta DimensionDelta, tolerance float64) Status {
	switch {
	case baseline.Passed && !current.Passed:
		return StatusRegressed
	case !baseline.Passed && current.Passed:
		return StatusImproved
	case delta.Score < -tolerance:
		return StatusRegressed
	case delta.Score > tolerance:
		return StatusImproved
	default:
		return StatusUnchanged
	}
}

func (s *Summary) add(status Status) {
	s.Total++
	switch status {
	case StatusAdded:
		s.Added++
	case StatusMissing:
		s.Missing++
	case StatusImproved:
		s.Improved++
	case StatusRegressed:
		s.Regressed++
	case StatusUnchanged:
		s.Unchanged++
	}
}
