package sqlite

import (
	"database/sql"
	"fmt"
	"time"
)

// CurrentSchemaVersion is the latest applied migration ID.
const CurrentSchemaVersion = 3

type migration struct {
	version int
	name    string
	up      func(tx *sql.Tx) error
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}

	applied, err := s.appliedMigrations()
	if err != nil {
		return err
	}

	// Existing installs created before versioning: stamp baseline so ALTER steps stay idempotent.
	if len(applied) == 0 {
		exists, err := s.tableExists("domains")
		if err != nil {
			return err
		}
		if exists {
			if err := s.stampMigration(1, "baseline_core"); err != nil {
				return err
			}
			applied[1] = true
		}
	}

	for _, m := range schemaMigrations() {
		if applied[m.version] {
			continue
		}
		if err := s.applyMigration(m); err != nil {
			// Another process may have applied the same version concurrently.
			if again, aerr := s.appliedMigrations(); aerr == nil && again[m.version] {
				applied[m.version] = true
				continue
			}
			return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
		}
		applied[m.version] = true
	}
	return nil
}

func schemaMigrations() []migration {
	return []migration{
		{1, "baseline_core", migrateV1Baseline},
		{2, "mailbox_content_rev_and_quotas", migrateV2Columns},
		{3, "tls_rpt_reports", migrateV3TLSRPT},
	}
}

func migrateV1Baseline(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS domains (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  enabled INTEGER NOT NULL DEFAULT 1,
  catch_all TEXT NOT NULL DEFAULT '',
  dkim_selector TEXT NOT NULL DEFAULT 'wernan',
  dkim_private TEXT NOT NULL DEFAULT '',
  dkim_public TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS mailboxes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
  local_part TEXT NOT NULL COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  quota_bytes INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  UNIQUE(domain_id, local_part)
);
CREATE TABLE IF NOT EXISTS aliases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
  local_part TEXT NOT NULL COLLATE NOCASE,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 1,
  UNIQUE(domain_id, local_part)
);
CREATE TABLE IF NOT EXISTS folder_uid (
  mailbox_id INTEGER NOT NULL,
  folder TEXT NOT NULL,
  next_uid INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(mailbox_id, folder)
);
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  folder TEXT NOT NULL,
  uid INTEGER NOT NULL,
  message_id TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  from_addr TEXT NOT NULL DEFAULT '',
  to_addrs TEXT NOT NULL DEFAULT '',
  date TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  flags_json TEXT NOT NULL DEFAULT '[]',
  maildir_rel TEXT NOT NULL,
  spam_score REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  UNIQUE(mailbox_id, folder, uid)
);
CREATE TABLE IF NOT EXISTS quarantine (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mailbox_id INTEGER NOT NULL DEFAULT 0,
  maildir_rel TEXT NOT NULL,
  subject TEXT NOT NULL DEFAULT '',
  from_addr TEXT NOT NULL DEFAULT '',
  verdict_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  resolved_at TEXT,
  resolution TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS queue_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 8,
  next_at TEXT NOT NULL,
  locked_until TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_queue_next ON queue_jobs(next_at);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS spam_signals (
  key TEXT PRIMARY KEY,
  weight REAL NOT NULL,
  hits INTEGER NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS dmarc_reports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  org_name TEXT NOT NULL,
  report_id TEXT NOT NULL DEFAULT '',
  date_begin TEXT NOT NULL,
  date_end TEXT NOT NULL,
  source_ip TEXT NOT NULL,
  message_count INTEGER NOT NULL DEFAULT 0,
  dkim_result TEXT NOT NULL DEFAULT '',
  spf_result TEXT NOT NULL DEFAULT '',
  disposition TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(mailbox_id, report_id, date_begin, date_end, source_ip, dkim_result, spf_result, disposition)
);
CREATE INDEX IF NOT EXISTS idx_dmarc_reports_created ON dmarc_reports(id DESC);
CREATE TABLE IF NOT EXISTS mail_filters (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 1,
  priority INTEGER NOT NULL DEFAULT 0,
  match_field TEXT NOT NULL,
  match_op TEXT NOT NULL,
  match_value TEXT NOT NULL,
  action TEXT NOT NULL,
  action_arg TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_mail_filters_mailbox ON mail_filters(mailbox_id, priority, id);
`)
	return err
}

func migrateV2Columns(tx *sql.Tx) error {
	// Idempotent ALTERs for upgrades from pre-versioned installs.
	_, _ = tx.Exec(`ALTER TABLE folder_uid ADD COLUMN uid_validity INTEGER NOT NULL DEFAULT 1`)
	_, _ = tx.Exec(`ALTER TABLE domains ADD COLUMN default_quota_bytes INTEGER NOT NULL DEFAULT 0`)
	_, _ = tx.Exec(`ALTER TABLE mailboxes ADD COLUMN content_rev INTEGER NOT NULL DEFAULT 0`)
	return nil
}

func migrateV3TLSRPT(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS tls_rpt_reports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  org_name TEXT NOT NULL DEFAULT '',
  report_id TEXT NOT NULL DEFAULT '',
  date_begin TEXT NOT NULL DEFAULT '',
  date_end TEXT NOT NULL DEFAULT '',
  policy_domain TEXT NOT NULL DEFAULT '',
  success_count INTEGER NOT NULL DEFAULT 0,
  failure_count INTEGER NOT NULL DEFAULT 0,
  result_type TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(mailbox_id, report_id, date_begin, date_end, policy_domain, result_type)
);
CREATE INDEX IF NOT EXISTS idx_tls_rpt_reports_created ON tls_rpt_reports(id DESC);
`)
	return err
}

func (s *Store) applyMigration(m migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := m.up(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?,?,?)`,
		m.version, m.name, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) stampMigration(version int, name string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO schema_migrations(version, name, applied_at) VALUES(?,?,?)`,
		version, name, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) appliedMigrations() (map[int]bool, error) {
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func (s *Store) tableExists(name string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	return n > 0, err
}

// SchemaVersion returns the highest applied migration version.
func (s *Store) SchemaVersion() (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}
