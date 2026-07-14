package mail

import (
	"bytes"
	"fmt"
	"io"
	"mime/quotedprintable"
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

	// STATUS UNSEEN for selectable mailboxes (badges in the client sidebar).
	for i := range folders {
		if folderHasAttr(folders[i].Attributes, "\\Noselect") {
			continue
		}
		status, err := c.Status(folders[i].Name, []imap.StatusItem{
			imap.StatusUnseen,
			imap.StatusMessages,
		})
		if err != nil {
			continue
		}
		folders[i].Unseen = status.Unseen
		folders[i].Messages = status.Messages
	}

	return folders, nil
}

func folderHasAttr(attrs []string, want string) bool {
	want = strings.ToLower(want)
	for _, a := range attrs {
		if strings.EqualFold(a, want) || strings.ToLower(a) == want {
			return true
		}
	}
	return false
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

	// Opening a message marks it read (best-effort).
	_ = UpdateFlags(creds, folder, id, FlagUpdate{Add: []string{imap.SeenFlag}})

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
			decoded := repairBodyText(string(b))
			switch {
			case strings.HasPrefix(ct, "text/plain"):
				if text == "" {
					text = decoded
				}
			case strings.HasPrefix(ct, "text/html"):
				if html == "" {
					html = decoded
				}
			}
		}
	}
	return text, html, total, nil
}

// repairBodyText fixes bodies that were stored as literal quoted-printable
// without a proper Content-Transfer-Encoding header (legacy send bug).
func repairBodyText(s string) string {
	if !looksLikeRawQP(s) {
		return s
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(s)))
	if err != nil || len(decoded) == 0 {
		return s
	}
	return string(decoded)
}

func looksLikeRawQP(s string) bool {
	// Typical UTF-8 Cyrillic/Latin as QP: =D0=BF or =C3=A9
	n := 0
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '=' && isHex(s[i+1]) && isHex(s[i+2]) {
			n++
			if n >= 3 {
				return true
			}
			i += 2
		}
	}
	return false
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
}

// FindSentFolder returns the mailbox with \Sent, or a name heuristic.
func FindSentFolder(creds session.Credentials) (string, error) {
	folders, err := ListFolders(creds)
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		for _, a := range f.Attributes {
			if strings.EqualFold(a, `\Sent`) || strings.Contains(strings.ToLower(a), "sent") {
				return f.Name, nil
			}
		}
	}
	for _, f := range folders {
		n := strings.ToLower(f.Name)
		if strings.Contains(n, "sent") || strings.Contains(n, "отправлен") {
			return f.Name, nil
		}
	}
	return "", fmt.Errorf("sent folder not found")
}

func FindTrashFolder(creds session.Credentials) (string, error) {
	folders, err := ListFolders(creds)
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		for _, a := range f.Attributes {
			al := strings.ToLower(a)
			if strings.Contains(al, "trash") || strings.Contains(al, "bin") {
				return f.Name, nil
			}
		}
	}
	for _, f := range folders {
		n := strings.ToLower(f.Name)
		if strings.Contains(n, "trash") || strings.Contains(n, "deleted") || strings.Contains(n, "корзин") {
			return f.Name, nil
		}
	}
	return "", fmt.Errorf("trash folder not found")
}

// AppendToSent stores a raw RFC822 message in the Sent mailbox.
func AppendToSent(creds session.Credentials, raw []byte) error {
	name, err := FindSentFolder(creds)
	if err != nil {
		return err
	}
	c, err := ConnectIMAP(creds)
	if err != nil {
		return err
	}
	defer func() { _ = c.Logout() }()

	literal := bytes.NewReader(raw)
	return c.Append(name, []string{imap.SeenFlag}, time.Now(), literal)
}

// FlagUpdate describes IMAP flag changes.
type FlagUpdate struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// UpdateFlags adds/removes IMAP flags on a UID.
func UpdateFlags(creds session.Credentials, folder, id string, upd FlagUpdate) error {
	if folder == "" {
		folder = "INBOX"
	}
	var uid uint32
	if _, err := fmt.Sscanf(id, "%d", &uid); err != nil || uid == 0 {
		return fmt.Errorf("invalid message id")
	}
	c, err := ConnectIMAP(creds)
	if err != nil {
		return err
	}
	defer func() { _ = c.Logout() }()

	if _, err := c.Select(folder, false); err != nil {
		return err
	}
	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)

	if len(upd.Add) > 0 {
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		vals := make([]interface{}, len(upd.Add))
		for i, f := range upd.Add {
			vals[i] = f
		}
		if err := c.UidStore(seqset, item, vals, nil); err != nil {
			return err
		}
	}
	if len(upd.Remove) > 0 {
		item := imap.FormatFlagsOp(imap.RemoveFlags, true)
		vals := make([]interface{}, len(upd.Remove))
		for i, f := range upd.Remove {
			vals[i] = f
		}
		if err := c.UidStore(seqset, item, vals, nil); err != nil {
			return err
		}
	}
	return nil
}

// MarkSeen sets \Seen on a message.
func MarkSeen(creds session.Credentials, folder, id string) error {
	return UpdateFlags(creds, folder, id, FlagUpdate{Add: []string{imap.SeenFlag}})
}

// TrashMessage moves a message to Trash (COPY+DELETE) or flags \Deleted.
func TrashMessage(creds session.Credentials, folder, id string) error {
	if folder == "" {
		folder = "INBOX"
	}
	var uid uint32
	if _, err := fmt.Sscanf(id, "%d", &uid); err != nil || uid == 0 {
		return fmt.Errorf("invalid message id")
	}

	c, err := ConnectIMAP(creds)
	if err != nil {
		return err
	}
	defer func() { _ = c.Logout() }()

	if _, err := c.Select(folder, false); err != nil {
		return err
	}
	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)

	trash, trashErr := FindTrashFolder(creds)
	if trashErr == nil && !strings.EqualFold(trash, folder) {
		if err := c.UidCopy(seqset, trash); err != nil {
			return err
		}
	}

	item := imap.FormatFlagsOp(imap.AddFlags, true)
	if err := c.UidStore(seqset, item, []interface{}{imap.DeletedFlag}, nil); err != nil {
		return err
	}
	return c.Expunge(nil)
}
