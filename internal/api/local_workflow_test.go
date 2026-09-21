package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"novelclaw/internal/config"
	"novelclaw/internal/model"
	"novelclaw/internal/storage"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIncompleteTranslationPreservesExistingChapter(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ตอนที่ 1\n\nคำแปลไม่ครบ"}}}})
	}))
	defer upstream.Close()
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.ConfigPath = filepath.Join(cfg.DataDir, "config.json")
	if err := cfg.ConfigureProvider("custom", upstream.URL, nil, "test-model", "", true); err != nil {
		t.Fatal(err)
	}
	store := storage.NewStore(cfg.DataDir)
	_, h := SetupRouter(cfg, store)
	if err := store.SaveChapter("preserve", 1, "Source", "Original translation", []string{"First paragraph.", "Second paragraph."}, []string{"เดิมหนึ่ง", "เดิมสอง"}); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetChapter("preserve", 1)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	chapters, _ := store.ListChapters("preserve")
	counters := &jobCounters{}
	h.translateOneChapter(context.Background(), "test", model.TranslateRequest{NovelSlug: "preserve", StartChapter: 1, EndChapter: 1, Force: true}, cfg.ActiveProvider(), 1, 1, 1, &model.NovelGlossary{}, "", "", "", []string{"test-model"}, chapters, counters)
	after, err := store.GetChapter("preserve", 1)
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, _ := json.Marshal(after)
	if calls.Load() != 3 || counters.written.Load() != 0 || counters.takeError() == "" || string(beforeJSON) != string(afterJSON) {
		t.Fatalf("incomplete translation changed chapter or did not fail after retries: calls=%d written=%d", calls.Load(), counters.written.Load())
	}
}

func TestLocalImportTranslateReadExportWorkflow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ตอนที่ 1 เริ่มต้น\n\nเขาเปิดหนังสือ\n\nเธอนั่งอ่านอยู่ข้างหน้าต่าง"}}}})
	}))
	defer upstream.Close()
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.ConfigPath = filepath.Join(cfg.DataDir, "config.json")
	if err := cfg.ConfigureProvider("custom", upstream.URL, nil, "test-model", "", true); err != nil {
		t.Fatal(err)
	}
	store := storage.NewStore(cfg.DataDir)
	router, h := SetupRouter(cfg, store)
	imported := postImport(t, router, map[string]any{"novelSlug": "workflow", "novelTitle": "Book", "startChapter": 1, "rawContent": "He opened a book.\nShe read by the window."})
	if imported.Code != 200 {
		t.Fatalf("import: %s", imported.Body.String())
	}
	chapters, _ := store.ListChapters("workflow")
	counters := &jobCounters{}
	h.translateOneChapter(context.Background(), "test", model.TranslateRequest{NovelSlug: "workflow", StartChapter: 1, EndChapter: 1}, cfg.ActiveProvider(), 1, 1, 1, &model.NovelGlossary{}, "", "", "", []string{"test-model"}, chapters, counters)
	if counters.written.Load() != 1 {
		t.Fatalf("translation failed: %s", counters.takeError())
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/novels/workflow/chapters/1", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "เขาเปิดหนังสือ") {
		t.Fatalf("read: %s", w.Body.String())
	}
	for _, format := range []string{"txt", "markdown", "epub"} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/novels/workflow/export?format="+format, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("export %s: %d %s", format, w.Code, w.Body.String())
		}
	}
}
