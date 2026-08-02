// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"math"
	"strings"
	"testing"
)

func TestSummarizeComparisonsSeparatesCorrectnessAndEfficiency(t *testing.T) {
	t.Parallel()

	observations := []Observation{
		comparisonObservation("baseline", "case-1", 1, 0, 100, 20, 0.20),
		comparisonObservation("candidate", "case-1", 1, 1, 120, 18, 0.25),
		comparisonObservation("baseline", "case-2", 2, 1, 200, 40, 0.40),
		comparisonObservation("candidate", "case-2", 2, 1, 160, 30, 0.35),
	}
	report := SummarizeComparisons(observations)
	if len(report.EvalSets) != 1 || len(report.EvalSets[0].Comparisons) != 1 {
		t.Fatalf("report = %#v", report)
	}
	comparison := report.EvalSets[0].Comparisons[0]
	if comparison.Correctness.Lift == nil || *comparison.Correctness.Lift != 0.5 ||
		comparison.Correctness.CandidateWins != 1 || comparison.Correctness.Ties != 1 {
		t.Fatalf("correctness = %#v", comparison.Correctness)
	}
	if comparison.TotalTokens.MeanDelta == nil || *comparison.TotalTokens.MeanDelta != -10 {
		t.Fatalf("token metric = %#v", comparison.TotalTokens)
	}
	if comparison.TotalMS.MeanDelta == nil || *comparison.TotalMS.MeanDelta != -6 {
		t.Fatalf("latency metric = %#v", comparison.TotalMS)
	}
	if comparison.EstimatedCostUSD.MeanDelta == nil || math.Abs(*comparison.EstimatedCostUSD.MeanDelta) > 1e-12 {
		t.Fatalf("cost metric = %#v", comparison.EstimatedCostUSD)
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", report.Diagnostics)
	}
}

func TestSummarizeComparisonsReportsIncompleteObservations(t *testing.T) {
	t.Parallel()

	missingCandidate := comparisonObservation("baseline", "case-2", 2, 1, 10, 10, 0.1)
	unscoredCandidate := comparisonObservation("candidate", "case-1", 1, 0, 10, 10, 0.1)
	unscoredCandidate.Outcome = OutcomeUnscored
	unscoredCandidate.Score = nil
	report := SummarizeComparisons([]Observation{
		comparisonObservation("baseline", "case-1", 1, 1, 10, 10, 0.1),
		unscoredCandidate,
		missingCandidate,
	})
	if len(report.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v", report.Diagnostics)
	}
	reasons := []string{report.Diagnostics[0].Reason, report.Diagnostics[1].Reason}
	if !containsString(reasons, "unscored-observation") || !containsString(reasons, "missing-observation") {
		t.Fatalf("diagnostic reasons = %#v", reasons)
	}
	comparison := report.EvalSets[0].Comparisons[0]
	if comparison.Correctness.EligiblePairs != 0 || comparison.Correctness.TotalPairs != 2 {
		t.Fatalf("correctness coverage = %#v", comparison.Correctness)
	}
}

func TestSummarizeComparisonsClassifiesDuplicateAndFailedRuns(t *testing.T) {
	t.Parallel()

	duplicate := comparisonObservation("baseline", "duplicate", 1, 1, 1, 1, 0)
	errored := comparisonObservation("candidate", "errored", 2, 0, 1, 1, 0)
	errored.Outcome = OutcomeErrored
	skipped := comparisonObservation("candidate", "skipped", 3, 0, 1, 1, 0)
	skipped.Outcome = OutcomeSkipped
	pending := comparisonObservation("candidate", "pending", 4, 0, 1, 1, 0)
	pending.Outcome = OutcomePending
	report := SummarizeComparisons([]Observation{
		duplicate,
		duplicate,
		comparisonObservation("candidate", "duplicate", 1, 1, 1, 1, 0),
		comparisonObservation("baseline", "errored", 2, 1, 1, 1, 0),
		errored,
		comparisonObservation("baseline", "skipped", 3, 1, 1, 1, 0),
		skipped,
		comparisonObservation("baseline", "pending", 4, 1, 1, 1, 0),
		pending,
	})
	reasons := make([]string, 0, len(report.Diagnostics))
	for _, diagnostic := range report.Diagnostics {
		reasons = append(reasons, diagnostic.Reason)
	}
	for _, expected := range []string{
		"duplicate-observation",
		"errored-observation",
		"skipped-observation",
		"pending-observation",
	} {
		if !containsString(reasons, expected) {
			t.Fatalf("diagnostic reasons %v missing %q", reasons, expected)
		}
	}
}

func TestFormatComparisonReportIsPlainAndActionable(t *testing.T) {
	t.Parallel()

	report := SummarizeComparisons([]Observation{
		comparisonObservation("baseline", "case", 1, 0, 10, 20, 0.1),
		comparisonObservation("candidate", "case", 1, 1, 8, 15, 0.08),
	})
	formatted := FormatComparisonReport(report)
	for _, expected := range []string{"Eval Comparisons", "prompt quality", "+100.0 pp", "Tokens", "-$0.0200"} {
		if !strings.Contains(formatted, expected) {
			t.Fatalf("formatted report missing %q:\n%s", expected, formatted)
		}
	}
	if strings.Contains(formatted, "\x1b[") {
		t.Fatalf("formatted report contains terminal control codes: %q", formatted)
	}
}

func comparisonObservation(
	harness, group string,
	repetition int,
	score, tokens, milliseconds, cost float64,
) Observation {
	return Observation{
		EvalSet:          "prompt quality",
		GroupKey:         group,
		TestName:         "answers",
		File:             "internal/evals/example_eval_test.go",
		Harness:          harness,
		Baseline:         "baseline",
		Candidates:       []string{"candidate"},
		Repetition:       repetition,
		Outcome:          OutcomeScored,
		Score:            floatPointer(score),
		TotalTokens:      floatPointer(tokens),
		TotalMS:          floatPointer(milliseconds),
		EstimatedCostUSD: floatPointer(cost),
	}
}

func floatPointer(value float64) *float64 {
	return &value
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
