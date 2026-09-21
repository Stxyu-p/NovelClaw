package storage

import (
	"fmt"
	"novelclaw/internal/model"
	"sync"
	"testing"
)

func TestConcurrentGlossaryMergesPreserveAllTerms(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveGlossary(&model.NovelGlossary{NovelSlug: "book", Terms: []model.GlossaryItem{{Term: "curated", Target: "keep"}}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.MergeGlossaryTerms("book", []model.GlossaryItem{{Term: "curated", Target: "wrong"}, {Term: fmt.Sprint(i), Target: "new"}})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	glossary, err := s.GetGlossary("book")
	if err != nil || len(glossary.Terms) != 21 || glossary.Terms[0].Target != "keep" {
		t.Fatalf("lost curated or concurrent terms: %#v %v", glossary, err)
	}
}

func TestGeneratedMemoryCannotOverwriteNewManualEdit(t *testing.T) {
	s := NewStore(t.TempDir())
	initial, _ := s.GetNovelMemory("book")
	if err := s.SaveNovelMemory(&model.NovelMemory{NovelSlug: "book", StorySummary: "manual correction"}); err != nil {
		t.Fatal(err)
	}
	saved, err := s.SaveGeneratedMemory(&model.NovelMemory{NovelSlug: "book", StorySummary: "stale AI"}, initial.UpdatedAt)
	if err != nil || saved {
		t.Fatalf("stale generated memory accepted: %v %v", saved, err)
	}
	current, _ := s.GetNovelMemory("book")
	if current.StorySummary != "manual correction" {
		t.Fatal("manual edit lost")
	}
}
