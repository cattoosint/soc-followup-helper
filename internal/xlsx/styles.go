package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

// RowFills reports the background colour of each row's first cell, as a
// six-digit hex string, or "" where the row has no fill.
//
// This exists so the review sheet's colours can be checked without trusting
// the code that wrote them to also read them back correctly. A green row is a
// promise to the analyst that a case is done; it is worth verifying.
func RowFills(filename string) ([]string, error) {
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.TrimPrefix(f.Name, "/")] = f
	}

	fillOfStyle, err := readStyleFills(files["xl/styles.xml"])
	if err != nil {
		return nil, err
	}

	sheet := files["xl/worksheets/sheet1.xml"]
	if sheet == nil {
		return nil, nil
	}
	rc, err := sheet.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var out []string
	current := ""
	inRow, firstCellSeen := false, false

	dec := xml.NewDecoder(rc)
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
			case "row":
				inRow, firstCellSeen, current = true, false, ""
			case "c":
				if inRow && !firstCellSeen {
					firstCellSeen = true
					for _, a := range t.Attr {
						if a.Name.Local == "s" {
							if n, err := strconv.Atoi(a.Value); err == nil &&
								n >= 0 && n < len(fillOfStyle) {
								current = fillOfStyle[n]
							}
						}
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "row" && inRow {
				out = append(out, current)
				inRow = false
			}
		}
	}
	return out, nil
}

// readStyleFills resolves each cellXfs entry to its fill colour.
func readStyleFills(f *zip.File) ([]string, error) {
	if f == nil {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var fills []string     // fill index -> colour
	var styleFill []string // cellXfs index -> colour

	inFills, inCellXfs := false, false
	pending := ""

	dec := xml.NewDecoder(rc)
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
			case "fills":
				inFills = true
			case "cellXfs":
				inCellXfs = true
			case "patternFill":
				if inFills {
					pending = ""
					for _, a := range t.Attr {
						if a.Name.Local == "patternType" && a.Value != "solid" {
							pending = ""
						}
					}
				}
			case "fgColor":
				if inFills {
					for _, a := range t.Attr {
						if a.Name.Local == "rgb" {
							v := a.Value
							if len(v) == 8 { // strip the alpha byte
								v = v[2:]
							}
							pending = strings.ToUpper(v)
						}
					}
				}
			case "xf":
				if inCellXfs {
					colour := ""
					for _, a := range t.Attr {
						if a.Name.Local == "fillId" {
							if n, err := strconv.Atoi(a.Value); err == nil &&
								n >= 0 && n < len(fills) {
								colour = fills[n]
							}
						}
					}
					styleFill = append(styleFill, colour)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "fill":
				if inFills {
					fills = append(fills, pending)
					pending = ""
				}
			case "fills":
				inFills = false
			case "cellXfs":
				inCellXfs = false
			}
		}
	}
	return styleFill, nil
}
