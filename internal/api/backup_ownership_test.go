package api

import (
	"archive/zip"
	"fmt"
	"novelclaw/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupPruningNeverDeletesUnrelatedArchives(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("personal-%d.zip", i)), []byte("mine"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pruneBackups(dir)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 10 {
		t.Fatalf("unrelated archives pruned: %d %v", len(entries), err)
	}
}

func TestBackupDoesNotIncludeBackupDirectoryWhenDataIsDot(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("backups", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("backups/personal.zip", []byte("not novel data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("novel.json", []byte(`{"title":"book"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.DataDir = "."
	info, err := CreateBackup(cfg)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(filepath.Join("backups", info.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "novel.json" {
		t.Fatalf("backup included its own directory: %#v", archive.File)
	}
}
