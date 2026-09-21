package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"novelclaw/internal/config"
	"novelclaw/internal/model"
	"novelclaw/internal/scraper"
	"novelclaw/internal/storage"
	"novelclaw/internal/translator"
)

type novelSource interface {
	FetchChapterContext(context.Context, string) (*scraper.ScrapedChapter, error)
	FetchTOCContext(context.Context, string) (*scraper.ScrapedNovelInfo, error)
}

// APIHandler coordinates API requests
type APIHandler struct {
	cfg         *config.AppConfig
	store       *storage.Store
	scraper     novelSource
	translator  *translator.Client
	sse         *SSEBroker
	importDelay time.Duration
	jobsMu      sync.Mutex
	coverMu     sync.Mutex
	activeJobs  map[string]*model.TranslationProgress
	cancels     map[string]context.CancelFunc
}

// NewAPIHandler initializes API handlers
func NewAPIHandler(cfg *config.AppConfig, store *storage.Store, sse *SSEBroker) *APIHandler {
	return &APIHandler{
		cfg:         cfg,
		store:       store,
		scraper:     scraper.NewUniversalScraper(),
		translator:  translator.NewClient(cfg),
		sse:         sse,
		importDelay: 400 * time.Millisecond,
		activeJobs:  make(map[string]*model.TranslationProgress),
		cancels:     make(map[string]context.CancelFunc),
	}
}

// safeSlug sanitizes a URL path slug to prevent path traversal and HTML
// attribute injection. Only [A-Za-z0-9_-] survive, so "..", separators and
// quotes are impossible in the result.
func safeSlug(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	raw = strings.TrimSpace(b.String())
	if raw == "" {
		return "_invalid_"
	}
	return raw
}

// WriteJSON sends a standard JSON response. Encode before committing headers
// so serialization failures can still produce a valid HTTP 500 response.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		log.Printf("encode JSON response: %v", err)
		status = http.StatusInternalServerError
		payload = []byte(`{"error":"failed to encode response"}`)
	}
	payload = append(payload, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := w.Write(payload); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

// WriteError sends a JSON error response
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// ListNovels returns all novels
func (h *APIHandler) ListNovels(w http.ResponseWriter, r *http.Request) {
	novels, err := h.store.ListNovels()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"novels": novels,
	})
}

// GetNovel returns a single novel
func (h *APIHandler) GetNovel(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	novel, err := h.store.GetNovel(slug)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNovelNotFound) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, novel)
}

// SaveNovel creates or updates a novel
func (h *APIHandler) SaveNovel(w http.ResponseWriter, r *http.Request) {
	var n model.Novel
	if err := decodeJSONBody(w, r, &n, bodySmall); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	if err := h.store.SaveNovel(&n); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, n)
}

// ListChapters returns the chapter list for a novel
func (h *APIHandler) ListChapters(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	chapters, err := h.store.ListChapters(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"chapters": chapters,
	})
}

// GetChapter returns a single chapter's content
func (h *APIHandler) GetChapter(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	numStr := r.PathValue("num")
	chNum, err := strconv.Atoi(numStr)
	if err != nil || chNum <= 0 {
		WriteError(w, http.StatusBadRequest, "Invalid chapter number")
		return
	}

	chapter, err := h.store.GetChapter(slug, chNum)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrChapterNotFound) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return
	}

	// NOTE: deliberately no writes here — GET must stay idempotent. Chapters
	// are sanitized at write time (storage.SaveChapter) and legacy dirty
	// chapters can be fixed explicitly via
	// POST /api/novels/{slug}/chapters/{num}/repair (RepairChapter).

	WriteJSON(w, http.StatusOK, chapter)
}

// RepairChapter re-sanitizes a stored chapter against the novel glossary and
// persists the result. Use for legacy chapters saved before write-time
// sanitization existed (or after a builtin-glossary correction like the QA R2
// term fixes). Idempotent: repairing a clean chapter is a no-op rewrite.
func (h *APIHandler) RepairChapter(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	chNum, err := strconv.Atoi(r.PathValue("num"))
	if err != nil || chNum <= 0 {
		WriteError(w, http.StatusBadRequest, "Invalid chapter number")
		return
	}

	chapter, err := h.store.RepairChapter(slug, chNum)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, chapter)
}

