package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"novelclaw/internal/model"
	"novelclaw/internal/scraper"
	"novelclaw/internal/storage"
)

// Import handles importing from URL or raw pasted text
func (h *APIHandler) Import(w http.ResponseWriter, r *http.Request) {
	var req model.ImportRequest
	if err := decodeJSONBody(w, r, &req, bodyImport); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	// Case 1: Manual Paste
	if req.RawContent != "" {
		if req.NovelSlug == "" {
			WriteError(w, http.StatusBadRequest, "novelSlug is required for raw paste")
			return
		}
		lines := strings.Split(req.RawContent, "\n")
		paragraphs := make([]string, 0, len(lines))
		for _, line := range lines {
			if text := strings.TrimSpace(line); text != "" {
				paragraphs = append(paragraphs, text)
			}
		}
		if len(paragraphs) == 0 {
			WriteError(w, http.StatusBadRequest, "rawContent has no readable paragraphs")
			return
		}

		// Ensure the novel exists. Only create metadata for a true not-found;
		// corrupt/unreadable metadata must surface as an error instead of being overwritten.
		if _, err := h.store.GetNovel(req.NovelSlug); err != nil {
			if !errors.Is(err, storage.ErrNovelNotFound) {
				WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			novelTitle := strings.TrimSpace(req.NovelTitle)
			if novelTitle == "" { // backward compatibility with older clients
				novelTitle = strings.TrimSpace(req.Title)
			}
			if novelTitle == "" {
				novelTitle = req.NovelSlug
			}
			if err := h.store.SaveNovel(&model.Novel{
				Slug: req.NovelSlug, Title: novelTitle, Genre: req.Genre,
				SourceLang: "cn", TargetLang: "th", UpdatedAt: time.Now(),
			}); err != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("save novel metadata: %v", err))
				return
			}
		}

		chNum := req.StartChapter
		if chNum <= 0 {
			existingChapters, err := h.store.ListChapters(req.NovelSlug)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list chapters: %v", err))
				return
			}
			if len(existingChapters) > 0 {
				chNum = existingChapters[len(existingChapters)-1].ChapterNo + 1
			} else {
				chNum = 1
			}
		}

		chTitle := req.Title
		if chTitle == "" {
			chTitle = fmt.Sprintf("ตอนที่ %d", chNum)
		}

		err := h.store.SaveChapter(req.NovelSlug, chNum, chTitle, "", paragraphs, nil)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"chapterNo": chNum,
			"message":   fmt.Sprintf("บันทึกตอนที่ %d เรียบร้อยแล้ว", chNum),
		})
		return
	}

	// Case 2: URL Import
	if req.URL == "" {
		WriteError(w, http.StatusBadRequest, "URL or rawContent is required")
		return
	}

	jobID := fmt.Sprintf("import_%d", time.Now().UnixNano())
	jobCtx, cancel := context.WithCancel(context.Background())

	h.jobsMu.Lock()
	h.cancels[jobID] = cancel
	h.activeJobs[jobID] = &model.TranslationProgress{
		JobID:     jobID,
		NovelSlug: req.NovelSlug,
		Status:    "running",
		Message:   "กำลังนำเข้านิยายจาก URL...",
	}
	h.jobsMu.Unlock()

	go func() {
		defer func() {
			h.jobsMu.Lock()
			delete(h.cancels, jobID)
			delete(h.activeJobs, jobID)
			h.jobsMu.Unlock()
		}()
		h.runImportJob(jobCtx, jobID, req)
	}()

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":  "import_started",
		"jobId":   jobID,
		"message": "Import job started in background",
	})
}

