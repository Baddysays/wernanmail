package mail

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	gomessage "github.com/emersion/go-message/mail"

	"github.com/Baddysays/wernanmail/server/internal/session"
)

// ConnectIMAP dials and authenticates. Caller must Close the client.
func ConnectIMAP(creds session.Credentials) (*client.Client, error) {
	addr := fmt.Sprintf("%s:%d", creds.IMAPHost, creds.IMAPPort)
	var (
		c   *client.Client
		err error
	)
	if creds.TLS {
		c, err = client.DialTLS(addr, nil)
	} else {
		c, err = client.Dial(addr)
	}
	if err != nil {
		return nil, fmt.Errorf("imap dial: %w", err)
	}
	if err := c.Login(creds.Username, creds.Password); err != nil {
		_ = c.Logout()
		return nil, fmt.Errorf("imap login: %w", err)
	}
	return c, nil
}

// VerifyLogin checks IMAP credentials by connecting and logging in.
func VerifyLogin(creds session.Credentials) error {
	c, err := ConnectIMAP(creds)
	if err != nil {
		return err
	}
	defer func() { _ = c.Logout() }()
	return nil
}

// ListFolders returns IMAP mailboxes.
func ListFolders(creds session.Credentials) ([]Folder, error) {
	c, err := ConnectIMAP(creds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Logout() }()

	mailboxes := make(chan *imap.MailboxInfo, 32)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", mailboxes)
	}()

	var folders []Folder
	for m := range mailboxes {
		attrs := make([]string, 0, len(m.Attributes))
		attrs = append(attrs, m.Attributes...)
		folders = append(folders, Folder{
			Name:       m.Name,
			Delimiter:  m.Delimiter,
			Attributes: attrs,
		})
	}
	if err := <-done; err != nil {
		return nil, err
	}
	return folders, nil
}

// ListMessages fetches recent message summaries from a folder.
func ListMessages(creds session.Credentials, folder string, limit uint32) ([]MessageSummary, error) {
	if folder == "" {
		folder = "INBOX"
	}
	if limit == 0 {
		limit = 50
	}

	c, err := ConnectIMAP(creds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Logout() }()

	mbox, err := c.Select(folder, true)
	if err != nil {
		return nil, err
	}
	if mbox.Messages == 0 {
		return []MessageSummary{}, nil
	}

	from := uint32(1)
	if mbox.Messages > limit {
		from = mbox.Messages - limit + 1
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchFlags,
		imap.FetchUid,
		imap.FetchRFC822Size,
		imap.FetchInternalDate,
	}

	messages := make(chan *imap.Message, int(limit))
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	var out []MessageSummary
	for msg := range messages {
		if msg == nil || msg.Envelope == nil {
			continue
		}
		date := msg.Envelope.Date
		if date.IsZero() && !msg.InternalDate.IsZero() {
			date = msg.InternalDate
		}
		sum := MessageSummary{
			ID:      fmt.Sprintf("%d", msg.Uid),
			UID:     msg.Uid,
			Subject: msg.Envelope.Subject,
			From:    mapAddresses(msg.Envelope.From),
			To:      mapAddresses(msg.Envelope.To),
			Date:    date,
			Flags:   msg.Flags,
			Size:    msg.Size,
		}
		out = append(out, sum)
	}
	if err := <-done; err != nil {
		return nil, err
	}

	// Newest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// GetMessage fetches one message by UID.
func GetMessage(creds session.Credentials, folder, id string) (*Message, error) {
	if folder == "" {
		folder = "INBOX"
	}
	var uid uint32
	if _, err := fmt.Sscanf(id, "%d", &uid); err != nil || uid == 0 {
		return nil, fmt.Errorf("invalid message id")
	}

	c, err := ConnectIMAP(creds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Logout() }()

	if _, err := c.Select(folder, true); err != nil {
		return nil, err
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchFlags,
		imap.FetchUid,
		imap.FetchRFC822Size,
		imap.FetchInternalDate,
		section.FetchItem(),
	}

	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.UidFetch(seqset, items, messages)
	}()

	var msg *imap.Message
	for m := range messages {
		msg = m
	}
	if err := <-done; err != nil {
		return nil, err
	}
	if msg == nil || msg.Envelope == nil {
		return nil, errNotFound
	}

	date := msg.Envelope.Date
	if date.IsZero() && !msg.InternalDate.IsZero() {
		date = msg.InternalDate
	}

	result := &Message{
		MessageSummary: MessageSummary{
			ID:      fmt.Sprintf("%d", msg.Uid),
			UID:     msg.Uid,
			Subject: msg.Envelope.Subject,
			From:    mapAddresses(msg.Envelope.From),
			To:      mapAddresses(msg.Envelope.To),
			Date:    date,
			Flags:   msg.Flags,
			Size:    msg.Size,
		},
		CC: mapAddresses(msg.Envelope.Cc),
	}

	r := msg.GetBody(section)
	if r != nil {
		text, html, rawSize, parseErr := parseBody(r)
		if parseErr == nil {
			result.Text = text
			result.HTML = html
			result.RawSize = rawSize
		}
	}
	return result, nil
}

var errNotFound = fmt.Errorf("message not found")

func IsNotFound(err error) bool {
	return err == errNotFound || (err != nil && strings.Contains(err.Error(), "invalid message id"))
}

func mapAddresses(in []*imap.Address) []Address {
	if len(in) == 0 {
		return nil
	}
	out := make([]Address, 0, len(in))
	for _, a := range in {
		if a == nil {
			continue
		}
		out = append(out, Address{
			Name:    a.PersonalName,
			Address: a.Address(),
		})
	}
	return out
}

func parseBody(r io.Reader) (text, html string, rawSize int, err error) {
	mr, err := gomessage.CreateReader(r)
	if err != nil {
		return "", "", 0, err
	}
	var total int
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return text, html, total, err
		}
		b, readErr := io.ReadAll(p.Body)
		if readErr != nil {
			continue
		}
		total += len(b)
		switch h := p.Header.(type) {
		case *gomessage.InlineHeader:
			ct, _, _ := h.ContentType()
			switch {
			case strings.HasPrefix(ct, "text/plain"):
				if text == "" {
					text = string(b)
				}
			case strings.HasPrefix(ct, "text/html"):
				if html == "" {
					html = string(b)
				}
			}
		}
	}
	return text, html, total, nil
}