// GlossaryCheck scans a chapter range and reports chapters where a glossary
// term appears in the source but its expected translation is missing from
// the translated text. Read-only; pairs with the repair endpoint.
func (h *APIHandler) GlossaryCheck(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	start, err := positiveQueryInt(r, "start", 1)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	end, err := positiveQueryInt(r, "end", start)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if end < start {
		WriteError(w, http.StatusBadRequest, "end must be greater than or equal to start")
		return
	}
	if end-start > 200 { // ponytail: cap scan size; paginate if ever needed
		end = start + 200
	}

	glossary, err := h.store.GetGlossary(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(glossary.Terms) == 0 {
		WriteJSON(w, http.StatusOK, map[string]interface{}{"issues": []interface{}{}, "scanned": 0})
		return
	}

	type issue struct {
		ChapterNo int      `json:"chapterNo"`
		Term      string   `json:"term"`
		Expected  string   `json:"expected"`
		Missing   []string `json:"missing"` // paragraphs whose translation lacks the term
	}
	var issues []issue
	scanned := 0

	for chNo := start; chNo <= end; chNo++ {
		ch, err := h.store.GetChapter(slug, chNo)
		if err != nil {
			if errors.Is(err, storage.ErrChapterNotFound) {
				continue
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(ch.SourceText) == 0 || len(ch.TranslatedText) == 0 {
			continue
		}
		scanned++
		srcJoined := strings.Join(ch.SourceText, "\n")
		thJoined := strings.Join(ch.TranslatedText, "\n")
		seen := map[string]bool{}
		for _, t := range glossary.Terms {
			if t.Term == "" || t.Target == "" || seen[t.Term] {
				continue
			}
			seen[t.Term] = true
			if !strings.Contains(srcJoined, t.Term) || strings.Contains(thJoined, t.Target) {
				continue
			}
			var missing []string
			for i, srcPara := range ch.SourceText {
				if !strings.Contains(srcPara, t.Term) {
					continue
				}
				if i < len(ch.TranslatedText) && strings.Contains(ch.TranslatedText[i], t.Target) {
					continue
				}
				missing = append(missing, fmt.Sprintf("ย่อหน้า %d", i+1))
				if len(missing) >= 5 {
					break
				}
			}
			issues = append(issues, issue{ChapterNo: chNo, Term: t.Term, Expected: t.Target, Missing: missing})
		}
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{"scanned": scanned, "issues": issues})
}

// GetGlossary returns glossary for a novel
func (h *APIHandler) GetGlossary(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	glossary, err := h.store.GetGlossary(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, glossary)
}

// SaveGlossary saves glossary terms for a novel
func (h *APIHandler) SaveGlossary(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	var g model.NovelGlossary
	if err := decodeJSONBody(w, r, &g, bodySmall); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	g.NovelSlug = slug
	if err := h.store.SaveGlossary(&g); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, g)
}

// DiscoverGlossary scans chapters and extracts terms using LLM
func (h *APIHandler) DiscoverGlossary(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	var req model.DiscoverGlossaryRequest
	if err := decodeJSONBody(w, r, &req, bodyTiny); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	req.NovelSlug = slug

	if req.StartChapter <= 0 {
		req.StartChapter = 1
	}
	if req.EndChapter <= 0 {
		req.EndChapter = req.StartChapter + 2
	}

	novel, err := h.store.GetNovel(slug)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNovelNotFound) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return
	}

	// Gather sample paragraphs from requested chapters. Missing numbers are
	// allowed, but an unreadable/corrupt chapter must not be silently skipped.
	var sampleParagraphs []string
	for chNo := req.StartChapter; chNo <= req.EndChapter; chNo++ {
		ch, err := h.store.GetChapter(slug, chNo)
		if err != nil {
			if errors.Is(err, storage.ErrChapterNotFound) {
				continue
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(ch.SourceText) > 0 {
			sampleParagraphs = append(sampleParagraphs, ch.SourceText...)
		}
	}

	if len(sampleParagraphs) == 0 {
		WriteError(w, http.StatusBadRequest, "No source chapters available to scan")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	discovered, err := h.translator.DiscoverGlossaryTerms(ctx, novel.Title, sampleParagraphs, req.Model)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("Discovery failed: %v", err))
		return
	}

	// Merge into existing glossary but DO NOT auto-save: the user reviews the
	// discovered terms in the UI and saves explicitly via SaveGlossary.
	existing, err := h.store.GetGlossary(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	termMap := make(map[string]bool)
	for _, t := range existing.Terms {
		termMap[t.Term] = true
	}

	// Only count/report terms that actually land in the table — the model's
	// raw list includes builtins (locked by the sanitizer) and duplicates.
	added := make([]model.GlossaryItem, 0)
	skippedBuiltin := 0
	for _, d := range discovered {
		if d.Term == "" || d.Target == "" || termMap[d.Term] {
			continue
		}
		// Same guard as auto discovery: builtin-covered terms are already
		// enforced by the sanitizer with locked values.
		if _, builtin := translator.BuiltinNovelGlossary[d.Term]; builtin {
			skippedBuiltin++
			continue
		}
		existing.Terms = append(existing.Terms, d)
		termMap[d.Term] = true
		added = append(added, d)
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"discovered":     discovered,
		"added":          added,
		"skippedBuiltin": skippedBuiltin,
		"glossary":       existing,
	})
}

// GetBookmark gets bookmark position
func (h *APIHandler) GetBookmark(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	bm, err := h.store.GetBookmark(slug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, bm)
}

// SaveBookmark saves bookmark position
func (h *APIHandler) SaveBookmark(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	var bm model.Bookmark
	if err := decodeJSONBody(w, r, &bm, bodyTiny); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	bm.NovelSlug = slug
	if bm.ChapterNo < 1 || bm.ScrollPercentage < 0 || bm.ScrollPercentage > 100 {
		WriteError(w, http.StatusBadRequest, "invalid bookmark position")
		return
	}
	// Opening a chapter posts scrollPercentage 0; don't let it wipe the saved
	// position when the same chapter is re-opened (refresh / TTS hand-off).
	if bm.ScrollPercentage <= 0 {
		if existing, err := h.store.GetBookmark(slug); err == nil && existing.ChapterNo == bm.ChapterNo {
			bm.ScrollPercentage = existing.ScrollPercentage
		}
	}
	if err := h.store.SaveBookmark(&bm); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, bm)
}

// CancelJob stops an active background translation job
func (h *APIHandler) CancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	h.jobsMu.Lock()
	var slug string
	if p, ok := h.activeJobs[jobID]; ok {
		slug = p.NovelSlug
	}
	// Cancelling one job stops every queued/running job of the same novel,
	// so a single "stop" button really stops the whole queue.
	for id, p := range h.activeJobs {
		if cancel, ok := h.cancels[id]; ok && (slug == "" && id == jobID || slug != "" && p.NovelSlug == slug) {
			cancel()
			delete(h.cancels, id)
		}
	}
	h.jobsMu.Unlock()

	h.sse.Broadcast(model.TranslationProgress{
		JobID:     jobID,
		NovelSlug: slug,
		Status:    "cancelled",
		Message:   "ยกเลิกงานเรียบร้อยแล้ว",
	})

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"jobId":   jobID,
	})
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
