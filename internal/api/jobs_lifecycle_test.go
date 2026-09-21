package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"novelclaw/internal/config"
	"novelclaw/internal/model"
	"novelclaw/internal/storage"
	"os"
	"path/filepath"
	"testing"
)

func TestSparseTranslatedJobUsesRealChapterCount(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	store := storage.NewStore(cfg.DataDir)
	store.SaveNovel(&model.Novel{Slug: "book", Title: "Book"})
	for _, n := range []int{1, 1000000} {
		if err := store.SaveChapter("book", n, "source", "แปล", nil, []string{"ข้อความ"}); err != nil {
			t.Fatal(err)
		}
	}
	broker := NewSSEBroker()
	events := subscribeBroker(broker)
	h := NewAPIHandler(cfg, store, broker)
	h.runTranslationJob(context.Background(), "sparse", model.TranslateRequest{NovelSlug: "book", StartChapter: 1, EndChapter: 1000000, Model: "unused"})
	got := drainEvents(t, events)
	if len(got) != 3 {
		t.Fatalf("expected only two real chapters plus final status, got %d events", len(got))
	}
	if got[0]["totalChapters"] != float64(2) || got[2]["status"] != "completed" {
		t.Fatalf("incorrect sparse progress: %#v", got)
	}
}

func TestJobsSnapshotExcludesCancelledAndCompleted(t *testing.T) {
	h := NewAPIHandler(config.DefaultConfig(), storage.NewStore(t.TempDir()), NewSSEBroker())
	for _, id := range []string{"active", "cancelled", "completed"} {
		h.activeJobs[id] = &model.TranslationProgress{JobID: id, Status: "running", Percentage: 45}
		if id != "cancelled" {
			h.cancels[id] = func() {}
		}
	}
	h.activeJobs["completed"].Status = "completed"
	w := httptest.NewRecorder()
	h.ListJobs(w, httptest.NewRequest("GET", "/api/jobs", nil))
	var response struct {
		Jobs []model.TranslationProgress `json:"jobs"`
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	if len(response.Jobs) != 1 || response.Jobs[0].JobID != "active" || response.Jobs[0].Percentage != 45 {
		t.Fatalf("incorrect snapshot: %s", w.Body.String())
	}
}

func TestResumeRejectsJobIDThatDoesNotMatchFile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	h := NewAPIHandler(cfg, storage.NewStore(cfg.DataDir), NewSSEBroker())
	os.MkdirAll(h.jobsDir(), 0755)
	victim := filepath.Join(cfg.DataDir, "victim.json")
	os.WriteFile(victim, []byte("keep"), 0600)
	os.WriteFile(filepath.Join(h.jobsDir(), "safe.json"), []byte(`{"jobId":"../victim","request":{"novelSlug":"book","startChapter":1,"endChapter":1}}`), 0600)
	h.ResumeInterruptedJobs()
	if _, err := os.Stat(victim); err != nil {
		t.Fatal(err)
	}
	if len(h.activeJobs) != 0 {
		t.Fatal("invalid persisted job was started")
	}
}
