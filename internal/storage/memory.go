package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"novelclaw/internal/model"
)

// GetNovelMemory loads long-term story/character memory for one novel.
// Missing memory is not an error; callers receive an empty structure.
func (s *Store) GetNovelMemory(slug string) (*model.NovelMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	slug = pathSafeSlug(slug)
	m := &model.NovelMemory{NovelSlug: slug, Characters: []model.CharacterMemory{}, Facts: []string{}}
	path := filepath.Join(s.DataDir, slug, "memory", "memory.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, err
	}
	m.NovelSlug = slug
	return m, nil
}

// SaveNovelMemory atomically persists curated long-term memory.
func (s *Store) SaveNovelMemory(m *model.NovelMemory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveNovelMemory(m)
}

func (s *Store) saveNovelMemory(m *model.NovelMemory) error {
	m.NovelSlug = pathSafeSlug(m.NovelSlug)
	m.UpdatedAt = time.Now()
	dir := filepath.Join(s.DataDir, m.NovelSlug, "memory")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "memory.json"), data)
}

// SaveGeneratedMemory refuses to overwrite a memory edited while AI was running.
func (s *Store) SaveGeneratedMemory(m *model.NovelMemory, expected time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.DataDir, pathSafeSlug(m.NovelSlug), "memory", "memory.json")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	var current model.NovelMemory
	if err == nil {
		if err := json.Unmarshal(data, &current); err != nil {
			return false, err
		}
	}
	if !current.UpdatedAt.Equal(expected) {
		return false, nil
	}
	if err := s.saveNovelMemory(m); err != nil {
		return false, err
	}
	return true, nil
}
