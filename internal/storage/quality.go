package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"novelclaw/internal/model"
)

func (s *Store) SaveQualityReport(report model.TranslationQualityReport) error {
	slug := pathSafeSlug(report.NovelSlug)
	writeLock := s.chapterWriteLock(slug, report.ChapterNo)
	writeLock.Lock()
	defer writeLock.Unlock()
	dir := filepath.Join(s.DataDir, slug, "qa")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("%04d.json", report.ChapterNo))
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	s.updateQualityReportCache(slug, report)
	return nil
}

func (s *Store) GetQualityReport(slug string, chapterNo int) (*model.TranslationQualityReport, error) {
	slug = pathSafeSlug(slug)
	path := filepath.Join(s.DataDir, slug, "qa", fmt.Sprintf("%04d.json", chapterNo))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report model.TranslationQualityReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func (s *Store) ListQualityReports(slug string) ([]model.TranslationQualityReport, error) {
	slug = pathSafeSlug(slug)
	cached, ok, generation := s.cachedQualityReports(slug)
	if ok {
		return cached, nil
	}
	dir := filepath.Join(s.DataDir, slug, "qa")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		empty := []model.TranslationQualityReport{}
		s.setQualityReportCache(slug, empty, generation)
		return empty, nil
	}
	if err != nil {
		return nil, err
	}
	reports := make([]model.TranslationQualityReport, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read quality report %s: %w", entry.Name(), err)
		}
		var report model.TranslationQualityReport
		if err := json.Unmarshal(data, &report); err != nil {
			return nil, fmt.Errorf("parse quality report %s: %w", entry.Name(), err)
		}
		reports = append(reports, report)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].ChapterNo < reports[j].ChapterNo })
	s.setQualityReportCache(slug, reports, generation)
	return reports, nil
}
