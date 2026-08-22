package core

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cattoosint/socfollowup-test/internal/xlsx"
)

// RowNote is a row that was read but not turned into a case, with the row
// number Excel shows so the analyst can go straight to it.
type RowNote struct {
	Line int
	Raw  string
}

// Stats records everything a read dropped. Nothing may be discarded without
// being counted here - a case that vanishes silently is a case nobody follows
// up on.
type Stats struct {
	RowsRead       int
	HiddenSkipped  int
	LineNumbers    []int
	Sheets         []string
	SheetUsed      string
	Unreadable     []RowNote
	Duplicates     []RowNote
	Blanks         []int
	DuplicateLines map[int]bool
}

// Table is a spreadsheet read into memory.
type Table struct {
	Headers []string
	Rows    []map[string]string
}

// uniqueHeaders makes repeated column names distinct.
//
// Exports sometimes carry the same heading twice. Collapsing them into one map
// key would silently keep only the last, so the tool could read case numbers
// from the wrong column.
func uniqueHeaders(headers []string) []string {
	seen := map[string]int{}
	out := make([]string, 0, len(headers))
	for i, h := range headers {
		name := strings.TrimSpace(h)
		if name == "" {
			name = "column" + strconv.Itoa(i+1)
		}
		if n, ok := seen[name]; ok {
			seen[name] = n + 1
			name = fmt.Sprintf("%s (%d)", name, n+1)
		} else {
			seen[name] = 1
		}
		out = append(out, name)
	}
	return out
}

// trimQuotes strips surrounding quotes from a pasted path. The quote
// characters are given by code point so this file carries no escape sequence.
func trimQuotes(s string) string {
	const dq, sq = 34, 39
	s = strings.TrimSpace(s)
	for len(s) > 1 {
		first, last := s[0], s[len(s)-1]
		if (first == dq && last == dq) || (first == sq && last == sq) {
			s = s[1 : len(s)-1]
			continue
		}
		break
	}
	return s
}

// ReadTable reads a .csv or .xlsx file. For .xlsx, rows hidden by a filter are
// skipped and counted - the analyst filtered them out on purpose.
func ReadTable(path string, stats *Stats) (Table, error) {
	path = trimQuotes(path)
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".xlsx" || ext == ".xlsm" {
		return readWorkbook(path, stats)
	}
	return readCSV(path, stats)
}

func readWorkbook(path string, stats *Stats) (Table, error) {
	book, err := xlsx.Open(path)
	if err != nil {
		// Open in Excel: work from a copy rather than making the analyst close
		// their sheet.
		copyPath, cerr := copyToTemp(path)
		if cerr != nil {
			return Table{}, fmt.Errorf("opening %s: %w", filepath.Base(path), err)
		}
		if book, err = xlsx.Open(copyPath); err != nil {
			return Table{}, fmt.Errorf("opening %s: %w", filepath.Base(path), err)
		}
	}

	if stats != nil && len(book.SheetNames) > 1 {
		// the active sheet is whichever tab was showing when it was saved
		stats.Sheets = book.SheetNames
		stats.SheetUsed = book.Active.Name
	}

	var kept [][]string
	var lines []int
	hidden := 0
	for i, row := range book.Active.Rows {
		excelRow := i + 1
		if excelRow > 1 && i < len(book.Active.Hidden) && book.Active.Hidden[i] {
			hidden++
			continue
		}
		kept = append(kept, row)
		lines = append(lines, excelRow) // the row number Excel shows
	}
	if stats != nil {
		stats.HiddenSkipped = hidden
		if len(lines) > 1 {
			stats.LineNumbers = lines[1:] // drop the heading row
		}
	}
	if len(kept) == 0 {
		return Table{}, nil
	}
	return buildTable(kept[0], kept[1:]), nil
}

