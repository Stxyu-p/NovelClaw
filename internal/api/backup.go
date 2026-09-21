package api

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"novelclaw/internal/config"
)

// backupKeep is how many recent archives to retain (older ones are deleted).
// ponytail: fixed retention; make it configurable when the first user hits it.
const backupKeep = 7

// backupStaleAfter is how long an auto backup may lag behind startup.
const backupStaleAfter = 24 * time.Hour

var backupMu sync.Mutex
var backupNamePattern = regexp.MustCompile(`^novelclaw-backup-\d{8}-\d{6}-[0-9a-f]{4}\.zip$`)

type backupInfo struct {
	Name    string    `json:"name"`
	SizeMB  float64   `json:"sizeMB"`
	ModTime time.Time `json:"modTime"`
}

func backupDir(cfg *config.AppConfig) string {
	return filepath.Join(filepath.Dir(cfg.DataDir), "backups")
}

// CreateBackup zips the whole data directory into backups/<timestamp>.zip
// and prunes old archives beyond backupKeep.
func CreateBackup(cfg *config.AppConfig) (backupInfo, error) {
	backupMu.Lock()
	defer backupMu.Unlock()

	dir := backupDir(cfg)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return backupInfo{}, err
	}

	// ponytail: random suffix defeats Windows clock granularity (~2ms) which
	// can hand out identical timestamps to back-to-back backups.
	name := fmt.Sprintf("novelclaw-backup-%s-%04x.zip", time.Now().Format("20060102-150405"), rand.IntN(0xffff))
	dest := filepath.Join(dir, name)
	tmpDest := dest + ".tmp"

	out, err := os.Create(tmpDest)
	if err != nil {
		return backupInfo{}, err
	}

	zw := zip.NewWriter(out)
	absoluteBackupDir, err := filepath.Abs(dir)
	if err != nil {
		out.Close()
		os.Remove(tmpDest)
		return backupInfo{}, err
	}
	err = filepath.WalkDir(cfg.DataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			absolutePath, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if absolutePath == absoluteBackupDir {
				return filepath.SkipDir
			}
			// Cache and restart-queue files are reproducible/transient. Excluding
			// them keeps backups small even when TTS audio grows into gigabytes.
			if d.Name() == ".cache" || d.Name() == ".jobs" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(cfg.DataDir), path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, src)
		closeErr := src.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		_ = os.Remove(tmpDest)
		return backupInfo{}, err
	}
	if err := zw.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpDest)
		return backupInfo{}, err
	}
	// Close before rename/pruning: an open handle blocks these operations on Windows.
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpDest)
		return backupInfo{}, err
	}
	if err := os.Rename(tmpDest, dest); err != nil {
		_ = os.Remove(tmpDest)
		return backupInfo{}, err
	}

	pruneBackups(dir)

	info, err := os.Stat(dest)
	if err != nil {
		return backupInfo{}, err
	}
	return backupInfo{Name: name, SizeMB: float64(info.Size()) / (1 << 20), ModTime: info.ModTime()}, nil
}

// ListBackups returns retained archives, newest first.
func ListBackups(cfg *config.AppConfig) []backupInfo {
	dir := backupDir(cfg)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []backupInfo{}
	}
	var list []backupInfo
	for _, e := range entries {
		if e.IsDir() || !backupNamePattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, backupInfo{
			Name:    e.Name(),
			SizeMB:  float64(info.Size()) / (1 << 20),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ModTime.After(list[j].ModTime) })
	return list
}

func pruneBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type archive struct {
		path string
		mod  time.Time
	}
	var list []archive
	for _, e := range entries {
		if e.IsDir() || !backupNamePattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, archive{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.Before(list[j].mod) })
	for len(list) > backupKeep {
		_ = os.Remove(list[0].path)
		list = list[1:]
	}
}

// MaybeAutoBackup creates a fresh backup at startup when the newest one is
// older than backupStaleAfter (or none exists).
func MaybeAutoBackup(cfg *config.AppConfig) {
	list := ListBackups(cfg)
	if len(list) > 0 && time.Since(list[0].ModTime) < backupStaleAfter {
		return
	}
	info, err := CreateBackup(cfg)
	if err != nil {
		log.Printf("Auto backup failed: %v", err)
		return
	}
	log.Printf("Auto backup created: %s (%.1f MB)", info.Name, info.SizeMB)
}

// BackupNow handles POST /api/backup.
func (h *APIHandler) BackupNow(w http.ResponseWriter, r *http.Request) {
	info, err := CreateBackup(h.cfg)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("Backup failed: %v", err))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "created",
		"backup":   info,
		"archives": ListBackups(h.cfg),
	})
}

// ListBackupsHandler handles GET /api/backup.
func (h *APIHandler) ListBackupsHandler(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]interface{}{"archives": ListBackups(h.cfg)})
}
