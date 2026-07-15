// Package domain holds mail-server entities (Phase 2).
package domain

import "time"

// Domain is a hosted mail domain.
type Domain struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Enabled           bool      `json:"enabled"`
	CatchAll          string    `json:"catchAll"`
	DefaultQuotaBytes int64     `json:"defaultQuotaBytes"`
	DKIMSelector      string    `json:"dkimSelector"`
	DKIMPrivate       string    `json:"-"`
	DKIMPublic        string    `json:"dkimPublic"`
	CreatedAt         time.Time `json:"createdAt"`
}

// Mailbox is a user mailbox under a domain.
type Mailbox struct {
	ID           int64     `json:"id"`
	DomainID     int64     `json:"domainId"`
	LocalPart    string    `json:"localPart"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"displayName"`
	QuotaBytes   int64     `json:"quotaBytes"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Address returns local@domain when domain name is known.
func (m Mailbox) Address(domainName string) string {
	return m.LocalPart + "@" + domainName
}

// Alias forwards one address to a mailbox.
type Alias struct {
	ID        int64
	DomainID  int64
	LocalPart string
	MailboxID int64
	Enabled   bool
}

// Folder names used by the store.
const (
	FolderInbox   = "INBOX"
	FolderSent    = "Sent"
	FolderDrafts  = "Drafts"
	FolderSpam    = "Spam"
	FolderTrash   = "Trash"
	FolderQuarantine = "Quarantine"
)

// Message is metadata for one stored mail.
type Message struct {
	ID         int64
	MailboxID  int64
	Folder     string
	UID        uint32
	MessageID  string
	Subject    string
	FromAddr   string
	ToAddrs    string // comma-separated
	Date       time.Time
	Size       int64
	Flags      []string
	MaildirRel string // relative path under maildir root
	SpamScore  float64
	CreatedAt  time.Time
}

// QueueJobKind classifies worker work.
type QueueJobKind string

const (
	JobInboundDeliver QueueJobKind = "inbound_deliver"
	JobOutboundSend   QueueJobKind = "outbound_send"
	JobBounce         QueueJobKind = "bounce"
)

// QueueJob is a durable unit of work.
type QueueJob struct {
	ID          int64        `json:"id"`
	Kind        QueueJobKind `json:"kind"`
	PayloadJSON string       `json:"payloadJson"`
	Attempts    int          `json:"attempts"`
	MaxAttempts int          `json:"maxAttempts"`
	NextAt      time.Time    `json:"nextAt"`
	LockedUntil *time.Time   `json:"lockedUntil,omitempty"`
	LastError   string       `json:"lastError"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// SpamAction is the antispam decision.
type SpamAction string

const (
	SpamDeliver    SpamAction = "deliver"
	SpamQuarantine SpamAction = "quarantine"
	SpamReject     SpamAction = "reject"
	SpamFlag       SpamAction = "flag"
)

// SpamReason explains one scoring contribution.
type SpamReason struct {
	Code   string  `json:"code"`
	Detail string  `json:"detail"`
	Score  float64 `json:"score"`
}

// SpamVerdict is the engine result for one message.
type SpamVerdict struct {
	Score   float64      `json:"score"`
	Action  SpamAction   `json:"action"`
	Reasons []SpamReason `json:"reasons"`
}

// QuarantineItem is a held message awaiting admin action.
type QuarantineItem struct {
	ID          int64      `json:"id"`
	MailboxID   int64      `json:"mailboxId"`
	MaildirRel  string     `json:"-"`
	Subject     string     `json:"subject"`
	FromAddr    string     `json:"fromAddr"`
	VerdictJSON string     `json:"verdictJson"`
	CreatedAt   time.Time  `json:"createdAt"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	Resolution  string     `json:"resolution"`
}

// AuditEntry is an admin/system audit row.
type AuditEntry struct {
	ID        int64     `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
}

// Setting is a typed key/value override.
type Setting struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}
