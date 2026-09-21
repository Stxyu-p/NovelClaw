package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"novelclaw/internal/config"
	"novelclaw/internal/model"
	"novelclaw/internal/translator"
)

type memoryGenerateRequest struct {
	StartChapter int    `json:"startChapter,omitempty"`
	EndChapter   int    `json:"endChapter,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
}

type qaRepairRequest struct {
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model,omitempty"`
	TargetScore int    `json:"targetScore,omitempty"`
}

func (h *APIHandler) GenerateMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	var req memoryGenerateRequest
	if err := decodeJSONBody(w, r, &req, bodyTiny); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid memory generation payload")
		return
	}
	chapters, err := h.store.ListChapters(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	selected, start, end, err := selectMemoryChapters(chapters, req.StartChapter, req.EndChapter)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, err := h.store.GetNovelMemory(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	glossary, err := h.store.GetGlossary(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	contextText, used, err := h.buildMemoryGenerationContext(slug, selected, glossary)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	provider, modelName, err := h.resolveIntelligenceProvider(req.Provider, req.Model)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	systemPrompt, userPrompt := translator.BuildMemoryExtractionPrompts(existing, contextText)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	raw, usedModel, err := h.translator.CompleteWithFallbackForProvider(
		ctx, provider, systemPrompt, userPrompt, []string{modelName}, 0.15,
	)
	if err != nil {
		WriteError(w, http.StatusBadGateway, fmt.Sprintf("generate memory: %v", err))
		return
	}
	candidate, err := translator.ParseNovelMemoryCandidate(raw)
	if err != nil {
		WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	candidate.NovelSlug = slug
	// The user explicitly picked this chapter range — treat it as fresh.
	merged := translator.MergeNovelMemory(existing, candidate, true)
	merged.NovelSlug = slug
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"candidate": candidate, "merged": merged,
		"startChapter": start, "endChapter": end, "chaptersUsed": used,
		"provider": provider.ID, "model": usedModel,
	})
}

func selectMemoryChapters(chapters []model.ChapterMeta, start, end int) ([]model.ChapterMeta, int, int, error) {
	eligible := make([]model.ChapterMeta, 0, len(chapters))
	for _, chapter := range chapters {
		if chapter.HasSource || chapter.HasTranslated {
			eligible = append(eligible, chapter)
		}
	}
	if len(eligible) == 0 {
		return nil, 0, 0, fmt.Errorf("no chapters available for memory generation")
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].ChapterNo < eligible[j].ChapterNo })
	if start <= 0 && end <= 0 {
		from := len(eligible) - 5
		if from < 0 {
			from = 0
		}
		selected := append([]model.ChapterMeta(nil), eligible[from:]...)
		return selected, selected[0].ChapterNo, selected[len(selected)-1].ChapterNo, nil
	}
	if start <= 0 {
		start = end
	}
	if end <= 0 {
		end = start
	}
	if end < start {
		return nil, 0, 0, fmt.Errorf("endChapter must be greater than or equal to startChapter")
	}
	selected := make([]model.ChapterMeta, 0, 10)
	for _, chapter := range eligible {
		if chapter.ChapterNo >= start && chapter.ChapterNo <= end {
			selected = append(selected, chapter)
		}
	}
	if len(selected) == 0 {
		return nil, 0, 0, fmt.Errorf("no chapters found in requested range")
	}
	if len(selected) > 10 {
		return nil, 0, 0, fmt.Errorf("memory generation is limited to 10 chapters per request")
	}
	return selected, selected[0].ChapterNo, selected[len(selected)-1].ChapterNo, nil
}

func (h *APIHandler) buildMemoryGenerationContext(slug string, chapters []model.ChapterMeta, glossary *model.NovelGlossary) (string, int, error) {
	var b strings.Builder
	if glossary != nil {
		known := make([]model.GlossaryItem, 0, 100)
		for _, item := range glossary.Terms {
			if item.Category == "character" && item.Term != "" && item.Target != "" {
				known = append(known, item)
				if len(known) >= 100 {
					break
				}
			}
		}
		if len(known) > 0 {
			data, _ := json.Marshal(known)
			b.WriteString("Known character glossary: ")
			b.Write(data)
			b.WriteString("\n\n")
		}
	}
	used := 0
	for _, meta := range chapters {
		chapter, err := h.store.GetChapter(slug, meta.ChapterNo)
		if err != nil {
			return "", used, err
		}
		payload := map[string]interface{}{
			"chapterNo":       meta.ChapterNo,
			"sourceTitle":     chapter.SourceTitle,
			"translatedTitle": chapter.TranslatedTitle,
			"source":          chapter.SourceText,
			"translation":     chapter.TranslatedText,
		}
		data, _ := json.Marshal(payload)
		if b.Len()+len(data) > 42000 && used > 0 {
			break
		}
		if len(data) > 42000 {
			data = data[:42000]
		}
		b.Write(data)
		b.WriteString("\n")
		used++
	}
	if used == 0 {
		return "", 0, fmt.Errorf("chapter context is empty")
	}
	return b.String(), used, nil
}

