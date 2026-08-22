package core

import (
	"reflect"
	"testing"
)

// A plain substring test would make case 610529 match a mail about SOC1610529
// - a different case entirely. Go's regexp engine has no lookbehind, so this
// boundary is hand-written and needs its own proof.
func TestLabelMatchesCaseRespectsDigitBoundaries(t *testing.T) {
	cases := []struct {
		label string
		num   string
		want  bool
	}{
		{"RE: SOC610529 Suspicious login", "610529", true},
		{"[CLOSED] [SOC#610529] alert", "610529", true},
		{"SOC-610529 follow up", "610529", true},
		{"soc 610529 lowercase subject", "610529", true},
		{"610529", "610529", true},

		// the defect this exists for: a longer number that contains ours
		{"RE: SOC1610529 different case", "610529", false},
		{"RE: SOC6105290 different case", "610529", false},
		{"RE: SOC16105290 different case", "610529", false},

		{"", "610529", false},
		{"RE: SOC700001 another case", "610529", false},
	}
	for _, c := range cases {
		if got := LabelMatchesCase(c.label, c.num); got != c.want {
			t.Errorf("LabelMatchesCase(%q, %q) = %v, want %v",
				c.label, c.num, got, c.want)
		}
	}
}

// A repeated case number must still match on its second occurrence, even when
// the first one is bounded by digits.
func TestLabelMatchesCaseScansPastABlockedMatch(t *testing.T) {
	if !LabelMatchesCase("SOC1610529 quoted, real one is SOC610529", "610529") {
		t.Fatal("stopped at the first blocked match instead of scanning on")
	}
}

func TestBuildQueriesOrderAndForms(t *testing.T) {
	// no subject: forms: they matched nothing live, and leaving one in the
	// search box after a miss made the tool look like it searched wrongly
	want := []string{
		"SOC610529",
		"SOC 610529",
		"610529",
	}
	got := BuildQueries("610529")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query list drifted:\n got %q\nwant %q", got, want)
	}
}

func TestCaseNumberShape(t *testing.T) {
	for _, s := range []string{"12345", "610529", "12345678"} {
		if CaseNumRE.FindString(s) != s {
			t.Errorf("%q should be a whole case number", s)
		}
	}
	if CaseNumRE.MatchString("1234") {
		t.Error("4 digits should not be a case number")
	}
	if got := CaseNumRE.FindString("SOC610529"); got != "610529" {
		t.Errorf("want 610529 out of SOC610529, got %q", got)
	}
}

func TestNoResultsPhrasesAreRecognised(t *testing.T) {
	for _, s := range []string{
		"We couldn't find anything",
		"We didn't find any results",
		"No results found",
		"Nothing that matches your search",
		"nothing matched",
	} {
		if !NoResultsRE.MatchString(s) {
			t.Errorf("not recognised as an empty search: %q", s)
		}
	}
	if NoResultsRE.MatchString("RE: SOC610529 Suspicious login") {
		t.Error("a real subject was read as an empty search")
	}
}

func TestDraftMarkersAreDetected(t *testing.T) {
	if !LooksLikeDraft("[Draft] RE: SOC610529") {
		t.Error("an unsent draft was not detected")
	}
	if LooksLikeDraft("SOC Alerts 1:10 PM RE: SOC610529") {
		t.Error("a real message was mistaken for a draft")
	}
}
