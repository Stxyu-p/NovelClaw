package storage

import (
	"sort"

	"novelclaw/internal/model"
)

func cloneQualityReport(report model.TranslationQualityReport) model.TranslationQualityReport {
	if len(report.Issues) > 0 {
		report.Issues = append([]model.TranslationQualityIssue(nil), report.Issues...)
	}
	return report
}

func (s *Store) cachedQualityReports(slug string) ([]model.TranslationQualityReport, bool, uint64) {
	s.qaCacheMu.RLock()
	generation := s.qaGeneration
	if !s.qaCacheLoaded[slug] {
		s.qaCacheMu.RUnlock()
		return nil, false, generation
	}
	cache := s.qaCache[slug]
	reports := make([]model.TranslationQualityReport, 0, len(cache))
	for _, report := range cache {
		reports = append(reports, cloneQualityReport(report))
	}
	s.qaCacheMu.RUnlock()
	sort.Slice(reports, func(i, j int) bool { return reports[i].ChapterNo < reports[j].ChapterNo })
	return reports, true, generation
}

func (s *Store) setQualityReportCache(slug string, reports []model.TranslationQualityReport, generation uint64) {
	cache := make(map[int]model.TranslationQualityReport, len(reports))
	for _, report := range reports {
		cache[report.ChapterNo] = cloneQualityReport(report)
	}
	s.qaCacheMu.Lock()
	if generation == s.qaGeneration {
		s.qaCache[slug] = cache
		s.qaCacheLoaded[slug] = true
	}
	s.qaCacheMu.Unlock()
}

func (s *Store) updateQualityReportCache(slug string, report model.TranslationQualityReport) {
	s.qaCacheMu.Lock()
	defer s.qaCacheMu.Unlock()
	s.qaGeneration++
	if !s.qaCacheLoaded[slug] {
		return
	}
	if s.qaCache[slug] == nil {
		s.qaCache[slug] = make(map[int]model.TranslationQualityReport)
	}
	s.qaCache[slug][report.ChapterNo] = cloneQualityReport(report)
}
