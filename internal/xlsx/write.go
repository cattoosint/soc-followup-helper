package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// Row is one line of the sheet, with how it should look.
type Row struct {
	Values []string
	// FillRGB is a six-digit hex colour, or "" for no fill.
	FillRGB string
	Bold    bool
	// Hidden marks the row as filtered out, the way Excel's own filter does.
	Hidden bool
}

// Doc is a whole sheet ready to be written.
type Doc struct {
	SheetName string
	Rows      []Row
	// ColWidths is in Excel's character units; zero means leave it alone.
	ColWidths []float64
	// FreezeTopRow keeps the header on screen while the analyst scrolls.
	FreezeTopRow bool
	// AutoFilter puts the dropdown arrows on the header row.
	AutoFilter bool
}

// Save writes doc as an .xlsx file.
//
// Strings are written inline rather than through a shared-string table: it
// costs a little size and saves a whole index that would have to stay in step
// with the cells.
func Save(filename string, doc Doc) error {
	if doc.SheetName == "" {
		doc.SheetName = "Sheet1"
	}

	styles, fillIndex := buildStyles(doc.Rows)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	parts := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"xl/workbook.xml", workbookXMLFor(doc.SheetName)},
		{"xl/_rels/workbook.xml.rels", workbookRelsXML},
		{"xl/styles.xml", styles},
		{"xl/worksheets/sheet1.xml", sheetXML(doc, fillIndex)},
	}
	for _, part := range parts {
		w, err := zw.Create(part.name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(part.body)); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(filename, buf.Bytes(), 0o600)
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const workbookRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

func workbookXMLFor(sheetName string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="` + escape(sheetName) + `" sheetId="1" r:id="rId1"/></sheets>
</workbook>`
}

// buildStyles emits styles.xml and returns, per colour, the cellXfs index.
//
// Excel requires fill 0 to be "none" and fill 1 to be "gray125"; a file that
// omits them opens as corrupt. Everything after those is ours.
func buildStyles(rows []Row) (string, map[string]int) {
	var colours []string
	seen := map[string]bool{}
	for _, r := range rows {
		c := strings.ToUpper(strings.TrimPrefix(r.FillRGB, "#"))
		if c != "" && !seen[c] {
			seen[c] = true
			colours = append(colours, c)
		}
	}

	var fills strings.Builder
	fills.WriteString(`<fill><patternFill patternType="none"/></fill>`)
	fills.WriteString(`<fill><patternFill patternType="gray125"/></fill>`)
	for _, c := range colours {
		fills.WriteString(`<fill><patternFill patternType="solid"><fgColor rgb="FF` +
			c + `"/><bgColor indexed="64"/></patternFill></fill>`)
	}

	// cellXfs: 0 plain, 1 bold, then one per colour, then one bold-per-colour.
	var xfs strings.Builder
	xfs.WriteString(`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`)
	xfs.WriteString(`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>`)

	index := map[string]int{}
	next := 2
	for i, c := range colours {
		fillID := i + 2
		xfs.WriteString(fmt.Sprintf(
			`<xf numFmtId="0" fontId="0" fillId="%d" borderId="0" xfId="0" applyFill="1"/>`,
			fillID))
		index[c] = next
		next++
	}
	for i, c := range colours {
		fillID := i + 2
		xfs.WriteString(fmt.Sprintf(
			`<xf numFmtId="0" fontId="1" fillId="%d" borderId="0" xfId="0" applyFill="1" applyFont="1"/>`,
			fillID))
		index["bold:"+c] = next
		next++
	}

	styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
<fills count="` + fmt.Sprint(len(colours)+2) + `">` + fills.String() + `</fills>
<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="` + fmt.Sprint(next) + `">` + xfs.String() + `</cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
</styleSheet>`
	return styles, index
}

// styleFor picks the cellXfs index for a row.
func styleFor(r Row, index map[string]int) int {
	colour := strings.ToUpper(strings.TrimPrefix(r.FillRGB, "#"))
	switch {
	case colour != "" && r.Bold:
		if id, ok := index["bold:"+colour]; ok {
			return id
		}
	case colour != "":
		if id, ok := index[colour]; ok {
			return id
		}
	case r.Bold:
		return 1
	}
	if r.Bold {
		return 1
	}
	return 0
}

func sheetXML(doc Doc, fillIndex map[string]int) string {
	width := 0
	for _, r := range doc.Rows {
		if len(r.Values) > width {
			width = len(r.Values)
		}
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)

	if doc.FreezeTopRow {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0" tabSelected="1">` +
			`<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>` +
			`</sheetView></sheetViews>`)
	}

	if len(doc.ColWidths) > 0 {
		b.WriteString(`<cols>`)
		for i, w := range doc.ColWidths {
			if w <= 0 {
				continue
			}
			b.WriteString(fmt.Sprintf(
				`<col min="%d" max="%d" width="%.2f" customWidth="1"/>`,
				i+1, i+1, w))
		}
		b.WriteString(`</cols>`)
	}

	b.WriteString(`<sheetData>`)
	for i, row := range doc.Rows {
		rowNum := i + 1
		style := styleFor(row, fillIndex)
		if row.Hidden {
			b.WriteString(fmt.Sprintf(`<row r="%d" hidden="1">`, rowNum))
		} else {
			b.WriteString(fmt.Sprintf(`<row r="%d">`, rowNum))
		}
		for c := 0; c < width; c++ {
			value := ""
			if c < len(row.Values) {
				value = row.Values[c]
			}
			ref := ColumnName(c) + fmt.Sprint(rowNum)
			if value == "" {
				// Still emit the cell so the fill covers the whole row.
				if style != 0 {
					b.WriteString(fmt.Sprintf(`<c r="%s" s="%d"/>`, ref, style))
				}
				continue
			}
			b.WriteString(fmt.Sprintf(
				`<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
				ref, style, escape(value)))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)

	if doc.AutoFilter && len(doc.Rows) > 0 && width > 0 {
		b.WriteString(fmt.Sprintf(`<autoFilter ref="A1:%s%d"/>`,
			ColumnName(width-1), len(doc.Rows)))
	}

	b.WriteString(`</worksheet>`)
	return b.String()
}

// escape makes text safe for XML, and drops the control characters Excel
// refuses to open a file over.
func escape(s string) string {
	cleaned := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		cleaned = append(cleaned, r)
	}
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(string(cleaned))); err != nil {
		return ""
	}
	return buf.String()
}
