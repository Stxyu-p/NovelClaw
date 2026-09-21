package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"novelclaw/internal/config"
	"novelclaw/internal/model"
	"novelclaw/internal/storage"
	"novelclaw/internal/translator"
)

// Translate handles translating chapters with smart chunking and genre presets
func (h *APIHandler) Translate(w http.ResponseWriter, r *http.Request) {
	var req model.TranslateRequest
	if err := decodeJSONBody(w, r, &req, bodyTiny); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if req.NovelSlug == "" {
		WriteError(w, http.StatusBadRequest, "novelSlug is required")
		return
	}
	req.Provider = config.NormalizeProviderID(req.Provider)
	if req.Provider == "" {
		req.Provider = h.cfg.GetProvider()
	}
	if _, ok := config.ProviderByID(req.Provider); !ok {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("unsupported provider %q", req.Provider))
		return
	}
	providerSnapshot := h.cfg.ProviderRuntime(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		req.Model = strings.TrimSpace(providerSnapshot.Model)
	}
	if req.Model == "" {
		WriteError(w, http.StatusBadRequest, "no translation model is configured")
		return
	}

	// Persist a user-selected model only for the active profile. Jobs targeting
	// another explicit provider must not mutate whichever provider is active in UI.
	if req.Provider == h.cfg.GetProvider() && req.Model != h.cfg.GetDefaultModel() {
		if err := h.cfg.SetActiveModel(req.Model); err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("save selected model: %v", err))
			return
		}
		providerSnapshot = h.cfg.ProviderRuntime(req.Provider)
	}

	if req.StartChapter <= 0 {
		req.StartChapter = 1
	}
	if req.EndChapter <= 0 {
		req.EndChapter = req.StartChapter
	}
	if req.EndChapter < req.StartChapter {
		WriteError(w, http.StatusBadRequest, "endChapter must not precede startChapter")
		return
	}
	chapters, err := h.store.ListChapters(req.NovelSlug)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(chapters) == 0 || req.EndChapter > chapters[len(chapters)-1].ChapterNo {
		WriteError(w, http.StatusBadRequest, "translation range exceeds the available catalog")
		return
	}

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	if err := h.persistJob(jobID, req); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("persist translation job: %v", err))
		return
	}
	jobCtx, cancel := context.WithCancel(context.Background())

	h.jobsMu.Lock()
	h.cancels[jobID] = cancel
	h.activeJobs[jobID] = &model.TranslationProgress{
		JobID:          jobID,
		NovelSlug:      req.NovelSlug,
		TotalChapters:  req.EndChapter - req.StartChapter + 1,
		CurrentChapter: req.StartChapter,
		Status:         "running",
		Percentage:     0,
	}
	h.jobsMu.Unlock()

	// Start translation worker with the provider runtime snapshot captured above.
	go h.runTranslationJobWithProvider(jobCtx, jobID, req, providerSnapshot)

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":   jobID,
		"status":  "translation_started",
		"message": fmt.Sprintf("Translating chapters %d - %d", req.StartChapter, req.EndChapter),
	})
}

func (h *APIHandler) runTranslationJob(ctx context.Context, jobID string, req model.TranslateRequest) {
	req.Provider = config.NormalizeProviderID(req.Provider)
	if req.Provider == "" {
		req.Provider = h.cfg.GetProvider()
	}
	provider := h.cfg.ProviderRuntime(req.Provider)
	if strings.TrimSpace(req.Model) == "" {
		req.Model = provider.Model
	}
	h.runTranslationJobWithProvider(ctx, jobID, req, provider)
}

