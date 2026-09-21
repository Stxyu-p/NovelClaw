package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"novelclaw/internal/model"
	"novelclaw/internal/translator"
)

var ErrIncompleteTranslation = errors.New("คำแปลยังมีคำที่แปลไม่ครบ เพิ่มคำศัพท์หรือแปลใหม่ก่อนบันทึก")

// ListChapters returns summary metadata for all chapters in a novel
func (s *Store) ListChapters(slug string) ([]model.ChapterMeta, error) {
	slug = pathSafeSlug(slug)
	cached, ok, generation := s.getChapterCache(slug)
	if ok {
		return cached, nil
	}

	chaptersDir := filepath.Join(s.DataDir, slug, "chapters")
	entries, err := os.ReadDir(chaptersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []model.ChapterMeta{}, nil
		}
		return nil, fmt.Errorf("read chapters directory: %w", err)
	}

	chapterMap := make(map[int]*model.ChapterMeta)
	if data, readErr := os.ReadFile(filepath.Join(chaptersDir, "catalog.json")); readErr == nil {
		var catalog []model.ChapterMeta
		if json.Unmarshal(data, &catalog) == nil {
			for i := range catalog {
				item := catalog[i]
				chapterMap[item.ChapterNo] = &item
			}
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		parts := strings.Split(strings.TrimSuffix(name, ".json"), ".")
		if len(parts) == 0 {
			continue
		}

		chNum, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		meta, exists := chapterMap[chNum]
		if !exists {
			meta = &model.ChapterMeta{
				ChapterNo: chNum,
			}
			chapterMap[chNum] = meta
		}

		// Lightweight title read: translated file wins (carries both titles),
		// otherwise source title fills the gap. Read each file at most once.
		isTh := strings.Contains(name, ".th.") || strings.Contains(name, ".translated.")
		needTitle := (!isTh && meta.TitleSource == "" && meta.TitleTranslated == "") || (isTh && meta.TitleTranslated == "")
		if needTitle {
			data, err := os.ReadFile(filepath.Join(chaptersDir, name))
			if err != nil {
				return nil, fmt.Errorf("read chapter metadata %s: %w", name, err)
			}
			sourceTitle, translatedTitle, err := extractChapterTitles(data)
			if err != nil {
				return nil, fmt.Errorf("parse chapter metadata %s: %w", name, err)
			}
			if isTh && translatedTitle == "" && sourceTitle != "" {
				translatedTitle, sourceTitle = sourceTitle, ""
			}
			if translatedTitle != "" {
				meta.TitleTranslated = cleanChapterTitle(translatedTitle)
			}
			if sourceTitle != "" {
				meta.TitleSource = cleanChapterTitle(sourceTitle)
			}
		}

		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat chapter metadata %s: %w", name, err)
		}
		if info.ModTime().After(meta.UpdatedAt) {
			meta.UpdatedAt = info.ModTime()
		}

		isSource := strings.Contains(name, ".cn.") || strings.Contains(name, ".source.")
		isTranslated := strings.Contains(name, ".th.") || strings.Contains(name, ".translated.")

		if isSource {
			meta.HasSource = true
		}
		if isTranslated {
			meta.HasTranslated = true
		}
	}

	var result []model.ChapterMeta
	for _, meta := range chapterMap {
		result = append(result, *meta)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ChapterNo < result[j].ChapterNo
	})

	s.setChapterCache(slug, result, generation)
	return cloneChapterMeta(result), nil
}

// SaveChapterCatalog stores metadata for the complete source catalog, including locked chapters.
func (s *Store) SaveChapterCatalog(slug string, chapters []model.ChapterMeta) error {
	slug = pathSafeSlug(slug)
	dir := filepath.Join(s.DataDir, slug, "chapters")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(chapters, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "catalog.json.tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, "catalog.json")); err != nil {
		return err
	}
	s.invalidateChapterCache(slug)
	s.scheduleNovelStatsUpdate(slug)
	return nil
}

