package reviewservice

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func docxBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, e := z.Create("word/document.xml")
	if e != nil {
		t.Fatal(e)
	}
	f.Write([]byte(s))
	z.Close()
	return b.Bytes()
}
func TestDOCXRejectsMalformedXML(t *testing.T) {
	text, e := ExtractDOCXText(docxBytes(t, `<doc><p><t>partial</t></p><p>`))
	if e == nil || text != "" {
		t.Fatalf("partial success: %q %v", text, e)
	}
}
func TestDOCXMarkupAndTextLimits(t *testing.T) {
	markup := `<doc>` + strings.Repeat(`<format/>`, 70000) + `<p><t>ok</t><tab/><t>next</t><br/><t>line</t></p></doc>`
	s, e := ExtractDOCXText(docxBytes(t, markup))
	if e != nil || s != "ok\tnext\nline" {
		t.Fatalf("markup rejected: %q %v", s, e)
	}
	for _, xml := range []string{`<doc><p><t>` + strings.Repeat("x", (512<<10)+1) + `</t></p></doc>`, `<doc>` + strings.Repeat(" ", 4<<20) + `</doc>`} {
		if _, e := ExtractDOCXText(docxBytes(t, xml)); e == nil {
			t.Fatal("oversized document accepted")
		}
	}
	if _, e := ExtractDOCXText(make([]byte, (20<<20)+1)); e == nil {
		t.Fatal("oversized upload accepted")
	}
}
