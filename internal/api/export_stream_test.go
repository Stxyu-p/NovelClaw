package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"novelclaw/internal/config"
	"novelclaw/internal/model"
	"novelclaw/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type discardExport struct {
	header http.Header
	status int
}

func (w *discardExport) Header() http.Header         { return w.header }
func (w *discardExport) WriteHeader(status int)      { w.status = status }
func (w *discardExport) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkExportBook(b *testing.B) {
	dir := b.TempDir()
	store := storage.NewStore(dir)
	if err := store.SaveNovel(&model.Novel{Slug: "book", Title: "Benchmark"}); err != nil {
		b.Fatal(err)
	}
	chapterDir := filepath.Join(dir, "book", "chapters")
	os.MkdirAll(chapterDir, 0755)
	paragraphs := make([]string, 100)
	for i := range paragraphs {
		paragraphs[i] = strings.Repeat("Reading a local novel should use bounded memory. ", 10)
	}
	payload, _ := json.Marshal(map[string]any{"title": "Chapter", "paragraphs": paragraphs})
	for i := 1; i <= 100; i++ {
		if err := os.WriteFile(filepath.Join(chapterDir, fmt.Sprintf("%04d.th.json", i)), payload, 0600); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := store.ListChapters("book"); err != nil {
		b.Fatal(err)
	}
	h := NewAPIHandler(config.DefaultConfig(), store, NewSSEBroker())
	b.ReportAllocs()
	for b.Loop() {
		r := httptest.NewRequest("GET", "/api/novels/book/export?format=txt", nil)
		r.SetPathValue("slug", "book")
		w := &discardExport{header: make(http.Header)}
		h.ExportNovel(w, r)
		if w.status >= 400 {
			b.Fatalf("export status %d", w.status)
		}
	}
}

func TestExportFormatsPreserveContent(t *testing.T) {
	_, _, router, cleanup := setupTestEnv(t)
	defer cleanup()
	for _, format := range []string{"txt", "markdown", "epub"} {
		t.Run(format, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/api/novels/test-novel/export?format="+format, nil))
			if w.Code != 200 {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if format != "epub" && !strings.Contains(w.Body.String(), "ย่อหน้าที่ 1") {
				t.Fatal("missing chapter text")
			}
			if format == "txt" && !strings.HasPrefix(w.Body.String(), "\xef\xbb\xbf") {
				t.Fatal("missing UTF8 BOM")
			}
		})
	}
}