func readCSV(path string, stats *Stats) (Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		copyPath, cerr := copyToTemp(path)
		if cerr != nil {
			return Table{}, fmt.Errorf("opening %s: %w", filepath.Base(path), err)
		}
		if data, err = os.ReadFile(copyPath); err != nil {
			return Table{}, fmt.Errorf("opening %s: %w", filepath.Base(path), err)
		}
	}

	// Excel writes CSV in the local codepage, not UTF-8, so a name with an
	// accent in it would otherwise make the whole export unreadable.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		data = decodeCP1252(data)
	}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1 // ragged exports are reported, not fatal
	reader.LazyQuotes = true
	var records [][]string
	for {
		rec, rerr := reader.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return Table{}, fmt.Errorf("reading %s: %w", filepath.Base(path), rerr)
		}
		records = append(records, rec)
	}
	if len(records) == 0 {
		return Table{}, nil
	}
	if stats != nil {
		stats.HiddenSkipped = 0
		stats.LineNumbers = nil
		for i := range records[1:] {
			stats.LineNumbers = append(stats.LineNumbers, i+2)
		}
	}
	return buildTable(records[0], records[1:]), nil
}

// buildTable pairs each row with the headers, padding short rows. Excel and
// the CSV writer both trim trailing empty cells, so a row can be narrower than
// the heading row.
func buildTable(headerRow []string, dataRows [][]string) Table {
	headers := uniqueHeaders(headerRow)
	rows := make([]map[string]string, 0, len(dataRows))
	for _, r := range dataRows {
		row := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(r) {
				row[h] = r[i]
			} else {
				row[h] = ""
			}
		}
		rows = append(rows, row)
	}
	return Table{Headers: headers, Rows: rows}
}

func copyToTemp(path string) (string, error) {
	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst := filepath.Join(os.TempDir(), "_soc_followup_"+filepath.Base(path))
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return "", err
	}
	return dst, nil
}

// DetectCaseColumn picks the column most likely to hold case numbers.
func DetectCaseColumn(headers []string, rows []map[string]string) string {
	best, bestScore := "", 0.0

	// Rows with nothing in them at all do not count. Excel keeps a <row>
	// element for a row whose contents were deleted, so a 50-row export can
	// carry 120 empty ones - enough to drag an unambiguous "Incident ID"
	// column under the threshold and make the tool refuse a sheet it could
	// read perfectly well.
	populated := 0
	for _, r := range rows {
		for _, v := range r {
			if strings.TrimSpace(v) != "" {
				populated++
				break
			}
		}
	}
	if populated == 0 {
		populated = 1
	}
	for _, col := range headers {
		if col == "" {
			continue
		}
		filled, hits := 0, 0
		for _, r := range rows {
			v := strings.TrimSpace(r[col])
			if v == "" {
				continue
			}
			filled++
			if CaseNumRE.MatchString(v) {
				hits++
			}
		}
		if filled == 0 {
			continue
		}
		// Score against every row that has ANY data, not just this column's
		// filled ones - otherwise a column with two stray numbers and 300
		// blanks scores a perfect 1.0 and beats the real id column.
		//
		// Wholly empty rows are excluded: Excel keeps <row> elements for rows
		// whose contents were deleted, and counting those sank a perfectly
		// unambiguous column below the threshold, so the tool refused a sheet
		// it could read.
		frac := float64(hits) / float64(populated)

		namedLikeACase := false
		lower := strings.ToLower(col)
		for _, h := range ColumnHints {
			if strings.Contains(lower, h) {
				namedLikeACase = true
				break
			}
		}
		bonus, threshold := 0.0, 0.6
		if namedLikeACase {
			// A column called "Case ID" counts even if some rows are
			// unparseable - a messy export should not stop the tool dead.
			// Those rows are reported by Preflight rather than dropped.
			bonus, threshold = 0.5, 0.3
		}
		if frac >= threshold && frac+bonus > bestScore {
			best, bestScore = col, frac+bonus
		}
	}
	return best
}

