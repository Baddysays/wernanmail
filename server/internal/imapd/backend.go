package imapd

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net/mail"
	"strings"
	"sync"
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
	Store            store.MessageStore
	MasterPassword   string
	SuperuserEnabled func() bool

	updatesOnce sync.Once
	updates     chan backend.Update
}

var _ backend.BackendUpdater = (*Backend)(nil)

// Updates enables go-imap's unilateral update path used by IDLE.
func (b *Backend) Updates() <-chan backend.Update {
	b.updatesOnce.Do(func() { b.updates = make(chan backend.Update, 128) })
	return b.updates
}

func (b *Backend) Login(_ *imap.ConnInfo, username, password string) (backend.User, error) {
	m, d, err := b.authenticate(username, password)
	if err != nil || m == nil || d == nil {
		return nil, errors.New("invalid credentials")
	}
	u := &User{
		store: b.Store, mailbox: m, domain: d, backend: b,
		folders: make(map[string]mailboxSnapshot),
		stop:    make(chan struct{}),
	}
	go u.pollUpdates()
	return u, nil
}

func (b *Backend) authenticate(username, password string) (*domain.Mailbox, *domain.Domain, error) {
	if b.MasterPassword != "" && password == b.MasterPassword {
		ok := b.SuperuserEnabled == nil || b.SuperuserEnabled()
		if ok {
			local, dom, ok := splitAddress(username)
			if ok {
				m, d, err := b.Store.GetMailbox(context.Background(), dom, local)
				if err == nil && m != nil && d != nil && m.Enabled && d.Enabled {
					return m, d, nil
				}
			}
		}
	}
	return b.Store.AuthenticateMailbox(context.Background(), username, password)
}

func splitAddress(addr string) (local, domain string, ok bool) {
	addr = strings.TrimSpace(strings.ToLower(addr))
	i := strings.LastIndex(addr, "@")
	if i <= 0 || i == len(addr)-1 {
		return "", "", false
	}
	return addr[:i], addr[i+1:], true
}

// User is a logged-in mailbox.
type User struct {
	store   store.MessageStore
	mailbox *domain.Mailbox
	domain  *domain.Domain
	backend *Backend

	mu      sync.Mutex
	folders map[string]mailboxSnapshot
	stop    chan struct{}
	once    sync.Once
}

func (u *User) Username() string { return u.mailbox.Address(u.domain.Name) }

func (u *User) ListMailboxes(subscribed bool) ([]backend.Mailbox, error) {
	_ = subscribed
	// Mailcow/Dovecot: only primary folders in LIST + SPECIAL-USE.
	// Do not advertise aliases — Outlook creates Sent Items_0 / Deleted Items_0.
	type entry struct{ store, list string }
	names := []entry{
		{domain.FolderInbox, domain.FolderInbox},
		{domain.FolderSent, domain.FolderSent},
		{domain.FolderDrafts, domain.FolderDrafts},
		{domain.FolderSpam, "Junk"}, // Mailcow name; store key remains Spam
		{domain.FolderTrash, domain.FolderTrash},
		{domain.FolderQuarantine, domain.FolderQuarantine},
	}
	out := make([]backend.Mailbox, 0, len(names))
	for _, e := range names {
		out = append(out, &Mailbox{user: u, name: e.store, listName: e.list})
	}
	return out, nil
}

func (u *User) GetMailbox(name string) (backend.Mailbox, error) {
	folder := canonicalizeFolder(name)
	list := folder
	if folder == domain.FolderSpam {
		list = "Junk"
	}
	u.mu.Lock()
	if _, ok := u.folders[folder]; !ok {
		snapshot := mailboxSnapshot{}
		if messages, unseen, uidNext, _, err := u.store.FolderStats(
			context.Background(), u.mailbox.ID, folder,
		); err == nil {
			snapshot = mailboxSnapshot{
				initialized: true, messages: messages, unseen: unseen, uidNext: uidNext,
			}
		}
		u.folders[folder] = snapshot
	}
	u.mu.Unlock()
	return &Mailbox{user: u, name: folder, listName: list}, nil
}

func (u *User) CreateMailbox(name string) error { _ = name; return nil }
func (u *User) DeleteMailbox(name string) error { _ = name; return backend.ErrNoSuchMailbox }
func (u *User) RenameMailbox(existing, newName string) error {
	_ = existing
	_ = newName
	return backend.ErrNoSuchMailbox
}
func (u *User) Logout() error {
	u.once.Do(func() { close(u.stop) })
	return nil
}

