package kb

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// ErrUnsupportedType lets the HTTP handler answer 415 instead of a generic 400.
var ErrUnsupportedType = fmt.Errorf("unsupported file type")

// ExtractText turns an uploaded file's bytes into plain text, picked by extension.
// Everything downstream (chunker, embedder, store) only ever sees this text.
func ExtractText(filename string, data []byte) (string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".txt", ".md":
		return extractPlain(filename, data)
	case ".pdf":
		return extractPDF(filename, data)
	case ".docx":
		return extractDOCX(filename, data)
	default:
		return "", fmt.Errorf("extract %q: %w (supported: .txt .md .pdf .docx)", filename, ErrUnsupportedType)
	}
}

// extractPlain validates UTF-8 (catches a binary mis-named .txt) and normalizes CRLF,
// so Windows files don't break the chunker's blank-line paragraph detection.
func extractPlain(filename string, data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", fmt.Errorf("extract %q: not valid UTF-8 text", filename)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n"), nil
}

// extractPDF pulls the text of all pages. The parser can panic on malformed PDFs,
// so it runs behind a recover and reports that as a normal error.
func extractPDF(filename string, data []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("extract %q: malformed PDF: %v", filename, r)
		}
	}()

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("extract %q: %w", filename, err)
	}
	r, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("extract %q: %w", filename, err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("extract %q: %w", filename, err)
	}
	return string(raw), nil
}

// extractDOCX: a .docx is a ZIP; the text lives in word/document.xml. Stdlib only:
// unzip, walk the XML, collect <w:t> text, newline per paragraph (</w:p>).
func extractDOCX(filename string, data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("extract %q: not a valid docx (zip): %w", filename, err)
	}
	var doc io.ReadCloser
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc, err = f.Open()
			if err != nil {
				return "", fmt.Errorf("extract %q: %w", filename, err)
			}
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("extract %q: no word/document.xml inside — not a docx?", filename)
	}
	defer doc.Close()

	var b strings.Builder
	dec := xml.NewDecoder(doc)
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("extract %q: broken document.xml: %w", filename, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteString("\n\n") // paragraph boundary — what the chunker splits on
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}