func (h *APIHandler) runTranslationJobWithProvider(ctx context.Context, jobID string, req model.TranslateRequest, provider config.ActiveProvider) {
	defer func() {
		h.removeJobFile(jobID)
		h.jobsMu.Lock()
		delete(h.cancels, jobID)
		delete(h.activeJobs, jobID)
		h.jobsMu.Unlock()
	}()

	total := req.EndChapter - req.StartChapter + 1
	failContext := func(label string, err error) bool {
		if err == nil {
			return false
		}
		message := fmt.Sprintf("เตรียมบริบทการแปลไม่สำเร็จ (%s): %v", label, err)
		h.broadcastProgress(model.TranslationProgress{
			JobID: jobID, NovelSlug: req.NovelSlug, Status: "error",
			Message: message, Percentage: 100, ErrorDetails: err.Error(),
		})
		return true
	}

	novel, err := h.store.GetNovel(req.NovelSlug)
	if err != nil && !errors.Is(err, storage.ErrNovelNotFound) {
		failContext("ข้อมูลเรื่อง", err)
		return
	}
	genre := req.Genre
	if genre == "" && novel != nil {
		genre = novel.Genre
	}
	glossary, err := h.store.GetGlossary(req.NovelSlug)
	if failContext("อภิธานศัพท์", err) {
		return
	}
	memory, err := h.store.GetNovelMemory(req.NovelSlug)
	if failContext("Story Memory", err) {
		return
	}
	styleRules, err := h.store.GetStyleRules(req.NovelSlug)
	if failContext("Style Rules", err) {
		return
	}
	chapterList, err := h.store.ListChapters(req.NovelSlug)
	if failContext("สารบัญตอน", err) {
		return
	}
	var selected []int
	for _, chapter := range chapterList {
		if chapter.ChapterNo >= req.StartChapter && chapter.ChapterNo <= req.EndChapter && (chapter.HasSource || chapter.HasTranslated) {
			selected = append(selected, chapter.ChapterNo)
		}
	}
	total = len(selected)
	if total == 0 {
		failContext("สารบัญตอน", fmt.Errorf("ไม่พบตอนที่มีเนื้อหาในช่วงที่เลือก"))
		return
	}
	memoryContext := translator.BuildMemoryContext(memory, glossary)

	// Model fallback chain: primary model first, then any fallbacks the UI
	// sent (e.g. other models from the same gateway). A dead model in the
	// chain can no longer kill the whole queue.
	modelChain := []string{req.Model}
	for _, m := range req.FallbackModels {
		if m != "" && m != req.Model && !slices.Contains(modelChain, m) {
			modelChain = append(modelChain, m)
		}
	}
	successCount := 0
	var lastError string

	// Parallel workers: chapters are translated in waves of cfg.Parallel.
	// A barrier between waves lets the first chapter of each wave use real
	// prev-context (all earlier waves are already saved). Other chapters in
	// a wave fall back to source-tail context because their predecessor may
	// still be translating.
	workers := h.cfg.GetParallel()
	if workers < 1 {
		workers = 1
	}
	if workers > 8 { // ponytail: soft cap against config typos; raise if gateway handles more
		workers = 8
	}
	if workers > total {
		workers = total
	}
	jc := &jobCounters{}

	for waveStart := 0; waveStart < total; waveStart += workers {
		select {
		case <-ctx.Done():
			log.Printf("Translation job %s cancelled by user\n", jobID)
			return
		default:
		}

		waveEnd := waveStart + workers
		if waveEnd > total {
			waveEnd = total
		}
		waveSize := waveEnd - waveStart
		done := make(chan struct{}, waveSize)

		for index := waveStart; index < waveEnd; index++ {
			go func(index int) {
				defer func() { done <- struct{}{} }()
				h.translateOneChapter(ctx, jobID, req, provider, selected[index], total, index+1, glossary, styleRules, memoryContext,
					genre, modelChain, chapterList, jc)
			}(index)
		}
		for i := 0; i < waveSize; i++ {
			<-done
		}
		if ctx.Err() != nil {
			return
		}

		successCount += int(jc.success.Swap(0))
		if errText := jc.takeError(); errText != "" {
			lastError = errText
		}
	}

	if jc.written.Load() > 0 {
		// New translations landed — let the AI refresh story memory on its own,
		// and bootstrap glossary discovery when the novel has no terms yet
		// (e.g. novels imported before auto-discovery existed).
		h.AutoGenerateMemory(req.NovelSlug)
		if glossary == nil || len(glossary.Terms) == 0 {
			h.AutoDiscoverGlossary(req.NovelSlug)
		}
	}

	finalStatus := "completed"
	finalMsg := fmt.Sprintf("การแปลเสร็จสิ้น (%d/%d ตอน)", successCount, total)
	switch {
	case successCount == 0:
		finalStatus = "error"
		if lastError != "" {
			finalMsg = fmt.Sprintf("การแปลล้มเหลว: %s", lastError)
		} else {
			finalMsg = "การแปลล้มเหลว: ไม่พบตอนที่มีต้นฉบับพร้อมแปล"
		}
	case successCount < total:
		finalStatus = "partial"
		finalMsg = fmt.Sprintf("การแปลเสร็จบางส่วน (%d/%d ตอน)", successCount, total)
	}

	h.broadcastProgress(model.TranslationProgress{
		JobID:        jobID,
		NovelSlug:    req.NovelSlug,
		Status:       finalStatus,
		Message:      finalMsg,
		Percentage:   100,
		ErrorDetails: lastError,
	})
}

