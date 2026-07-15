// Package store defines persistence for the Phase 2 mail server.
package store

import (
	"context"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/domain"
)

// MessageStore is mailbox / message metadata + body paths.
type MessageStore interface {
	Close() error

	// Domains
	ListDomains(ctx context.Context) ([]domain.Domain, error)
	GetDomainByName(ctx context.Context, name string) (*domain.Domain, error)
	UpsertDomain(ctx context.Context, d *domain.Domain) error

	// Mailboxes
	ListMailboxes(ctx context.Context, domainID int64) ([]domain.Mailbox, error)
	GetMailbox(ctx context.Context, domainName, localPart string) (*domain.Mailbox, *domain.Domain, error)
	GetMailboxByID(ctx context.Context, id int64) (*domain.Mailbox, error)
	UpsertMailbox(ctx context.Context, m *domain.Mailbox) error
	AuthenticateMailbox(ctx context.Context, address, password string) (*domain.Mailbox, *domain.Domain, error)

	// Aliases
	ListAliases(ctx context.Context, domainID int64) ([]domain.Alias, error)
	UpsertAlias(ctx context.Context, a *domain.Alias) error
	ResolveRecipient(ctx context.Context, address string) (mailboxID int64, err error)

	// Messages
	NextUID(ctx context.Context, mailboxID int64, folder string) (uint32, error)
	FolderStats(ctx context.Context, mailboxID int64, folder string) (messages, unseen uint32, uidNext, uidValidity uint32, err error)
	AppendMessage(ctx context.Context, msg *domain.Message, raw []byte) error
	ListMessages(ctx context.Context, mailboxID int64, folder string, limit int) ([]domain.Message, error)
	GetMessage(ctx context.Context, mailboxID int64, folder string, uid uint32) (*domain.Message, []byte, error)
	UpdateFlags(ctx context.Context, mailboxID int64, folder string, uid uint32, add, remove []string) error
	MoveMessage(ctx context.Context, mailboxID int64, folder string, uid uint32, toFolder string) error
	CopyMessage(ctx context.Context, mailboxID int64, folder string, uid uint32, toFolder string) (newUID uint32, err error)
	DeleteMessage(ctx context.Context, mailboxID int64, folder string, uid uint32) error
	ExpungeDeleted(ctx context.Context, mailboxID int64, folder string) (int, error)
	UsageBytes(ctx context.Context, mailboxID int64) (int64, error)

	// Quarantine
	AddQuarantine(ctx context.Context, q *domain.QuarantineItem, raw []byte) error
	ListQuarantine(ctx context.Context, limit int) ([]domain.QuarantineItem, error)
	ResolveQuarantine(ctx context.Context, id int64, resolution string) error
	GetQuarantineRaw(ctx context.Context, id int64) (*domain.QuarantineItem, []byte, error)
	PurgeQuarantineOlderThan(ctx context.Context, olderThan time.Time, limit int) (int, error)
	DeleteMessagesOlderThan(ctx context.Context, olderThan time.Time, limit int) (int, error)

	// Settings & audit
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	ListSettings(ctx context.Context) ([]domain.Setting, error)
	AddAudit(ctx context.Context, e *domain.AuditEntry) error
	ListAudit(ctx context.Context, limit int) ([]domain.AuditEntry, error)
}

// QueueStore persists durable jobs.
type QueueStore interface {
	Enqueue(ctx context.Context, job *domain.QueueJob) error
	Claim(ctx context.Context, workerID string, lease time.Duration) (*domain.QueueJob, error)
	Complete(ctx context.Context, id int64) error
	Fail(ctx context.Context, id int64, errMsg string, retryAt time.Time, dead bool) error
	Retry(ctx context.Context, id int64) error
	DeleteJob(ctx context.Context, id int64) error
	List(ctx context.Context, limit int) ([]domain.QueueJob, error)
	Count(ctx context.Context) (pending, dead int, err error)
}
