package triage_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/triage"
)

func issue(n int, title string, labels []string, reporters ...string) triage.Issue {
	return triage.Issue{Number: n, Title: title, Labels: labels, Reporters: reporters}
}

func numbers(issues []triage.Issue) []int {
	var out []int
	for _, i := range issues {
		out = append(out, i.Number)
	}
	return out
}

func TestW7B7_SeverityIsTheMostSevereLabel(t *testing.T) {
	cases := []struct {
		labels []string
		want   int
	}{
		{[]string{"sev1"}, 1},
		{[]string{"bug", "sev3"}, 3},
		{[]string{"sev3", "sev2"}, 2},
		{[]string{"sev2", "sev1", "sev3"}, 1},
		{[]string{"bug"}, 0},
		{nil, 0},
	}
	for _, tc := range cases {
		if got := triage.Severity(triage.Issue{Labels: tc.labels}); got != tc.want {
			t.Errorf("Severity(%v) = %d, want %d", tc.labels, got, tc.want)
		}
	}
}

func TestW7B7_RankOrdersBySeverityThenDistinctReporters(t *testing.T) {
	got := triage.Rank([]triage.Issue{
		issue(1, "typo on cart", []string{"sev3"}, "asha", "ravi", "meera"),
		issue(2, "order button dead on slow network", []string{"sev1"}, "ravi"),
		issue(3, "map pin jumps", []string{"sev2"}, "asha"),
		issue(4, "rider app crash", []string{"sev2"}, "asha", "kabir"),
	})
	if want := []int{2, 4, 3, 1}; !slices.Equal(numbers(got), want) {
		t.Fatalf("rank = %v, want %v", numbers(got), want)
	}
}

func TestW7B7_RankCountsEachReporterOnce(t *testing.T) {
	got := triage.Rank([]triage.Issue{
		issue(1, "a", []string{"sev2"}, "Asha", "asha", " ASHA "),
		issue(2, "b", []string{"sev2"}, "ravi", "meera"),
	})
	if want := []int{2, 1}; !slices.Equal(numbers(got), want) {
		t.Fatalf("rank = %v, want %v: three mentions by one person are one reporter", numbers(got), want)
	}
}

func TestW7B7_RankBreaksTiesWithTheLowerIssueNumber(t *testing.T) {
	got := triage.Rank([]triage.Issue{
		issue(9, "x", []string{"sev2"}, "asha"),
		issue(4, "y", []string{"sev2"}, "ravi"),
		issue(6, "z", []string{"sev2"}, "meera"),
	})
	if want := []int{4, 6, 9}; !slices.Equal(numbers(got), want) {
		t.Fatalf("rank = %v, want %v", numbers(got), want)
	}
}

func TestW7B7_RankDoesNotMutateItsInput(t *testing.T) {
	in := []triage.Issue{
		issue(3, "c", []string{"sev3"}, "a"),
		issue(1, "a", []string{"sev1"}, "a"),
	}
	triage.Rank(in)
	if in[0].Number != 3 || in[1].Number != 1 {
		t.Fatalf("input order changed: %v", numbers(in))
	}
}

func TestW7B7_PlanFixesTheTopFiveAndDefersTheRest(t *testing.T) {
	var in []triage.Issue
	for n := 1; n <= 8; n++ {
		in = append(in, issue(n, "bug", []string{"sev2"}, "asha"))
	}
	fix, deferred, err := triage.Plan(in, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(numbers(fix), []int{1, 2, 3, 4, 5}) || !slices.Equal(numbers(deferred), []int{6, 7, 8}) {
		t.Fatalf("fix = %v deferred = %v", numbers(fix), numbers(deferred))
	}
}

func TestW7B7_PlanNeverSelectsWontfixIssues(t *testing.T) {
	in := []triage.Issue{
		{Number: 1, Title: "a", Labels: []string{"sev1", "wontfix"}, Reporters: []string{"asha", "ravi"}, Body: "Reason: needs a redesign, out of scope for week 7"},
		issue(2, "b", []string{"sev3"}, "asha"),
	}
	fix, deferred, err := triage.Plan(in, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(numbers(fix), []int{2}) || !slices.Equal(numbers(deferred), []int{1}) {
		t.Fatalf("fix = %v deferred = %v, want fix [2] deferred [1]", numbers(fix), numbers(deferred))
	}
}

func TestW7B7_PlanRejectsAnUntriagedIssue(t *testing.T) {
	_, _, err := triage.Plan([]triage.Issue{issue(12, "no severity", []string{"bug"}, "asha")}, 5)
	if err == nil || !strings.Contains(err.Error(), "12") {
		t.Fatalf("err = %v, want an error naming issue 12", err)
	}
}

func TestW7B7_PlanRejectsWontfixWithoutAWrittenReason(t *testing.T) {
	for _, body := range []string{"", "not now", "Reason:", "Reason:   "} {
		_, _, err := triage.Plan([]triage.Issue{{Number: 7, Labels: []string{"sev2", "wontfix"}, Reporters: []string{"asha"}, Body: body}}, 5)
		if err == nil || !strings.Contains(err.Error(), "7") {
			t.Errorf("body %q: err = %v, want an error naming issue 7", body, err)
		}
	}
}

func TestW7B7_PlanWithFewerIssuesThanSlotsFixesAllOfThem(t *testing.T) {
	fix, deferred, err := triage.Plan([]triage.Issue{issue(1, "a", []string{"sev1"}, "asha"), issue(2, "b", []string{"sev2"}, "ravi")}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(fix) != 2 || len(deferred) != 0 {
		t.Fatalf("fix = %v deferred = %v", numbers(fix), numbers(deferred))
	}
}

func TestW7B7_ReportListsWhatWasFixedAndWhatWasDeliberatelyLeft(t *testing.T) {
	fixed := []triage.Issue{issue(2, "order button dead on slow network", []string{"sev1"}, "ravi", "asha")}
	left := []triage.Issue{{Number: 1, Title: "dark mode", Labels: []string{"sev3", "wontfix"}, Reporters: []string{"meera"}, Body: "Reason: not worth the risk this week"}}
	report := triage.Report(fixed, left)
	for _, want := range []string{"## Fixed", "#2", "order button dead on slow network", "sev1", "2 reporters", "## Not fixed", "#1", "dark mode", "not worth the risk this week"} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
	if strings.Index(report, "## Fixed") > strings.Index(report, "## Not fixed") {
		t.Error("the Fixed section must come before Not fixed")
	}
}