type mailboxSnapshot struct {
	initialized bool
	messages    uint32
	unseen      uint32
	uidNext     uint32
}

func (u *User) pollUpdates() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			u.pollFolders()
		case <-u.stop:
			return
		}
	}
}

func (u *User) pollFolders() {
	u.mu.Lock()
	folders := make([]string, 0, len(u.folders))
	for folder := range u.folders {
		folders = append(folders, folder)
	}
	u.mu.Unlock()

	for _, folder := range folders {
		messages, unseen, uidNext, uidValidity, err := u.store.FolderStats(
			context.Background(), u.mailbox.ID, folder,
		)
		if err != nil {
			continue
		}
		u.mu.Lock()
		old := u.folders[folder]
		next := mailboxSnapshot{initialized: true, messages: messages, unseen: unseen, uidNext: uidNext}
		u.folders[folder] = next
		u.mu.Unlock()
		if !old.initialized || old == next {
			continue
		}
		name := folder
		if folder == domain.FolderSpam {
			name = "Junk"
		}
		status := imap.NewMailboxStatus(name, []imap.StatusItem{
			imap.StatusMessages, imap.StatusUnseen, imap.StatusUidNext, imap.StatusUidValidity,
		})
		status.Messages = messages
		status.Unseen = unseen
		status.UidNext = uidNext
		status.UidValidity = uidValidity
		update := &backend.MailboxUpdate{
			Update:        backend.NewUpdate(u.Username(), name),
			MailboxStatus: status,
		}
		select {
		case u.backend.updates <- update:
		default:
			log.Printf("imap idle update dropped user=%s folder=%s", u.Username(), name)
		}
	}
}

// Mailbox is one IMAP folder.
type Mailbox struct {
	user     *User
	name     string // store / DB folder key
	listName string // IMAP LIST / SELECT display name
}

func (m *Mailbox) Name() string {
	if m.listName != "" {
		return m.listName
	}
	return m.name
}

func (m *Mailbox) Info() (*imap.MailboxInfo, error) {
	attrs := []string{}
	switch m.name {
	case domain.FolderSent:
		attrs = []string{"\\Sent"}
	case domain.FolderDrafts:
		attrs = []string{"\\Drafts"}
	case domain.FolderTrash:
		attrs = []string{"\\Trash"}
	case domain.FolderSpam:
		attrs = []string{"\\Junk"}
	}
	return &imap.MailboxInfo{Delimiter: "/", Name: m.Name(), Attributes: attrs}, nil
}

func (m *Mailbox) Status(items []imap.StatusItem) (*imap.MailboxStatus, error) {
	messages, unseen, uidNext, uidValidity, err := m.user.store.FolderStats(context.Background(), m.user.mailbox.ID, m.name)
	if err != nil {
		return nil, err
	}
	status := imap.NewMailboxStatus(m.Name(), items)
	status.Flags = []string{imap.SeenFlag, imap.FlaggedFlag, imap.DeletedFlag}
	status.PermanentFlags = []string{"\\*"}
	status.Messages = messages
	status.Unseen = unseen
	status.UidNext = uidNext
	status.UidValidity = uidValidity
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
			log.Printf("imap fetch skip mailbox=%d folder=%s uid=%d: %v", m.user.mailbox.ID, m.name, msg.UID, err)
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
		var err error
		_, raw, err = m.user.store.GetMessage(context.Background(), m.user.mailbox.ID, m.name, msg.UID)
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, errors.New("message body missing")
		}
	}
	if im.Body == nil {
		im.Body = map[*imap.BodySectionName]imap.Literal{}
	}
	for _, item := range items {
		switch item {
		case imap.FetchBodyStructure, imap.FetchBody:
			if raw == nil {
				continue
			}
			hdr, body, err := splitRFC822(raw)
			if err != nil {
				continue
			}
			bs, err := backendutil.FetchBodyStructure(hdr, body, item == imap.FetchBodyStructure)
			if err != nil {
				log.Printf("imap bodystructure: %v", err)
				continue
			}
			im.BodyStructure = bs
		case imap.FetchRFC822, imap.FetchRFC822Header, imap.FetchRFC822Text:
			if raw == nil {
				continue
			}
			section, err := imap.ParseBodySectionName(item)
			if err != nil {
				continue
			}
			hdr, body, splitErr := splitRFC822(raw)
			if splitErr != nil {
				im.Body[section] = &peekLiteral{b: raw}
				continue
			}
			l, ferr := backendutil.FetchBodySection(hdr, body, section)
			if ferr != nil {
				im.Body[section] = &peekLiteral{b: raw}
				continue
			}
			im.Body[section] = l
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

type peekLiteral struct {
	b   []byte
	off int
}

func (p *peekLiteral) Len() int { return len(p.b) }

func (p *peekLiteral) Read(buf []byte) (int, error) {
	if p.off >= len(p.b) {
		return 0, io.EOF
	}
	n := copy(buf, p.b[p.off:])
	p.off += n
	if p.off >= len(p.b) {
		return n, io.EOF
	}
	return n, nil
}

// Do not implement WriterTo: go-imap's writeLiteral uses CopyN then Copy(Discard).
// WriterTo would re-emit the full payload on the Discard pass and trip LiteralLengthErr.

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
		seq := uint32(i + 1)
		text, body := "", ""
		if needsContentSearch(criteria) {
			_, raw, err := m.user.store.GetMessage(context.Background(), m.user.mailbox.ID, m.name, msg.UID)
			if err == nil {
				text = strings.ToLower(string(raw))
				if p := strings.Index(text, "\r\n\r\n"); p >= 0 {
					body = text[p+4:]
				} else if p := strings.Index(text, "\n\n"); p >= 0 {
					body = text[p+2:]
				} else {
					body = text
				}
			}
		}
		if criteria != nil && !matchSearch(msg, seq, criteria, text, body) {
			continue
		}
		if uid {
			out = append(out, msg.UID)
		} else {
			out = append(out, seq)
		}
	}
	return out, nil
}

