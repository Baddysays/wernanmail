package imapd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/backendutil"
	"github.com/emersion/go-imap/server"
	"github.com/emersion/go-message/textproto"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/store"
)

// Backend is an IMAP backend over MessageStore.
type Backend struct {
	Store store.MessageStore
}

func (b *Backend) Login(_ *imap.ConnInfo, username, password string) (backend.User, error) {
	m, d, err := b.Store.AuthenticateMailbox(context.Background(), username, password)
	if err != nil || m == nil || d == nil {
		return nil, errors.New("invalid credentials")
	}
	return &User{store: b.Store, mailbox: m, domain: d}, nil
}

// User is a logged-in mailbox.
type User struct {
	store   store.MessageStore
	mailbox *domain.Mailbox
	domain  *domain.Domain
}

func (u *User) Username() string { return u.mailbox.Address(u.domain.Name) }

func (u *User) ListMailboxes(subscribed bool) ([]backend.Mailbox, error) {
	_ = subscribed
	names := []string{
		domain.FolderInbox, domain.FolderSent, domain.FolderDrafts,
		domain.FolderSpam, domain.FolderTrash, domain.FolderQuarantine,
	}
	out := make([]backend.Mailbox, 0, len(names))
	for _, n := range names {
		out = append(out, &Mailbox{user: u, name: n})
	}
	return out, nil
}

func (u *User) GetMailbox(name string) (backend.Mailbox, error) {
	return &Mailbox{user: u, name: name}, nil
}

func (u *User) CreateMailbox(name string) error { _ = name; return nil }
func (u *User) DeleteMailbox(name string) error { _ = name; return backend.ErrNoSuchMailbox }
func (u *User) RenameMailbox(existing, newName string) error {
	_ = existing
	_ = newName
	return backend.ErrNoSuchMailbox
}
func (u *User) Logout() error { return nil }

// Mailbox is one IMAP folder.
type Mailbox struct {
	user *User
	name string
}

func (m *Mailbox) Name() string              { return m.name }
func (m *Mailbox) Info() (*imap.MailboxInfo, error) {
	return &imap.MailboxInfo{Delimiter: "/", Name: m.name}, nil
}

func (m *Mailbox) Status(items []imap.StatusItem) (*imap.MailboxStatus, error) {
	msgs, err := m.user.store.ListMessages(context.Background(), m.user.mailbox.ID, m.name, 10000)
	if err != nil {
		return nil, err
	}
	status := imap.NewMailboxStatus(m.name, items)
	status.Flags = []string{imap.SeenFlag, imap.FlaggedFlag, imap.DeletedFlag}
	status.PermanentFlags = []string{"\\*"}
	status.Messages = uint32(len(msgs))
	unseen := uint32(0)
	var uidNext uint32 = 1
	for _, msg := range msgs {
		if !hasFlag(msg.Flags, imap.SeenFlag) {
			unseen++
		}
		if msg.UID+1 > uidNext {
			uidNext = msg.UID + 1
		}
	}
	status.Unseen = unseen
	status.UidNext = uidNext
	status.UidValidity = 1
	return status, nil
}

func (m *Mailbox) SetSubscribed(bool) error { return nil }
func (m *Mailbox) Check() error             { return nil }

func (m *Mailbox) ListMessages(uid bool, seqSet *imap.SeqSet, items []imap.FetchItem, ch chan<- *imap.Message) error {
	defer close(ch)
	msgs, err := m.user.store.ListMessages(context.Background(), m.user.mailbox.ID, m.name, 5000)
	if err != nil {
		return err
	}
	// ListMessages returns newest first; IMAP seq is oldest-first.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	for i, msg := range msgs {
		seqNum := uint32(i + 1)
		id := seqNum
		if uid {
			id = msg.UID
		}
		if seqSet != nil && !seqSet.Contains(id) {
			continue
		}
		im, err := m.fetchOne(&msg, seqNum, items)
		if err != nil {
			continue
		}
		ch <- im
	}
	return nil
}

