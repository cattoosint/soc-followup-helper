package core

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The Go port must answer exactly as the reviewed Python original did.
// testdata/python_truth.json is generated from that original; if a change here
// drifts from it, this fails rather than silently altering which mail gets
// replied to.

const stampLayout = "2006-01-02T15:04:05"

type truth struct {
	Now        string `json:"now"`
	SenderPart []struct {
		Header string `json:"header"`
		Sender string `json:"sender"`
	} `json:"sender_part"`
	NormalizeText []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	} `json:"normalize_text"`
	LabelMatchesCase []struct {
		Label string `json:"label"`
		Num   string `json:"num"`
		Match bool   `json:"match"`
	} `json:"label_matches_case"`
	ParseDayFirst   []stampCase         `json:"parse_row_time_day_first"`
	ParseMonthFirst []stampCase         `json:"parse_row_time_month_first"`
	BuildQueries    map[string][]string `json:"build_queries"`
	ShouldAutoSend  []struct {
		Header  string `json:"header"`
		Body    string `json:"body"`
		Keyword string `json:"keyword"`
		Sender  string `json:"sender"`
		Send    bool   `json:"send"`
		Why     string `json:"why"`
	} `json:"should_auto_send"`
}

type stampCase struct {
	Label string `json:"label"`
	Stamp string `json:"stamp"`
}

func loadTruth(t *testing.T) truth {
	t.Helper()
	raw, err := os.ReadFile("testdata/python_truth.json")
	if err != nil {
		t.Fatalf("reading ground truth: %v", err)
	}
	var tr truth
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatalf("parsing ground truth: %v", err)
	}
	return tr
}

func TestConformsToPythonSenderPart(t *testing.T) {
	for _, c := range loadTruth(t).SenderPart {
		if got := SenderPart(c.Header); got != c.Sender {
			t.Errorf("SenderPart(%q)\n got %q\nwant %q", c.Header, got, c.Sender)
		}
	}
}

func TestConformsToPythonNormalizeText(t *testing.T) {
	for _, c := range loadTruth(t).NormalizeText {
		if got := NormalizeText(c.In); got != c.Out {
			t.Errorf("NormalizeText(%q)\n got %q\nwant %q", c.In, got, c.Out)
		}
	}
}

func TestConformsToPythonLabelMatchesCase(t *testing.T) {
	for _, c := range loadTruth(t).LabelMatchesCase {
		if got := LabelMatchesCase(c.Label, c.Num); got != c.Match {
			t.Errorf("LabelMatchesCase(%q, %q) = %v, want %v",
				c.Label, c.Num, got, c.Match)
		}
	}
}

// The one deliberate divergence from the Python original: its two trailing
// subject: queries are not built any more (see BuildQueries). The truth file
// is left exactly as the Python produced it, and the divergence is applied
// here in the open, so every other query must still match it character for
// character.
func withoutSubjectQueries(want []string) []string {
	kept := want[:0:0]
	for _, q := range want {
		if !strings.HasPrefix(q, "subject:") {
			kept = append(kept, q)
		}
	}
	return kept
}

func TestConformsToPythonBuildQueries(t *testing.T) {
	for num, truth := range loadTruth(t).BuildQueries {
		want := withoutSubjectQueries(truth)
		if len(want) == len(truth) {
			t.Fatalf("BuildQueries(%s): the truth file has no subject: "+
				"queries to drop - has it been rewritten?", num)
		}
		got := BuildQueries(num)
		if len(got) != len(want) {
			t.Fatalf("BuildQueries(%s): got %d queries, want %d",
				num, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("BuildQueries(%s)[%d] = %q, want %q",
					num, i, got[i], want[i])
			}
		}
	}
}

func TestConformsToPythonParseRowTime(t *testing.T) {
	tr := loadTruth(t)
	now, err := time.ParseInLocation(stampLayout, tr.Now, time.Local)
	if err != nil {
		t.Fatalf("bad fixture clock: %v", err)
	}
	t.Cleanup(func() { SetDayFirstForTest(nil) })

	for _, variant := range []struct {
		name     string
		dayFirst bool
		cases    []stampCase
	}{
		{"day-first machine", true, tr.ParseDayFirst},
		{"month-first machine", false, tr.ParseMonthFirst},
	} {
		flag := variant.dayFirst
		SetDayFirstForTest(&flag)
		for _, c := range variant.cases {
			ts, ok := ParseRowTime(c.Label, now)
			got := ""
			if ok {
				got = ts.Format(stampLayout)
			}
			if got != c.Stamp {
				t.Errorf("%s: ParseRowTime(%q)\n got %q\nwant %q",
					variant.name, c.Label, got, c.Stamp)
			}
		}
	}
}

func TestConformsToPythonShouldAutoSend(t *testing.T) {
	for _, c := range loadTruth(t).ShouldAutoSend {
		send, why := ShouldAutoSend(c.Header, c.Body, c.Keyword, c.Sender)
		if send != c.Send {
			t.Errorf("ShouldAutoSend(%q, %q, %q, %q) = %v, want %v\n go why: %s\npy why: %s",
				c.Header, c.Body, c.Keyword, c.Sender, send, c.Send, why, c.Why)
			continue
		}
		if why != c.Why {
			t.Errorf("reason drifted for (%q, %q, %q, %q)\n got %q\nwant %q",
				c.Header, c.Body, c.Keyword, c.Sender, why, c.Why)
		}
	}
}
