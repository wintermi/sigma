// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ObservationOutcome identifies whether a comparative run can be scored.
type ObservationOutcome string

const (
	// OutcomeScored has a finite average judge score.
	OutcomeScored ObservationOutcome = "scored"
	// OutcomeUnscored completed without a judge score.
	OutcomeUnscored ObservationOutcome = "unscored"
	// OutcomeSkipped was skipped by the Go test.
	OutcomeSkipped ObservationOutcome = "skipped"
	// OutcomePending did not reach a final test status.
	OutcomePending ObservationOutcome = "pending"
	// OutcomeErrored failed in the harness, judge, persistence, or Go test.
	OutcomeErrored ObservationOutcome = "errored"
)

// Observation is one comparative harness result.
type Observation struct {
	EvalSet          string
	GroupKey         string
	TestName         string
	File             string
	Harness          string
	Baseline         string
	Candidates       []string
	Repetition       int
	Outcome          ObservationOutcome
	Score            *float64
	TotalTokens      *float64
	TotalMS          *float64
	EstimatedCostUSD *float64
}

// PairedMetricSummary reports paired candidate-minus-baseline telemetry.
type PairedMetricSummary struct {
	TotalPairs    int      `json:"totalPairs"`
	EligiblePairs int      `json:"eligiblePairs"`
	BaselineMean  *float64 `json:"baselineMean"`
	CandidateMean *float64 `json:"candidateMean"`
	MeanDelta     *float64 `json:"meanDelta"`
}

// CorrectnessLiftSummary reports paired pass-rate lift.
type CorrectnessLiftSummary struct {
	TotalPairs        int      `json:"totalPairs"`
	EligiblePairs     int      `json:"eligiblePairs"`
	BaselinePassRate  *float64 `json:"baselinePassRate"`
	CandidatePassRate *float64 `json:"candidatePassRate"`
	Lift              *float64 `json:"lift"`
	BaselineWins      int      `json:"baselineWins"`
	CandidateWins     int      `json:"candidateWins"`
	Ties              int      `json:"ties"`
}

// HarnessPairComparison compares one candidate with its declared baseline.
type HarnessPairComparison struct {
	Baseline         string                 `json:"baseline"`
	Candidate        string                 `json:"candidate"`
	Correctness      CorrectnessLiftSummary `json:"correctness"`
	TotalTokens      PairedMetricSummary    `json:"totalTokens"`
	TotalMS          PairedMetricSummary    `json:"totalMs"`
	EstimatedCostUSD PairedMetricSummary    `json:"estimatedCostUsd"`
}

// ComparisonDiagnostic explains an incomplete paired observation.
type ComparisonDiagnostic struct {
	EvalSet    string `json:"evalSet"`
	GroupKey   string `json:"groupKey"`
	TestName   string `json:"testName"`
	File       string `json:"file"`
	Repetition int    `json:"repetition"`
	Harness    string `json:"harness"`
	Reason     string `json:"reason"`
}

// EvalSetReport contains every baseline/candidate comparison for an eval set.
type EvalSetReport struct {
	EvalSet     string                  `json:"evalSet"`
	Comparisons []HarnessPairComparison `json:"comparisons"`
}

// ComparisonReport is the deterministic aggregate report schema.
type ComparisonReport struct {
	SchemaVersion int                    `json:"schemaVersion"`
	EvalSets      []EvalSetReport        `json:"evalSets"`
	Diagnostics   []ComparisonDiagnostic `json:"diagnostics"`
}

type harnessDescriptor struct {
	name  string
	index int
}

type observationGroup struct {
	evalSet    string
	groupKey   string
	testName   string
	file       string
	repetition int
	byHarness  map[string][]Observation
}

type evalSetData struct {
	baseline         harnessDescriptor
	candidatesByName map[string]harnessDescriptor
	groupsByKey      map[string]*observationGroup
}

type observationPair struct {
	baseline  Observation
	candidate Observation
}