// jobCounters accumulates per-chapter results from parallel workers.
type jobCounters struct {
	success atomic.Int64
	written atomic.Int64
	errMu   sync.Mutex
	lastErr string
}

func (jc *jobCounters) setError(errText string) {
	if errText == "" {
		return
	}
	jc.errMu.Lock()
	jc.lastErr = errText
	jc.errMu.Unlock()
}

func (jc *jobCounters) takeError() string {
	jc.errMu.Lock()
	defer jc.errMu.Unlock()
	errText := jc.lastErr
	jc.lastErr = ""
	return errText
}

// translateOneChapter translates a single chapter; safe to run concurrently
// for different chapter numbers within one job.
func (h *APIHandler) translateOneChapter(ctx context.Context, jobID string, req model.TranslateRequest, provider config.ActiveProvider,
	chNo, total, currentIdx int, glossary *model.NovelGlossary, styleRules, memoryContext, genre string,
	modelChain []string, chapterList []model.ChapterMeta, jc *jobCounters) {

	pct := int((float64(currentIdx) / float64(total)) * 100)

	h.broadcastProgress(model.TranslationProgress{
		JobID:          jobID,
		NovelSlug:      req.NovelSlug,
		CurrentChapter: chNo,
		TotalChapters:  total,
		Status:         "running",
		Message:        fmt.Sprintf("กำลังแปลตอนที่ %d... (%d/%d)", chNo, currentIdx, total),
		Percentage:     pct,
	})

	content, err := h.store.GetChapter(req.NovelSlug, chNo)
	if err != nil {
		if errors.Is(err, storage.ErrChapterNotFound) {
			log.Printf("Chapter %d not found, skipping\n", chNo)
			return
		}
		message := fmt.Sprintf("อ่านตอนที่ %d ไม่สำเร็จ: %v", chNo, err)
		log.Printf("%s\n", message)
		jc.setError(message)
		h.broadcastProgress(model.TranslationProgress{
			JobID: jobID, NovelSlug: req.NovelSlug, CurrentChapter: chNo,
			Status: "error", Message: message, ErrorDetails: err.Error(),
		})
		return
	}
	if len(content.TranslatedText) > 0 && !req.Force {
		log.Printf("Chapter %d already translated, skipping\n", chNo)
		jc.success.Add(1)
		return
	}
	if len(content.SourceText) == 0 {
		log.Printf("Chapter %d has no source text, skipping\n", chNo)
		return
	}

	// Previous context: last 3 paragraphs of the nearest preceding chapter
	// (gap-aware: chapter numbers can skip, e.g. 72 -> 86). Prefer the real
	// translated predecessor; if it is still translating in the same wave,
	// use that predecessor's source tail instead of the current chapter.
	prevContext := previousChapterContext(h.store, req.NovelSlug, chNo, chapterList)

	relevantGlossary := translator.FilterRelevantGlossary(glossary, content.SourceText)
	systemPrompt := translator.BuildSystemPromptWithMemory(relevantGlossary, prevContext, genre, styleRules, memoryContext)

	// Smart Chunking for long chapters (max 25 paragraphs or 750 chars per chunk)
	chunks := translator.SplitParagraphsIntoChunks(content.SourceText, 750)
	var fullTranslatedParagraphs []string
	var finalTransTitle string
	chapterFailed := false
	retriedChunks := 0

	for chunkIdx, chunk := range chunks {
		select {
		case <-ctx.Done():
			log.Printf("Translation job %s cancelled during chapter %d\n", jobID, chNo)
			return
		default:
		}

		chunkUserPrompt := translator.BuildUserPrompt(content.SourceTitle, chunk.Paragraphs)
		if chunkIdx > 0 && len(fullTranslatedParagraphs) > 0 {
			lastChunkEnd := fullTranslatedParagraphs[len(fullTranslatedParagraphs)-1]
			chunkUserPrompt = fmt.Sprintf("[เนื้อหาก่อนหน้านี้ในบทเดียวกัน: ...%s]\n\n%s", lastChunkEnd, chunkUserPrompt)
		}

		chunkUserPromptBase := chunkUserPrompt
		var tTitle string
		var tParagraphs []string
		for attempt := 1; ; attempt++ {
			if attempt > 1 {
				// Low temperature makes identical prompts reproduce the same
				// output, so retries carry an explicit corrective nudge.
				chunkUserPrompt = fmt.Sprintf("%s\n\n[เตือน: ครั้งก่อนได้มาเพียง %d จาก %d ย่อหน้า — ห้ามรวม/ตัด/ข้ามย่อหน้า ต้องส่งครบทุกย่อหน้า]", chunkUserPromptBase, len(tParagraphs), len(chunk.Paragraphs))
			}
			chunkCtx, chunkCancel := context.WithTimeout(ctx, 120*time.Second)
			rawOutput, _, err := h.translator.CompleteWithFallbackForProvider(chunkCtx, provider, systemPrompt, chunkUserPrompt, modelChain, req.Temperature)
			chunkCancel()

			if err != nil {
				log.Printf("Translation error on chapter %d (chunk %d): %v\n", chNo, chunkIdx, err)
				jc.setError(err.Error())
				h.broadcastProgress(model.TranslationProgress{
					JobID:          jobID,
					NovelSlug:      req.NovelSlug,
					CurrentChapter: chNo,
					Status:         "error",
					Message:        fmt.Sprintf("ข้อผิดพลาดตอนที่ %d: %v", chNo, err),
					ErrorDetails:   err.Error(),
				})
				chapterFailed = true
				break
			}

			tTitle, tParagraphs = translator.ParseTranslationOutput(rawOutput)
			if len(tParagraphs) == len(chunk.Paragraphs) {
				break
			}
			retriedChunks++
			if attempt >= 3 {
				log.Printf("chapter %d (chunk %d): model kept returning %d/%d paragraphs after %d attempts\n", chNo, chunkIdx, len(tParagraphs), len(chunk.Paragraphs), attempt)
				message := fmt.Sprintf("ตอนที่ %d: โมเดลส่ง %d/%d ย่อหน้าหลังลอง 3 ครั้ง จึงเก็บคำแปลเดิมไว้", chNo, len(tParagraphs), len(chunk.Paragraphs))
				jc.setError(message)
				h.broadcastProgress(model.TranslationProgress{JobID: jobID, NovelSlug: req.NovelSlug, CurrentChapter: chNo, Status: "error", Message: message, ErrorDetails: message})
				chapterFailed = true
				break
			}
			log.Printf("chapter %d (chunk %d): model returned %d/%d paragraphs, retrying (attempt %d/3)\n", chNo, chunkIdx, len(tParagraphs), len(chunk.Paragraphs), attempt+1)
		}
		if chapterFailed {
			break
		}

		if finalTransTitle == "" && tTitle != "" {
			finalTransTitle = tTitle
		}
		fullTranslatedParagraphs = append(fullTranslatedParagraphs, tParagraphs...)
	}

	if chapterFailed || len(fullTranslatedParagraphs) == 0 {
		return
	}

	if finalTransTitle == "" {
		finalTransTitle = fmt.Sprintf("ตอนที่ %d", chNo)
	}

	var gMap map[string]string
	if glossary != nil && len(glossary.Terms) > 0 {
		gMap = make(map[string]string)
		for _, t := range glossary.Terms {
			gMap[t.Term] = t.Target
		}
	}
	cleanTitle, titleDiag := translator.SanitizeTextWithDiagnostics(finalTransTitle, gMap)
	cleanParagraphs, paragraphDiag := translator.SanitizeParagraphsWithDiagnostics(fullTranslatedParagraphs, gMap)
	finalTransTitle = cleanTitle
	fullTranslatedParagraphs = cleanParagraphs
	sanitizeDiag := translator.MergeSanitizationDiagnostics(titleDiag, paragraphDiag)
	if sanitizeDiag.RemovedUnknownHanzi > 0 {
		message := fmt.Sprintf("ตอนที่ %d: คำแปลยังมีคำที่แปลไม่ครบ จึงไม่บันทึกทับฉบับเดิม — เพิ่มคำศัพท์หรือเลือกโมเดลอื่นแล้วลองอีกครั้ง", chNo)
		jc.setError(message)
		h.broadcastProgress(model.TranslationProgress{JobID: jobID, NovelSlug: req.NovelSlug, CurrentChapter: chNo, Status: "error", Message: message, ErrorDetails: message})
		return
	}

	// Consistency check: glossary terms present in the source should have
	// their expected target somewhere in the translation. Mismatches are
	// reported (not blocking) so the user can confirm glossary entries.
	var warnings []string
	if glossary != nil && len(glossary.Terms) > 0 {
		srcJoined := strings.Join(content.SourceText, "\n")
		thJoined := strings.Join(fullTranslatedParagraphs, "\n")
		seenTerm := map[string]bool{}
		for _, t := range glossary.Terms {
			if t.Term == "" || t.Target == "" || seenTerm[t.Term] {
				continue
			}
			seenTerm[t.Term] = true
			if strings.Contains(srcJoined, t.Term) && !strings.Contains(thJoined, t.Target) {
				warnings = append(warnings, fmt.Sprintf("พบ %q แต่ไม่พบ %q ในฉบับแปล", t.Term, t.Target))
				if len(warnings) >= 5 {
					break
				}
			}
		}
	}

	qaReport := translator.EvaluateTranslationQuality(req.NovelSlug, chNo, content.SourceText, fullTranslatedParagraphs, glossary)
	translator.ApplySanitizationDiagnostics(&qaReport, sanitizeDiag)
	for _, issue := range qaReport.Issues {
		if issue.Severity == "error" {
			warnings = append(warnings, "QA: "+issue.Message)
		}
	}

	if retriedChunks > 0 {
		warnings = append(warnings, fmt.Sprintf("ระบบส่งใหม่อัตโนมัติ %d ครั้งเพราะโมเดลส่งย่อหน้าไม่ครบ — ผลลัพธ์สุดท้ายครบถ้วน", retriedChunks))
	}

	if ctx.Err() != nil {
		return
	}
	if err := h.store.SaveChapter(req.NovelSlug, chNo, content.SourceTitle, finalTransTitle, nil, fullTranslatedParagraphs); err != nil {
		log.Printf("SaveChapter %d failed: %v\n", chNo, err)
		jc.setError(err.Error())
		return
	}
	if err := h.store.SaveQualityReport(qaReport); err != nil {
		log.Printf("Save QA report %d failed: %v\n", chNo, err)
		warnings = append(warnings, fmt.Sprintf("QA: บันทึกรายงานคุณภาพไม่สำเร็จ (%v) — คำแปลถูกบันทึกแล้ว", err))
	}
	jc.success.Add(1)
	jc.written.Add(1)

	h.sse.Broadcast(map[string]interface{}{
		"type":      "chapter_translated",
		"jobId":     jobID,
		"novelSlug": req.NovelSlug,
		"chapterNo": chNo,
		"title":     finalTransTitle,
		"warnings":  warnings,
		"qaScore":   qaReport.Score,
		"qaIssues":  qaReport.Issues,
	})
}

