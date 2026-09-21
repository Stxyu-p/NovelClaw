package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"novelclaw/internal/model"
)

var (
	ErrNovelNotFound   = errors.New("novel not found")
	ErrChapterNotFound = errors.New("chapter not found")
)

// Store handles all file operations for novels, chapters, bookmarks, and glossaries
type Store struct {
	DataDir string
	mu      sync.RWMutex

	statsMu    sync.Mutex
	statsTimer map[string]*time.Timer

	chapterCacheMu    sync.RWMutex
	chapterCache      map[string][]model.ChapterMeta
	chapterGeneration uint64

	glossaryCacheMu    sync.RWMutex
	glossaryCache      map[string]map[string]string
	glossaryGeneration uint64

	qaCacheMu     sync.RWMutex
	qaCache       map[string]map[int]model.TranslationQualityReport
	qaCacheLoaded map[string]bool
	qaGeneration  uint64

	// Fixed striped locks serialize writes to the same chapter/file family
	// without making unrelated chapters wait on one global filesystem lock.
	chapterWriteLocks [64]sync.Mutex
}

// NewStore creates a new storage manager
func NewStore(dataDir string) *Store {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Printf("ERROR: cannot create storage directory %s: %v", dataDir, err)
	}
	return &Store{
		DataDir:       dataDir,
		statsTimer:    make(map[string]*time.Timer),
		chapterCache:  make(map[string][]model.ChapterMeta),
		glossaryCache: make(map[string]map[string]string),
		qaCache:       make(map[string]map[int]model.TranslationQualityReport),
		qaCacheLoaded: make(map[string]bool),
	}
}

// ListNovels returns all novels available in DataDir
func (s *Store) ListNovels() ([]model.Novel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.DataDir)
	if err != nil {
		return nil, err
	}

	var novels []model.Novel
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		slug := entry.Name()
		novelPath := filepath.Join(s.DataDir, slug, "novel.json")
		data, err := os.ReadFile(novelPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read novel metadata %s: %w", slug, err)
			}
			// Legacy/import folders without novel.json may still be shown, but use
			// the directory timestamp so repeated Library loads remain stable.
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil, fmt.Errorf("stat novel directory %s: %w", slug, infoErr)
			}
			novels = append(novels, model.Novel{
				Slug: slug, Title: slug, SourceLang: "cn", TargetLang: "th", UpdatedAt: info.ModTime(),
			})
			continue
		}

		var n model.Novel
		if err := json.Unmarshal(data, &n); err != nil {
			return nil, fmt.Errorf("parse novel metadata %s: %w", slug, err)
		}
		if n.Slug == "" {
			n.Slug = slug
		}
		novels = append(novels, n)
	}

	// Sort by updatedAt descending
	sort.Slice(novels, func(i, j int) bool {
		return novels[i].UpdatedAt.After(novels[j].UpdatedAt)
	})

	return novels, nil
}

// GetNovel returns a single novel by its slug
func (s *Store) GetNovel(slug string) (*model.Novel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	slug = pathSafeSlug(slug)
	novelPath := filepath.Join(s.DataDir, slug, "novel.json")
	data, err := os.ReadFile(novelPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNovelNotFound, slug)
		}
		return nil, fmt.Errorf("read novel %s: %w", slug, err)
	}

	var n model.Novel
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	if n.Slug == "" {
		n.Slug = slug
	}
	return &n, nil
}

// SaveNovel saves or updates novel metadata
func (s *Store) SaveNovel(n *model.Novel) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if n.Slug == "" {
		n.Slug = sanitizeSlug(n.Title)
	}
	n.Slug = pathSafeSlug(n.Slug)
	n.UpdatedAt = time.Now()

	novelDir := filepath.Join(s.DataDir, n.Slug)
	if err := os.MkdirAll(novelDir, 0755); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(novelDir, "chapters"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(novelDir, "glossary"), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}

	return writeFileAtomic(filepath.Join(novelDir, "novel.json"), data)
}

// writeFileAtomic writes data via a temp file + rename so a crash mid-write
// can never leave a half-written chapter file behind.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sanitizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	if result.Len() == 0 {
		return fmt.Sprintf("novel-%d", time.Now().Unix())
	}
	return result.String()
}

// pathSafeSlug strips everything outside [A-Za-z0-9_-] so a slug can never
// contain path separators or ".." and therefore cannot escape DataDir.
// Unlike sanitizeSlug it does not lowercase, so existing on-disk slugs keep
// working for reads.
func pathSafeSlug(slug string) string {
	var b strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "_invalid_"
	}
	return b.String()
}
