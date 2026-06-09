package compare

import (
	"math"
	"sort"
	"strings"
	"time"

	eval "github.com/igcodinap/go-eval"
)

const defaultFlakyScoreStdDev = 0.05

// ResultsSummary aggregates one JSONL result file.
type ResultsSummary struct {
	Total            int
	Passed           int
	Failed           int
	PassRate         float64
	ScenarioTotal    int
	ScenarioPassed   int
	ScenarioFailed   int
	ScenarioRuns     int
	ScenarioPassRuns int
	ByMetric         map[string]MetricSummary
	ByTier           map[string]MetricSummary
	ByFlow           map[string]MetricSummary
	ByDataset        map[string]MetricSummary
	ByCase           map[string]MetricSummary
	Flaky            []FlakySummary
}

// MetricSummary aggregates result rows for one metric.
type MetricSummary struct {
	Count       int
	Passed      int
	Failed      int
	PassRate    float64
	MeanScore   float64
	StdDev      float64
	MinScore    float64
	MaxScore    float64
	MeanLatency time.Duration
	P95Latency  time.Duration
	MeanTokens  float64
	P95Tokens   float64
}

// SummaryOptions configures reliability summary aggregation.
type SummaryOptions struct {
	Identity IdentityFunc

	// FlakyScoreStdDev overrides the default and policy flaky-score threshold
	// when positive.
	FlakyScoreStdDev float64

	// Policy supplies stable case identity and flaky-score thresholds when direct
	// options do not override them.
	Policy *Policy
}

// FlakySummary identifies repeated rows with mixed pass/fail or score variance.
type FlakySummary struct {
	Identity   Identity
	Count      int
	Passed     int
	Failed     int
	MeanScore  float64
	StdDev     float64
	MixedPass  bool
	ScoreFlaky bool
}

type metricAccumulator struct {
	count        int
	passed       int
	scoreSum     float64
	scoreSquares float64
	minScore     float64
	maxScore     float64
	latencySum   int64
	tokenSum     int
	latencies    []int64
	tokens       []int

	flakyThreshold    float64
	hasFlakyThreshold bool
}

// SummarizeFile reads and summarizes one JSONL result file.
func SummarizeFile(path string) (ResultsSummary, error) {
	results, err := ReadJSONLFile(path)
	if err != nil {
		return ResultsSummary{}, err
	}
	return Summarize(results), nil
}

// SummarizeFileWithPolicy reads and summarizes one JSONL result file using policy.
func SummarizeFileWithPolicy(path string, policy Policy) (ResultsSummary, error) {
	results, err := ReadJSONLFile(path)
	if err != nil {
		return ResultsSummary{}, err
	}
	return SummarizeWithPolicy(results, policy), nil
}

// Summarize aggregates one set of result rows by pass/fail and metric.
func Summarize(results []eval.RunResult) ResultsSummary {
	return SummarizeWithOptions(results, SummaryOptions{})
}

// SummarizeWithPolicy aggregates one set of result rows using compare policy for
// stable case identity and flaky-score thresholds.
func SummarizeWithPolicy(results []eval.RunResult, policy Policy) ResultsSummary {
	return SummarizeWithOptions(results, SummaryOptions{Policy: &policy})
}

