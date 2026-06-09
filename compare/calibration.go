package compare

import (
	"fmt"
	"math"
	"sort"

	eval "github.com/igcodinap/go-eval"
)

// CalibrationOptions configures judge disagreement and pairwise reports.
type CalibrationOptions struct {
	Identity       IdentityFunc
	CaseIDKey      string
	JudgeKey       string
	VariantKey     string
	ScoreTolerance float64
}

// CalibrationReport summarizes multi-judge agreement for one result set.
type CalibrationReport struct {
	Summary       CalibrationSummary
	Disagreements []JudgeDisagreement
	Pairwise      []PairwiseReport
}

// CalibrationSummary counts calibration groups and disagreements.
type CalibrationSummary struct {
	TotalGroups        int
	DisagreementGroups int
	JudgeCount         int
	PairwiseCount      int
}

// JudgeScore is one judge's score for an identity.
type JudgeScore struct {
	Judge  string
	Count  int
	Passes int
	Score  float64
	Passed bool
	Reason string
}

// JudgeDisagreement captures a case where judges materially disagreed.
type JudgeDisagreement struct {
	Identity          Identity
	Tier              string
	Flow              string
	Dataset           string
	Judges            []JudgeScore
	ScoreRange        float64
	PassDisagreement  bool
	ScoreDisagreement bool
}

// PairwiseReport aggregates pairwise wins between judges or variants.
type PairwiseReport struct {
	Left           string
	Right          string
	Count          int
	LeftWins       int
	RightWins      int
	Ties           int
	MeanScoreDelta float64
}

// CalibrateFile reads a JSONL result file and computes calibration summaries.
func CalibrateFile(path string, opts CalibrationOptions) (CalibrationReport, error) {
	results, err := ReadJSONLFile(path)
	if err != nil {
		return CalibrationReport{}, err
	}
	return Calibrate(results, opts), nil
}

// Calibrate computes judge disagreement and pairwise comparison summaries.
func Calibrate(results []eval.RunResult, opts CalibrationOptions) CalibrationReport {
	identity := calibrationIdentity(opts)
	tolerance := math.Abs(opts.ScoreTolerance)
	groups := map[Identity][]eval.RunResult{}
	judgeNames := map[string]struct{}{}

	for _, result := range results {
		if isScenarioSummaryRow(result) {
			continue
		}
		id := identity(result)
		groups[id] = append(groups[id], result)
		judgeNames[judgeName(result, opts.JudgeKey)] = struct{}{}
	}

	ids := make([]Identity, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i int, j int) bool {
		return compareIdentity(ids[i], ids[j]) < 0
	})

	report := CalibrationReport{
		Summary: CalibrationSummary{
			TotalGroups: len(ids),
			JudgeCount:  len(judgeNames),
		},
	}
	for _, id := range ids {
		disagreement := disagreementForGroup(id, groups[id], opts.JudgeKey, tolerance)
		if disagreement.PassDisagreement || disagreement.ScoreDisagreement {
			report.Disagreements = append(report.Disagreements, disagreement)
		}
	}
	report.Pairwise = pairwiseReports(groups, opts)
	report.Summary.DisagreementGroups = len(report.Disagreements)
	report.Summary.PairwiseCount = len(report.Pairwise)
	return report
}

func calibrationIdentity(opts CalibrationOptions) IdentityFunc {
	if opts.Identity != nil {
		return opts.Identity
	}
	if opts.CaseIDKey != "" {
		return StableCaseIDFromMetadata(opts.CaseIDKey)
	}
	return CaseIDFromMetadata("")
}

func disagreementForGroup(id Identity, rows []eval.RunResult, judgeKey string, tolerance float64) JudgeDisagreement {
	byJudge := map[string]*calibrationAggregate{}
	for _, row := range rows {
		name := judgeName(row, judgeKey)
		addCalibrationAggregate(byJudge, name, row)
	}
	judges := make([]string, 0, len(byJudge))
	for judge := range byJudge {
		judges = append(judges, judge)
	}
	sort.Strings(judges)

	out := JudgeDisagreement{Identity: id}
	minScore := math.Inf(1)
	maxScore := math.Inf(-1)
	passedSeen := map[bool]struct{}{}
	for _, judge := range judges {
		agg := byJudge[judge]
		row := agg.first
		if out.Tier == "" {
			out.Tier = metadataString(row.Metadata, "tier")
			out.Flow = metadataString(row.Metadata, "flow")
			out.Dataset = metadataString(row.Metadata, "dataset")
		}
		score := agg.meanScore()
		passed := agg.allPassed()
		out.Judges = append(out.Judges, JudgeScore{
			Judge:  judge,
			Count:  agg.count,
			Passes: agg.passes,
			Score:  score,
			Passed: passed,
			Reason: agg.reason(),
		})
		minScore = math.Min(minScore, score)
		maxScore = math.Max(maxScore, score)
		passedSeen[passed] = struct{}{}
		if agg.passes > 0 && agg.passes < agg.count {
			passedSeen[true] = struct{}{}
			passedSeen[false] = struct{}{}
		}
	}
	if len(out.Judges) > 0 {
		out.ScoreRange = maxScore - minScore
	}
	out.PassDisagreement = len(passedSeen) > 1
	out.ScoreDisagreement = out.ScoreRange > tolerance
	return out
}

