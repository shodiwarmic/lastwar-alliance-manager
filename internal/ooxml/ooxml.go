// Package ooxml writes minimal, valid OOXML packages (.docx / .xlsx) in memory: the
// standard minimal part structures LibreOffice/Collabora accept, with no template assets
// shipped. The app's "Create New" flow uses the blank variants; the demo generator
// (internal/demo) uses the content variants for its sample files, so both share one writer.
package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type part struct {
	name    string
	content string
}

// pack writes the given parts into a ZIP (OOXML is a ZIP container). Part order is not
// significant for OOXML (unlike ODF's stored-first mimetype).
func pack(parts []part) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// BlankDocx returns an empty Word document (a single empty paragraph).
func BlankDocx() ([]byte, error) { return Docx(nil) }

// BlankXlsx returns an empty workbook with a single empty sheet.
func BlankXlsx() ([]byte, error) { return Xlsx("Sheet1", nil) }

// Docx returns a Word document with one paragraph per string. A paragraph starting with
// "# " is written as a bold heading line.
func Docx(paragraphs []string) ([]byte, error) {
	var body strings.Builder
	if len(paragraphs) == 0 {
		body.WriteString("<w:p/>")
	}
	for _, p := range paragraphs {
		if h, ok := strings.CutPrefix(p, "# "); ok {
			body.WriteString(`<w:p><w:r><w:rPr><w:b/><w:sz w:val="32"/></w:rPr><w:t xml:space="preserve">` + esc(h) + `</w:t></w:r></w:p>`)
			continue
		}
		body.WriteString(`<w:p><w:r><w:t xml:space="preserve">` + esc(p) + `</w:t></w:r></w:p>`)
	}
	return pack([]part{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>` + body.String() + `</w:body>
</w:document>`},
	})
}

// colName returns the spreadsheet column letters for a zero-based index (0 → A, 26 → AA).
func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// Xlsx returns a workbook with one sheet holding rows. A cell that parses as an integer is
// written as a number; everything else as an inline string.
func Xlsx(sheetName string, rows [][]string) ([]byte, error) {
	if sheetName == "" {
		sheetName = "Sheet1"
	}
	var data strings.Builder
	for r, row := range rows {
		fmt.Fprintf(&data, `<row r="%d">`, r+1)
		for c, v := range row {
			ref := colName(c) + strconv.Itoa(r+1)
			if _, err := strconv.ParseInt(v, 10, 64); err == nil {
				fmt.Fprintf(&data, `<c r="%s"><v>%s</v></c>`, ref, v)
			} else {
				fmt.Fprintf(&data, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, esc(v))
			}
		}
		data.WriteString(`</row>`)
	}
	sheetData := "<sheetData/>"
	if data.Len() > 0 {
		sheetData = colWidths(rows) + "<sheetData>" + data.String() + "</sheetData>"
	}
	return pack([]part{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="` + esc(sheetName) + `" sheetId="1" r:id="rId1"/></sheets>
</workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`},
		{"xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + sheetData + `</worksheet>`},
	})
}

// Blank returns the bytes and extension for a blank document of the given kind. Kind
// matches the app's file_type vocabulary ("document" / "spreadsheet").
func Blank(kind string) (data []byte, ext string, err error) {
	switch kind {
	case "document":
		b, e := BlankDocx()
		return b, ".docx", e
	case "spreadsheet":
		b, e := BlankXlsx()
		return b, ".xlsx", e
	}
	return nil, "", fmt.Errorf("unknown document kind %q", kind)
}

// colWidths sizes each column to its longest cell (in characters, plus padding), so a
// sheet opens readable rather than with every value cut off at the default width.
func colWidths(rows [][]string) string {
	var widths []int
	for _, row := range rows {
		for c, v := range row {
			for len(widths) <= c {
				widths = append(widths, 0)
			}
			widths[c] = max(widths[c], utf8.RuneCountInString(v))
		}
	}
	var b strings.Builder
	b.WriteString("<cols>")
	for c, w := range widths {
		fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, c+1, c+1, min(max(w, 6)+2, 60))
	}
	b.WriteString("</cols>")
	return b.String()
}