// SummarizeComparisons creates deterministic paired comparison summaries.
func SummarizeComparisons(observations []Observation) ComparisonReport {
	dataBySet := make(map[string]*evalSetData)
	for _, observation := range observations {
		data := dataBySet[observation.EvalSet]
		if data == nil {
			data = &evalSetData{
				baseline:         harnessDescriptor{name: observation.Baseline},
				candidatesByName: make(map[string]harnessDescriptor),
				groupsByKey:      make(map[string]*observationGroup),
			}
			dataBySet[observation.EvalSet] = data
		}
		for index, candidate := range observation.Candidates {
			existing, ok := data.candidatesByName[candidate]
			if !ok || index < existing.index {
				data.candidatesByName[candidate] = harnessDescriptor{name: candidate, index: index}
			}
		}
		groupID := strings.Join([]string{observation.File, observation.TestName, observation.GroupKey}, "\x00")
		group := data.groupsByKey[groupID]
		if group == nil {
			group = &observationGroup{
				evalSet:    observation.EvalSet,
				groupKey:   observation.GroupKey,
				testName:   observation.TestName,
				file:       observation.File,
				repetition: observation.Repetition,
				byHarness:  make(map[string][]Observation),
			}
			data.groupsByKey[groupID] = group
		}
		group.byHarness[observation.Harness] = append(group.byHarness[observation.Harness], observation)
	}

	evalSetNames := make([]string, 0, len(dataBySet))
	for name := range dataBySet {
		evalSetNames = append(evalSetNames, name)
	}
	sort.Strings(evalSetNames)
	report := ComparisonReport{SchemaVersion: 1}
	for _, evalSet := range evalSetNames {
		data := dataBySet[evalSet]
		candidates := orderedCandidates(data)
		groups := orderedObservationGroups(data)
		setReport := EvalSetReport{EvalSet: evalSet}
		for _, candidate := range candidates {
			setReport.Comparisons = append(
				setReport.Comparisons,
				compareHarnesses(data.baseline, candidate, groups),
			)
		}
		report.EvalSets = append(report.EvalSets, setReport)
		report.Diagnostics = append(report.Diagnostics, comparisonDiagnostics(data, candidates, groups)...)
	}
	sort.Slice(report.Diagnostics, func(i, j int) bool {
		left, right := report.Diagnostics[i], report.Diagnostics[j]
		if left.EvalSet != right.EvalSet {
			return left.EvalSet < right.EvalSet
		}
		if left.File != right.File {
			return left.File < right.File
		}
		if left.TestName != right.TestName {
			return left.TestName < right.TestName
		}
		if left.GroupKey != right.GroupKey {
			return left.GroupKey < right.GroupKey
		}
		if left.Repetition != right.Repetition {
			return left.Repetition < right.Repetition
		}
		if left.Harness != right.Harness {
			return left.Harness < right.Harness
		}
		return left.Reason < right.Reason
	})
	return report
}

func orderedCandidates(data *evalSetData) []harnessDescriptor {
	candidates := make([]harnessDescriptor, 0, len(data.candidatesByName))
	for _, candidate := range data.candidatesByName {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].index < candidates[j].index ||
			(candidates[i].index == candidates[j].index && candidates[i].name < candidates[j].name)
	})
	return candidates
}

func orderedObservationGroups(data *evalSetData) []*observationGroup {
	groups := make([]*observationGroup, 0, len(data.groupsByKey))
	for _, group := range data.groupsByKey {
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].file != groups[j].file {
			return groups[i].file < groups[j].file
		}
		if groups[i].testName != groups[j].testName {
			return groups[i].testName < groups[j].testName
		}
		if groups[i].groupKey != groups[j].groupKey {
			return groups[i].groupKey < groups[j].groupKey
		}
		return groups[i].repetition < groups[j].repetition
	})
	return groups
}

func compareHarnesses(
	baseline, candidate harnessDescriptor,
	groups []*observationGroup,
) HarnessPairComparison {
	pairs := pairObservations(groups, baseline.name, candidate.name)
	return HarnessPairComparison{
		Baseline:         baseline.name,
		Candidate:        candidate.name,
		Correctness:      summarizeCorrectness(pairs, len(groups)),
		TotalTokens:      summarizeMetric(pairs, len(groups), func(o Observation) *float64 { return o.TotalTokens }),
		TotalMS:          summarizeMetric(pairs, len(groups), func(o Observation) *float64 { return o.TotalMS }),
		EstimatedCostUSD: summarizeMetric(pairs, len(groups), func(o Observation) *float64 { return o.EstimatedCostUSD }),
	}
}

