package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// parts opens a package and returns every part's content, failing on a part that is not
// well-formed XML.
func parts(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		c, _ := io.ReadAll(rc)
		rc.Close()
		d := xml.NewDecoder(bytes.NewReader(c))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed XML: %v", f.Name, err)
			}
		}
		out[f.Name] = string(c)
	}
	return out
}

func TestBlank(t *testing.T) {
	for kind, want := range map[string]string{"document": "word/document.xml", "spreadsheet": "xl/worksheets/sheet1.xml"} {
		b, ext, err := Blank(kind)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if _, ok := parts(t, b)[want]; !ok {
			t.Errorf("%s (%s) has no %s", kind, ext, want)
		}
	}
	if _, _, err := Blank("presentation"); err == nil {
		t.Error("unknown kind accepted")
	}
}

func TestContentIsEscaped(t *testing.T) {
	b, err := Docx([]string{"# Rules & <conduct>", "Be kind: \"always\""})
	if err != nil {
		t.Fatal(err)
	}
	doc := parts(t, b)["word/document.xml"]
	if !strings.Contains(doc, "Rules &amp; &lt;conduct&gt;") || !strings.Contains(doc, "<w:b/>") {
		t.Errorf("heading not written as escaped bold text:\n%s", doc)
	}

	b, err = Xlsx("VS & Schedule", [][]string{{"Name", "Points"}, {"Ålfa <1>", "1234"}})
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, b)
	sheet := p["xl/worksheets/sheet1.xml"]
	if !strings.Contains(sheet, `<c r="B2"><v>1234</v></c>`) {
		t.Errorf("number not written as a number:\n%s", sheet)
	}
	if !strings.Contains(sheet, "Ålfa &lt;1&gt;") {
		t.Errorf("string not escaped:\n%s", sheet)
	}
	if !strings.Contains(sheet, `<col min="1" max="1" width="10" customWidth="1"/>`) {
		t.Errorf("column A not sized to its longest cell (Ålfa <1>, 8 chars + 2):\n%s", sheet)
	}
	if !strings.Contains(p["xl/workbook.xml"], `name="VS &amp; Schedule"`) {
		t.Error("sheet name not escaped")
	}
}

func TestColName(t *testing.T) {
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ", 702: "AAA"} {
		if got := colName(i); got != want {
			t.Errorf("colName(%d) = %s, want %s", i, got, want)
		}
	}
}
