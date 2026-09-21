package api

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"novelclaw/internal/storage"
	"os"
	"strings"
	"time"
)

type exportStream func(func(exportChapter) error) error

var errNoExportChapters = errors.New("No translated chapters found in the specified range")

// ExportNovel loads one chapter at a time. Temporary output keeps failures
// atomic for the download without retaining an entire novel in RAM.
func (h *APIHandler) ExportNovel(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "txt"
	}
	if format != "txt" && format != "md" && format != "markdown" && format != "epub" {
		WriteError(w, http.StatusBadRequest, "Unsupported export format. Use txt, markdown, or epub")
		return
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
	chapters, err := h.store.ListChapters(slug)
	if err != nil {
		WriteError(w, 500, err.Error())
		return
	}
	if len(chapters) == 0 {
		WriteError(w, 404, "No chapters found to export")
		return
	}
	maxNo := chapters[len(chapters)-1].ChapterNo
	start, err := positiveQueryInt(r, "start", 1)
	if err != nil {
		WriteError(w, 400, err.Error())
		return
	}
	end, err := positiveQueryInt(r, "end", maxNo)
	if err != nil {
		WriteError(w, 400, err.Error())
		return
	}
	if start > end || end > maxNo {
		WriteError(w, 400, "Invalid export chapter range")
		return
	}
	stream := exportStream(func(yield func(exportChapter) error) error {
		count := 0
		for _, meta := range chapters {
			if err := r.Context().Err(); err != nil {
				return err
			}
			if meta.ChapterNo < start || meta.ChapterNo > end || !meta.HasTranslated {
				continue
			}
			content, err := h.store.GetChapter(slug, meta.ChapterNo)
			if errors.Is(err, storage.ErrChapterNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if len(content.TranslatedText) == 0 {
				continue
			}
			title := content.TranslatedTitle
			if title == "" {
				title = fmt.Sprintf("ตอนที่ %d", meta.ChapterNo)
			}
			if err := yield(exportChapter{ChapterNo: meta.ChapterNo, Title: title, Paragraphs: content.TranslatedText}); err != nil {
				return err
			}
			count++
		}
		if count == 0 {
			return errNoExportChapters
		}
		return nil
	})
	title := novel.TranslatedTitle
	if title == "" {
		title = novel.Title
	}
	if title == "" {
		title = slug
	}
	filename := sanitizeSlug(title)
	if filename == "" {
		filename = "novel"
	}
	filename = fmt.Sprintf("%s_ch%d-%d", filename, start, end)
	if format == "epub" {
		err = serveEPUBStream(w, r, filename+".epub", slug, title, novel.Author, stream)
	} else {
		err = serveTextExport(w, r, filename, title, novel.Author, format, start, end, stream)
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNoExportChapters) {
			status = http.StatusBadRequest
		}
		WriteError(w, status, err.Error())
	}
}

func serveTextExport(w http.ResponseWriter, r *http.Request, filename, title, author, format string, start, end int, stream exportStream) error {
	tmp, err := os.CreateTemp("", "novelclaw-export-*.tmp")
	if err != nil {
		return err
	}
	defer func() { tmp.Close(); os.Remove(tmp.Name()) }()
	body := bufio.NewWriterSize(tmp, 32*1024)
	count := 0
	err = stream(func(ch exportChapter) error {
		if format == "txt" {
			fmt.Fprintf(body, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n %s\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n", ch.Title)
			for _, p := range ch.Paragraphs {
				fmt.Fprintf(body, "  %s\n\n", p)
			}
			fmt.Fprint(body, "\n\n")
		} else {
			fmt.Fprintf(body, "## %s\n\n", ch.Title)
			for _, p := range ch.Paragraphs {
				fmt.Fprintf(body, "%s\n\n", p)
			}
			fmt.Fprint(body, "\n---\n\n")
		}
		count++
		return body.Flush()
	})
	if err != nil {
		return err
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	info, err := tmp.Stat()
	if err != nil {
		return err
	}
	var header strings.Builder
	if format == "txt" {
		header.WriteString("\xef\xbb\xbf====================================================\n")
		fmt.Fprintf(&header, " ชื่อเรื่อง: %s\n", title)
		if author != "" {
			fmt.Fprintf(&header, " ผู้แต่ง: %s\n", author)
		}
		fmt.Fprintf(&header, " ตอนที่: %d - %d (รวม %d ตอน)\n ส่งออกเมื่อ: %s\n แปลและจัดทำโดย: NovelClaw AI\n====================================================\n\n\n", start, end, count, time.Now().Format("02/01/2006 15:04:05"))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		filename += ".txt"
	} else {
		fmt.Fprintf(&header, "# %s\n\n", title)
		if author != "" {
			fmt.Fprintf(&header, "**ผู้แต่ง**: %s  \n", author)
		}
		fmt.Fprintf(&header, "**จำนวนตอน**: %d - %d  \n**วันที่ส่งออก**: %s  \n\n---\n\n", start, end, time.Now().Format("02/01/2006 15:04"))
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		filename += ".md"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprint(int64(header.Len())+info.Size()))
	if _, err := io.Copy(w, io.MultiReader(strings.NewReader(header.String()), tmp)); err != nil {
		// Headers have been committed; never append JSON to a partial download.
		log.Printf("export download interrupted: %v", err)
	}
	return nil
}