// ExtractCases returns the column used and the case numbers in sheet order,
// deduplicated. Everything it declines to use is recorded in stats.
func ExtractCases(path, column string, stats *Stats) (string, []string, error) {
	if stats == nil {
		stats = &Stats{}
	}
	table, err := ReadTable(path, stats)
	if err != nil {
		return "", nil, err
	}
	if len(table.Headers) == 0 {
		return "", nil, fmt.Errorf("no header row found in %s", filepath.Base(path))
	}
	if column != "" {
		found := false
		for _, h := range table.Headers {
			if h == column {
				found = true
				break
			}
		}
		if !found {
			return "", nil, fmt.Errorf("column %q not in file. Available: %s",
				column, strings.Join(table.Headers, ", "))
		}
	} else {
		column = DetectCaseColumn(table.Headers, table.Rows)
		if column == "" {
			return "", nil, fmt.Errorf(
				"could not auto-detect the case-number column. Available columns: %s",
				strings.Join(table.Headers, ", "))
		}
	}

	// report the row numbers Excel shows, which skip over filtered-out rows
	lines := stats.LineNumbers
	if len(lines) != len(table.Rows) {
		lines = nil
		for i := range table.Rows {
			lines = append(lines, i+2)
		}
	}

	seen := map[string]bool{}
	var cases []string
	stats.RowsRead = len(table.Rows)
	stats.Unreadable = nil
	stats.Duplicates = nil
	stats.Blanks = nil
	stats.DuplicateLines = map[int]bool{}

	for i, row := range table.Rows {
		line := lines[i]
		raw := strings.TrimSpace(row[column])
		m := CaseNumberIn(raw)
		if m == "" {
			if raw != "" {
				if len(raw) > 40 {
					raw = raw[:40]
				}
				stats.Unreadable = append(stats.Unreadable, RowNote{Line: line, Raw: raw})
			} else {
				// an empty cell is still a dropped row
				stats.Blanks = append(stats.Blanks, line)
			}
			continue
		}
		if seen[m] {
			stats.Duplicates = append(stats.Duplicates, RowNote{Line: line, Raw: m})
			stats.DuplicateLines[line] = true
			continue
		}
		seen[m] = true
		cases = append(cases, m)
	}
	return column, cases, nil
}

// PreflightReport is a dry run of an export: what will be processed, and what
// will be ignored. Nothing touches Outlook.
type PreflightReport struct {
	Column       string
	Columns      []string
	Cases        []string
	Stats        Stats
	HeaderIsData []string
	// IsCSV means no Excel filter survived into this file, so every row will
	// be followed up - including the ones the analyst filtered out.
	IsCSV bool
}

// Preflight reads an export and reports what a run would do with it.
func Preflight(path, column string) (PreflightReport, error) {
	var stats Stats
	isCSV := strings.EqualFold(filepath.Ext(path), ".csv")
	col, cases, err := ExtractCases(path, column, &stats)
	if err != nil {
		return PreflightReport{}, err
	}
	table, err := ReadTable(path, nil)
	if err != nil {
		return PreflightReport{}, err
	}
	// An export with no header row would have its first case number read as a
	// column name - one case silently missing from the whole run.
	var headerIsData []string
	for _, h := range table.Headers {
		if h != "" && CaseNumRE.MatchString(h) {
			headerIsData = append(headerIsData, h)
		}
	}
	return PreflightReport{
		Column:       col,
		Columns:      table.Headers,
		Cases:        cases,
		Stats:        stats,
		HeaderIsData: headerIsData,
		IsCSV:        isCSV,
	}, nil
}

