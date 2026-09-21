package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// maxCoverUpload bounds one cover upload; a 4 MB ceiling is generous for any
// legitimate web cover while rejecting accidental multi-MB originals.
const maxCoverUpload = 4 << 20

var coverMIMEs = map[string]string{
	"image/webp": ".webp",
	"image/jpeg": ".jpg",
	"image/png":  ".png",
}

// UploadNovelCover accepts a raw image body and stores it as the novel's
// cover.jpg/.png/.webp. The type comes from sniffed content, not the
// client's declared Content-Type, so a renamed executable cannot masquerade
// as an image.
func (h *APIHandler) UploadNovelCover(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	if slug == "" {
		WriteError(w, http.StatusBadRequest, "invalid novel slug")
		return
	}
	if _, err := h.store.GetNovel(slug); err != nil {
		WriteError(w, http.StatusNotFound, "novel not found")
		return
	}
	sniffed := make([]byte, 512)
	n, err := io.ReadFull(r.Body, sniffed)
	if err != nil && err != io.ErrUnexpectedEOF {
		WriteError(w, http.StatusBadRequest, "cannot read upload")
		return
	}
	mime := http.DetectContentType(sniffed[:n])
	ext, ok := coverMIMEs[mime]
	if !ok {
		WriteError(w, http.StatusUnsupportedMediaType, fmt.Sprintf("unsupported image type %q (use webp, jpg or png)", mime))
		return
	}
	coverPath := filepath.Join(h.store.DataDir, slug, "cover"+ext)
	h.coverMu.Lock()
	defer h.coverMu.Unlock()
	tmp := coverPath + ".upload"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "cannot write cover")
		return
	}
	if _, err := file.Write(sniffed[:n]); err != nil {
		file.Close()
		os.Remove(tmp)
		WriteError(w, http.StatusInternalServerError, "cannot write cover")
		return
	}
	written := int64(n)
	copied, err := io.Copy(file, io.LimitReader(r.Body, maxCoverUpload-written+1))
	if err != nil {
		file.Close()
		os.Remove(tmp)
		WriteError(w, http.StatusInternalServerError, "cannot write cover")
		return
	}
	written += copied
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		WriteError(w, http.StatusInternalServerError, "cannot write cover")
		return
	}
	if written > maxCoverUpload {
		os.Remove(tmp)
		WriteError(w, http.StatusRequestEntityTooLarge, "cover image too large (max 4 MB)")
		return
	}
	if err := os.Rename(tmp, coverPath); err != nil {
		os.Remove(tmp)
		WriteError(w, http.StatusInternalServerError, "cannot store cover")
		return
	}
	// Remove old extensions only after the new cover is safely installed.
	for _, candidateExt := range []string{".webp", ".jpg", ".jpeg", ".png"} {
		if candidateExt != ext {
			_ = os.Remove(filepath.Join(h.store.DataDir, slug, "cover"+candidateExt))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *APIHandler) GetNovelCover(w http.ResponseWriter, r *http.Request) {
	slug := safeSlug(r.PathValue("slug"))
	candidates := []struct {
		name string
		mime string
	}{
		{"cover.webp", "image/webp"},
		{"cover.jpg", "image/jpeg"},
		{"cover.jpeg", "image/jpeg"},
		{"cover.png", "image/png"},
	}
	for _, candidate := range candidates {
		path := filepath.Join(h.store.DataDir, slug, candidate.name)
		file, err := os.Open(path)
		if err == nil {
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				WriteError(w, http.StatusInternalServerError, "failed to read novel cover")
				return
			}
			w.Header().Set("Content-Type", candidate.mime)
			w.Header().Set("Cache-Control", "private, max-age=3600")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeContent(w, r, candidate.name, info.ModTime(), file)
			return
		}
		if !os.IsNotExist(err) {
			WriteError(w, http.StatusInternalServerError, "failed to read novel cover")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