// SummarizeWithOptions aggregates one set of result rows with reliability options.
func SummarizeWithOptions(results []eval.RunResult, opts SummaryOptions) ResultsSummary {
	identity := opts.Identity
	if identity == nil {
		if opts.Policy != nil && opts.Policy.CaseIDKey != "" {
			identity = StableCaseIDFromMetadata(opts.Policy.CaseIDKey)
		} else {
			identity = CaseIDFromMetadata("")
		}
	}
	explicitFlakyThreshold := opts.FlakyScoreStdDev > 0
	flakyThreshold := opts.FlakyScoreStdDev
	if flakyThreshold <= 0 {
		flakyThreshold = defaultFlakyScoreStdDev
	}

	summary := ResultsSummary{
		ByMetric:  map[string]MetricSummary{},
		ByTier:    map[string]MetricSummary{},
		ByFlow:    map[string]MetricSummary{},
		ByDataset: map[string]MetricSummary{},
		ByCase:    map[string]MetricSummary{},
	}
	metricAccumulators := map[string]*metricAccumulator{}
	tierAccumulators := map[string]*metricAccumulator{}
	flowAccumulators := map[string]*metricAccumulator{}
	datasetAccumulators := map[string]*metricAccumulator{}
	caseAccumulators := map[Identity]*metricAccumulator{}

	for _, result := range results {
		if isScenarioSummaryRow(result) {
			summary.addScenario(result)
			continue
		}
		summary.Total++
		if result.Passed {
			summary.Passed++
		} else {
			summary.Failed++
		}

		addToAccumulator(metricAccumulators, result.Metric, result)
		if tier := metadataString(result.Metadata, "tier"); tier != "" {
			addToAccumulator(tierAccumulators, tier, result)
		}
		if flow := metadataString(result.Metadata, "flow"); flow != "" {
			addToAccumulator(flowAccumulators, flow, result)
		}
		if dataset := metadataString(result.Metadata, "dataset"); dataset != "" {
			addToAccumulator(datasetAccumulators, dataset, result)
		}
		addToIdentityAccumulator(
			caseAccumulators,
			identity(result),
			result,
			flakyThresholdForResult(result, flakyThreshold, explicitFlakyThreshold, opts.Policy),
		)
	}

	if summary.Total > 0 {
		summary.PassRate = float64(summary.Passed) / float64(summary.Total)
	}
	for metric, acc := range metricAccumulators {
		summary.ByMetric[metric] = acc.summary()
	}
	for tier, acc := range tierAccumulators {
		summary.ByTier[tier] = acc.summary()
	}
	for flow, acc := range flowAccumulators {
		summary.ByFlow[flow] = acc.summary()
	}
	for dataset, acc := range datasetAccumulators {
		summary.ByDataset[dataset] = acc.summary()
	}
	for identity, acc := range caseAccumulators {
		caseSummary := acc.summary()
		summary.ByCase[summaryIdentityKey(identity)] = caseSummary
		if flaky := flakySummary(identity, caseSummary, acc.flakyThresholdOrDefault(flakyThreshold)); flaky.Count > 0 {
			summary.Flaky = append(summary.Flaky, flaky)
		}
	}
	sort.Slice(summary.Flaky, func(i int, j int) bool {
		return compareIdentity(summary.Flaky[i].Identity, summary.Flaky[j].Identity) < 0
	})
	return summary
}

func isScenarioSummaryRow(result eval.RunResult) bool {
	return result.Kind == "scenario_summary" || result.Metric == "_scenario_summary"
}

func (a *metricAccumulator) add(result eval.RunResult) {
	a.count++
	if result.Passed {
		a.passed++
	}
	a.scoreSum += result.Score
	a.scoreSquares += result.Score * result.Score
	a.minScore = math.Min(a.minScore, result.Score)
	a.maxScore = math.Max(a.maxScore, result.Score)
	a.latencySum += result.LatencyNS
	a.tokenSum += result.Tokens
	a.latencies = append(a.latencies, result.LatencyNS)
	a.tokens = append(a.tokens, result.Tokens)
}

