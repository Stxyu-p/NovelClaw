package storage

import "novelclaw/internal/model"

func cloneChapterMeta(items []model.ChapterMeta) []model.ChapterMeta {
	if len(items) == 0 {
		return []model.ChapterMeta{}
	}
	return append([]model.ChapterMeta(nil), items...)
}

func (s *Store) getChapterCache(slug string) ([]model.ChapterMeta, bool, uint64) {
	s.chapterCacheMu.RLock()
	items, ok := s.chapterCache[slug]
	generation := s.chapterGeneration
	if ok {
		items = cloneChapterMeta(items)
	}
	s.chapterCacheMu.RUnlock()
	return items, ok, generation
}

func (s *Store) setChapterCache(slug string, items []model.ChapterMeta, generation uint64) {
	s.chapterCacheMu.Lock()
	if generation == s.chapterGeneration {
		s.chapterCache[slug] = cloneChapterMeta(items)
	}
	s.chapterCacheMu.Unlock()
}

func (s *Store) invalidateChapterCache(slug string) {
	s.chapterCacheMu.Lock()
	s.chapterGeneration++
	delete(s.chapterCache, slug)
	s.chapterCacheMu.Unlock()
}
