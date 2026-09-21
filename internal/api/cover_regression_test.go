package api

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCoverReplacementAndUploadLimit(t *testing.T) {
	cfg, _, router, cleanup := setupTestEnv(t)
	defer cleanup()
	old := filepath.Join(cfg.DataDir, "test-novel", "cover.webp")
	if err := os.WriteFile(old, []byte("previous cover"), 0600); err != nil {
		t.Fatal(err)
	}
	jpeg := append([]byte{0xff, 0xd8, 0xff}, bytes.Repeat([]byte{0}, 20)...)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/novels/test-novel/cover", bytes.NewReader(jpeg)))
	if w.Code != 204 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old format still shadows uploaded cover")
	}
	tooLarge := append([]byte{0xff, 0xd8, 0xff}, make([]byte, maxCoverUpload)...)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/novels/test-novel/cover", bytes.NewReader(tooLarge)))
	if w.Code != 413 {
		t.Fatalf("oversized upload accepted: %d", w.Code)
	}
	saved, err := os.ReadFile(filepath.Join(cfg.DataDir, "test-novel", "cover.jpg"))
	if err != nil || !bytes.Equal(saved, jpeg) {
		t.Fatal("oversized upload changed existing cover")
	}
}
