package storage

import (
	"sync"

	"novelclaw/internal/model"
)

func glossaryItemsMap(items []model.GlossaryItem) map[string]string {
	out := make(map[string]string, len(items))
	for _, item := range items {
		if item.Term != "" && item.Target != "" {
			out[item.Term] = item.Target
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// glossaryMap returns an immutable cached term map. Cache entries are replaced,
// never mutated, so concurrent sanitizers can read them without copying.
// It is retained for internal tests; persistence paths use glossaryMapStrict so
// a corrupt glossary cannot silently degrade sanitization quality.
func (s *Store) glossaryMap(slug string) map[string]string {
	loaded, _ := s.glossaryMapStrict(slug)
	return loaded
}

func (s *Store) glossaryMapStrict(slug string) (map[string]string, error) {
	slug = pathSafeSlug(slug)
	s.glossaryCacheMu.RLock()
	cached, ok := s.glossaryCache[slug]
	generation := s.glossaryGeneration
	s.glossaryCacheMu.RUnlock()
	if ok {
		return cached, nil
	}

	glossary, err := s.GetGlossary(slug)
	if err != nil {
		return nil, err
	}
	loaded := glossaryItemsMap(glossary.Terms)
	s.glossaryCacheMu.Lock()
	if existing, exists := s.glossaryCache[slug]; exists {
		loaded = existing
	} else if generation == s.glossaryGeneration {
		s.glossaryCache[slug] = loaded
	}
	s.glossaryCacheMu.Unlock()
	return loaded, nil
}
func (s *Store) invalidateGlossaryCache(slug string) {
	slug = pathSafeSlug(slug)
	s.glossaryCacheMu.Lock()
	s.glossaryGeneration++
	delete(s.glossaryCache, slug)
	s.glossaryCacheMu.Unlock()
}

func (s *Store) chapterWriteLock(slug string, chapterNo int) *sync.Mutex {
	hash := uint64(chapterNo)
	for _, r := range slug {
		hash = hash*131 + uint64(r)
	}
	return &s.chapterWriteLocks[hash%uint64(len(s.chapterWriteLocks))]
}
