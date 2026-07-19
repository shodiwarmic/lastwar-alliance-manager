// blank_documents.go - Generates minimal, valid empty OOXML documents (.docx / .xlsx)
// in-memory for the "Create New" flow, so a blank file can be created and opened
// straight into Collabora without an upload. These are the standard minimal package
// structures LibreOffice/Collabora accept; no external template assets are shipped.

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
)

type zipEntry struct {
	name    string
	content string
}

// buildOOXMLZip writes the given parts into a ZIP (OOXML is a ZIP container). Part
// order is not significant for OOXML (unlike ODF's stored-first mimetype).
func buildOOXMLZip(entries []zipEntry) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// blankDocxBytes returns an empty Word document (a single empty paragraph).
func blankDocxBytes() ([]byte, error) {
	return buildOOXMLZip([]zipEntry{
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
  <w:body><w:p/></w:body>
</w:document>`},
	})
}

// blankXlsxBytes returns an empty workbook with a single empty sheet.
func blankXlsxBytes() ([]byte, error) {
	return buildOOXMLZip([]zipEntry{
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
  <sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets>
</workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`},
		{"xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`},
	})
}

// blankDocumentBytes returns the bytes + extension for a blank document of the given
// kind. Kind matches the app's file_type vocabulary ("document" / "spreadsheet").
func blankDocumentBytes(kind string) (data []byte, ext string, err error) {
	switch kind {
	case "document":
		b, e := blankDocxBytes()
		return b, ".docx", e
	case "spreadsheet":
		b, e := blankXlsxBytes()
		return b, ".xlsx", e
	}
	return nil, "", fmt.Errorf("unknown document kind %q", kind)
}