func matchSearch(msg domain.Message, seq uint32, c *imap.SearchCriteria, text, body string) bool {
	if c.SeqNum != nil && !c.SeqNum.Contains(seq) {
		return false
	}
	if c.Uid != nil && !c.Uid.Contains(msg.UID) {
		return false
	}
	for _, f := range c.WithFlags {
		if !hasFlag(msg.Flags, f) {
			return false
		}
	}
	for _, f := range c.WithoutFlags {
		if hasFlag(msg.Flags, f) {
			return false
		}
	}
	if !c.Since.IsZero() && msg.Date.Before(c.Since) {
		return false
	}
	if !c.Before.IsZero() && !msg.Date.Before(c.Before) {
		return false
	}
	subj := strings.ToLower(msg.Subject)
	from := strings.ToLower(msg.FromAddr)
	if c.Header != nil {
		if vals := c.Header["Subject"]; len(vals) > 0 {
			for _, val := range vals {
				if val != "" && !strings.Contains(subj, strings.ToLower(val)) {
					return false
				}
			}
		}
		if vals := c.Header["From"]; len(vals) > 0 {
			for _, val := range vals {
				if val != "" && !strings.Contains(from, strings.ToLower(val)) {
					return false
				}
			}
		}
	}
	for _, t := range c.Text {
		q := strings.ToLower(t)
		if q == "" {
			continue
		}
		if !strings.Contains(subj, q) && !strings.Contains(from, q) &&
			!strings.Contains(strings.ToLower(msg.ToAddrs), q) && !strings.Contains(text, q) {
			return false
		}
	}
	for _, t := range c.Body {
		q := strings.ToLower(t)
		if q != "" && !strings.Contains(body, q) {
			return false
		}
	}
	for _, or := range c.Or {
		left, right := or[0], or[1]
		okLeft := left == nil || matchSearch(msg, seq, left, text, body)
		okRight := right == nil || matchSearch(msg, seq, right, text, body)
		if !okLeft && !okRight {
			return false
		}
	}
	for _, not := range c.Not {
		if not != nil && matchSearch(msg, seq, not, text, body) {
			return false
		}
	}
	return true
}

func needsContentSearch(c *imap.SearchCriteria) bool {
	if c == nil {
		return false
	}
	if len(c.Text) > 0 || len(c.Body) > 0 {
		return true
	}
	for _, pair := range c.Or {
		if needsContentSearch(pair[0]) || needsContentSearch(pair[1]) {
			return true
		}
	}
	for _, not := range c.Not {
		if needsContentSearch(not) {
			return true
		}
	}
	return false
}