func (m *Mailbox) fetchOne(msg *domain.Message, seqNum uint32, items []imap.FetchItem) (*imap.Message, error) {
	im := imap.NewMessage(seqNum, items)
	im.Uid = msg.UID
	needBody := false
	for _, item := range items {
		switch item {
		case imap.FetchFlags:
			im.Flags = msg.Flags
		case imap.FetchEnvelope:
			im.Envelope = &imap.Envelope{
				Subject:   msg.Subject,
				Date:      msg.Date,
				From:      []*imap.Address{parseAddr(msg.FromAddr)},
				To:        parseAddrList(msg.ToAddrs),
				MessageId: msg.MessageID,
			}
		case imap.FetchBodyStructure, imap.FetchBody:
			needBody = true
		case imap.FetchRFC822Size:
			im.Size = uint32(msg.Size)
		case imap.FetchUid:
			im.Uid = msg.UID
		case imap.FetchInternalDate:
			im.InternalDate = msg.Date
		default:
			if strings.HasPrefix(string(item), "BODY") || strings.HasPrefix(string(item), "RFC822") {
				needBody = true
			}
		}
	}
	var raw []byte
	if needBody {
		_, raw, _ = m.user.store.GetMessage(context.Background(), m.user.mailbox.ID, msg.UID)
	}
	if im.Body == nil {
		im.Body = map[*imap.BodySectionName]imap.Literal{}
	}
	for _, item := range items {
		switch item {
		case imap.FetchRFC822, imap.FetchRFC822Header, imap.FetchRFC822Text:
			if raw != nil {
				sect, err := imap.ParseBodySectionName(item)
				if err == nil {
					im.Body[sect] = &peekLiteral{b: raw}
				}
			}
		default:
			if strings.HasPrefix(string(item), "BODY[") || strings.HasPrefix(string(item), "BODY.PEEK[") {
				section, err := imap.ParseBodySectionName(item)
				if err != nil || raw == nil {
					continue
				}
				hdr, body, err := splitRFC822(raw)
				if err != nil {
					im.Body[section] = &peekLiteral{b: raw}
					continue
				}
				l, err := backendutil.FetchBodySection(hdr, body, section)
				if err != nil {
					im.Body[section] = &peekLiteral{b: raw}
					continue
				}
				im.Body[section] = l
			}
		}
	}
	return im, nil
}

func splitRFC822(raw []byte) (textproto.Header, io.Reader, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return textproto.Header{}, nil, err
	}
	h := textproto.Header{}
	for k, vals := range msg.Header {
		for _, v := range vals {
			h.Add(k, v)
		}
	}
	return h, msg.Body, nil
}

type peekLiteral struct{ b []byte }

func (p *peekLiteral) Len() int                           { return len(p.b) }
func (p *peekLiteral) Read(b []byte) (int, error)         { return copy(b, p.b), io.EOF }
func (p *peekLiteral) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(p.b)
	return int64(n), err
}

func (m *Mailbox) SearchMessages(uid bool, criteria *imap.SearchCriteria) ([]uint32, error) {
	msgs, err := m.user.store.ListMessages(context.Background(), m.user.mailbox.ID, m.name, 5000)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	var out []uint32
	for i, msg := range msgs {
		if criteria != nil && len(criteria.WithFlags) > 0 {
			ok := true
			for _, f := range criteria.WithFlags {
				if !hasFlag(msg.Flags, f) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		if uid {
			out = append(out, msg.UID)
		} else {
			out = append(out, uint32(i+1))
		}
	}
	return out, nil
}

func (m *Mailbox) CreateMessage(flags []string, date time.Time, body imap.Literal) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	msg := &domain.Message{
		MailboxID: m.user.mailbox.ID,
		Folder:    m.name,
		Flags:     flags,
		Date:      date,
		Subject:   "",
		FromAddr:  "",
	}
	return m.user.store.AppendMessage(context.Background(), msg, raw)
}

func (m *Mailbox) UpdateMessagesFlags(uid bool, seqSet *imap.SeqSet, op imap.FlagsOp, flags []string) error {
	msgs, err := m.user.store.ListMessages(context.Background(), m.user.mailbox.ID, m.name, 5000)
	if err != nil {
		return err
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	for i, msg := range msgs {
		id := uint32(i + 1)
		if uid {
			id = msg.UID
		}
		if seqSet != nil && !seqSet.Contains(id) {
			continue
		}
		switch op {
		case imap.AddFlags:
			_ = m.user.store.UpdateFlags(context.Background(), m.user.mailbox.ID, msg.UID, flags, nil)
		case imap.RemoveFlags:
			_ = m.user.store.UpdateFlags(context.Background(), m.user.mailbox.ID, msg.UID, nil, flags)
		case imap.SetFlags:
			_ = m.user.store.UpdateFlags(context.Background(), m.user.mailbox.ID, msg.UID, flags, msg.Flags)
		}
	}
	return nil
}

func (m *Mailbox) CopyMessages(uid bool, seqSet *imap.SeqSet, destName string) error {
	_ = uid
	_ = seqSet
	_ = destName
	return errors.New("COPY not implemented")
}

func (m *Mailbox) Expunge() error { return nil }

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}

func parseAddr(s string) *imap.Address {
	s = strings.TrimSpace(s)
	if s == "" {
		return &imap.Address{}
	}
	addr, err := mail.ParseAddress(s)
	if err != nil {
		return &imap.Address{MailboxName: s}
	}
	local, domain := "", ""
	if i := strings.LastIndex(addr.Address, "@"); i >= 0 {
		local, domain = addr.Address[:i], addr.Address[i+1:]
	} else {
		local = addr.Address
	}
	return &imap.Address{PersonalName: addr.Name, MailboxName: local, HostName: domain}
}

func parseAddrList(s string) []*imap.Address {
	parts := strings.Split(s, ",")
	out := make([]*imap.Address, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, parseAddr(p))
		}
	}
	return out
}

// ListenAndServe starts IMAP.
func ListenAndServe(addr string, be *Backend) error {
	s := server.New(be)
	s.Addr = addr
	s.AllowInsecureAuth = true
	log.Printf("imap listening on %s", addr)
	return s.ListenAndServe()
}