func pairwiseReports(groups map[Identity][]eval.RunResult, opts CalibrationOptions) []PairwiseReport {
	key := opts.VariantKey
	if key == "" {
		key = opts.JudgeKey
	}
	tolerance := math.Abs(opts.ScoreTolerance)
	index := map[[2]string]*pairwiseAccumulator{}
	for _, rows := range groups {
		byName := map[string]*calibrationAggregate{}
		for _, row := range rows {
			name := pairwiseName(row, key, opts.JudgeKey)
			if name != "" {
				addCalibrationAggregate(byName, name, row)
			}
		}
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				pair := [2]string{names[i], names[j]}
				acc := index[pair]
				if acc == nil {
					acc = &pairwiseAccumulator{}
					index[pair] = acc
				}
				acc.add(byName[names[i]].meanScore(), byName[names[j]].meanScore(), tolerance)
			}
		}
	}
	pairs := make([][2]string, 0, len(index))
	for pair := range index {
		pairs = append(pairs, pair)
	}
	sort.Slice(pairs, func(i int, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	out := make([]PairwiseReport, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, index[pair].report(pair[0], pair[1]))
	}
	return out
}

type pairwiseAccumulator struct {
	count     int
	leftWins  int
	rightWins int
	ties      int
	deltaSum  float64
}

func (a *pairwiseAccumulator) add(leftScore float64, rightScore float64, tolerance float64) {
	a.count++
	delta := leftScore - rightScore
	a.deltaSum += delta
	switch {
	case delta > tolerance:
		a.leftWins++
	case delta < -tolerance:
		a.rightWins++
	default:
		a.ties++
	}
}

type calibrationAggregate struct {
	first    eval.RunResult
	count    int
	passes   int
	scoreSum float64
	reasons  []string
}

func addCalibrationAggregate(groups map[string]*calibrationAggregate, name string, row eval.RunResult) {
	agg := groups[name]
	if agg == nil {
		agg = &calibrationAggregate{first: row}
		groups[name] = agg
	}
	agg.count++
	if row.Passed {
		agg.passes++
	}
	agg.scoreSum += row.Score
	if row.Reason != "" && len(agg.reasons) < 2 {
		agg.reasons = append(agg.reasons, row.Reason)
	}
}

func (a calibrationAggregate) meanScore() float64 {
	if a.count == 0 {
		return 0
	}
	return a.scoreSum / float64(a.count)
}

func (a calibrationAggregate) allPassed() bool {
	return a.count > 0 && a.passes == a.count
}

func (a calibrationAggregate) reason() string {
	switch len(a.reasons) {
	case 0:
		return ""
	case 1:
		return a.reasons[0]
	default:
		return fmt.Sprintf("%d runs; first reasons: %s; %s", a.count, a.reasons[0], a.reasons[1])
	}
}

func (a pairwiseAccumulator) report(left string, right string) PairwiseReport {
	meanDelta := 0.0
	if a.count > 0 {
		meanDelta = a.deltaSum / float64(a.count)
	}
	return PairwiseReport{
		Left:           left,
		Right:          right,
		Count:          a.count,
		LeftWins:       a.leftWins,
		RightWins:      a.rightWins,
		Ties:           a.ties,
		MeanScoreDelta: meanDelta,
	}
}

func judgeName(result eval.RunResult, key string) string {
	if key != "" {
		if value := metadataValueString(result.Metadata, key); value != "" {
			return value
		}
	}
	for _, fallback := range []string{"judge", "judge_name"} {
		if value := metadataValueString(result.Metadata, fallback); value != "" {
			return value
		}
	}
	return "unknown"
}

func pairwiseName(result eval.RunResult, key string, judgeKey string) string {
	if key == "" {
		return judgeName(result, judgeKey)
	}
	if value := metadataValueString(result.Metadata, key); value != "" {
		return value
	}
	if key == judgeKey {
		return judgeName(result, judgeKey)
	}
	return ""
}

func metadataValueString(metadata map[string]any, key string) string {
	if value, ok := metadata[key]; ok && value != nil {
		return fmt.Sprint(value)
	}
	return ""
}
