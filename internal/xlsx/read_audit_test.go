package xlsx

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This package had no tests at all, which is how both of these survived. It
// reads the analyst's export by hand, so a row read wrongly here is a case
// followed up wrongly - or not at all.

// writeBook builds a minimal .xlsx around one sheet's XML.
func writeBook(t *testing.T, sheetXML string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.xlsx")

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("[Content_Types].xml",
		`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`)
	add("_rels/.rels",
		`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	add("xl/workbook.xml",
		`<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `+
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
			`<sheets><sheet name="Incidents" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels",
		`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`)
	add("xl/worksheets/sheet1.xml", sheetXML)

	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sheet(rows string) string {
	return `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<sheetData>` + rows + `</sheetData></worksheet>`
}

// The r attribute is optional in spreadsheetml. Without it every cell in the
// row used to land in column A, so only the last one survived - the case
// numbers vanished silently and the tool then said it could not find a
// case-number column.
func TestCellsWithoutAPositionKeepTheirOrder(t *testing.T) {
	path := writeBook(t, sheet(
		`<row r="1"><c t="inlineStr"><is><t>id</t></is></c>`+
			`<c t="inlineStr"><is><t>name</t></is></c></row>`+
			`<row r="2"><c t="inlineStr"><is><t>610529</t></is></c>`+
			`<c t="inlineStr"><is><t>Suspicious login</t></is></c></row>`))

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("could not read the book: %v", err)
	}
	if len(wb.Active.Rows) < 2 {
		t.Fatalf("got %d rows, want 2", len(wb.Active.Rows))
	}
	header := wb.Active.Rows[0]
	if len(header) != 2 || header[0] != "id" || header[1] != "name" {
		t.Fatalf("header read as %q - cells without an r attribute collapsed "+
			"into one column, taking the case numbers with them", header)
	}
	if got := wb.Active.Rows[1]; len(got) != 2 || got[0] != "610529" {
		t.Errorf("data row read as %q, want the case number in column 1", got)
	}
}

// columnIndex fed its result straight to make([]string, n). A long reference
// asked for an impossible allocation, and "out of memory" is a fatal error no
// recover can catch - the process simply died on a damaged file.
func TestAMalformedCellReferenceIsReportedNotFatal(t *testing.T) {
	for _, ref := range []string{"ABCDEFGHIJ1", "AAAAAAAAAAAAAA1", "ZZZZZZZZ9"} {
		path := writeBook(t, sheet(
			`<row r="1"><c r="`+ref+`" t="inlineStr"><is><t>x</t></is></c></row>`))

		wb, err := Open(path)
		if err == nil {
			t.Errorf("reference %q was accepted (%d rows) instead of being "+
				"reported as damaged", ref, len(wb.Active.Rows))
			continue
		}
		if !strings.Contains(err.Error(), "malformed") {
			t.Errorf("reference %q: error %q does not tell the analyst the "+
				"file is damaged", ref, err)
		}
	}
}

// An ordinary reference must still work - the bound must not have broken
// normal reading.
func TestOrdinaryReferencesStillRead(t *testing.T) {
	path := writeBook(t, sheet(
		`<row r="1"><c r="A1" t="inlineStr"><is><t>id</t></is></c>`+
			`<c r="C1" t="inlineStr"><is><t>third</t></is></c></row>`))

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("could not read: %v", err)
	}
	got := wb.Active.Rows[0]
	if len(got) != 3 || got[0] != "id" || got[2] != "third" {
		t.Errorf("row read as %q, want id in column A and third in column C", got)
	}
}

// The analyst's Excel filter is expressed as hidden rows. Including one is
// worse than doing nothing: it follows up a case they deliberately excluded.
func TestHiddenRowsAreReportedAsHidden(t *testing.T) {
	path := writeBook(t, sheet(
		`<row r="1"><c r="A1" t="inlineStr"><is><t>id</t></is></c></row>`+
			`<row r="2"><c r="A2" t="inlineStr"><is><t>610529</t></is></c></row>`+
			`<row r="3" hidden="1"><c r="A3" t="inlineStr"><is><t>700001</t></is></c></row>`))

	wb, err := Open(path)
	if err != nil {
		t.Fatalf("could not read: %v", err)
	}
	if len(wb.Active.Rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(wb.Active.Rows))
	}
	if wb.Active.Hidden[1] {
		t.Error("a visible row was reported hidden - it would be skipped")
	}
	if !wb.Active.Hidden[2] {
		t.Error("a hidden row was reported visible - the analyst's filter was " +
			"ignored and an excluded case would be followed up")
	}
}
