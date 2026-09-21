package api

import (
	"net/http"
	"novelclaw/internal/model"
	"sort"
)

// ListJobs restores progress after a reload or SSE reconnect without polling.
func (h *APIHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	h.jobsMu.Lock()
	jobs := make([]model.TranslationProgress, 0, len(h.activeJobs))
	for id, progress := range h.activeJobs {
		if _, running := h.cancels[id]; running && progress.Status == "running" {
			jobs = append(jobs, *progress)
		}
	}
	h.jobsMu.Unlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].JobID < jobs[j].JobID })
	WriteJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *APIHandler) broadcastProgress(progress model.TranslationProgress) {
	h.jobsMu.Lock()
	if existing, exists := h.activeJobs[progress.JobID]; exists {
		copy := progress
		if progress.Status == "error" && progress.Percentage < 100 {
			copy.Status = "running"
			copy.Percentage = existing.Percentage
			copy.TotalChapters = existing.TotalChapters
		}
		h.activeJobs[progress.JobID] = &copy
	}
	h.jobsMu.Unlock()
	h.sse.Broadcast(progress)
}
