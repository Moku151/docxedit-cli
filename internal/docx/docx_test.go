package docx

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

type testEntry struct {
	name    string
	content string
}

func TestInspectValidDOCX(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "valid.docx")
	writeTestDOCX(t, filename, validEntries())

	parts, err := Inspect(filename)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	want := []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"}
	if len(parts) != len(want) {
		t.Fatalf("Inspect() returned %d parts, want %d", len(parts), len(want))
	}
	for index := range want {
		if parts[index].Name != want[index] {
			t.Errorf("parts[%d] = %q, want %q", index, parts[index].Name, want[index])
		}
	}
}

func TestRewriteChangesOnlySelectedPayload(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.docx")
	destination := filepath.Join(directory, "destination.docx")
	writeTestDOCX(t, source, validEntries())
	before := fileHash(t, source)

	changedDocument := filepath.Join(directory, "document.xml")
	changedXML := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>changed</w:t></w:r></w:p></w:body></w:document>`
	if err := os.WriteFile(changedDocument, []byte(changedXML), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	changed := map[string]string{"word/document.xml": changedDocument}

	if err := Rewrite(source, destination, changed); err != nil {
		t.Fatalf("Rewrite() error = %v", err)
	}
	if err := Validate(destination); err != nil {
		t.Fatalf("Validate(rewritten) error = %v", err)
	}
	if err := VerifyRawCopies(source, destination, changed); err != nil {
		t.Fatalf("VerifyRawCopies() error = %v", err)
	}
	if got := fileHash(t, source); got != before {
		t.Fatal("Rewrite modified the source archive")
	}
	if got := readZipPart(t, destination, "word/document.xml"); got != changedXML {
		t.Fatalf("rewritten document = %q, want %q", got, changedXML)
	}
}

func TestValidateRejectsMalformedXML(t *testing.T) {
	entries := validEntries()
	entries[2].content = `<w:document xmlns:w="urn:test"><broken></w:document>`
	filename := filepath.Join(t.TempDir(), "broken.docx")
	writeTestDOCX(t, filename, entries)

	err := Validate(filename)
	if err == nil || !strings.Contains(err.Error(), "invalid XML") {
		t.Fatalf("Validate() error = %v, want invalid XML error", err)
	}
}

func TestValidateRejectsDoctype(t *testing.T) {
	entries := validEntries()
	entries[2].content = `<!DOCTYPE document><w:document xmlns:w="urn:test"/>`
	filename := filepath.Join(t.TempDir(), "doctype.docx")
	writeTestDOCX(t, filename, entries)

	err := Validate(filename)
	if err == nil || !strings.Contains(err.Error(), "DOCTYPE") {
		t.Fatalf("Validate() error = %v, want DOCTYPE rejection", err)
	}
}

func TestValidateXMLSupportsUTF16(t *testing.T) {
	xmlText := `<?xml version="1.0" encoding="UTF-16"?><root>ok</root>`
	units := utf16.Encode([]rune(xmlText))
	data := []byte{0xff, 0xfe}
	for _, unit := range units {
		var encoded [2]byte
		binary.LittleEndian.PutUint16(encoded[:], unit)
		data = append(data, encoded[:]...)
	}
	if err := validateXML("utf16.xml", data); err != nil {
		t.Fatalf("validateXML() error = %v", err)
	}
}

func TestValidateRejectsMissingRelationshipTarget(t *testing.T) {
	entries := validEntries()
	entries[1].content = rootRelationships("word/missing.xml")
	filename := filepath.Join(t.TempDir(), "missing-target.docx")
	writeTestDOCX(t, filename, entries)

	err := Validate(filename)
	if err == nil || !strings.Contains(err.Error(), "targets missing part") {
		t.Fatalf("Validate() error = %v, want missing relationship target", err)
	}
}

func TestValidateRejectsSignedPackage(t *testing.T) {
	entries := append(validEntries(), testEntry{name: "_xmlsignatures/sig1.xml", content: `<Signature/>`})
	filename := filepath.Join(t.TempDir(), "signed.docx")
	writeTestDOCX(t, filename, entries)

	err := Validate(filename)
	if err == nil || !strings.Contains(err.Error(), "digitally signed") {
		t.Fatalf("Validate() error = %v, want signed-package rejection", err)
	}
}

func TestValidateRejectsUnsafeAndDuplicateNames(t *testing.T) {
	tests := []struct {
		name  string
		extra testEntry
		want  string
	}{
		{name: "unsafe", extra: testEntry{name: "../evil.xml", content: `<evil/>`}, want: "unsafe ZIP entry path"},
		{name: "duplicate", extra: testEntry{name: "WORD/document.xml", content: `<other/>`}, want: "duplicate package path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), test.name+".docx")
			writeTestDOCX(t, filename, append(validEntries(), test.extra))
			err := Validate(filename)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func validEntries() []testEntry {
	return []testEntry{
		{name: "[Content_Types].xml", content: `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`},
		{name: "_rels/.rels", content: rootRelationships("word/document.xml")},
		{name: "word/document.xml", content: `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p/></w:body></w:document>`},
	}
}

func rootRelationships(target string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="` + target + `"/>
</Relationships>`
}

func writeTestDOCX(t *testing.T, filename string, entries []testEntry) {
	t.Helper()
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetModTime(time.Date(2025, time.January, 2, 3, 4, 6, 0, time.UTC))
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, entry.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func fileHash(t *testing.T, filename string) [sha256.Size]byte {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func readZipPart(t *testing.T, filename, name string) string {
	t.Helper()
	reader, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(stream)
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatalf("part %q not found", name)
	return ""
}