// runImportJob performs the URL import in the background. It reports
// processed/imported/failed counts relative to the selected range and never
// reports a full success when persistence or chapter fetching failed.
func (h *APIHandler) runImportJob(ctx context.Context, jobID string, req model.ImportRequest) {
	// Prefer a real TOC when one is detectable. Generic scrapers already require
	// multiple chapter links, so this avoids misclassifying a novel homepage as
	// one giant chapter while still falling back cleanly for direct chapter URLs.
	toc, tocErr := h.scraper.FetchTOCContext(ctx, req.URL)
	if tocErr == nil && toc == nil {
		tocErr = fmt.Errorf("scraper returned an empty TOC result")
	}
	if ctx.Err() != nil {
		h.broadcastImport(jobID, "import_cancelled", req.NovelSlug, map[string]interface{}{
			"current": 0, "total": 0, "imported": 0, "failed": 0, "percentage": 0,
		})
		return
	}
	if tocErr == nil && len(toc.Chapters) > 0 {
		h.importTOC(ctx, jobID, req, toc)
		return
	}

	chapter, chapterErr := h.scraper.FetchChapterContext(ctx, req.URL)
	if chapterErr == nil && chapter == nil {
		chapterErr = fmt.Errorf("scraper returned an empty chapter result")
	}
	if ctx.Err() != nil {
		h.broadcastImport(jobID, "import_cancelled", req.NovelSlug, map[string]interface{}{
			"current": 0, "total": 0, "imported": 0, "failed": 0, "percentage": 0,
		})
		return
	}
	if chapterErr == nil && len(chapter.Paragraphs) > 0 {
		h.importSingleChapter(jobID, req, chapter)
		return
	}

	msg := "นำเข้าล้มเหลว: URL นี้อ่านไม่ได้ ทั้งรายตอนและสารบัญ"
	if tocErr != nil {
		msg = fmt.Sprintf("%s: %v", msg, tocErr)
	} else if chapterErr != nil {
		msg = fmt.Sprintf("%s: %v", msg, chapterErr)
	}
	h.broadcastImport(jobID, "import_error", req.NovelSlug, map[string]interface{}{"message": msg})
}

