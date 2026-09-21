package storage

import (
	"novelclaw/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestOldChapterScanCannotRepublishAfterWrite(t *testing.T) {
	s := NewStore(t.TempDir())
	_, _, before := s.getChapterCache("book")
	s.invalidateChapterCache("book")
	s.setChapterCache("book", []model.ChapterMeta{{ChapterNo: 1}}, before)
	if _, ok, _ := s.getChapterCache("book"); ok {
		t.Fatal("stale scan was cached after invalidation")
	}
}

func TestOldQAScanCannotEraseConcurrentReport(t *testing.T) {
	s := NewStore(t.TempDir())
	_, _, before := s.cachedQualityReports("book")
	if err := s.SaveQualityReport(model.TranslationQualityReport{NovelSlug: "book", ChapterNo: 1, Score: 95}); err != nil {
		t.Fatal(err)
	}
	s.setQualityReportCache("book", nil, before)
	reports, err := s.ListQualityReports("book")
	if err != nil || len(reports) != 1 {
		t.Fatalf("concurrent report lost: %#v %v", reports, err)
	}
}

func TestPartialChapterWriteInvalidatesMetadata(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveChapter("book", 1, "one", "", []string{"source"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListChapters("book"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, "book", "glossary.json"), []byte("broken json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveChapter("book", 2, "two", "สอง", []string{"source"}, []string{"แปล"}); err == nil {
		t.Fatal("expected glossary error")
	}
	chapters, err := s.ListChapters("book")
	if err != nil || len(chapters) != 2 {
		t.Fatalf("persisted source hidden by cache: %#v %v", chapters, err)
	}
}
