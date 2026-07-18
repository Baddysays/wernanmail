package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/store"
	"github.com/Baddysays/wernanmail/server/internal/store/maildir"
)

// Store is SQLite + Maildir persistence.
type Store struct {
	db       *sql.DB
	md       *maildir.Root
	appendMu sync.Mutex
}

var (
	_ store.MessageStore = (*Store)(nil)
	_ store.QueueStore   = (*Store)(nil)
)

// Open creates/opens the database and maildir root.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "mail.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{
		db: db,
		md: &maildir.Root{Base: filepath.Join(dataDir, "maildir")},
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.MkdirAll(s.md.Base, 0o750); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) bumpContentRev(ctx context.Context, mailboxID int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE mailboxes SET content_rev = content_rev + 1 WHERE id=?`, mailboxID)
}

func (s *Store) MailboxContentRev(ctx context.Context, mailboxID int64) (int64, error) {
	var rev int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(content_rev,0) FROM mailboxes WHERE id=?`, mailboxID).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rev, err
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func (s *Store) ListDomains(ctx context.Context) ([]domain.Domain, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id,d.name,d.enabled,d.catch_all,COALESCE(d.default_quota_bytes,0),d.dkim_selector,d.dkim_private,d.dkim_public,d.created_at,
			(SELECT COUNT(*) FROM mailboxes m WHERE m.domain_id=d.id) AS mailbox_count
		FROM domains d ORDER BY d.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Domain
	for rows.Next() {
		var d domain.Domain
		var en int
		var created string
		if err := rows.Scan(&d.ID, &d.Name, &en, &d.CatchAll, &d.DefaultQuotaBytes, &d.DKIMSelector, &d.DKIMPrivate, &d.DKIMPublic, &created, &d.MailboxCount); err != nil {
			return nil, err
		}
		d.Enabled = en == 1
		d.CreatedAt = parseTime(created)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDomainByName(ctx context.Context, name string) (*domain.Domain, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,enabled,catch_all,COALESCE(default_quota_bytes,0),dkim_selector,dkim_private,dkim_public,created_at FROM domains WHERE name = ? COLLATE NOCASE`, name)
	var d domain.Domain
	var en int
	var created string
	if err := row.Scan(&d.ID, &d.Name, &en, &d.CatchAll, &d.DefaultQuotaBytes, &d.DKIMSelector, &d.DKIMPrivate, &d.DKIMPublic, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	d.Enabled = en == 1
	d.CreatedAt = parseTime(created)
	return &d, nil
}

func (s *Store) UpsertDomain(ctx context.Context, d *domain.Domain) error {
	en := 0
	if d.Enabled {
		en = 1
	}
	if d.DKIMSelector == "" {
		d.DKIMSelector = "wernan"
	}
	if d.ID == 0 {
		d.CreatedAt = time.Now().UTC()
		res, err := s.db.ExecContext(ctx, `INSERT INTO domains(name,enabled,catch_all,default_quota_bytes,dkim_selector,dkim_private,dkim_public,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			d.Name, en, d.CatchAll, d.DefaultQuotaBytes, d.DKIMSelector, d.DKIMPrivate, d.DKIMPublic, now())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		d.ID = id
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE domains SET name=?, enabled=?, catch_all=?, default_quota_bytes=?, dkim_selector=?, dkim_private=?, dkim_public=? WHERE id=?`,
		d.Name, en, d.CatchAll, d.DefaultQuotaBytes, d.DKIMSelector, d.DKIMPrivate, d.DKIMPublic, d.ID)
	return err
}

func (s *Store) ListMailboxes(ctx context.Context, domainID int64) ([]domain.Mailbox, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,domain_id,local_part,password_hash,display_name,quota_bytes,enabled,created_at FROM mailboxes WHERE domain_id=? ORDER BY local_part`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Mailbox
	for rows.Next() {
		m, err := scanMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanMailbox(row scannable) (*domain.Mailbox, error) {
	var m domain.Mailbox
	var en int
	var created string
	if err := row.Scan(&m.ID, &m.DomainID, &m.LocalPart, &m.PasswordHash, &m.DisplayName, &m.QuotaBytes, &en, &created); err != nil {
		return nil, err
	}
	m.Enabled = en == 1
	m.CreatedAt = parseTime(created)
	return &m, nil
}

func (s *Store) GetMailbox(ctx context.Context, domainName, localPart string) (*domain.Mailbox, *domain.Domain, error) {
	d, err := s.GetDomainByName(ctx, domainName)
	if err != nil || d == nil {
		return nil, d, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT id,domain_id,local_part,password_hash,display_name,quota_bytes,enabled,created_at FROM mailboxes WHERE domain_id=? AND local_part=? COLLATE NOCASE`, d.ID, localPart)
	m, err := scanMailbox(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, d, nil
	}
	return m, d, err
}

func (s *Store) GetMailboxByID(ctx context.Context, id int64) (*domain.Mailbox, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,domain_id,local_part,password_hash,display_name,quota_bytes,enabled,created_at FROM mailboxes WHERE id=?`, id)
	m, err := scanMailbox(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

func (s *Store) UpsertMailbox(ctx context.Context, m *domain.Mailbox) error {
	en := 0
	if m.Enabled {
		en = 1
	}
	if m.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO mailboxes(domain_id,local_part,password_hash,display_name,quota_bytes,enabled,created_at) VALUES(?,?,?,?,?,?,?)`,
			m.DomainID, m.LocalPart, m.PasswordHash, m.DisplayName, m.QuotaBytes, en, now())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		m.ID = id
		m.CreatedAt = time.Now().UTC()
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE mailboxes SET local_part=?, password_hash=?, display_name=?, quota_bytes=?, enabled=? WHERE id=?`,
		m.LocalPart, m.PasswordHash, m.DisplayName, m.QuotaBytes, en, m.ID)
	return err
}

func (s *Store) DeleteMailbox(ctx context.Context, domainID, mailboxID int64) error {
	m, err := s.GetMailboxByID(ctx, mailboxID)
	if err != nil {
		return err
	}
	if m == nil || m.DomainID != domainID {
		return sql.ErrNoRows
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM folder_uid WHERE mailbox_id=?`, mailboxID)
	rows, qerr := s.db.QueryContext(ctx, `SELECT maildir_rel FROM quarantine WHERE mailbox_id=?`, mailboxID)
	if qerr == nil {
		for rows.Next() {
			var rel string
			if rows.Scan(&rel) == nil {
				_ = s.md.Remove(rel)
			}
		}
		_ = rows.Close()
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM quarantine WHERE mailbox_id=?`, mailboxID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM queue_jobs WHERE payload_json LIKE ?`, fmt.Sprintf(`%%"mailboxId":%d%%`, mailboxID))
	drow := s.db.QueryRowContext(ctx, `SELECT catch_all FROM domains WHERE id=?`, domainID)
	var catchAll string
	if drow.Scan(&catchAll) == nil && strings.EqualFold(strings.TrimSpace(catchAll), m.LocalPart) {
		_, _ = s.db.ExecContext(ctx, `UPDATE domains SET catch_all='' WHERE id=?`, domainID)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM mailboxes WHERE id=? AND domain_id=?`, mailboxID, domainID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	_ = s.md.RemoveMailbox(mailboxID)
	return nil
}

func (s *Store) DeleteDomain(ctx context.Context, domainID int64) error {
	mboxes, err := s.ListMailboxes(ctx, domainID)
	if err != nil {
		return err
	}
	for _, m := range mboxes {
		if err := s.DeleteMailbox(ctx, domainID, m.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE id=?`, domainID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// HashPassword returns a bcrypt hash.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func (s *Store) AuthenticateMailbox(ctx context.Context, address, password string) (*domain.Mailbox, *domain.Domain, error) {
	local, domainName, ok := splitAddress(address)
	if !ok {
		return nil, nil, fmt.Errorf("invalid address")
	}
	m, d, err := s.GetMailbox(ctx, domainName, local)
	if err != nil || m == nil || d == nil || !m.Enabled || !d.Enabled {
		return nil, nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(m.PasswordHash), []byte(password)); err != nil {
		return nil, nil, fmt.Errorf("auth failed")
	}
	return m, d, nil
}

func splitAddress(addr string) (local, domain string, ok bool) {
	addr = strings.TrimSpace(strings.ToLower(addr))
	i := strings.LastIndex(addr, "@")
	if i <= 0 || i == len(addr)-1 {
		return "", "", false
	}
	return addr[:i], addr[i+1:], true
}

func (s *Store) ListAliases(ctx context.Context, domainID int64) ([]domain.Alias, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,domain_id,local_part,mailbox_id,enabled FROM aliases WHERE domain_id=?`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Alias
	for rows.Next() {
		var a domain.Alias
		var en int
		if err := rows.Scan(&a.ID, &a.DomainID, &a.LocalPart, &a.MailboxID, &en); err != nil {
			return nil, err
		}
		a.Enabled = en == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAlias(ctx context.Context, a *domain.Alias) error {
	en := 0
	if a.Enabled {
		en = 1
	}
	if a.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO aliases(domain_id,local_part,mailbox_id,enabled) VALUES(?,?,?,?)`, a.DomainID, a.LocalPart, a.MailboxID, en)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		a.ID = id
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE aliases SET local_part=?, mailbox_id=?, enabled=? WHERE id=?`, a.LocalPart, a.MailboxID, en, a.ID)
	return err
}

func (s *Store) DeleteAlias(ctx context.Context, domainID, aliasID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM aliases WHERE id=? AND domain_id=?`, aliasID, domainID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ResolveRecipient(ctx context.Context, address string) (int64, error) {
	local, domainName, ok := splitAddress(address)
	if !ok {
		return 0, fmt.Errorf("invalid recipient")
	}
	d, err := s.GetDomainByName(ctx, domainName)
	if err != nil || d == nil || !d.Enabled {
		return 0, fmt.Errorf("unknown domain")
	}
	row := s.db.QueryRowContext(ctx, `SELECT mailbox_id FROM aliases WHERE domain_id=? AND local_part=? COLLATE NOCASE AND enabled=1`, d.ID, local)
	var mid int64
	if err := row.Scan(&mid); err == nil {
		return mid, nil
	}
	m, _, err := s.GetMailbox(ctx, domainName, local)
	if err != nil {
		return 0, err
	}
	if m != nil && m.Enabled {
		return m.ID, nil
	}
	if d.CatchAll != "" {
		m, _, err := s.GetMailbox(ctx, domainName, d.CatchAll)
		if err != nil || m == nil {
			return 0, fmt.Errorf("no mailbox")
		}
		return m.ID, nil
	}
	return 0, fmt.Errorf("no mailbox")
}

func (s *Store) NextUID(ctx context.Context, mailboxID int64, folder string) (uint32, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var next int64
	err = tx.QueryRowContext(ctx, `SELECT next_uid FROM folder_uid WHERE mailbox_id=? AND folder=?`, mailboxID, folder).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) {
		next = 1
		uidVal := time.Now().Unix()
		if uidVal < 1 {
			uidVal = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO folder_uid(mailbox_id,folder,next_uid,uid_validity) VALUES(?,?,2,?)`, mailboxID, folder, uidVal); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE folder_uid SET next_uid=next_uid+1 WHERE mailbox_id=? AND folder=?`, mailboxID, folder); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return uint32(next), nil
}

func (s *Store) FolderStats(ctx context.Context, mailboxID int64, folder string) (messages, unseen, uidNext, uidValidity uint32, err error) {
	var total, unseenN int64
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE mailbox_id=? AND folder=?`, mailboxID, folder).Scan(&total)
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE mailbox_id=? AND folder=? AND flags_json NOT LIKE '%Seen%'`, mailboxID, folder).Scan(&unseenN)
	if err != nil {
		return
	}
	messages = uint32(total)
	unseen = uint32(unseenN)

	var next, uv int64
	err = s.db.QueryRowContext(ctx, `SELECT next_uid, COALESCE(uid_validity,1) FROM folder_uid WHERE mailbox_id=? AND folder=?`, mailboxID, folder).Scan(&next, &uv)
	if errors.Is(err, sql.ErrNoRows) {
		uidNext, uidValidity, err = 1, 1, nil
		return
	}
	if err != nil {
		err2 := s.db.QueryRowContext(ctx, `SELECT next_uid FROM folder_uid WHERE mailbox_id=? AND folder=?`, mailboxID, folder).Scan(&next)
		if errors.Is(err2, sql.ErrNoRows) {
			uidNext, uidValidity, err = 1, 1, nil
			return
		}
		if err2 != nil {
			err = err2
			return
		}
		uidNext, uidValidity, err = uint32(next), 1, nil
		return
	}
	if next < 1 {
		next = 1
	}
	if uv < 1 {
		uv = 1
	}
	uidNext, uidValidity = uint32(next), uint32(uv)
	return
}

func (s *Store) AppendMessage(ctx context.Context, msg *domain.Message, raw []byte) error {
	s.appendMu.Lock()
	defer s.appendMu.Unlock()

	mb, err := s.GetMailboxByID(ctx, msg.MailboxID)
	if err != nil {
		return err
	}
	if mb == nil {
		return fmt.Errorf("mailbox %d not found", msg.MailboxID)
	}
	if mb.QuotaBytes > 0 {
		used, err := s.UsageBytes(ctx, msg.MailboxID)
		if err != nil {
			return err
		}
		if used+int64(len(raw)) > mb.QuotaBytes {
			return store.ErrQuotaExceeded
		}
	}
	if msg.Folder == "" {
		msg.Folder = domain.FolderInbox
	}
	if msg.UID == 0 {
		uid, err := s.NextUID(ctx, msg.MailboxID, msg.Folder)
		if err != nil {
			return err
		}
		msg.UID = uid
	}
	rel, err := s.md.WriteNew(msg.MailboxID, msg.Folder, raw)
	if err != nil {
		return err
	}
	msg.MaildirRel = rel
	msg.Size = int64(len(raw))
	if msg.Date.IsZero() {
		msg.Date = time.Now().UTC()
	}
	flags, _ := json.Marshal(msg.Flags)
	if flags == nil {
		flags = []byte("[]")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO messages(mailbox_id,folder,uid,message_id,subject,from_addr,to_addrs,date,size,flags_json,maildir_rel,spam_score,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		msg.MailboxID, msg.Folder, msg.UID, msg.MessageID, msg.Subject, msg.FromAddr, msg.ToAddrs,
		msg.Date.UTC().Format(time.RFC3339Nano), msg.Size, string(flags), msg.MaildirRel, msg.SpamScore, now())
	if err != nil {
		_ = s.md.Remove(rel)
		return err
	}
	s.bumpContentRev(ctx, msg.MailboxID)
	return nil
}

func (s *Store) ListMessages(ctx context.Context, mailboxID int64, folder string, limit int) ([]domain.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,mailbox_id,folder,uid,message_id,subject,from_addr,to_addrs,date,size,flags_json,maildir_rel,spam_score,created_at
FROM messages WHERE mailbox_id=? AND folder=? ORDER BY uid DESC LIMIT ?`, mailboxID, folder, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func scanMessage(row scannable) (*domain.Message, error) {
	var m domain.Message
	var date, created, flags string
	if err := row.Scan(&m.ID, &m.MailboxID, &m.Folder, &m.UID, &m.MessageID, &m.Subject, &m.FromAddr, &m.ToAddrs, &date, &m.Size, &flags, &m.MaildirRel, &m.SpamScore, &created); err != nil {
		return nil, err
	}
	m.Date = parseTime(date)
	m.CreatedAt = parseTime(created)
	_ = json.Unmarshal([]byte(flags), &m.Flags)
	return &m, nil
}

func (s *Store) GetMessage(ctx context.Context, mailboxID int64, folder string, uid uint32) (*domain.Message, []byte, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,mailbox_id,folder,uid,message_id,subject,from_addr,to_addrs,date,size,flags_json,maildir_rel,spam_score,created_at
FROM messages WHERE mailbox_id=? AND folder=? AND uid=?`, mailboxID, folder, uid)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	raw, err := s.md.Read(m.MaildirRel)
	return m, raw, err
}

func (s *Store) UpdateFlags(ctx context.Context, mailboxID int64, folder string, uid uint32, add, remove []string) error {
	row := s.db.QueryRowContext(ctx, `SELECT flags_json FROM messages WHERE mailbox_id=? AND folder=? AND uid=?`, mailboxID, folder, uid)
	var flagsJSON string
	if err := row.Scan(&flagsJSON); err != nil {
		return err
	}
	var flags []string
	_ = json.Unmarshal([]byte(flagsJSON), &flags)
	set := map[string]struct{}{}
	for _, f := range flags {
		set[f] = struct{}{}
	}
	for _, f := range add {
		set[f] = struct{}{}
	}
	for _, f := range remove {
		delete(set, f)
	}
	flags = flags[:0]
	for f := range set {
		flags = append(flags, f)
	}
	b, _ := json.Marshal(flags)
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET flags_json=? WHERE mailbox_id=? AND folder=? AND uid=?`, string(b), mailboxID, folder, uid)
	if err != nil {
		return err
	}
	s.bumpContentRev(ctx, mailboxID)
	return nil
}

func (s *Store) MoveMessage(ctx context.Context, mailboxID int64, folder string, uid uint32, toFolder string) error {
	m, raw, err := s.GetMessage(ctx, mailboxID, folder, uid)
	if err != nil || m == nil {
		return err
	}
	newUID, err := s.NextUID(ctx, mailboxID, toFolder)
	if err != nil {
		return err
	}
	rel, err := s.md.WriteNew(mailboxID, toFolder, raw)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE messages SET folder=?, uid=?, maildir_rel=? WHERE id=?`, toFolder, newUID, rel, m.ID)
	if err != nil {
		return err
	}
	_ = s.md.Remove(m.MaildirRel)
	s.bumpContentRev(ctx, mailboxID)
	return nil
}

func (s *Store) CopyMessage(ctx context.Context, mailboxID int64, folder string, uid uint32, toFolder string) (uint32, error) {
	m, raw, err := s.GetMessage(ctx, mailboxID, folder, uid)
	if err != nil || m == nil {
		return 0, err
	}
	msg := &domain.Message{
		MailboxID: mailboxID,
		Folder:    toFolder,
		Subject:   m.Subject,
		FromAddr:  m.FromAddr,
		ToAddrs:   m.ToAddrs,
		Date:      m.Date,
		MessageID: m.MessageID,
		Flags:     append([]string{}, m.Flags...),
		SpamScore: m.SpamScore,
	}
	if err := s.AppendMessage(ctx, msg, raw); err != nil {
		return 0, err
	}
	return msg.UID, nil
}

func (s *Store) DeleteMessage(ctx context.Context, mailboxID int64, folder string, uid uint32) error {
	m, _, err := s.GetMessage(ctx, mailboxID, folder, uid)
	if err != nil || m == nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE mailbox_id=? AND folder=? AND uid=?`, mailboxID, folder, uid); err != nil {
		return err
	}
	_ = s.md.Remove(m.MaildirRel)
	s.bumpContentRev(ctx, mailboxID)
	return nil
}

func (s *Store) ExpungeDeleted(ctx context.Context, mailboxID int64, folder string) (int, error) {
	msgs, err := s.ListMessages(ctx, mailboxID, folder, 10000)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range msgs {
		deleted := false
		for _, f := range m.Flags {
			if strings.EqualFold(f, `\Deleted`) || strings.EqualFold(f, "\\Deleted") {
				deleted = true
				break
			}
		}
		if !deleted {
			continue
		}
		if err := s.DeleteMessage(ctx, mailboxID, folder, m.UID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) UsageBytes(ctx context.Context, mailboxID int64) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM messages WHERE mailbox_id=?`, mailboxID).Scan(&n)
	return n.Int64, err
}

func (s *Store) AddQuarantine(ctx context.Context, q *domain.QuarantineItem, raw []byte) error {
	rel, err := s.md.WriteQuarantine(q.MailboxID, raw)
	if err != nil {
		return err
	}
	q.MaildirRel = rel
	res, err := s.db.ExecContext(ctx, `INSERT INTO quarantine(mailbox_id,maildir_rel,subject,from_addr,verdict_json,created_at,resolution) VALUES(?,?,?,?,?,?,?)`,
		q.MailboxID, q.MaildirRel, q.Subject, q.FromAddr, q.VerdictJSON, now(), "")
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	q.ID = id
	q.CreatedAt = time.Now().UTC()
	return nil
}

func (s *Store) ListQuarantine(ctx context.Context, limit int) ([]domain.QuarantineItem, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,mailbox_id,maildir_rel,subject,from_addr,verdict_json,created_at,resolved_at,resolution FROM quarantine WHERE resolution='' ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.QuarantineItem
	for rows.Next() {
		var q domain.QuarantineItem
		var created string
		var resolved sql.NullString
		if err := rows.Scan(&q.ID, &q.MailboxID, &q.MaildirRel, &q.Subject, &q.FromAddr, &q.VerdictJSON, &created, &resolved, &q.Resolution); err != nil {
			return nil, err
		}
		q.CreatedAt = parseTime(created)
		if resolved.Valid {
			t := parseTime(resolved.String)
			q.ResolvedAt = &t
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) CountQuarantine(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quarantine WHERE resolution=''`).Scan(&n)
	return n, err
}

func (s *Store) ResolveQuarantine(ctx context.Context, id int64, resolution string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE quarantine SET resolution=?, resolved_at=? WHERE id=?`, resolution, now(), id)
	return err
}

func (s *Store) GetQuarantineRaw(ctx context.Context, id int64) (*domain.QuarantineItem, []byte, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,mailbox_id,maildir_rel,subject,from_addr,verdict_json,created_at,resolved_at,resolution FROM quarantine WHERE id=?`, id)
	var q domain.QuarantineItem
	var created string
	var resolved sql.NullString
	if err := row.Scan(&q.ID, &q.MailboxID, &q.MaildirRel, &q.Subject, &q.FromAddr, &q.VerdictJSON, &created, &resolved, &q.Resolution); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	q.CreatedAt = parseTime(created)
	raw, err := s.md.Read(q.MaildirRel)
	return &q, raw, err
}

func (s *Store) PurgeQuarantineOlderThan(ctx context.Context, olderThan time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	cutoff := olderThan.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT id, maildir_rel FROM quarantine WHERE created_at < ? ORDER BY id ASC LIMIT ?`, cutoff, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type row struct {
		id  int64
		rel string
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.rel); err != nil {
			return 0, err
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range batch {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM quarantine WHERE id=?`, r.id); err != nil {
			return n, err
		}
		_ = s.md.Remove(r.rel)
		n++
	}
	return n, nil
}

func (s *Store) DeleteMessagesOlderThan(ctx context.Context, olderThan time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	cutoff := olderThan.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT mailbox_id, uid, maildir_rel FROM messages WHERE created_at < ? ORDER BY id ASC LIMIT ?`, cutoff, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type row struct {
		mailboxID int64
		uid       uint32
		rel       string
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.mailboxID, &r.uid, &r.rel); err != nil {
			return 0, err
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range batch {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE mailbox_id=? AND uid=?`, r.mailboxID, r.uid); err != nil {
			return n, err
		}
		_ = s.md.Remove(r.rel)
		n++
	}
	return n, nil
}

func (s *Store) AddDMARCReports(ctx context.Context, reports []domain.DMARCReport) error {
	if len(reports) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i := range reports {
		r := &reports[i]
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO dmarc_reports
(mailbox_id,org_name,report_id,date_begin,date_end,source_ip,message_count,dkim_result,spf_result,disposition,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`, r.MailboxID, r.OrgName, r.ReportID,
			r.DateBegin.UTC().Format(time.RFC3339Nano), r.DateEnd.UTC().Format(time.RFC3339Nano),
			r.SourceIP, r.Count, r.DKIMResult, r.SPFResult, r.Disposition, now())
		if err != nil {
			return err
		}
		if id, _ := res.LastInsertId(); id > 0 {
			r.ID = id
			r.CreatedAt = time.Now().UTC()
		}
	}
	return tx.Commit()
}

func (s *Store) ListDMARCReports(ctx context.Context, limit int) ([]domain.DMARCReport, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,mailbox_id,org_name,report_id,date_begin,date_end,
source_ip,message_count,dkim_result,spf_result,disposition,created_at
FROM dmarc_reports ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DMARCReport
	for rows.Next() {
		var r domain.DMARCReport
		var begin, end, created string
		if err := rows.Scan(&r.ID, &r.MailboxID, &r.OrgName, &r.ReportID, &begin, &end,
			&r.SourceIP, &r.Count, &r.DKIMResult, &r.SPFResult, &r.Disposition, &created); err != nil {
			return nil, err
		}
		r.DateBegin, r.DateEnd, r.CreatedAt = parseTime(begin), parseTime(end), parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AddTLSRPTReports(ctx context.Context, reports []domain.TLSRPTReport) error {
	if len(reports) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i := range reports {
		r := &reports[i]
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tls_rpt_reports
(mailbox_id,org_name,report_id,date_begin,date_end,policy_domain,success_count,failure_count,result_type,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?)`, r.MailboxID, r.OrgName, r.ReportID,
			r.DateBegin.UTC().Format(time.RFC3339Nano), r.DateEnd.UTC().Format(time.RFC3339Nano),
			r.PolicyDomain, r.SuccessCount, r.FailureCount, r.ResultType, now())
		if err != nil {
			return err
		}
		if id, _ := res.LastInsertId(); id > 0 {
			r.ID = id
			r.CreatedAt = time.Now().UTC()
		}
	}
	return tx.Commit()
}

func (s *Store) ListTLSRPTReports(ctx context.Context, limit int) ([]domain.TLSRPTReport, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,mailbox_id,org_name,report_id,date_begin,date_end,
policy_domain,success_count,failure_count,result_type,created_at
FROM tls_rpt_reports ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TLSRPTReport
	for rows.Next() {
		var r domain.TLSRPTReport
		var begin, end, created string
		if err := rows.Scan(&r.ID, &r.MailboxID, &r.OrgName, &r.ReportID, &begin, &end,
			&r.PolicyDomain, &r.SuccessCount, &r.FailureCount, &r.ResultType, &created); err != nil {
			return nil, err
		}
		r.DateBegin, r.DateEnd, r.CreatedAt = parseTime(begin), parseTime(end), parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListMailFilters(ctx context.Context, mailboxID int64) ([]domain.MailFilter, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,mailbox_id,enabled,priority,match_field,match_op,match_value,action,action_arg
FROM mail_filters WHERE mailbox_id=? ORDER BY priority ASC,id ASC`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MailFilter
	for rows.Next() {
		var f domain.MailFilter
		var enabled int
		if err := rows.Scan(&f.ID, &f.MailboxID, &enabled, &f.Priority, &f.MatchField, &f.MatchOp, &f.MatchValue, &f.Action, &f.ActionArg); err != nil {
			return nil, err
		}
		f.Enabled = enabled == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceMailFilters(ctx context.Context, mailboxID int64, filters []domain.MailFilter) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mail_filters WHERE mailbox_id=?`, mailboxID); err != nil {
		return err
	}
	for _, f := range filters {
		enabled := 0
		if f.Enabled {
			enabled = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mail_filters
(mailbox_id,enabled,priority,match_field,match_op,match_value,action,action_arg) VALUES(?,?,?,?,?,?,?,?)`,
			mailboxID, enabled, f.Priority, f.MatchField, f.MatchOp, f.MatchValue, f.Action, f.ActionArg); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, now())
	return err
}

// LearnSpamSignals adjusts a bounded set of lightweight antispam weights.
func (s *Store) LearnSpamSignals(ctx context.Context, keys []string, delta float64) error {
	if len(keys) == 0 || delta == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO spam_signals(key,weight,hits,updated_at) VALUES(?,?,1,?)
ON CONFLICT(key) DO UPDATE SET weight=spam_signals.weight+excluded.weight, hits=spam_signals.hits+1, updated_at=excluded.updated_at`,
			key, delta, now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LookupSpamSignals returns weights for the requested keys only.
func (s *Store) LookupSpamSignals(ctx context.Context, keys []string) (map[string]float64, error) {
	out := make(map[string]float64, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(keys))
	placeholders := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		args = append(args, key)
		placeholders = append(placeholders, "?")
	}
	if len(args) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key,weight FROM spam_signals WHERE key IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var weight float64
		if err := rows.Scan(&key, &weight); err != nil {
			return nil, err
		}
		out[key] = weight
	}
	return out, rows.Err()
}

func (s *Store) ListSettings(ctx context.Context) ([]domain.Setting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key,value,updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Setting
	for rows.Next() {
		var st domain.Setting
		var updated string
		if err := rows.Scan(&st.Key, &st.Value, &updated); err != nil {
			return nil, err
		}
		st.UpdatedAt = parseTime(updated)
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) AddAudit(ctx context.Context, e *domain.AuditEntry) error {
	res, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(actor,action,target,detail,created_at) VALUES(?,?,?,?,?)`,
		e.Actor, e.Action, e.Target, e.Detail, now())
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	e.ID = id
	e.CreatedAt = time.Now().UTC()
	return nil
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]domain.AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor,action,target,detail,created_at FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AuditEntry
	for rows.Next() {
		var e domain.AuditEntry
		var created string
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.Detail, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- QueueStore ---

func (s *Store) Enqueue(ctx context.Context, job *domain.QueueJob) error {
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 8
	}
	if job.NextAt.IsZero() {
		job.NextAt = time.Now().UTC()
	}
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO queue_jobs(kind,payload_json,attempts,max_attempts,next_at,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		string(job.Kind), job.PayloadJSON, job.Attempts, job.MaxAttempts, job.NextAt.UTC().Format(time.RFC3339Nano), job.LastError, ts, ts)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	job.ID = id
	return nil
}

func (s *Store) Claim(ctx context.Context, workerID string, lease time.Duration) (*domain.QueueJob, error) {
	_ = workerID
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	nowT := time.Now().UTC()
	row := tx.QueryRowContext(ctx, `SELECT id,kind,payload_json,attempts,max_attempts,next_at,locked_until,last_error,created_at,updated_at
FROM queue_jobs WHERE attempts < max_attempts AND next_at <= ? AND (locked_until IS NULL OR locked_until < ?)
ORDER BY id ASC LIMIT 1`, nowT.Format(time.RFC3339Nano), nowT.Format(time.RFC3339Nano))
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	until := nowT.Add(lease).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE queue_jobs SET locked_until=?, attempts=attempts+1, updated_at=? WHERE id=?`, until, now(), job.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job.Attempts++
	return job, nil
}

func scanJob(row scannable) (*domain.QueueJob, error) {
	var j domain.QueueJob
	var kind, next, created, updated string
	var locked sql.NullString
	if err := row.Scan(&j.ID, &kind, &j.PayloadJSON, &j.Attempts, &j.MaxAttempts, &next, &locked, &j.LastError, &created, &updated); err != nil {
		return nil, err
	}
	j.Kind = domain.QueueJobKind(kind)
	j.NextAt = parseTime(next)
	j.CreatedAt = parseTime(created)
	j.UpdatedAt = parseTime(updated)
	if locked.Valid {
		t := parseTime(locked.String)
		j.LockedUntil = &t
	}
	return &j, nil
}

func (s *Store) Complete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM queue_jobs WHERE id=?`, id)
	return err
}

func (s *Store) Fail(ctx context.Context, id int64, errMsg string, retryAt time.Time, dead bool) error {
	if dead {
		_, err := s.db.ExecContext(ctx, `UPDATE queue_jobs SET last_error=?, locked_until=NULL, attempts=max_attempts, updated_at=? WHERE id=?`, errMsg, now(), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE queue_jobs SET last_error=?, next_at=?, locked_until=NULL, updated_at=? WHERE id=?`,
		errMsg, retryAt.UTC().Format(time.RFC3339Nano), now(), id)
	return err
}

func (s *Store) Retry(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE queue_jobs SET attempts=0, next_at=?, locked_until=NULL, last_error='', updated_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339Nano), now(), id)
	return err
}

func (s *Store) DeleteJob(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM queue_jobs WHERE id=?`, id)
	return err
}

func (s *Store) List(ctx context.Context, limit int) ([]domain.QueueJob, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,payload_json,attempts,max_attempts,next_at,locked_until,last_error,created_at,updated_at FROM queue_jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.QueueJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (s *Store) Count(ctx context.Context) (pending, dead int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM queue_jobs WHERE attempts < max_attempts`).Scan(&pending)
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM queue_jobs WHERE attempts >= max_attempts`).Scan(&dead)
	return
}