func pairObservations(groups []*observationGroup, baseline, candidate string) []observationPair {
	var pairs []observationPair
	for _, group := range groups {
		baselineRuns := group.byHarness[baseline]
		candidateRuns := group.byHarness[candidate]
		if len(baselineRuns) == 1 && len(candidateRuns) == 1 {
			pairs = append(pairs, observationPair{baseline: baselineRuns[0], candidate: candidateRuns[0]})
		}
	}
	return pairs
}

func summarizeCorrectness(pairs []observationPair, totalPairs int) CorrectnessLiftSummary {
	summary := CorrectnessLiftSummary{TotalPairs: totalPairs}
	var baselinePasses, candidatePasses int
	for _, pair := range pairs {
		if pair.baseline.Outcome != OutcomeScored || pair.candidate.Outcome != OutcomeScored ||
			pair.baseline.Score == nil || pair.candidate.Score == nil {
			continue
		}
		summary.EligiblePairs++
		baselinePassed := *pair.baseline.Score >= 1
		candidatePassed := *pair.candidate.Score >= 1
		if baselinePassed {
			baselinePasses++
		}
		if candidatePassed {
			candidatePasses++
		}
		switch {
		case baselinePassed == candidatePassed:
			summary.Ties++
		case baselinePassed:
			summary.BaselineWins++
		default:
			summary.CandidateWins++
		}
	}
	if summary.EligiblePairs == 0 {
		return summary
	}
	baselineRate := float64(baselinePasses) / float64(summary.EligiblePairs)
	candidateRate := float64(candidatePasses) / float64(summary.EligiblePairs)
	lift := preciseDifference(candidateRate, baselineRate)
	summary.BaselinePassRate = &baselineRate
	summary.CandidatePassRate = &candidateRate
	summary.Lift = &lift
	return summary
}

func summarizeMetric(
	pairs []observationPair,
	totalPairs int,
	selectValue func(Observation) *float64,
) PairedMetricSummary {
	summary := PairedMetricSummary{TotalPairs: totalPairs}
	var baselineTotal, candidateTotal float64
	for _, pair := range pairs {
		if pair.baseline.Outcome != OutcomeScored || pair.candidate.Outcome != OutcomeScored {
			continue
		}
		baselineValue := selectValue(pair.baseline)
		candidateValue := selectValue(pair.candidate)
		if !finitePointer(baselineValue) || !finitePointer(candidateValue) {
			continue
		}
		summary.EligiblePairs++
		baselineTotal += *baselineValue
		candidateTotal += *candidateValue
	}
	if summary.EligiblePairs == 0 {
		return summary
	}
	baselineMean := baselineTotal / float64(summary.EligiblePairs)
	candidateMean := candidateTotal / float64(summary.EligiblePairs)
	delta := preciseDifference(candidateMean, baselineMean)
	summary.BaselineMean = &baselineMean
	summary.CandidateMean = &candidateMean
	summary.MeanDelta = &delta
	return summary
}

func finitePointer(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0)
}

func preciseDifference(left, right float64) float64 {
	formatted := strconv.FormatFloat(left-right, 'g', 15, 64)
	value, err := strconv.ParseFloat(formatted, 64)
	if err != nil {
		return left - right
	}
	return value
}

func comparisonDiagnostics(
	data *evalSetData,
	candidates []harnessDescriptor,
	groups []*observationGroup,
) []ComparisonDiagnostic {
	harnesses := append([]harnessDescriptor{data.baseline}, candidates...)
	var diagnostics []ComparisonDiagnostic
	for _, group := range groups {
		for _, harness := range harnesses {
			observations := group.byHarness[harness.name]
			reason := ""
			switch {
			case len(observations) == 0:
				reason = "missing-observation"
			case len(observations) > 1:
				reason = "duplicate-observation"
			case observations[0].Outcome == OutcomeErrored:
				reason = "errored-observation"
			case observations[0].Outcome == OutcomeUnscored:
				reason = "unscored-observation"
			case observations[0].Outcome == OutcomeSkipped:
				reason = "skipped-observation"
			case observations[0].Outcome == OutcomePending:
				reason = "pending-observation"
			case observations[0].Outcome != OutcomeScored:
				reason = "unscorable-outcome"
			}
			if reason == "" {
				continue
			}
			diagnostics = append(diagnostics, ComparisonDiagnostic{
				EvalSet:    group.evalSet,
				GroupKey:   group.groupKey,
				TestName:   group.testName,
				File:       group.file,
				Repetition: group.repetition,
				Harness:    harness.name,
				Reason:     reason,
			})
		}
	}
	return diagnostics
}