// GetChapter returns the content of a specific chapter. Chapter files are
// atomically replaced on writes, so reads do not need the global store lock.
func (s *Store) GetChapter(slug string, chapterNo int) (*model.ChapterContent, error) {
	slug = pathSafeSlug(slug)
	numStr := fmt.Sprintf("%04d", chapterNo)
	chaptersDir := filepath.Join(s.DataDir, slug, "chapters")
	content := &model.ChapterContent{NovelSlug: slug, ChapterNo: chapterNo, Status: "source"}

	sourceFound, err := readChapterCandidates([]string{
		filepath.Join(chaptersDir, numStr+".cn.json"),
		filepath.Join(chaptersDir, numStr+".source.json"),
	}, content, true)
	if err != nil {
		return nil, fmt.Errorf("read chapter %d source: %w", chapterNo, err)
	}
	translatedFound, err := readChapterCandidates([]string{
		filepath.Join(chaptersDir, numStr+".th.json"),
		filepath.Join(chaptersDir, numStr+".translated.json"),
	}, content, false)
	if err != nil {
		return nil, fmt.Errorf("read chapter %d translation: %w", chapterNo, err)
	}
	if translatedFound {
		content.Status = "translated"
	}
	if !sourceFound && !translatedFound {
		return nil, fmt.Errorf("%w: %d in novel %s", ErrChapterNotFound, chapterNo, slug)
	}
	if len(content.SourceText) == 0 && len(content.TranslatedText) == 0 {
		return nil, fmt.Errorf("chapter %d contains no readable paragraphs", chapterNo)
	}
	return content, nil
}

