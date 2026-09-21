package api

import (
	"net/http"
	"os"
	"strconv"

	"novelclaw/internal/model"
	"novelclaw/internal/translator"
)

func (h *APIHandler) GetMemory(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	memory, err := h.store.GetNovelMemory(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Seed the view from character glossary entries until curated memory exists.
	if len(memory.Characters) == 0 {
		glossary, err := h.store.GetGlossary(slug)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, item := range glossary.Terms {
			if item.Category == "character" && item.Target != "" {
				memory.Characters = append(memory.Characters, model.CharacterMemory{
					SourceName: item.Term, ThaiName: item.Target, Notes: item.Notes,
				})
			}
		}
	}
	WriteJSON(w, http.StatusOK, memory)
}

func (h *APIHandler) SaveMemory(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	if slug == "" {
		WriteError(w, http.StatusBadRequest, "invalid novel slug")
		return
	}
	var memory model.NovelMemory
	if err := decodeJSONBody(w, r, &memory, bodySmall); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid memory payload")
		return
	}
	if len(memory.Characters) > 200 || len(memory.Facts) > 200 {
		WriteError(w, http.StatusBadRequest, "memory contains too many entries")
		return
	}
	memory.NovelSlug = slug
	if err := h.store.SaveNovelMemory(&memory); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, memory)
}

func (h *APIHandler) ListQualityReports(w http.ResponseWriter, r *http.Request) {
	reports, err := h.store.ListQualityReports(safeSlug(r.PathValue("slug")))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{"reports": reports})
}

func (h *APIHandler) GetQualityReport(w http.ResponseWriter, r *http.Request) {
	chapterNo, err := strconv.Atoi(r.PathValue("num"))
	if err != nil || chapterNo < 1 {
		WriteError(w, http.StatusBadRequest, "invalid chapter number")
		return
	}
	report, err := h.store.GetQualityReport(safeSlug(r.PathValue("slug")), chapterNo)
	if os.IsNotExist(err) {
		WriteError(w, http.StatusNotFound, "quality report not found")
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, report)
}

func (h *APIHandler) RebuildQualityReports(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	start, err := positiveQueryInt(r, "start", 1)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	end, err := positiveQueryInt(r, "end", 0)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if end > 0 && end < start {
		WriteError(w, http.StatusBadRequest, "end must be greater than or equal to start")
		return
	}
	chapters, err := h.store.ListChapters(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	glossary, err := h.store.GetGlossary(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	reports := make([]model.TranslationQualityReport, 0, len(chapters))
	for _, meta := range chapters {
		if r.Context().Err() != nil {
			return
		}
		if meta.ChapterNo < start || (end > 0 && meta.ChapterNo > end) || !meta.HasTranslated {
			continue
		}
		chapter, err := h.store.GetChapter(slug, meta.ChapterNo)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(chapter.TranslatedText) == 0 {
			continue
		}
		report := translator.EvaluateTranslationQuality(
			slug, meta.ChapterNo, chapter.SourceText, chapter.TranslatedText, glossary,
		)
		if err := h.store.SaveQualityReport(report); err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		reports = append(reports, report)
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"rebuilt": len(reports),
		"reports": reports,
	})
}
