package core

import (
	"github.com/cattoosint/socfollowup-test/internal/xlsx"
)

// A green row means the analyst can stop thinking about that case. Nothing
// else may be painted green: an unverified send is amber, a duplicate row is
// grey, and anything still needing a human is red.
const (
	StatusSent           = "SENT"
	StatusSentUnverified = "SENT_UNVERIFIED"
	StatusSkipped        = "SKIPPED"
	StatusNotFound       = "NOT_FOUND"
	StatusError          = "ERROR"
	StatusQuit           = "QUIT"
	StatusDuplicate      = "DUPLICATE"
	StatusPending        = "PENDING"
)

// styleEntry is what the analyst reads and the colour it is painted.
type styleEntry struct {
	Text   string
	Colour string
}

// StatusStyle maps a status to its wording and colour.
var StatusStyle = map[string]styleEntry{
	StatusSent:           {"Replied", "C6EFCE"},               // green
	StatusSentUnverified: {"Replied (unconfirmed)", "FFEB9C"}, // amber
	StatusSkipped:        {"Skipped", "FFEB9C"},               // amber
	StatusNotFound:       {"NOT FOUND", "FFC7CE"},             // red
	StatusError:          {"ERROR", "FFC7CE"},                 // red
	StatusQuit:           {"Stopped", "E7E6E6"},               // grey
	StatusDuplicate:      {"Duplicate row", "E7E6E6"},         // grey
	StatusPending:        {"Not done yet", "FFF2CC"},          // pale yellow
}

// WriteReviewSheet copies the input sheet, adds a status column and a plain
// English reason, and colours every row.
//
// duplicateLines marks rows repeating a case handled further up, so a second
// row for the same case is not painted as though it were replied to in its own
// right.
func WriteReviewSheet(srcPath, column string, statusByCase map[string]string,
	outPath string, duplicateLines map[int]bool,
	reasonByCase map[string]string) error {

	if duplicateLines == nil {
		duplicateLines = map[int]bool{}
	}
	if reasonByCase == nil {
		reasonByCase = map[string]string{}
	}

	var stats Stats
	table, err := ReadTable(srcPath, &stats)
	if err != nil {
		return err
	}
	lines := stats.LineNumbers
	if len(lines) != len(table.Rows) {
		lines = nil
		for i := range table.Rows {
			lines = append(lines, i+2)
		}
	}

	doc := xlsx.Doc{
		SheetName:    "follow-up",
		FreezeTopRow: true,
		AutoFilter:   true,
	}

	header := make([]string, 0, len(table.Headers)+2)
	header = append(header, table.Headers...)
	header = append(header, "Follow-up status", "Why")
	doc.Rows = append(doc.Rows, xlsx.Row{Values: header, Bold: true})

	for i, row := range table.Rows {
		values := make([]string, 0, len(header))
		for _, h := range table.Headers {
			values = append(values, row[h])
		}

		num := CaseNumRE.FindString(row[column])
		status := StatusPending
		switch {
		case duplicateLines[lines[i]]:
			status = StatusDuplicate
		case num != "":
			if s, ok := statusByCase[num]; ok {
				status = s
			}
		}

		style, ok := StatusStyle[status]
		if !ok {
			style = StatusStyle[StatusPending]
		}

		var why string
		switch status {
		case StatusDuplicate:
			why = duplicateReason(statusByCase[num])
		case StatusPending:
			if num == "" {
				// The run DID reach this row and deliberately dropped it: the
				// cell holds no case number. Saying it was never reached sends
				// the analyst to re-run the sheet, where it is dropped again,
				// identically, forever.
				why = "No case number in this row - nothing was searched for. " +
					"Fix the cell, or follow this one up by hand"
			} else {
				why = "The run ended before reaching this case"
			}
		default:
			if num != "" {
				why = reasonByCase[num]
			}
		}

		values = append(values, style.Text, why)
		doc.Rows = append(doc.Rows, xlsx.Row{Values: values, FillRGB: style.Colour})
	}

	width := len(header)
	doc.ColWidths = make([]float64, width)
	for i := 0; i < width; i++ {
		switch {
		case i == width-1:
			doc.ColWidths[i] = 70 // the Why column carries a sentence
		case i == width-2:
			doc.ColWidths[i] = 22
		default:
			w := float64(len(table.Headers[i]) + 4)
			if w < 10 {
				w = 10
			}
			if w > 44 {
				w = 44
			}
			doc.ColWidths[i] = w
		}
	}

	return xlsx.Save(outPath, doc)
}

// duplicateReason describes a duplicate row by what actually happened to the
// first occurrence.
//
// It used to say "handled there" unconditionally, without ever consulting the
// earlier row's outcome - so when that row errored or was never reached, the
// sheet asserted a follow-up that never took place.
func duplicateReason(earlier string) string {
	switch earlier {
	case StatusSent:
		return "Same case appears earlier in the sheet - replied there"
	case "":
		return "Same case appears earlier in the sheet"
	default:
		return "Same case appears earlier in the sheet, which ended as " +
			earlier + " - check that row"
	}
}