// FormatComparisonReport renders a stable plain-text terminal report.
func FormatComparisonReport(report ComparisonReport) string {
	hasComparisons := false
	for _, evalSet := range report.EvalSets {
		if len(evalSet.Comparisons) > 0 {
			hasComparisons = true
			break
		}
	}
	if !hasComparisons {
		return ""
	}
	lines := []string{"Eval Comparisons"}
	for _, evalSet := range report.EvalSets {
		lines = append(lines, "  "+evalSet.EvalSet)
		for index, comparison := range evalSet.Comparisons {
			if index > 0 {
				lines = append(lines, "")
			}
			lines = append(lines,
				formatReportLine("Baseline", comparison.Baseline),
				formatReportLine("Candidate", fmt.Sprintf("%s (%d/%d pairs)",
					comparison.Candidate,
					comparison.Correctness.EligiblePairs,
					comparison.Correctness.TotalPairs,
				)),
			)
			if comparison.Correctness.Lift == nil {
				lines = append(lines, formatReportLine("Pass rate", "unavailable"))
			} else {
				lines = append(lines, formatReportLine("Pass rate", fmt.Sprintf(
					"%+.1f pp (candidate %.1f%%, baseline %.1f%%)",
					*comparison.Correctness.Lift*100,
					*comparison.Correctness.CandidatePassRate*100,
					*comparison.Correctness.BaselinePassRate*100,
				)))
			}
			lines = append(lines,
				formatMetric("Tokens", comparison.TotalTokens, func(value float64) string {
					return fmt.Sprintf("%.1f", value)
				}, ""),
				formatMetric("Latency", comparison.TotalMS, func(value float64) string {
					return fmt.Sprintf("%.1fms", value)
				}, "ms"),
				formatMetric("Est. cost", comparison.EstimatedCostUSD, func(value float64) string {
					return fmt.Sprintf("$%.4f", value)
				}, "$"),
			)
		}
	}
	if len(report.Diagnostics) > 0 {
		lines = append(lines, "  Incomplete observations")
		for _, diagnostic := range report.Diagnostics {
			lines = append(lines, fmt.Sprintf(
				"    %s: %s/%s repetition %d, harness %s",
				diagnostic.Reason,
				diagnostic.File,
				diagnostic.TestName,
				diagnostic.Repetition,
				diagnostic.Harness,
			))
		}
	}
	return strings.Join(lines, "\n")
}

func formatReportLine(label, value string) string {
	return fmt.Sprintf("    %9s  %s", label, value)
}

func formatMetric(label string, metric PairedMetricSummary, formatValue func(float64) string, unitPrefix string) string {
	coverage := ""
	if metric.EligiblePairs > 0 && metric.EligiblePairs != metric.TotalPairs {
		coverage = fmt.Sprintf(" (%d/%d pairs)", metric.EligiblePairs, metric.TotalPairs)
	}
	if metric.BaselineMean == nil || metric.CandidateMean == nil || metric.MeanDelta == nil {
		return formatReportLine(label, "unavailable"+coverage)
	}
	delta := fmt.Sprintf("%+.1f", *metric.MeanDelta)
	switch unitPrefix {
	case "ms":
		delta += "ms"
	case "$":
		delta = fmt.Sprintf("%+.4f", *metric.MeanDelta)
		if strings.HasPrefix(delta, "-") {
			delta = "-$" + strings.TrimPrefix(delta, "-")
		} else {
			delta = "+$" + strings.TrimPrefix(delta, "+")
		}
	}
	return formatReportLine(label, fmt.Sprintf(
		"%s (candidate %s, baseline %s)%s",
		delta,
		formatValue(*metric.CandidateMean),
		formatValue(*metric.BaselineMean),
		coverage,
	))
}
