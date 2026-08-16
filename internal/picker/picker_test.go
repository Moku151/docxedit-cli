package picker

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"docxedit/internal/docx"
)

func TestModelPreselectsMainDocumentAndFilters(t *testing.T) {
	initial := newModel([]docx.Part{
		{Name: "word/styles.xml", UncompressedSize: 12},
		{Name: "word/document.xml", UncompressedSize: 24},
		{Name: "_rels/.rels", UncompressedSize: 8},
	})
	if !initial.selected["word/document.xml"] {
		t.Fatal("word/document.xml was not preselected")
	}

	updated, _ := initial.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))
	filtered := updated.(model)
	if len(filtered.filtered) != 2 {
		t.Fatalf("filter returned %d items, want 2", len(filtered.filtered))
	}
}

func TestSpaceTogglesCurrentPart(t *testing.T) {
	initial := newModel([]docx.Part{{Name: "custom.xml", UncompressedSize: 1}})
	updated, _ := initial.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "}))
	result := updated.(model)
	if !result.selected["custom.xml"] {
		t.Fatal("space did not select current part")
	}
}
