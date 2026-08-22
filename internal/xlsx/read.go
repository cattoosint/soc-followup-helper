// Package xlsx reads and writes the small slice of the Excel format this tool
// needs, using nothing but the standard library.
//
// It replaces excelize. That library is good, but vendoring it dragged in
// golang.org/x/text and friends - about 10 MB of source for reading a few
// columns and writing a coloured copy back. The deliverable has to fit through
// a mail size limit on a machine where email is the only route left, so the
// weight mattered more than the convenience.
//
// An .xlsx file is a zip of XML. Reading means: find the active sheet, resolve
// shared strings, and walk the rows while noting which ones Excel has hidden.
// Writing means emitting a minimal workbook with inline strings, solid fills, a
// frozen header and an autofilter. Nothing else is supported, and nothing else
// is needed.
package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Sheet is one worksheet read into memory.
type Sheet struct {
	Name string
	// Rows holds every row's cell values, padded to the widest row.
	Rows [][]string
	// Hidden reports, per row, whether Excel had it filtered out.
	Hidden []bool
}

// Workbook is what a file yielded.
type Workbook struct {
	SheetNames []string
	Active     Sheet
}

// Open reads the active sheet of an .xlsx file.
//
// The active sheet is whichever tab was showing when the analyst saved it,
// which is the one they mean.
func Open(filename string) (*Workbook, error) {
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path.Base(filename), err)
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.TrimPrefix(f.Name, "/")] = f
	}

	book, err := readWorkbookXML(files)
	if err != nil {
		return nil, err
	}
	rels, err := readRels(files)
	if err != nil {
		return nil, err
	}
	shared, err := readSharedStrings(files)
	if err != nil {
		return nil, err
	}

	if len(book.Sheets) == 0 {
		return nil, fmt.Errorf("%s has no worksheets", path.Base(filename))
	}
	active := book.ActiveTab
	if active < 0 || active >= len(book.Sheets) {
		active = 0
	}

	names := make([]string, 0, len(book.Sheets))
	for _, s := range book.Sheets {
		names = append(names, s.Name)
	}

	target := rels[book.Sheets[active].RID]
	if target == "" {
		// Some writers omit the relationship; fall back to position.
		target = fmt.Sprintf("worksheets/sheet%d.xml", active+1)
	}
	sheetFile := resolveSheetPath(files, target)
	if sheetFile == nil {
		return nil, fmt.Errorf("could not find the worksheet inside %s",
			path.Base(filename))
	}

	rows, hidden, err := readSheet(sheetFile, shared)
	if err != nil {
		return nil, err
	}
	return &Workbook{
		SheetNames: names,
		Active: Sheet{
			Name:   book.Sheets[active].Name,
			Rows:   rows,
			Hidden: hidden,
		},
	}, nil
}

type workbookSheet struct {
	Name string `xml:"name,attr"`
	RID  string `xml:"id,attr"`
}

type workbookXML struct {
	Sheets    []workbookSheet
	ActiveTab int
}

func readWorkbookXML(files map[string]*zip.File) (*workbookXML, error) {
	f := files["xl/workbook.xml"]
	if f == nil {
		return nil, fmt.Errorf("not an .xlsx file: no xl/workbook.xml inside")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	out := &workbookXML{}
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "sheet":
			var s workbookSheet
			for _, a := range start.Attr {
				switch a.Name.Local {
				case "name":
					s.Name = a.Value
				case "id":
					s.RID = a.Value
				}
			}
			out.Sheets = append(out.Sheets, s)
		case "workbookView":
			for _, a := range start.Attr {
				if a.Name.Local == "activeTab" {
					if n, err := strconv.Atoi(a.Value); err == nil {
						out.ActiveTab = n
					}
				}
			}
		}
	}
	return out, nil
}

func readRels(files map[string]*zip.File) (map[string]string, error) {
	rels := map[string]string{}
	f := files["xl/_rels/workbook.xml.rels"]
	if f == nil {
		return rels, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "Relationship" {
			var id, target string
			for _, a := range start.Attr {
				switch a.Name.Local {
				case "Id":
					id = a.Value
				case "Target":
					target = a.Value
				}
			}
			if id != "" {
				rels[id] = target
			}
		}
	}
	return rels, nil
}

// resolveSheetPath copes with the several ways a relationship target can be
// written: relative to xl/, absolute, or with a leading slash.
func resolveSheetPath(files map[string]*zip.File, target string) *zip.File {
	target = strings.TrimPrefix(target, "/")
	for _, candidate := range []string{
		"xl/" + target,
		target,
		"xl/" + path.Clean(target),
	} {
		if f := files[candidate]; f != nil {
			return f
		}
	}
	return nil
}

// readSharedStrings loads the shared string table, which is where most cell
// text actually lives.
func readSharedStrings(files map[string]*zip.File) ([]string, error) {
	f := files["xl/sharedStrings.xml"]
	if f == nil {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var out []string
	dec := xml.NewDecoder(rc)
	var current strings.Builder
	inItem, inText := false, false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inItem, current = true, strings.Builder{}
			case "t":
				inText = true
			}
		case xml.CharData:
			if inItem && inText {
				current.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "si":
				out = append(out, current.String())
				inItem = false
			}
		}
	}
	return out, nil
}