func (h *APIHandler) importTOC(ctx context.Context, jobID string, req model.ImportRequest, toc *scraper.ScrapedNovelInfo) {
	slug := req.NovelSlug
	if slug == "" {
		slug = sanitizeSlug(toc.Title)
	}
	catalog := make([]model.ChapterMeta, 0, len(toc.Chapters))
	for i, item := range toc.Chapters {
		chapterNo := item.ChapterNo
		if chapterNo <= 0 {
			chapterNo = i + 1
		}
		catalog = append(catalog, model.ChapterMeta{ChapterNo: chapterNo, TitleSource: item.Title, Locked: item.Locked, SourceURL: item.URL})
	}
	if err := h.store.SaveChapterCatalog(slug, catalog); err != nil {
		h.broadcastImport(jobID, "import_error", slug, map[string]interface{}{"message": fmt.Sprintf("บันทึกสารบัญไม่สำเร็จ: %v", err)})
		return
	}
	start, end, total, err := normalizeImportRange(req.StartChapter, req.EndChapter, len(toc.Chapters))
	if err != nil {
		h.broadcastImport(jobID, "import_error", slug, map[string]interface{}{"message": err.Error()})
		return
	}
	if ctx.Err() != nil {
		h.broadcastImport(jobID, "import_cancelled", slug, map[string]interface{}{
			"current": 0, "total": total, "imported": 0, "failed": 0, "percentage": 0,
		})
		return
	}
	if err := h.saveImportedNovel(slug, toc.Title, req.NovelTitle, toc.Author, req.Genre, toc.Description, toc.CoverURL, req.URL); err != nil {
		h.broadcastImport(jobID, "import_error", slug, map[string]interface{}{
			"message": fmt.Sprintf("บันทึกข้อมูลเรื่องไม่สำเร็จ: %v", err),
		})
		return
	}

	processed, imported, failed, locked := 0, 0, 0, 0
	lastError := ""
	for i := start - 1; i < end; i++ {
		select {
		case <-ctx.Done():
			h.broadcastImport(jobID, "import_cancelled", slug, map[string]interface{}{
				"current": processed, "total": total,
				"imported": imported, "failed": failed,
				"percentage": importPercentage(processed, total),
			})
			return
		default:
		}
		item := toc.Chapters[i]
		chapterNo := item.ChapterNo
		if chapterNo <= 0 {
			chapterNo = i + 1
		}
		if item.Locked {
			locked++
			processed++
			h.broadcastImport(jobID, "import_progress", slug, map[string]interface{}{
				"current": processed, "total": total, "imported": imported, "failed": failed, "locked": locked,
				"percentage": importPercentage(processed, total), "title": item.Title,
			})
			continue
		}
		ch, fetchErr := h.scraper.FetchChapterContext(ctx, item.URL)
		if fetchErr == nil && ch == nil {
			fetchErr = fmt.Errorf("scraper returned an empty chapter result")
		}
		if ctx.Err() != nil {
			h.broadcastImport(jobID, "import_cancelled", slug, map[string]interface{}{
				"current": processed, "total": total,
				"imported": imported, "failed": failed,
				"percentage": importPercentage(processed, total),
			})
			return
		}
		if fetchErr != nil || len(ch.Paragraphs) == 0 {
			failed++
			if fetchErr != nil {
				lastError = fetchErr.Error()
			} else {
				lastError = "ไม่มีเนื้อหาบท"
			}
		} else if saveErr := h.store.SaveChapter(slug, chapterNo, ch.Title, "", ch.Paragraphs, nil); saveErr != nil {
			failed++
			lastError = saveErr.Error()
			log.Printf("Import save chapter %d failed: %v\n", chapterNo, saveErr)
		} else {
			imported++
		}
		processed++

		h.broadcastImport(jobID, "import_progress", slug, map[string]interface{}{
			"current": processed, "total": total,
			"imported": imported, "failed": failed,
			"percentage": importPercentage(processed, total),
			"title":      item.Title,
		})

		if i+1 < end && h.importDelay > 0 {
			timer := time.NewTimer(h.importDelay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				h.broadcastImport(jobID, "import_cancelled", slug, map[string]interface{}{
					"current": processed, "total": total,
					"imported": imported, "failed": failed,
					"percentage": importPercentage(processed, total),
				})
				return
			case <-timer.C:
			}
		}
	}

	fields := map[string]interface{}{
		"current": total, "total": total,
		"imported": imported, "failed": failed, "locked": locked,
		"percentage": 100,
	}
	switch {
	case imported == 0:
		fields["message"] = fmt.Sprintf("นำเข้าไม่สำเร็จ (%d/%d ตอน): %s", failed, total, lastError)
		h.broadcastImport(jobID, "import_error", slug, fields)
	case failed > 0:
		fields["message"] = fmt.Sprintf("นำเข้าเสร็จบางส่วน: ดึงต้นฉบับสำเร็จ %d ตอน, ล็อก %d ตอน, ล้มเหลว %d ตอน", imported, locked, failed)
		h.broadcastImport(jobID, "import_partial", slug, fields)
	default:
		fields["message"] = fmt.Sprintf("สารบัญครบ %d ตอน • ดึงต้นฉบับสาธารณะสำเร็จ %d ตอน • ล็อก VIP %d ตอน", total, imported, locked)
		h.broadcastImport(jobID, "import_done", slug, fields)
	}
	// Fresh source text landed — let the AI refresh the glossary on its own.
	h.AutoDiscoverGlossary(slug)
}

func (h *APIHandler) importSingleChapter(jobID string, req model.ImportRequest, chapter *scraper.ScrapedChapter) {
	slug := req.NovelSlug
	if slug == "" {
		if req.NovelTitle != "" {
			slug = sanitizeSlug(req.NovelTitle)
		} else {
			slug = fmt.Sprintf("imported-%d", time.Now().UnixNano())
		}
	}
	title := strings.TrimSpace(req.NovelTitle)
	if title == "" {
		title = chapter.Title
	}
	if err := h.ensureImportedNovel(slug, title, req.Genre); err != nil {
		h.broadcastImport(jobID, "import_error", slug, map[string]interface{}{
			"message": fmt.Sprintf("บันทึกข้อมูลเรื่องไม่สำเร็จ: %v", err),
		})
		return
	}
	chapterNo := chapter.ChapterNo
	if chapterNo <= 0 {
		chapters, err := h.store.ListChapters(slug)
		if err != nil {
			h.broadcastImport(jobID, "import_error", slug, map[string]interface{}{
				"message": fmt.Sprintf("อ่านสารบัญเดิมไม่สำเร็จ: %v", err),
			})
			return
		}
		chapterNo = 1
		if len(chapters) > 0 {
			chapterNo = chapters[len(chapters)-1].ChapterNo + 1
		}
	}
	if err := h.store.SaveChapter(slug, chapterNo, chapter.Title, "", chapter.Paragraphs, nil); err != nil {
		h.broadcastImport(jobID, "import_error", slug, map[string]interface{}{
			"message": fmt.Sprintf("บันทึกตอนที่ %d ไม่สำเร็จ: %v", chapterNo, err),
		})
		return
	}
	h.broadcastImport(jobID, "import_done", slug, map[string]interface{}{
		"chapterNo": chapterNo,
		"current":   1, "total": 1,
		"imported": 1, "failed": 0,
		"percentage": 100,
	})
	h.AutoDiscoverGlossary(slug)
}
func (h *APIHandler) ensureImportedNovel(slug, title, genre string) error {
	_, err := h.store.GetNovel(slug)
	if err == nil {
		return nil
	}
	if !errors.Is(err, storage.ErrNovelNotFound) {
		return err
	}
	if strings.TrimSpace(title) == "" {
		title = slug
	}
	return h.store.SaveNovel(&model.Novel{
		Slug: slug, Title: title, Genre: genre,
		SourceLang: "cn", TargetLang: "th", UpdatedAt: time.Now(),
	})
}

