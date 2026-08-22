package core

import (
	"strings"
	"testing"
)

// More regressions from the adversarial audit. Each was demonstrated against
// the old code.

// keyword == "" was rejected BEFORE normalisation but matched AFTER it, and
// NormalizeText strips every space and zero-width character. So a phrase of
// spaces normalised to "", strings.Contains(x, "") is always true, and
// auto-send quietly collapsed from two conditions to one - while the log
// cheerfully reported a match.
func TestAWhitespacePhraseDoesNotMatchEveryMail(t *testing.T) {
	body := "SOC700001 alert. Host quarantined. Nothing else."
	sender := "alerts@example.com"

	for _, phrase := range []string{" ", "\t", "   ", "​"} {
		ok, why := ShouldAutoSend("SOC Alerts <alerts@example.com>", body,
			phrase, sender)
		if ok {
			t.Errorf("phrase %q matched a body that does not contain it "+
				"(reason %q) - auto-send would fire on every mail",
				phrase, why)
		}
	}

	// and a real phrase must still work
	if ok, why := ShouldAutoSend("SOC Alerts <alerts@example.com>",
		"Please follow up on this. "+body, "follow up", sender); !ok {
		t.Errorf("a genuine phrase no longer matches: %s", why)
	}
}

// CaseNumRE.FindString returned the first 5-8 digit run anywhere in the cell,
// so a longer id was silently reshaped into a different, valid-looking case
// number. The tool then searched for a case the analyst never wrote - and two
// 9-digit ids sharing 8 leading digits collapsed into one, the second row
// painted grey "handled there" for a case nobody touched.
func TestAnOutOfShapeCaseCellIsRefusedNotReshaped(t *testing.T) {
	refused := []string{
		"123456789",     // too long - must not become 12345678
		"1234567890",    // ditto
		"INC0000610529", // must not become 00006105
		"1.2345678E7",   // an exponent-formatted cell
		"1234",          // too short
	}
	for _, raw := range refused {
		if got := CaseNumberIn(raw); got != "" {
			t.Errorf("cell %q was read as case %q - the analyst never wrote "+
				"that number", raw, got)
		}
	}

	accepted := map[string]string{
		"610529":     "610529",
		"SOC610529":  "610529",
		"SOC 610529": "610529",
		" 7109167 ":  "7109167",
		"#700001":    "700001",
	}
	for raw, want := range accepted {
		if got := CaseNumberIn(raw); got != want {
			t.Errorf("cell %q read as %q, want %q", raw, got, want)
		}
	}
}

// Two different ids that share eight leading digits must stay two cases.
func TestTwoLongIdsDoNotCollapseIntoOneCase(t *testing.T) {
	a, b := CaseNumberIn("123456789"), CaseNumberIn("123456781")
	if a != "" || b != "" {
		t.Fatalf("both ids were reshaped (%q, %q); if they collapse to the "+
			"same case the second row is reported as already handled", a, b)
	}
}

// Excel keeps a <row> element for a row whose contents were deleted, so a
// 50-row export can carry a long tail of empty ones. Counting those in the
// denominator dragged an unambiguous column under the threshold and made the
// tool refuse a sheet it could read perfectly well.
func TestBlankRowsDoNotHideTheCaseColumn(t *testing.T) {
	headers := []string{"Incident ID", "Name"}
	rows := []map[string]string{}
	for i := 0; i < 50; i++ {
		rows = append(rows, map[string]string{
			"Incident ID": "610529", "Name": "Suspicious login"})
	}
	for i := 0; i < 120; i++ {
		rows = append(rows, map[string]string{"Incident ID": "", "Name": ""})
	}

	if got := DetectCaseColumn(headers, rows); got != "Incident ID" {
		t.Errorf("detected %q, want \"Incident ID\" - 120 emptied rows below "+
			"the data made the tool refuse a sheet it can read", got)
	}
}

// The duplicate reason asserted a follow-up that may never have happened.
func TestADuplicateDoesNotClaimTheEarlierRowWasHandled(t *testing.T) {
	// This is a statement about wording, so it is checked where the wording
	// lives: a duplicate whose earlier row ERRORED must not say "handled".
	for _, earlier := range []string{StatusError, StatusNotFound, StatusSkipped} {
		why := duplicateReason(earlier)
		if strings.Contains(strings.ToLower(why), "handled there") ||
			strings.Contains(strings.ToLower(why), "replied there") {
			t.Errorf("earlier row ended %s, yet the duplicate row says %q",
				earlier, why)
		}
		if !strings.Contains(why, earlier) {
			t.Errorf("earlier row ended %s, but the reason %q does not say so",
				earlier, why)
		}
	}
	if why := duplicateReason(StatusSent); !strings.Contains(why, "replied there") {
		t.Errorf("earlier row was replied to, but the reason reads %q", why)
	}
}
