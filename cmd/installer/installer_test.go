package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFilePreservesExecutableBit(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	const content = "#!/bin/sh\necho credential-helper\n"
	if err := os.WriteFile(src, []byte(content), 0o755); err != nil {
		t.Fatalf("writing source file: %v", err)
	}
	dst := filepath.Join(dir, "dst")

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() unexpected error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading destination file: %v", err)
	}
	if string(got) != content {
		t.Errorf("destination content = %q, want %q", got, content)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stating destination file: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("destination mode = %v, want executable bit set", info.Mode().Perm())
	}
}
