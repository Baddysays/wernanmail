package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BackupDatabase writes a consistent snapshot of mail.db to destPath (must not exist).
// Uses SQLite VACUUM INTO so WAL is folded into the copy without stopping the stack.
func (s *Store) BackupDatabase(ctx context.Context, destPath string) error {
	abs, err := filepath.Abs(destPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("backup destination already exists: %s", abs)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return err
	}
	// Prefer forward slashes; quote for VACUUM INTO (bound params are unreliable across drivers).
	qpath := strings.ReplaceAll(filepath.ToSlash(abs), "'", "''")
	_, err = s.db.ExecContext(ctx, "VACUUM INTO '"+qpath+"'")
	if err != nil {
		_ = os.Remove(abs)
		return fmt.Errorf("VACUUM INTO: %w", err)
	}
	return nil
}

// MaildirBase returns the on-disk maildir root.
func (s *Store) MaildirBase() string {
	if s.md == nil {
		return ""
	}
	return s.md.Base
}