func (a metricAccumulator) summary() MetricSummary {
	mean := a.scoreSum / float64(a.count)
	variance := a.scoreSquares/float64(a.count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return MetricSummary{
		Count:       a.count,
		Passed:      a.passed,
		Failed:      a.count - a.passed,
		PassRate:    float64(a.passed) / float64(a.count),
		MeanScore:   mean,
		StdDev:      math.Sqrt(variance),
		MinScore:    a.minScore,
		MaxScore:    a.maxScore,
		MeanLatency: time.Duration(a.latencySum / int64(a.count)),
		P95Latency:  time.Duration(percentileInt64(a.latencies, 0.95)),
		MeanTokens:  float64(a.tokenSum) / float64(a.count),
		P95Tokens:   float64(percentileInt(a.tokens, 0.95)),
	}
}

func (s *ResultsSummary) addScenario(result eval.RunResult) {
	s.ScenarioTotal++
	passed := result.Passed
	runCount := 1
	passRuns := 0
	if result.ScenarioSummary != nil {
		passed = result.ScenarioSummary.Passed
		if result.ScenarioSummary.RunCount > 0 {
			runCount = result.ScenarioSummary.RunCount
		}
		passRuns = result.ScenarioSummary.PassRuns
	} else if passed {
		passRuns = 1
	}
	if passed {
		s.ScenarioPassed++
	} else {
		s.ScenarioFailed++
	}
	s.ScenarioRuns += runCount
	s.ScenarioPassRuns += passRuns
}

func addToAccumulator(accumulators map[string]*metricAccumulator, key string, result eval.RunResult) {
	if key == "" {
		return
	}
	acc := accumulators[key]
	if acc == nil {
		acc = &metricAccumulator{
			minScore: math.Inf(1),
			maxScore: math.Inf(-1),
		}
		accumulators[key] = acc
	}
	acc.add(result)
}

func addToIdentityAccumulator(accumulators map[Identity]*metricAccumulator, identity Identity, result eval.RunResult, flakyThreshold float64) {
	if summaryIdentityKey(identity) == "" {
		return
	}
	acc := accumulators[identity]
	if acc == nil {
		acc = &metricAccumulator{
			minScore: math.Inf(1),
			maxScore: math.Inf(-1),
		}
		accumulators[identity] = acc
	}
	acc.add(result)
	acc.setFlakyThreshold(flakyThreshold)
}

func (a *metricAccumulator) setFlakyThreshold(threshold float64) {
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return
	}
	if !a.hasFlakyThreshold || threshold < a.flakyThreshold {
		a.flakyThreshold = threshold
		a.hasFlakyThreshold = true
	}
}

func (a metricAccumulator) flakyThresholdOrDefault(defaultThreshold float64) float64 {
	if a.hasFlakyThreshold {
		return a.flakyThreshold
	}
	return defaultThreshold
}

func flakyThresholdForResult(result eval.RunResult, defaultThreshold float64, explicitDefault bool, policy *Policy) float64 {
	if policy == nil || explicitDefault {
		return defaultThreshold
	}
	threshold := defaultThreshold
	apply := func(metricPolicy MetricPolicy) {
		if metricPolicy.FlakyScoreStdDev != nil {
			threshold = math.Abs(*metricPolicy.FlakyScoreStdDev)
		}
	}
	apply(policy.Default)
	tier := tierFromResult(result)
	if tierPolicy, ok := policy.Tiers[tier]; ok {
		apply(tierPolicy)
	}
	if metricPolicy, ok := policy.Metrics[result.Metric]; ok {
		apply(metricPolicy)
	}
	if byTier, ok := policy.MetricTiers[result.Metric]; ok {
		if metricTierPolicy, ok := byTier[tier]; ok {
			apply(metricTierPolicy)
		}
	}
	return threshold
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return value
}

func summaryIdentityKey(identity Identity) string {
	parts := make([]string, 0, 3)
	if identity.TestName != "" {
		parts = append(parts, identity.TestName)
	}
	if identity.CaseName != "" {
		parts = append(parts, identity.CaseName)
	}
	if identity.Metric != "" {
		parts = append(parts, identity.Metric)
	}
	return strings.Join(parts, "/")
}

func flakySummary(identity Identity, summary MetricSummary, threshold float64) FlakySummary {
	mixedPass := summary.Passed > 0 && summary.Failed > 0
	scoreFlaky := summary.StdDev > threshold
	if summary.Count < 2 || (!mixedPass && !scoreFlaky) {
		return FlakySummary{}
	}
	return FlakySummary{
		Identity:   identity,
		Count:      summary.Count,
		Passed:     summary.Passed,
		Failed:     summary.Failed,
		MeanScore:  summary.MeanScore,
		StdDev:     summary.StdDev,
		MixedPass:  mixedPass,
		ScoreFlaky: scoreFlaky,
	}
}

func percentileInt64(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i int, j int) bool { return sorted[i] < sorted[j] })
	index := percentileIndex(len(sorted), p)
	return sorted[index]
}

func percentileInt(values []int, p float64) int {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	index := percentileIndex(len(sorted), p)
	return sorted[index]
}

func percentileIndex(length int, p float64) int {
	if length <= 1 {
		return 0
	}
	index := int(math.Ceil(p*float64(length))) - 1
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}
