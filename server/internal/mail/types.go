package mail

import "time"

// Folder is an IMAP mailbox/folder.
type Folder struct {
	Name       string   `json:"name"`
	Delimiter  string   `json:"delimiter,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
	Unseen     uint32   `json:"unseen,omitempty"`
	Messages   uint32   `json:"messages,omitempty"`
}

// Address is a mail address.
type Address struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

// MessageSummary is a list-row message.
type MessageSummary struct {
	ID            string    `json:"id"`
	UID           uint32    `json:"uid"`
	Subject       string    `json:"subject"`
	From          []Address `json:"from"`
	To            []Address `json:"to,omitempty"`
	Date          time.Time `json:"date"`
	Flags         []string  `json:"flags,omitempty"`
	Size          uint32    `json:"size,omitempty"`
	HasAttachment bool      `json:"hasAttachment,omitempty"`
	MessageID     string    `json:"messageId,omitempty"`
	Preview       string    `json:"preview,omitempty"`
}

// AttachmentMeta describes an attachment without its payload.
type AttachmentMeta struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
}

// Message is a full message payload for the reading pane.
type Message struct {
	MessageSummary
	CC          []Address        `json:"cc,omitempty"`
	Text        string           `json:"text,omitempty"`
	HTML        string           `json:"html,omitempty"`
	RawSize     int              `json:"rawSize,omitempty"`
	Attachments []AttachmentMeta `json:"attachments,omitempty"`
}

// OutboundAttachment is a base64-encoded file for compose/send.
type OutboundAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Content     string `json:"content"` // base64
}

// SendRequest is the compose/send body.
type SendRequest struct {
	To          []string             `json:"to"`
	CC          []string             `json:"cc,omitempty"`
	BCC         []string             `json:"bcc,omitempty"`
	Subject     string               `json:"subject"`
	Text        string               `json:"text"`
	HTML        string               `json:"html,omitempty"`
	Attachments []OutboundAttachment `json:"attachments,omitempty"`
	InReplyTo   string               `json:"inReplyTo,omitempty"`
	References  string               `json:"references,omitempty"`
}
