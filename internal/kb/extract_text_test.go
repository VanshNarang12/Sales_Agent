package kb

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestExtractPlainTextAndMarkdown(t *testing.T) {
	got, err := ExtractText("notes.txt", []byte("line one\r\n\r\nline two"))
	if err != nil {
		t.Fatalf("txt: %v", err)
	}
	if got != "line one\n\nline two" {
		t.Errorf("CRLF not normalized: %q", got)
	}
	if _, err := ExtractText("doc.md", []byte("# Heading\ntext")); err != nil {
		t.Errorf("md: %v", err)
	}
}

func TestExtractRejectsBinaryAsTxt(t *testing.T) {
	if _, err := ExtractText("evil.txt", []byte{0xff, 0xfe, 0x00, 0x81}); err == nil {
		t.Fatal("want error for non-UTF-8 bytes named .txt")
	}
}

func TestExtractUnsupportedType(t *testing.T) {
	_, err := ExtractText("deck.pptx", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("want unsupported-type error, got %v", err)
	}
}

func TestExtractDOCX(t *testing.T) {
	docXML := `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>First paragraph about pricing.</w:t></w:r></w:p>
    <w:p><w:r><w:t>Second </w:t></w:r><w:r><w:t>paragraph.</w:t></w:r></w:p>
  </w:body>
</w:document>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("word/document.xml")
	_, _ = f.Write([]byte(docXML))
	_ = zw.Close()

	got, err := ExtractText("offer.docx", buf.Bytes())
	if err != nil {
		t.Fatalf("docx: %v", err)
	}
	if !strings.Contains(got, "First paragraph about pricing.") {
		t.Errorf("missing paragraph 1: %q", got)
	}
	if !strings.Contains(got, "Second paragraph.") {
		t.Errorf("split runs not joined: %q", got)
	}
	if !strings.Contains(got, "\n\n") {
		t.Errorf("paragraph boundary missing: %q", got)
	}
}

func TestExtractDOCXNotAZip(t *testing.T) {
	if _, err := ExtractText("fake.docx", []byte("just text")); err == nil {
		t.Fatal("want error for non-zip docx")
	}
}

// minimalPDF builds a tiny valid one-page PDF containing the given text,
// computing the xref offsets at runtime.
func minimalPDF(text string) []byte {
	content := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, o := range objs {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestExtractPDF(t *testing.T) {
	got, err := ExtractText("pricing.pdf", minimalPDF("Enterprise plan needs a custom quote"))
	if err != nil {
		t.Fatalf("pdf: %v", err)
	}
	if !strings.Contains(got, "Enterprise plan needs a custom quote") {
		t.Errorf("pdf text not extracted: %q", got)
	}
}

func TestExtractPDFMalformed(t *testing.T) {
	if _, err := ExtractText("broken.pdf", []byte("%PDF-1.4 garbage")); err == nil {
		t.Fatal("want error for malformed pdf, not a panic")
	}
}
