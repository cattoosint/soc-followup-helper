package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cattoosint/socfollowup-test/internal/xlsx"
)

// Every case here is a way the tool could quietly mislead an analyst - a case
// dropped without being counted, or a row coloured as though it were handled.

// writeSheet builds a small .xlsx and returns its path. hiddenRows lists
// one-based sheet rows that Excel should treat as filtered out.
func writeSheet(t *testing.T, name string, headers []string, rows [][]string,
	hiddenRows ...int) string {
	t.Helper()

	hidden := map[int]bool{}
	for _, r := range hiddenRows {
		hidden[r] = true
	}

	doc := xlsx.Doc{SheetName: "Sheet1"}
	doc.Rows = append(doc.Rows, xlsx.Row{Values: headers, Bold: true})
	for i, r := range rows {
		doc.Rows = append(doc.Rows, xlsx.Row{Values: r, Hidden: hidden[i+2]})
	}

	path := filepath.Join(t.TempDir(), name)
	if err := xlsx.Save(path, doc); err != nil {
		t.Fatalf("saving %s: %v", name, err)
	}
	return path
}

func readBack(t *testing.T, path string) *xlsx.Workbook {
	t.Helper()
	book, err := xlsx.Open(path)
	if err != nil {
		t.Fatalf("reopening %s: %v", path, err)
	}
	return book
}

// --- choosing the case column --------------------------------------------

func TestMostlyEmptyColumnDoesNotBeatTheIDColumn(t *testing.T) {
	path := writeSheet(t, "columns.xlsx",
		[]string{"id", "name", "notes"},
		[][]string{
			{"7014563", "alert", ""},
			{"7014420", "alert", ""},
			{"7014294", "alert", ""},
			{"7014424", "alert", "999999"},
		})
	col, cases, err := ExtractCases(path, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if col != "id" || len(cases) != 4 {
		t.Fatalf("chose %q -> %v", col, cases)
	}
}

func TestRepeatedHeadingDoesNotShadowTheFirst(t *testing.T) {
	path := writeSheet(t, "dupe_heading.xlsx",
		[]string{"id", "name", "id"},
		[][]string{{"7014563", "alert", "junk"}})
	_, cases, err := ExtractCases(path, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0] != "7014563" {
		t.Fatalf("got %v", cases)
	}
}

// --- nothing dropped without being counted -------------------------------

func TestNothingIsDroppedSilently(t *testing.T) {
	path := writeSheet(t, "dropped.xlsx",
		[]string{"id", "name"},
		[][]string{
			{"7014563", "ok"},
			{"", "blank id"},
			{"NOPE", "unreadable"},
			{"7014563", "duplicate"},
			{"7014420", "ok"},
		})
	r, err := Preflight(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Stats.Blanks) != 1 {
		t.Errorf("blanks = %v, want 1", r.Stats.Blanks)
	}
	if len(r.Stats.Unreadable) != 1 || r.Stats.Unreadable[0].Line != 4 ||
		r.Stats.Unreadable[0].Raw != "NOPE" {
		t.Errorf("unreadable = %v, want row 4 NOPE", r.Stats.Unreadable)
	}
	if len(r.Stats.Duplicates) != 1 || r.Stats.Duplicates[0].Line != 5 ||
		r.Stats.Duplicates[0].Raw != "7014563" {
		t.Errorf("duplicates = %v, want row 5 7014563", r.Stats.Duplicates)
	}
	accounted := len(r.Cases) + len(r.Stats.Blanks) + len(r.Stats.Unreadable) +
		len(r.Stats.Duplicates)
	if accounted != r.Stats.RowsRead {
		t.Errorf("%d rows accounted for, %d read", accounted, r.Stats.RowsRead)
	}
}

// The analyst filtered those rows out on purpose; following them up anyway
// would undo the filtering they did.
func TestFilteredRowsAreSkippedAndCounted(t *testing.T) {
	path := writeSheet(t, "filtered.xlsx",
		[]string{"id", "name"},
		[][]string{
			{"700001", "visible"},
			{"700002", "hidden by the analyst"},
			{"700003", "visible"},
		},
		3) // sheet row 3 is the second data row

	var stats Stats
	_, cases, err := ExtractCases(path, "", &stats)
	if err != nil {
		t.Fatal(err)
	}
	if stats.HiddenSkipped != 1 {
		t.Errorf("HiddenSkipped = %d, want 1", stats.HiddenSkipped)
	}
	if len(cases) != 2 || cases[0] != "700001" || cases[1] != "700003" {
		t.Errorf("cases = %v, want the two visible rows", cases)
	}
}

// --- review sheet honesty ------------------------------------------------

// statusColumn locates the status column by name - a Why column follows it, so
// counting from the right would move when that changed.
func statusColumn(t *testing.T, book *xlsx.Workbook) int {
	t.Helper()
	if len(book.Active.Rows) == 0 {
		t.Fatal("the review sheet has no header row")
	}
	for i, h := range book.Active.Rows[0] {
		if strings.EqualFold(strings.TrimSpace(h), "follow-up status") {
			return i
		}
	}
	t.Fatal("no status column in the review sheet")
	return -1
}

func fillsOf(t *testing.T, path string) []string {
	t.Helper()
	fills, err := xlsx.RowFills(path)
	if err != nil {
		t.Fatalf("reading fills: %v", err)
	}
	return fills
}

func TestDuplicateRowIsNotPaintedAsReplied(t *testing.T) {
	src := writeSheet(t, "review_src.xlsx",
		[]string{"id", "name"},
		[][]string{
			{"700001", "handled"},
			{"700001", "same case again"},
			{"700002", "handled"},
		})
	var stats Stats
	col, _, err := ExtractCases(src, "", &stats)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "review.xlsx")
	err = WriteReviewSheet(src, col,
		map[string]string{"700001": StatusSent, "700002": StatusSent},
		out, stats.DuplicateLines, nil)
	if err != nil {
		t.Fatal(err)
	}

	book := readBack(t, out)
	sc := statusColumn(t, book)
	rows := book.Active.Rows
	fills := fillsOf(t, out)

	if got := rows[2][sc]; got != "Duplicate row" {
		t.Errorf("duplicate row status = %q", got)
	}
	if fills[2] == "C6EFCE" {
		t.Error("a duplicate row was painted green")
	}
	for _, r := range []int{1, 3} {
		if fills[r] != "C6EFCE" {
			t.Errorf("row %d fill = %q, want green", r+1, fills[r])
		}
	}
}