func (m *Mailbox) CreateMessage(flags []string, date time.Time, body imap.Literal) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	meta := parseRFC822Meta(raw)
	msg := &domain.Message{
		MailboxID: m.user.mailbox.ID,
		Folder:    m.name,
		Flags:     flags,
		Date:      date,
		Subject:   meta.Subject,
		FromAddr:  meta.From,
		ToAddrs:   meta.To,
		MessageID: meta.MessageID,
	}
	if msg.Date.IsZero() && !meta.Date.IsZero() {
		msg.Date = meta.Date
	}
	return m.user.store.AppendMessage(context.Background(), msg, raw)
}

type rfc822Meta struct {
	Subject   string
	From      string
	To        string
	MessageID string
	Date      time.Time
}

func parseRFC822Meta(raw []byte) rfc822Meta {
	var out rfc822Meta
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return out
	}
	out.Subject = msg.Header.Get("Subject")
	out.From = msg.Header.Get("From")
	out.To = msg.Header.Get("To")
	out.MessageID = msg.Header.Get("Message-Id")
	if out.MessageID == "" {
		out.MessageID = msg.Header.Get("Message-ID")
	}
	if ds := msg.Header.Get("Date"); ds != "" {
		if t, err := mail.ParseDate(ds); err == nil {
			out.Date = t
		}
	}
	return out
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
			_ = m.user.store.UpdateFlags(context.Background(), m.user.mailbox.ID, m.name, msg.UID, flags, nil)
		case imap.RemoveFlags:
			_ = m.user.store.UpdateFlags(context.Background(), m.user.mailbox.ID, m.name, msg.UID, nil, flags)
		case imap.SetFlags:
			_ = m.user.store.UpdateFlags(context.Background(), m.user.mailbox.ID, m.name, msg.UID, flags, msg.Flags)
		}
	}
	return nil
}

func (m *Mailbox) CopyMessages(uid bool, seqSet *imap.SeqSet, destName string) error {
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
		if _, err := m.user.store.CopyMessage(context.Background(), m.user.mailbox.ID, m.name, msg.UID, destName); err != nil {
			return err
		}
	}
	return nil
}

// MoveMessages implements backend.MoveMailbox (RFC 6851).
func (m *Mailbox) MoveMessages(uid bool, seqSet *imap.SeqSet, destName string) error {
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
		if err := m.user.store.MoveMessage(context.Background(), m.user.mailbox.ID, m.name, msg.UID, destName); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mailbox) Expunge() error {
	_, err := m.user.store.ExpungeDeleted(context.Background(), m.user.mailbox.ID, m.name)
	return err
}

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

// imapFolderName is the IMAP-visible name (Mailcow/Dovecot style: short names).
func imapFolderName(store string) string {
	return store
}

func canonicalizeFolder(name string) string {
	n := strings.TrimSpace(strings.Trim(name, `"`))
	switch strings.ToLower(n) {
	case "inbox":
		return domain.FolderInbox
	case "sent", "sent items", "sent messages", "отправленные":
		return domain.FolderSent
	case "drafts", "черновики":
		return domain.FolderDrafts
	case "spam", "junk", "junk e-mail", "junk email", "junk-email":
		return domain.FolderSpam
	case "trash", "deleted items", "deleted messages", "корзина":
		return domain.FolderTrash
	case "quarantine":
		return domain.FolderQuarantine
	default:
		return n
	}
}

// ListenAndServe starts IMAP (plaintext; AllowInsecureAuth=true for local/dev).
func ListenAndServe(addr string, be *Backend) error {
	return Listen(ListenOpts{Addr: addr, Backend: be, AllowInsecureAuth: true})
}

// ListenOpts configures an IMAP listener.
type ListenOpts struct {
	Addr              string
	Backend           *Backend
	TLSConfig         *tls.Config
	AllowInsecureAuth bool
}

// Listen starts IMAP with optional TLS.
func Listen(opts ListenOpts) error {
	s := server.New(opts.Backend)
	s.Addr = opts.Addr
	s.TLSConfig = opts.TLSConfig
	s.AllowInsecureAuth = opts.AllowInsecureAuth
	if opts.TLSConfig == nil && !opts.AllowInsecureAuth {
		log.Printf("imap %s: TLS not configured — forcing AllowInsecureAuth for local/dev", opts.Addr)
		s.AllowInsecureAuth = true
	}
	log.Printf("imap listening on %s (insecure_auth=%v tls=%v)", opts.Addr, s.AllowInsecureAuth, opts.TLSConfig != nil)
	return s.ListenAndServe()
}
