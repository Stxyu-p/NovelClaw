package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"novelclaw/internal/model"
)

// jobsDir returns the directory where pending job specs survive restarts.
func (h *APIHandler) jobsDir() string {
	return filepath.Join(h.cfg.DataDir, ".jobs")
}

// persistJob writes the job spec so an interrupted queue can be resumed
// after a restart. A job is not accepted by the API unless this durable write
// succeeds, otherwise the UI would promise restart safety that does not exist.
func (h *APIHandler) persistJob(jobID string, req model.TranslateRequest) error {
	if err := os.MkdirAll(h.jobsDir(), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(map[string]interface{}{"jobId": jobID, "request": req})
	if err != nil {
		return err
	}
	path := filepath.Join(h.jobsDir(), jobID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (h *APIHandler) removeJobFile(jobID string) {
	if err := os.Remove(filepath.Join(h.jobsDir(), jobID+".json")); err != nil && !os.IsNotExist(err) {
		log.Printf("remove persisted job %s: %v", jobID, err)
	}
}

func quarantineJobFile(path string, reason error) {
	dest := fmt.Sprintf("%s.invalid-%d", path, time.Now().UnixNano())
	if err := os.Rename(path, dest); err != nil {
		log.Printf("quarantine invalid job %s failed: %v (original error: %v)", path, err, reason)
		return
	}
	log.Printf("quarantined invalid job %s: %v", dest, reason)
}

// ResumeInterruptedJobs restarts translation jobs found on disk at startup.
// Already-translated chapters are skipped by the normal job logic, so a
// resumed batch continues where it died.
func (h *APIHandler) ResumeInterruptedJobs() {
	entries, err := os.ReadDir(h.jobsDir())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("read persisted jobs: %v", err)
		}
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(h.jobsDir(), e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("read persisted job %s: %v", path, err)
			continue
		}
		var spec struct {
			JobID   string                 `json:"jobId"`
			Request model.TranslateRequest `json:"request"`
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			quarantineJobFile(path, fmt.Errorf("invalid JSON: %w", err))
			continue
		}
		if spec.JobID == "" || spec.JobID != strings.TrimSuffix(e.Name(), ".json") || safeSlug(spec.JobID) != spec.JobID || spec.Request.NovelSlug == "" || spec.Request.StartChapter <= 0 || spec.Request.EndChapter < spec.Request.StartChapter {
			quarantineJobFile(path, fmt.Errorf("invalid persisted job fields"))
			continue
		}

		log.Printf("Resuming interrupted job %s (%s ch%d-%d)\n",
			spec.JobID, spec.Request.NovelSlug, spec.Request.StartChapter, spec.Request.EndChapter)

		jobCtx, cancel := context.WithCancel(context.Background())
		h.jobsMu.Lock()
		h.cancels[spec.JobID] = cancel
		h.activeJobs[spec.JobID] = &model.TranslationProgress{
			JobID:          spec.JobID,
			NovelSlug:      spec.Request.NovelSlug,
			TotalChapters:  spec.Request.EndChapter - spec.Request.StartChapter + 1,
			CurrentChapter: spec.Request.StartChapter,
			Status:         "running",
			Percentage:     0,
		}
		h.jobsMu.Unlock()

		go h.runTranslationJob(jobCtx, spec.JobID, spec.Request)
	}
}
