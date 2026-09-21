package api

import (
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"novelclaw/internal/config"
	"novelclaw/internal/storage"
	"novelclaw/internal/web"
)

// SetupRouter creates the HTTP handler with all API endpoints and embedded static files.
// It also returns the APIHandler so main can resume persisted jobs with the
// same SSE broker the routes use.
func SetupRouter(cfg *config.AppConfig, store *storage.Store) (http.Handler, *APIHandler) {
	sse := NewSSEBroker()
	h := NewAPIHandler(cfg, store, sse)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]any{
			"app":    "NovelClaw",
			"status": "ok",
		})
	})

	// SSE Events
	mux.Handle("GET /events", sse)

	// Novel Endpoints
	mux.HandleFunc("GET /api/novels", h.ListNovels)
	mux.HandleFunc("POST /api/novels", h.SaveNovel)
	mux.HandleFunc("GET /api/novels/{slug}", h.GetNovel)
	mux.HandleFunc("GET /api/novels/{slug}/cover", h.GetNovelCover)
	mux.HandleFunc("POST /api/novels/{slug}/cover", h.UploadNovelCover)
	mux.HandleFunc("GET /api/novels/{slug}/chapters", h.ListChapters)
	mux.HandleFunc("GET /api/novels/{slug}/chapters/{num}", h.GetChapter)
	mux.HandleFunc("POST /api/novels/{slug}/chapters/{num}/repair", h.RepairChapter)
	mux.HandleFunc("GET /api/novels/{slug}/glossary", h.GetGlossary)
	mux.HandleFunc("POST /api/novels/{slug}/glossary", h.SaveGlossary)
	mux.HandleFunc("GET /api/novels/{slug}/glossary/check", h.GlossaryCheck)
	mux.HandleFunc("GET /api/novels/{slug}/memory", h.GetMemory)
	mux.HandleFunc("POST /api/novels/{slug}/memory", h.SaveMemory)
	mux.HandleFunc("POST /api/novels/{slug}/memory/generate", h.GenerateMemoryCandidate)
	mux.HandleFunc("GET /api/novels/{slug}/qa", h.ListQualityReports)
	mux.HandleFunc("GET /api/novels/{slug}/qa/{num}", h.GetQualityReport)
	mux.HandleFunc("POST /api/novels/{slug}/qa/{num}/repair", h.RepairQualityWithAI)
	mux.HandleFunc("POST /api/novels/{slug}/qa/rebuild", h.RebuildQualityReports)
	mux.HandleFunc("GET /api/novels/{slug}/bookmark", h.GetBookmark)
	mux.HandleFunc("POST /api/novels/{slug}/bookmark", h.SaveBookmark)
	mux.HandleFunc("GET /api/novels/{slug}/export", h.ExportNovel)

	// Backup
	mux.HandleFunc("POST /api/backup", h.BackupNow)
	mux.HandleFunc("GET /api/backup", h.ListBackupsHandler)

	// Actions
	mux.HandleFunc("POST /api/import", h.Import)
	mux.HandleFunc("POST /api/translate", h.Translate)
	mux.HandleFunc("GET /api/jobs", h.ListJobs)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", h.CancelJob)
	mux.HandleFunc("POST /api/novels/{slug}/glossary/discover", h.DiscoverGlossary)
	mux.HandleFunc("POST /api/audio/speech", h.GenerateSpeech)

	// System Config, Providers & Models
	mux.HandleFunc("GET /api/models", h.ListModels)
	mux.HandleFunc("GET /api/providers", h.ListProviders)
	mux.HandleFunc("POST /api/providers/test", h.TestProvider)
	mux.HandleFunc("GET /api/config", h.GetConfig)
	mux.HandleFunc("GET /api/detect-providers", h.DetectProviders)
	mux.HandleFunc("POST /api/config", h.UpdateConfig)

	// Embedded Static Assets
	staticFS, err := fs.Sub(web.StaticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to load embedded static filesystem: %v", err)
	}

	fileServer := http.FileServer(http.FS(staticFS))
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If path doesn't have an extension or is root, serve index.html for SPA routing
		path := r.URL.Path
		if path == "/api" || strings.HasPrefix(path, "/api/") {
			WriteError(w, http.StatusNotFound, "API endpoint not found")
			return
		}
		if path == "/" || !strings.Contains(path, ".") {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}))

	// Same-origin security middleware. NovelClaw serves its own UI, so wildcard
	// CORS is unnecessary and would let an unrelated website drive mutating LAN
	// APIs from the user's browser. Direct LAN navigation still works normally.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")

		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
				WriteError(w, http.StatusForbidden, "cross-origin requests are not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return handler, h
}

// GetLocalIPs returns active non-loopback IPv4 addresses
func GetLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}

// PrintBanner prints server connection links
func PrintBanner(port int) {
	fmt.Println("==================================================")
	fmt.Println("       🐾 NOVELCLAW — High-Performance Reader      ")
	fmt.Println("==================================================")
	fmt.Printf(" [Local]: http://localhost:%d\n", port)
	for _, ip := range GetLocalIPs() {
		fmt.Printf(" [LAN]:   http://%s:%d\n", ip, port)
	}
	fmt.Println("==================================================")
}