func TestUnconfirmedReplyIsNotPlainGreen(t *testing.T) {
	src := writeSheet(t, "unconfirmed_src.xlsx",
		[]string{"id", "name"},
		[][]string{{"700001", "handled"}})
	out := filepath.Join(t.TempDir(), "review.xlsx")
	if err := WriteReviewSheet(src, "id",
		map[string]string{"700001": StatusSentUnverified}, out, nil, nil); err != nil {
		t.Fatal(err)
	}

	book := readBack(t, out)
	sc := statusColumn(t, book)
	fills := fillsOf(t, out)

	if !strings.Contains(strings.ToLower(book.Active.Rows[1][sc]), "unconfirmed") {
		t.Errorf("status = %q, want it to say unconfirmed", book.Active.Rows[1][sc])
	}
	if fills[1] == "C6EFCE" {
		t.Error("an unverified send was painted plain green")
	}
}

// --- the sheet says why --------------------------------------------------

func TestEveryOutcomeExplainsItself(t *testing.T) {
	src := writeSheet(t, "why_src.xlsx",
		[]string{"id", "name"},
		[][]string{
			{"700001", "handled"},
			{"700001", "same case again"},
			{"700002", "handled"},
		})
	var stats Stats
	col, _, err := ExtractCases(src, "", &stats)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "review.xlsx")
	err = WriteReviewSheet(src, col,
		map[string]string{"700001": StatusNotFound, "700002": StatusSkipped},
		out, stats.DuplicateLines,
		map[string]string{
			"700001": "Searched 5 ways - no mail mentions 700001",
			"700002": "Draft was opened; the analyst skipped it without sending",
		})
	if err != nil {
		t.Fatal(err)
	}

	rows := readBack(t, out).Active.Rows
	head := rows[0]
	if len(head) < 2 || head[len(head)-2] != "Follow-up status" ||
		head[len(head)-1] != "Why" {
		t.Fatalf("headers end with %v, want [Follow-up status Why]", head)
	}
	want := []struct {
		row      int
		contains string
	}{
		{1, "Searched 5 ways"},
		{2, "earlier in the sheet"},
		{3, "skipped it without sending"},
	}
	for _, w := range want {
		got := rows[w.row][len(rows[w.row])-1]
		if !strings.Contains(got, w.contains) {
			t.Errorf("row %d why = %q, want it to mention %q",
				w.row+1, got, w.contains)
		}
	}
}

func TestUnreachedCasesAreNotLeftBlank(t *testing.T) {
	src := writeSheet(t, "pending_src.xlsx",
		[]string{"id", "name"},
		[][]string{{"700001", "done"}, {"700002", "never reached"}})
	out := filepath.Join(t.TempDir(), "review.xlsx")
	if err := WriteReviewSheet(src, "id",
		map[string]string{"700001": StatusSent}, out, nil, nil); err != nil {
		t.Fatal(err)
	}
	rows := readBack(t, out).Active.Rows
	last := rows[2]
	if last[len(last)-2] != "Not done yet" {
		t.Errorf("unreached case status = %q", last[len(last)-2])
	}
	if !strings.Contains(last[len(last)-1], "ended before reaching") {
		t.Errorf("unreached case why = %q", last[len(last)-1])
	}
}

// --- encodings -----------------------------------------------------------

func TestExcelSavedCSVIsReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cp1252.csv")
	// "Café alert" as Excel writes it: 0xE9 for the accented e, not UTF-8
	data := append([]byte("id,name\n7014563,Caf"), 0xE9)
	data = append(data, []byte(" alert\n")...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, cases, err := ExtractCases(path, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0] != "7014563" {
		t.Fatalf("cases = %v", cases)
	}
}

// The whole point of decoding cp1252 is that the analyst's text survives.
func TestCP1252TextIsDecodedNotMangled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accents.csv")
	// "Café" and a Windows-1252 smart quote, which is where Latin-1 differs
	data := append([]byte("id,name\n7014563,Caf"), 0xE9)
	data = append(data, ' ', 0x92, 's', ' ', 'a', 'l', 'e', 'r', 't', '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	table, err := ReadTable(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := table.Rows[0]["name"]
	if !strings.Contains(name, "Café") {
		t.Errorf("name = %q, want the accent preserved", name)
	}
	if !strings.Contains(name, "’") {
		t.Errorf("name = %q, want the smart quote preserved", name)
	}
}
