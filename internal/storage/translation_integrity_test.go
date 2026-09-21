package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndRepairNeverDeleteUntranslatedWords(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveChapter("book", 1, "title", "title", []string{"source"}, []string{"ฉบับเดิม"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.DataDir, "book", "chapters", "0001.th.json")
	before, _ := os.ReadFile(path)
	if err := store.SaveChapter("book", 1, "title", "title", nil, []string{"ข้อความ 測試 จบ"}); !errors.Is(err, ErrIncompleteTranslation) {
		t.Fatalf("accepted damaging cleanup: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("original changed")
	}
	legacy := []byte(`{"title":"title","paragraphs":["ข้อความ 測試 จบ"]}`)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RepairChapter("book", 1); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	if string(legacy) != string(after) {
		t.Fatal("repair silently deleted unknown words")
	}
}
