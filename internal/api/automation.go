package api

// Automatic intelligence: after a fresh import the AI discovers glossary
// terms on its own; after a translation job the AI refreshes story memory
// (summary, characters, facts). Results are merged into the stored files:
// identity fields stay anchored to curation, while progression fields
// (role, notes) refresh from newer chapters; glossary entries are add-only
// — manual entries and corrections always win. Announced over SSE.

import (
	"context"
	"fmt"
	"log"
	"time"

	"novelclaw/internal/model"
	"novelclaw/internal/translator"
)

const autoIntelTimeout = 3 * time.Minute

// AutoDiscoverGlossary scans the first readable source chapters of a novel
// with the entity-discovery prompt and merges new terms into the glossary.
// Fire-and-forget: an LLM failure must never fail the import that triggered it.
func (h *APIHandler) AutoDiscoverGlossary(slug string) {
	// ponytail: one goroutine per import, no work queue — add one when
	// parallel imports of several novels actually happen.
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("auto glossary discovery panic (%s): %v", slug, rec)
			}
		}()

		chapters, err := h.store.ListChapters(slug)
		if err != nil {
			log.Printf("auto glossary discovery: list chapters %s: %v", slug, err)
			return
		}
		var withSource []model.ChapterMeta
		for _, meta := range chapters {
			if meta.HasSource {
				withSource = append(withSource, meta)
			}
		}
		if len(withSource) == 0 {
			return
		}
		// Stratified sample: opening / middle / latest chapters, so names
		// introduced in later arcs are not invisible to discovery.
		picks := []int{0, len(withSource) / 2, len(withSource) - 1}
		seenPick := map[int]bool{}
		var sample []model.ChapterMeta
		for _, idx := range picks {
			if !seenPick[idx] {
				sample = append(sample, withSource[idx])
				seenPick[idx] = true
			}
		}
		var paragraphs []string
		for _, meta := range sample {
			ch, err := h.store.GetChapter(slug, meta.ChapterNo)
			if err != nil {
				continue // tolerate individual unreadable chapters
			}
			paragraphs = append(paragraphs, ch.SourceText...)
		}
		if len(paragraphs) == 0 {
			return
		}
		title := slug
		if novel, err := h.store.GetNovel(slug); err == nil && novel.Title != "" {
			title = novel.Title
		}
		provider, modelName, err := h.resolveIntelligenceProvider("", "")
		if err != nil {
			log.Printf("auto glossary discovery: provider: %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), autoIntelTimeout)
		defer cancel()
		discovered, err := h.translator.DiscoverGlossaryTerms(ctx, title, paragraphs, modelName)
		if err != nil {
			log.Printf("auto glossary discovery (%s via %s): %v", slug, provider.ID, err)
			return
		}
		added, err := h.mergeDiscoveredGlossary(slug, discovered)
		if err != nil {
			log.Printf("auto glossary discovery: merge %s: %v", slug, err)
			return
		}
		if added == 0 {
			return
		}
		if h.sse != nil {
			h.sse.Broadcast(map[string]interface{}{
				"type":      "auto_intelligence",
				"novelSlug": slug,
				"kind":      "glossary",
				"message":   fmt.Sprintf("AI สแกนหาชื่อเฉพาะอัตโนมัติ — เพิ่ม %d ศัพท์ใหม่เข้าคลังศัพท์แล้ว", added),
				"added":     added,
			})
		}
	}()
}

// mergeDiscoveredGlossary adds only genuinely new terms; existing entries in
// the stored glossary (manual entries and corrections) always win. Returns
// how many terms were added.
func (h *APIHandler) mergeDiscoveredGlossary(slug string, discovered []model.GlossaryItem) (int, error) {
	if len(discovered) == 0 {
		return 0, nil
	}
	filtered := make([]model.GlossaryItem, 0, len(discovered))
	for _, term := range discovered {
		if _, builtin := translator.BuiltinNovelGlossary[term.Term]; !builtin {
			filtered = append(filtered, term)
		}
	}
	return h.store.MergeGlossaryTerms(slug, filtered)
}

// AutoGenerateMemory summarizes the most recent translated chapters into the
// novel memory (story summary, characters, facts) and merges the candidate
// into what already exists. Fire-and-forget like glossary discovery.
func (h *APIHandler) AutoGenerateMemory(slug string) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("auto memory generation panic (%s): %v", slug, rec)
			}
		}()

		chapters, err := h.store.ListChapters(slug)
		if err != nil {
			log.Printf("auto memory generation: list chapters %s: %v", slug, err)
			return
		}
		existing, err := h.store.GetNovelMemory(slug)
		if err != nil {
			log.Printf("auto memory generation: memory %s: %v", slug, err)
			return
		}
		// Prefer chapters translated AFTER the stored memory was written, so
		// scattered (out-of-order) translation still feeds fresh continuity;
		// fall back to the last translated chapters when memory is newer.
		var selected []model.ChapterMeta
		for _, meta := range chapters {
			if !meta.HasTranslated {
				continue
			}
			if existing == nil || meta.UpdatedAt.After(existing.UpdatedAt) {
				selected = append(selected, meta)
			}
		}
		if len(selected) == 0 {
			for _, meta := range chapters {
				if meta.HasTranslated {
					selected = append(selected, meta)
				}
			}
		}
		if len(selected) == 0 {
			return
		}
		if len(selected) > 5 {
			selected = selected[len(selected)-5:]
		}
		glossary, err := h.store.GetGlossary(slug)
		if err != nil {
			log.Printf("auto memory generation: glossary %s: %v", slug, err)
			return
		}
		contextText, used, err := h.buildMemoryGenerationContext(slug, selected, glossary)
		if err != nil || used == 0 {
			log.Printf("auto memory generation: context %s (used=%d): %v", slug, used, err)
			return
		}
		provider, modelName, err := h.resolveIntelligenceProvider("", "")
		if err != nil {
			log.Printf("auto memory generation: provider: %v", err)
			return
		}
		systemPrompt, userPrompt := translator.BuildMemoryExtractionPrompts(existing, contextText)
		ctx, cancel := context.WithTimeout(context.Background(), autoIntelTimeout)
		defer cancel()
		raw, _, err := h.translator.CompleteWithFallbackForProvider(ctx, provider, systemPrompt, userPrompt, []string{modelName}, 0.15)
		if err != nil {
			log.Printf("auto memory generation (%s via %s): %v", slug, provider.ID, err)
			return
		}
		candidate, err := translator.ParseNovelMemoryCandidate(raw)
		if err != nil {
			log.Printf("auto memory generation: parse %s: %v", slug, err)
			return
		}
		candidate.NovelSlug = slug
		fresh := existing == nil || len(selected) > 0
		merged := translator.MergeNovelMemory(existing, candidate, fresh)
		merged.NovelSlug = slug
		saved, err := h.store.SaveGeneratedMemory(merged, existing.UpdatedAt)
		if err != nil {
			log.Printf("auto memory generation: save %s: %v", slug, err)
			return
		}
		if !saved {
			return
		}
		if h.sse != nil {
			h.sse.Broadcast(map[string]interface{}{
				"type":         "auto_intelligence",
				"novelSlug":    slug,
				"kind":         "memory",
				"message":      "AI สรุปเนื้อเรื่องและตัวละคร อัปเดต Story Memory อัตโนมัติแล้ว",
				"chaptersUsed": used,
			})
		}
	}()
}
