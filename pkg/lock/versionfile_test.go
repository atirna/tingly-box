package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionFile_WriteReadRemove(t *testing.T) {
	dir := t.TempDir()
	vf := NewVersionFile(dir)

	if _, err := vf.Read(); err == nil {
		t.Error("Read should fail when version file does not exist")
	}

	if err := vf.Write("0.260828.1000"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	v, err := vf.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if v != "0.260828.1000" {
		t.Errorf("Expected version 0.260828.1000, got %q", v)
	}

	// Overwrite with a new version
	if err := vf.Write("0.260901.1200"); err != nil {
		t.Fatalf("Write (overwrite) failed: %v", err)
	}
	v, err = vf.Read()
	if err != nil {
		t.Fatalf("Read after overwrite failed: %v", err)
	}
	if v != "0.260901.1200" {
		t.Errorf("Expected version 0.260901.1200, got %q", v)
	}

	if err := vf.Remove(); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := vf.Read(); err == nil {
		t.Error("Read should fail after Remove")
	}

	// Removing a missing file is not an error
	if err := vf.Remove(); err != nil {
		t.Errorf("Remove on missing file should not fail, got: %v", err)
	}
}

func TestVersionFile_WriteEmpty(t *testing.T) {
	vf := NewVersionFile(t.TempDir())
	for _, v := range []string{"", "   ", "\n"} {
		if err := vf.Write(v); err == nil {
			t.Errorf("Write(%q) should fail", v)
		}
	}
}

func TestVersionFile_ReadEmptyContent(t *testing.T) {
	dir := t.TempDir()
	vf := NewVersionFile(dir)
	if err := os.WriteFile(filepath.Join(dir, versionFileName), []byte("\n"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if _, err := vf.Read(); err == nil {
		t.Error("Read should fail on empty content")
	}
}