// readSheet walks the rows, resolving cell values and noting hidden rows.
//
// Row and column positions are honoured rather than assumed: Excel omits empty
// cells entirely, so a row's third value is not necessarily in column C.
func readSheet(f *zip.File, shared []string) ([][]string, []bool, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()

	var rows [][]string
	var hidden []bool

	dec := xml.NewDecoder(rc)
	var (
		rowCells  map[int]string
		rowIndex  int
		rowHidden bool
		cellCol   int
		nextCol   int    // where a cell with no r attribute belongs
		badRef    string // a malformed cell reference, reported not ignored
		cellType  string
		inValue   bool
		inInline  bool
		cellText  strings.Builder
		widest    int
		haveRow   bool
	)

	flushRow := func() {
		if !haveRow {
			return
		}
		// Excel numbers rows from 1 and may skip empty ones; keep the file's
		// own numbering so a reported row matches what the analyst sees.
		for len(rows) < rowIndex-1 {
			rows = append(rows, nil)
			hidden = append(hidden, false)
		}
		width := 0
		for col := range rowCells {
			if col+1 > width {
				width = col + 1
			}
		}
		line := make([]string, width)
		for col, v := range rowCells {
			line[col] = v
		}
		if width > widest {
			widest = width
		}
		rows = append(rows, line)
		hidden = append(hidden, rowHidden)
		haveRow = false
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				flushRow()
				rowCells = map[int]string{}
				rowHidden = false
				nextCol = 0
				rowIndex = len(rows) + 1
				haveRow = true
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						if n, err := strconv.Atoi(a.Value); err == nil {
							rowIndex = n
						}
					case "hidden":
						rowHidden = a.Value == "1" || strings.EqualFold(a.Value, "true")
					}
				}
			case "c":
				// The r attribute is optional in spreadsheetml. Without one,
				// cellCol used to stay 0 and every cell in the row overwrote
				// column A, so only the last survived and the case numbers
				// vanished with no trace in Stats. Fall back to "the next
				// column along", which is what the position means.
				cellType, cellCol = "", nextCol
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						if n := columnIndex(a.Value); n >= 0 {
							cellCol = n
						} else {
							badRef = a.Value
						}
					case "t":
						cellType = a.Value
					}
				}
				nextCol = cellCol + 1
				cellText = strings.Builder{}
			case "v":
				inValue = true
			case "is":
				inInline = true
			case "t":
				if inInline {
					inValue = true
				}
			}
		case xml.CharData:
			if inValue {
				cellText.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inValue = false
			case "t":
				if inInline {
					inValue = false
				}
			case "is":
				inInline = false
			case "c":
				if rowCells != nil {
					rowCells[cellCol] = resolveCell(cellText.String(), cellType, shared)
				}
			case "row":
				flushRow()
			}
		}
	}
	flushRow()

	// Pad every row to the widest, so callers can index by column safely.
	for i := range rows {
		for len(rows[i]) < widest {
			rows[i] = append(rows[i], "")
		}
	}
	if badRef != "" {
		return nil, nil, fmt.Errorf("this sheet has a malformed cell reference "+
			"(%q). The file is damaged - open it in Excel and save it again",
			badRef)
	}
	return rows, hidden, nil
}

// resolveCell turns a raw cell value into text.
func resolveCell(raw, cellType string, shared []string) string {
	switch cellType {
	case "s":
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil &&
			n >= 0 && n < len(shared) {
			return shared[n]
		}
		return ""
	case "inlineStr", "str":
		return raw
	default:
		return raw
	}
}

// columnIndex turns a cell reference like "AB12" into a zero-based column.
// maxColumns is Excel's own limit (XFD). Beyond it a reference is malformed,
// and the index was used directly as a slice length: a 10-letter reference
// asked for a 90TB allocation, which is a fatal error no recover can catch.
const maxColumns = 16384

func columnIndex(ref string) int {
	col := 0
	letters := 0
	for i := 0; i < len(ref); i++ {
		c := ref[i]
		switch {
		case c >= 'A' && c <= 'Z':
			col = col*26 + int(c-'A') + 1
			letters++
		case c >= 'a' && c <= 'z':
			col = col*26 + int(c-'a') + 1
			letters++
		default:
			// digits start the row number; the column is complete
			if col > 0 {
				return col - 1
			}
			return 0
		}
		if letters > 3 || col > maxColumns {
			// Malformed. The result was used directly as a slice length, so a
			// long reference asked for an impossible allocation and killed the
			// process outright rather than reporting a bad file.
			return -1
		}
	}
	if col > 0 {
		return col - 1
	}
	return 0
}

// ColumnName turns a zero-based column index into "A", "B", ... "AA".
func ColumnName(index int) string {
	name := ""
	for n := index + 1; n > 0; n = (n - 1) / 26 {
		name = string(rune('A'+(n-1)%26)) + name
	}
	return name
}