func readChapterCandidates(paths []string, content *model.ChapterContent, isSource bool) (bool, error) {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		if err := parseChapterJSON(data, content, isSource); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// SaveChapter saves source or translated chapter content. Translated content
// is sanitized here (single choke point) so anything persisted is guaranteed
// zero-Hanzi — readers no longer sanitize on GET.
func (s *Store) SaveChapter(slug string, chapterNo int, sourceTitle, transTitle string, sourceParagraphs, transParagraphs []string) error {
	slug = pathSafeSlug(slug)
	writeLock := s.chapterWriteLock(slug, chapterNo)
	writeLock.Lock()
	defer writeLock.Unlock()
	changed := false
	defer func() {
		if changed {
			s.invalidateChapterCache(slug)
			s.scheduleNovelStatsUpdate(slug)
		}
	}()

	numStr := fmt.Sprintf("%04d", chapterNo)
	chaptersDir := filepath.Join(s.DataDir, slug, "chapters")
	if err := os.MkdirAll(chaptersDir, 0755); err != nil {
		return fmt.Errorf("create chapters directory: %w", err)
	}

	// Save source if provided
	if len(sourceParagraphs) > 0 {
		srcData := map[string]interface{}{
			"novelId":    slug,
			"chapterNo":  chapterNo,
			"sourceLang": "cn",
			"targetLang": "th",
			"title": map[string]string{
				"source": sourceTitle,
			},
			"status":     "source",
			"paragraphs": sourceParagraphs,
			"updatedAt":  time.Now().Format(time.RFC3339),
		}
		data, err := json.MarshalIndent(srcData, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal chapter %d source: %w", chapterNo, err)
		}
		if err := writeFileAtomic(filepath.Join(chaptersDir, numStr+".cn.json"), data); err != nil {
			return fmt.Errorf("save chapter %d source: %w", chapterNo, err)
		}
		changed = true
	}

	// Save translated if provided (sanitize before it ever touches disk).
	// A corrupt glossary is a hard error here; silently sanitizing without it
	// could persist inconsistent names that are difficult to repair later.
	if len(transParagraphs) > 0 {
		gMap, err := s.glossaryMapStrict(slug)
		if err != nil {
			return fmt.Errorf("load glossary for chapter %d: %w", chapterNo, err)
		}
		cleanTitle, titleDiag := translator.SanitizeTextWithDiagnostics(transTitle, gMap)
		cleaned, paragraphDiag := translator.SanitizeParagraphsWithDiagnostics(transParagraphs, gMap)
		if titleDiag.RemovedUnknownHanzi+paragraphDiag.RemovedUnknownHanzi > 0 {
			return ErrIncompleteTranslation
		}
		transTitle = cleanTitle
		thData := map[string]interface{}{
			"novelId":    slug,
			"chapterNo":  chapterNo,
			"sourceLang": "cn",
			"targetLang": "th",
			"title": map[string]string{
				"source":     sourceTitle,
				"translated": transTitle,
			},
			"status":     "translated",
			"paragraphs": cleaned,
			"updatedAt":  time.Now().Format(time.RFC3339),
		}
		data, err := json.MarshalIndent(thData, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal chapter %d translation: %w", chapterNo, err)
		}
		if err := writeFileAtomic(filepath.Join(chaptersDir, numStr+".th.json"), data); err != nil {
			return fmt.Errorf("save chapter %d translation: %w", chapterNo, err)
		}
		changed = true
	}

	return nil
}

// RepairChapter re-sanitizes an already-stored translated chapter against the
// current glossary + builtin rules and persists the result. Idempotent.
func (s *Store) RepairChapter(slug string, chapterNo int) (*model.ChapterContent, error) {
	chapter, err := s.GetChapter(slug, chapterNo)
	if err != nil {
		return nil, err
	}
	if len(chapter.TranslatedText) == 0 && !translator.HasHanzi(chapter.TranslatedTitle) {
		return chapter, nil // nothing to repair
	}
	if err := s.SaveChapter(slug, chapterNo, chapter.SourceTitle, chapter.TranslatedTitle, nil, chapter.TranslatedText); err != nil {
		if errors.Is(err, ErrIncompleteTranslation) {
			return chapter, nil
		}
		return nil, err
	}
	return s.GetChapter(slug, chapterNo)
}

func extractChapterTitles(data []byte) (string, string, error) {
	var raw struct {
		Title json.RawMessage `json:"title"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", "", err
	}
	var titleObj struct {
		Source     string `json:"source"`
		Translated string `json:"translated"`
	}
	if err := json.Unmarshal(raw.Title, &titleObj); err == nil && (titleObj.Source != "" || titleObj.Translated != "") {
		return titleObj.Source, titleObj.Translated, nil
	}
	var title string
	if err := json.Unmarshal(raw.Title, &title); err == nil {
		return title, "", nil
	}
	if len(raw.Title) == 0 || string(raw.Title) == "null" {
		return "", "", nil
	}
	return "", "", fmt.Errorf("unsupported title format")
}

// cleanChapterTitle strips leading breadcrumb parts some scrapers baked
// into titles ("A >> B >> 第2章 X" → "第2章 X"). Loops because some pages
// chain several navigation levels.
func cleanChapterTitle(t string) string {
	for {
		idx := strings.Index(t, ">>")
		if idx == -1 {
			return strings.TrimSpace(t)
		}
		t = t[idx+2:]
	}
}

// Helper: parse flexible chapter JSON (handles raw strings or {"text": "..."} objects, and string/struct titles)
func parseChapterJSON(data []byte, content *model.ChapterContent, isSource bool) error {
	var raw struct {
		Title      json.RawMessage `json:"title"`
		Paragraphs json.RawMessage `json:"paragraphs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var titleObj struct {
		Source     string `json:"source"`
		Translated string `json:"translated"`
	}
	if err := json.Unmarshal(raw.Title, &titleObj); err == nil && (titleObj.Source != "" || titleObj.Translated != "") {
		if isSource {
			content.SourceTitle = titleObj.Source
		} else {
			content.TranslatedTitle = titleObj.Translated
			if content.SourceTitle == "" {
				content.SourceTitle = titleObj.Source
			}
		}
	} else if len(raw.Title) > 0 && string(raw.Title) != "null" {
		var title string
		if err := json.Unmarshal(raw.Title, &title); err != nil {
			return fmt.Errorf("unsupported title format")
		}
		if isSource {
			content.SourceTitle = title
		} else {
			content.TranslatedTitle = title
		}
	}

	// Native files contain strings. Decode the array together instead of
	// allocating a RawMessage and a decoder for every paragraph. Keep the
	// flexible legacy path for mixed string/{text: ...} imports.
	var paragraphs []string
	if len(raw.Paragraphs) > 0 {
		if err := json.Unmarshal(raw.Paragraphs, &paragraphs); err != nil {
			var legacy []json.RawMessage
			if err := json.Unmarshal(raw.Paragraphs, &legacy); err != nil {
				return err
			}
			paragraphs = make([]string, len(legacy))
			for i, item := range legacy {
				if err := json.Unmarshal(item, &paragraphs[i]); err != nil {
					var obj struct {
						Text string `json:"text"`
					}
					if err := json.Unmarshal(item, &obj); err != nil {
						return fmt.Errorf("paragraph %d has unsupported format", i+1)
					}
					paragraphs[i] = obj.Text
				}
			}
		}
	}
	lines := paragraphs[:0]
	for _, text := range paragraphs {
		if text = strings.TrimSpace(text); text != "" {
			lines = append(lines, text)
		}
	}
	clear(paragraphs[len(lines):])
	if isSource {
		content.SourceText = lines
	} else {
		content.TranslatedText = lines
	}
	return nil
}
