package adminapi

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/domain"
)

var backupFullMu sync.Mutex

func (h *Handler) backupFull(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "admin.store", "store unavailable")
		return
	}
	if !backupFullMu.TryLock() {
		writeErr(w, http.StatusConflict, "admin.backup_busy", "another full backup is already running")
		return
	}
	defer backupFullMu.Unlock()

	tmp, err := os.MkdirTemp("", "wernanmail-backup-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.backup", err.Error())
		return
	}
	defer os.RemoveAll(tmp)

	dbSnap := filepath.Join(tmp, "mail.db")
	if err := h.Store.BackupDatabase(r.Context(), dbSnap); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.backup_db", err.Error())
		return
	}

	archivePath := filepath.Join(tmp, "backup.tar.gz")
	if err := buildDataArchive(archivePath, dbSnap, h.maildirPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.backup_pack", err.Error())
		return
	}

	fi, err := os.Stat(archivePath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.backup", err.Error())
		return
	}
	f, err := os.Open(archivePath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "admin.backup", err.Error())
		return
	}
	defer f.Close()

	name := fmt.Sprintf("wernanmail-data-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		return
	}
	_ = h.Store.AddAudit(r.Context(), &domain.AuditEntry{
		Actor:  h.actor(r),
		Action: "backup.full",
		Target: name,
	})
}

func (h *Handler) maildirPath() string {
	if h.Store != nil {
		if md := h.Store.MaildirBase(); md != "" {
			return md
		}
	}
	return filepath.Join(h.Cfg.DataDir, "maildir")
}

func buildDataArchive(archivePath, dbSnap, maildir string) error {
	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := writeFileToTar(tw, dbSnap, "mail.db"); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return err
	}
	if err := writeDirToTar(tw, maildir, "maildir"); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return err
	}
	return gz.Close()
}

func writeFileToTar(tw *tar.Writer, srcPath, nameInTar string) error {
	fi, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return err
	}
	hdr.Name = nameInTar
	hdr.ModTime = fi.ModTime()
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}

func writeDirToTar(tw *tar.Writer, root, prefix string) error {
	st, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			hdr := &tar.Header{Name: prefix + "/", Mode: 0o750, Typeflag: tar.TypeDir, ModTime: time.Now()}
			return tw.WriteHeader(hdr)
		}
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("maildir is not a directory")
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := prefix
		if rel != "." {
			name = filepath.ToSlash(filepath.Join(prefix, rel))
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = strings.TrimSuffix(name, "/") + "/"
			return tw.WriteHeader(hdr)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return writeFileToTar(tw, path, name)
	})
}