// previousChapterNo returns the largest chapter number strictly below chNo,
// or 0 when none exists. Handles gaps in chapter numbering.
func previousChapterNo(chapters []model.ChapterMeta, chNo int) int {
	prev := 0
	for _, ch := range chapters {
		if ch.ChapterNo < chNo && ch.ChapterNo > prev {
			prev = ch.ChapterNo
		}
	}
	return prev
}

// previousChapterContext builds continuity context from the actual predecessor.
// In parallel waves the translated predecessor may not exist yet, so source
// text from that predecessor is the safe fallback. Never use the current
// chapter as "previous" context because that leaks future text into the prompt.
func previousChapterContext(store *storage.Store, novelSlug string, chNo int, chapters []model.ChapterMeta) string {
	prevNo := previousChapterNo(chapters, chNo)
	if prevNo <= 0 {
		return ""
	}
	prevCh, err := store.GetChapter(novelSlug, prevNo)
	if err != nil {
		return ""
	}
	if len(prevCh.TranslatedText) > 0 {
		n := min(3, len(prevCh.TranslatedText))
		tail := prevCh.TranslatedText[len(prevCh.TranslatedText)-n:]
		return fmt.Sprintf("ตอนก่อนหน้า (%s) จบด้วย:\n%s", prevCh.TranslatedTitle, strings.Join(tail, "\n"))
	}
	if len(prevCh.SourceText) > 0 {
		n := min(3, len(prevCh.SourceText))
		tail := prevCh.SourceText[len(prevCh.SourceText)-n:]
		return fmt.Sprintf("[โหมดขนาน] ท้ายบทต้นฉบับตอนก่อนหน้า (ตอนที่ %d):\n%s", prevNo, strings.Join(tail, "\n"))
	}
	return ""
}