func (h *APIHandler) saveImportedNovel(slug, title, translatedTitle, author, genre, description, coverURL, sourceURL string) error {
	novel, err := h.store.GetNovel(slug)
	if err != nil && !errors.Is(err, storage.ErrNovelNotFound) {
		return err
	}
	if novel == nil {
		novel = &model.Novel{Slug: slug, SourceLang: "cn", TargetLang: "th"}
	}
	if title != "" {
		novel.Title = title
	}
	if translatedTitle != "" && translatedTitle != title {
		novel.TranslatedTitle = translatedTitle
	}
	if author != "" {
		novel.Author = author
	}
	if genre != "" {
		novel.Genre = genre
	}
	if description != "" {
		novel.Description = description
	}
	if coverURL != "" {
		novel.CoverURL = coverURL
	}
	if sourceURL != "" {
		if novel.SourceURLs == nil {
			novel.SourceURLs = make(map[string]string)
		}
		key := "web"
		lowerURL := strings.ToLower(sourceURL)
		if strings.Contains(lowerURL, "qidian.com") {
			key = "qidian"
		}
		if strings.Contains(lowerURL, "69shu") {
			key = "69shu"
		}
		novel.SourceURLs[key] = sourceURL
	}
	return h.store.SaveNovel(novel)
}

func normalizeImportRange(start, end, available int) (int, int, int, error) {
	if available <= 0 {
		return 0, 0, 0, fmt.Errorf("ไม่มีตอนให้ดาวน์โหลด")
	}
	if start <= 0 {
		start = 1
	}
	if start > available {
		return 0, 0, 0, fmt.Errorf("ตอนเริ่มต้น %d เกินจำนวนตอนทั้งหมด %d", start, available)
	}
	if end <= 0 || end > available {
		end = available
	}
	if end < start {
		return 0, 0, 0, fmt.Errorf("ตอนสิ้นสุดต้องไม่น้อยกว่าตอนเริ่มต้น")
	}
	return start, end, end - start + 1, nil
}

func importPercentage(processed, total int) int {
	if total <= 0 {
		return 0
	}
	pct := processed * 100 / total
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}
func (h *APIHandler) broadcastImport(jobID, eventType, slug string, fields map[string]interface{}) {
	h.jobsMu.Lock()
	if progress := h.activeJobs[jobID]; progress != nil {
		progress.NovelSlug = slug
		if percentage, ok := fields["percentage"].(int); ok {
			progress.Percentage = percentage
		}
		if total, ok := fields["total"].(int); ok {
			progress.TotalChapters = total
		}
		if current, ok := fields["current"].(int); ok {
			progress.CurrentChapter = current
		}
		if message, ok := fields["message"].(string); ok {
			progress.Message = message
		}
		if eventType != "import_progress" {
			progress.Status = eventType
		}
	}
	h.jobsMu.Unlock()
	event := map[string]interface{}{
		"type":      eventType,
		"jobId":     jobID,
		"novelSlug": slug,
	}
	for key, value := range fields {
		event[key] = value
	}
	h.sse.Broadcast(event)
}