// FormatPreflight renders a report for the analyst, leading with anything that
// would cause a case to be missed.
func FormatPreflight(r PreflightReport) string {
	var out []string
	add := func(format string, args ...any) {
		out = append(out, fmt.Sprintf(format, args...))
	}
	if r.IsCSV {
		// The tracker page warned about this and the console did not - and the
		// console is the form the deployment runbook hands you. A CSV silently
		// contains the rows the analyst filtered out, and following one of
		// those up is worse than doing nothing at all.
		add("!! This is a .csv, so no Excel filter survived into it.")
		add("   EVERY row below will be followed up, including any you had")
		add("   filtered out. Save the filtered sheet as .xlsx if that matters.")
		add("")
	}
	add("Case-number column : %q", r.Column)
	add("Columns in file    : %s", strings.Join(r.Columns, ", "))
	add("Rows read          : %d", r.Stats.RowsRead)
	if len(r.Stats.Sheets) > 1 {
		add("!! The workbook has %d sheets [%s].",
			len(r.Stats.Sheets), strings.Join(r.Stats.Sheets, ", "))
		add("   Reading %q - the one that was showing when it was saved.",
			r.Stats.SheetUsed)
	}
	if r.Stats.HiddenSkipped > 0 {
		add("Filtered out (Excel): %d row(s) skipped", r.Stats.HiddenSkipped)
	}
	add("Cases to process   : %d", len(r.Cases))
	if n := len(r.Stats.Duplicates); n > 0 {
		add("Duplicates ignored : %d (e.g. row %d: %s)",
			n, r.Stats.Duplicates[0].Line, r.Stats.Duplicates[0].Raw)
	}
	if n := len(r.Stats.Blanks); n > 0 {
		add("Blank case cells   : %d row(s) (e.g. row %d) - ignored",
			n, r.Stats.Blanks[0])
	}
	if n := len(r.Stats.Unreadable); n > 0 {
		add("!! NO CASE NUMBER FOUND in %d row(s) - these will be IGNORED:", n)
		for i, u := range r.Stats.Unreadable {
			if i == 10 {
				add("     ... and %d more", n-10)
				break
			}
			add("     row %d: %q", u.Line, u.Raw)
		}
		add("   (case numbers must be 5-8 digits; tell me the real format if " +
			"yours differ)")
	}
	if len(r.HeaderIsData) > 0 {
		shown := r.HeaderIsData
		if len(shown) > 3 {
			shown = shown[:3]
		}
		add("!! The first row looks like DATA, not column headings (%s).",
			strings.Join(shown, ", "))
		add("   Row 1 is always treated as headings, so that case would be " +
			"missed - add a heading row to the export.")
	}
	if len(r.Cases) > 0 {
		shown := make([]string, 0, 6)
		for i, c := range r.Cases {
			if i == 6 {
				break
			}
			shown = append(shown, "SOC"+c)
		}
		add("First few          : %s", strings.Join(shown, ", "))
	}
	return strings.Join(out, "\n")
}

// cp1252High maps the bytes 0x80-0x9F, the only range where Windows-1252
// differs from Latin-1. Zero means the byte is undefined in the codepage.
var cp1252High = [32]rune{
	0x20AC, 0, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017D, 0,
	0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0, 0x017E, 0x0178,
}

// decodeCP1252 converts a Windows-1252 byte stream to UTF-8.
//
// Excel writes CSV in the local codepage, not UTF-8, so a name with an accent
// in it would otherwise make the whole export unreadable. This is hand-written
// rather than pulled from golang.org/x/text: that package cost 7.5 MB of
// vendored source, and this is the only encoding that ever turns up.
func decodeCP1252(data []byte) []byte {
	var out strings.Builder
	out.Grow(len(data) + len(data)/8)
	for _, b := range data {
		switch {
		case b < 0x80:
			out.WriteByte(b)
		case b < 0xA0:
			if r := cp1252High[b-0x80]; r != 0 {
				out.WriteRune(r)
			} else {
				out.WriteRune(0xFFFD)
			}
		default:
			out.WriteRune(rune(b)) // Latin-1 range maps straight through
		}
	}
	return []byte(out.String())
}
