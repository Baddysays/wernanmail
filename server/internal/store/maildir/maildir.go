package maildir

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Root manages a Maildir tree.
type Root struct {
	Base string
}

// EnsureMailbox creates cur/new/tmp for a mailbox folder.
func (r *Root) EnsureMailbox(mailboxID int64, folder string) error {
	base := r.path(mailboxID, folder)
	for _, sub := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o750); err != nil {
			return err
		}
	}
	return nil
}

func (r *Root) path(mailboxID int64, folder string) string {
	safe := sanitizeFolder(folder)
	return filepath.Join(r.Base, fmt.Sprintf("%d", mailboxID), safe)
}

func sanitizeFolder(folder string) string {
	out := make([]rune, 0, len(folder))
	for _, c := range folder {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "INBOX"
	}
	return string(out)
}

// WriteNew writes raw bytes into new/ and returns a relative path from Base.
func (r *Root) WriteNew(mailboxID int64, folder string, raw []byte) (rel string, err error) {
	if err := r.EnsureMailbox(mailboxID, folder); err != nil {
		return "", err
	}
	name := uniqueName(mailboxID)
	abs := filepath.Join(r.path(mailboxID, folder), "new", name)
	if err := os.WriteFile(abs, raw, 0o640); err != nil {
		return "", err
	}
	rel, err = filepath.Rel(r.Base, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// WriteQuarantine stores raw under quarantine/.
func (r *Root) WriteQuarantine(idHint int64, raw []byte) (rel string, err error) {
	dir := filepath.Join(r.Base, "_quarantine")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	name := uniqueName(idHint) + ".eml"
	abs := filepath.Join(dir, name)
	if err := os.WriteFile(abs, raw, 0o640); err != nil {
		return "", err
	}
	rel, err = filepath.Rel(r.Base, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func uniqueName(idHint int64) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d.%d.%d.%s.wernan", time.Now().UnixNano(), os.Getpid(), idHint, hex.EncodeToString(b[:]))
}

// Read reads a relative maildir path.
func (r *Root) Read(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(r.Base, filepath.FromSlash(rel)))
}

// Remove deletes a relative path if present.
func (r *Root) Remove(rel string) error {
	if rel == "" {
		return nil
	}
	return os.Remove(filepath.Join(r.Base, filepath.FromSlash(rel)))
}
