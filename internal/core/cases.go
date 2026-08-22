package core

import (
	"regexp"
	"strings"
)

// CaseNumRE is the shape of a case number: 5 to 8 digits.
var CaseNumRE = regexp.MustCompile(`\d{5,8}`)

// digitRunRE finds each unbroken run of digits in a cell.
var digitRunRE = regexp.MustCompile(`\d+`)

// CaseNumberIn returns the case number in a cell, or "" when the cell does not
// hold exactly one well-formed case number.
//
// The rule is deliberately strict: the cell must contain exactly ONE run of
// digits, and that run must be 5 to 8 long. Taking the first 5-8 digits found
// anywhere - which is what CaseNumRE alone does - silently reshaped cells into
// different, valid-looking case numbers:
//
//	"123456789"     -> "12345678"   a case the analyst never wrote
//	"INC0000610529" -> "00006105"   ditto
//	"1.2345678E7"   -> "2345678"    an Excel-formatted number
//
// The tool then searched for that invented number, and LabelMatchesCase would
// happily accept an unrelated mail carrying it. Worse, two different 9-digit
// ids sharing eight leading digits collapsed into ONE case, and the second row
// was painted grey "appears earlier in the sheet" - a case reported covered
// that nothing ever touched.
//
// "SOC610529", "SOC 610529" and "#700001" all still read cleanly: a prefix is
// letters, not another run of digits.
func CaseNumberIn(raw string) string {
	runs := digitRunRE.FindAllString(raw, -1)
	if len(runs) != 1 {
		return "" // no number at all, or more than one - either way, ambiguous
	}
	if n := len(runs[0]); n < 5 || n > 8 {
		return ""
	}
	return runs[0]
}

// NoResultsRE recognises Outlook's several ways of saying a search matched
// nothing.
var NoResultsRE = regexp.MustCompile(
	`(?i)(couldn.t find|didn.t find|no results|nothing (?:that )?match)`)

// ColumnHints are the words that mark a column as likely to hold case numbers.
var ColumnHints = []string{"case", "incident", "ticket", "id", "number"}

// BuildQueries returns the search terms to try, in order, for one case number.
//
// Spacing matters, case does not - both proven live on 2026-08-04:
// "SOC610454" found nothing while "SOC 610454" matched, and "SOC610529"
// (uppercase) matched a mail whose subject is lowercase "soc610529". So both
// spacings are tried, but lowercase duplicates are not - they would only slow
// down cases that have no mail at all.
//
// The Python original ended with two subject: queries. They are gone. The
// subject: operator matched nothing in any live test, so they could only ever
// waste a search - and worse, they were the terms left sitting in the search
// box when a case was not found, which reads as if the tool searched for the
// wrong thing. What a run shows now is what actually has a chance of matching.
func BuildQueries(num string) []string {
	return []string{
		"SOC" + num,  // SOC610529  - the usual SOC format
		"SOC " + num, // SOC 610529 - and every punctuated form: proven live
		//               that this also finds [SOC#610529] and SOC-610529,
		//               because Outlook splits words on punctuation
		num, // 610529     - bare number
	}
}

// LabelMatchesCase reports whether num appears as a whole number in label.
//
// A plain substring test would make case 610529 match a mail about SOC1610529
// - a different case entirely - so digits either side disqualify the match.
//
// The Python original used lookaround ((?<!\d)num(?!\d)). Go's regexp engine
// (RE2) has no lookbehind or lookahead, so the digit boundaries are checked
// by hand here rather than silently degrading to a substring test.
func LabelMatchesCase(label, num string) bool {
	if label == "" || num == "" {
		return false
	}
	for start := 0; ; {
		i := strings.Index(label[start:], num)
		if i < 0 {
			return false
		}
		at := start + i
		end := at + len(num)
		if !isASCIIDigitAt(label, at-1) && !isASCIIDigitAt(label, end) {
			return true
		}
		start = at + 1
	}
}

// isASCIIDigitAt reports whether s[i] is a digit, treating out-of-range
// positions (either end of the string) as "not a digit".
func isASCIIDigitAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	return s[i] >= '0' && s[i] <= '9'
}
