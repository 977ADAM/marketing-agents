package reviewservice

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

const maxDocxText = 512 << 10

func ExtractDOCXText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		xmlBytes, err := io.ReadAll(io.LimitReader(rc, maxDocxText+1))
		if err != nil {
			return "", err
		}
		if len(xmlBytes) > maxDocxText {
			return "", errors.New("extracted text too large")
		}
		return parseDocxDocument(xmlBytes), nil
	}
	return "", errors.New("word/document.xml not found")
}

// parseDocxDocument собирает текст всех абзацев (w:p) документа, включая
// абзацы внутри таблиц (w:tbl/w:tr/w:tc). w:tab → '\t', w:br/w:cr → '\n'.
// Разбор идёт токенами, чтобы не зависеть от вложенности элементов.
func parseDocxDocument(xmlBytes []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	var out strings.Builder
	var line strings.Builder
	depth := 0      // вложенность в w:p
	inText := false // внутри w:t

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out.String()
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				depth++
			case "t":
				inText = true
			case "tab":
				if depth > 0 {
					line.WriteByte('\t')
				}
			case "br", "cr":
				if depth > 0 {
					line.WriteByte('\n')
				}
			}
		case xml.CharData:
			if inText && depth > 0 {
				line.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				depth--
				if depth == 0 {
					s := strings.TrimSpace(line.String())
					if s != "" {
						if out.Len() > 0 {
							out.WriteByte('\n')
						}
						out.WriteString(s)
					}
					line.Reset()
				}
			}
		}
	}
	return out.String()
}
