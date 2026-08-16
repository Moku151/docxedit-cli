package safereplace

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplacePreservesModeAndReplacesContent(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "document.docx")
	temporary := filepath.Join(directory, "replacement.tmp")
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Replace(temporary, target)
	if err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if !result.Committed {
		t.Fatal("Replace() did not report a commit")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("target content = %q, want new", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("target mode = %o, want 640", got)
	}
}

func TestReplaceRejectsSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is privilege-dependent on Windows")
	}
	directory := t.TempDir()
	realTarget := filepath.Join(directory, "real.docx")
	symlink := filepath.Join(directory, "link.docx")
	temporary := filepath.Join(directory, "replacement.tmp")
	if err := os.WriteFile(realTarget, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTarget, symlink); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Replace(temporary, symlink); err == nil {
		t.Fatal("Replace() accepted a symlink target")
	}
}