func (h *APIHandler) resolveIntelligenceProvider(providerID, modelName string) (config.ActiveProvider, string, error) {
	providerID = config.NormalizeProviderID(providerID)
	if providerID == "" {
		providerID = h.cfg.GetProvider()
	}
	if _, ok := config.ProviderByID(providerID); !ok {
		return config.ActiveProvider{}, "", fmt.Errorf("unsupported provider %q", providerID)
	}
	provider := h.cfg.ProviderRuntime(providerID)
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = strings.TrimSpace(provider.Model)
	}
	if modelName == "" {
		return provider, "", fmt.Errorf("no model configured for provider %s", provider.Name)
	}
	return provider, modelName, nil
}

func (h *APIHandler) RepairQualityWithAI(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	chapterNo, err := strconv.Atoi(r.PathValue("num"))
	if err != nil || chapterNo < 1 {
		WriteError(w, http.StatusBadRequest, "invalid chapter number")
		return
	}
	var req qaRepairRequest
	if err := decodeJSONBody(w, r, &req, bodyTiny); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid QA repair payload")
		return
	}
	if req.TargetScore == 0 {
		req.TargetScore = 90
	}
	if req.TargetScore < 50 || req.TargetScore > 100 {
		WriteError(w, http.StatusBadRequest, "targetScore must be between 50 and 100")
		return
	}
	chapter, err := h.store.GetChapter(slug, chapterNo)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(chapter.SourceText) == 0 || len(chapter.TranslatedText) == 0 {
		WriteError(w, http.StatusBadRequest, "chapter must contain source and translated text")
		return
	}
	glossary, err := h.store.GetGlossary(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	before := translator.EvaluateTranslationQuality(slug, chapterNo, chapter.SourceText, chapter.TranslatedText, glossary)
	repaired, err := h.store.RepairChapter(slug, chapterNo)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	after := translator.EvaluateTranslationQuality(slug, chapterNo, repaired.SourceText, repaired.TranslatedText, glossary)
	if err := h.store.SaveQualityReport(after); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if after.Score >= req.TargetScore {
		WriteJSON(w, http.StatusOK, map[string]interface{}{"mode": "deterministic", "improved": after.Score > before.Score, "previousScore": before.Score, "report": after})
		return
	}
	memory, err := h.store.GetNovelMemory(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	styleRules, err := h.store.GetStyleRules(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	memoryContext := translator.BuildMemoryContext(memory, glossary)
	provider, modelName, err := h.resolveIntelligenceProvider(req.Provider, req.Model)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	systemPrompt, userPrompt := translator.BuildQARepairPrompts(repaired, after, glossary, memoryContext, styleRules)
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	raw, usedModel, err := h.translator.CompleteWithFallbackForProvider(ctx, provider, systemPrompt, userPrompt, []string{modelName}, 0.15)
	if err != nil {
		WriteError(w, http.StatusBadGateway, fmt.Sprintf("repair translation: %v", err))
		return
	}
	title, paragraphs, err := translator.ParseQARepairCandidate(raw, len(repaired.SourceText))
	if err != nil {
		WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	gMap := make(map[string]string, len(glossary.Terms))
	for _, item := range glossary.Terms {
		if item.Term != "" && item.Target != "" {
			gMap[item.Term] = item.Target
		}
	}
	cleanTitle, titleDiag := translator.SanitizeTextWithDiagnostics(title, gMap)
	cleanParagraphs, paragraphDiag := translator.SanitizeParagraphsWithDiagnostics(paragraphs, gMap)
	if titleDiag.RemovedUnknownHanzi+paragraphDiag.RemovedUnknownHanzi > 0 {
		WriteError(w, http.StatusBadGateway, "คำแปลที่แก้ไขยังมีคำที่แปลไม่ครบ จึงเก็บฉบับเดิมไว้")
		return
	}
	if cleanTitle == "" {
		cleanTitle = repaired.TranslatedTitle
	}
	candidateReport := translator.EvaluateTranslationQuality(slug, chapterNo, repaired.SourceText, cleanParagraphs, glossary)
	translator.ApplySanitizationDiagnostics(&candidateReport, translator.MergeSanitizationDiagnostics(titleDiag, paragraphDiag))
	if candidateReport.Score <= after.Score {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"mode": "ai", "improved": false, "saved": false, "previousScore": before.Score,
			"baselineScore": after.Score, "candidateScore": candidateReport.Score, "report": after,
			"provider": provider.ID, "model": usedModel,
		})
		return
	}
	if err := h.store.SaveChapter(slug, chapterNo, repaired.SourceTitle, cleanTitle, nil, cleanParagraphs); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.SaveQualityReport(candidateReport); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"mode": "ai", "improved": true, "saved": true, "previousScore": before.Score,
		"baselineScore": after.Score, "candidateScore": candidateReport.Score, "report": candidateReport,
		"provider": provider.ID, "model": usedModel,
	})
}
