package smtpd

import (
	"context"
	"io"
	"log"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/pipeline"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store"
)

// Backend implements go-smtp Backend for inbound + submission.
type Backend struct {
	Store       store.MessageStore
	Pipeline    *pipeline.Inbound
	Limiter     *settings.Limiter
	RequireAuth bool // true for submission port
}

func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	remote := ""
	if c != nil && c.Conn() != nil {
		remote = c.Conn().RemoteAddr().String()
		if i := strings.LastIndex(remote, ":"); i > 0 {
			remote = remote[:i]
		}
		remote = strings.Trim(remote, "[]")
	}
	if b.Limiter != nil && !b.Limiter.Allow("smtp:"+remote) {
		return nil, &smtp.SMTPError{Code: 421, Message: "rate limited"}
	}
	return &Session{
		backend:  b,
		remoteIP: remote,
	}, nil
}

// Session is one SMTP transaction.
type Session struct {
	backend  *Backend
	remoteIP string
	helo     string
	from     string
	rcpts    []string
	authed   bool
}

func (s *Session) AuthMechanisms() []string {
	return []string{sasl.Plain}
}

func (s *Session) Auth(mech string) (sasl.Server, error) {
	if mech != sasl.Plain {
		return nil, smtp.ErrAuthUnsupported
	}
	return sasl.NewPlainServer(func(identity, username, password string) error {
		_ = identity
		m, d, err := s.backend.Store.AuthenticateMailbox(context.Background(), username, password)
		if err != nil || m == nil || d == nil {
			return smtp.ErrAuthFailed
		}
		s.authed = true
		return nil
	}), nil
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	if s.backend.RequireAuth && !s.authed {
		return smtp.ErrAuthRequired
	}
	s.from = from
	s.rcpts = nil
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.rcpts = append(s.rcpts, to)
	if !s.backend.RequireAuth {
		if _, err := s.backend.Store.ResolveRecipient(context.Background(), to); err != nil {
			return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "unknown recipient"}
		}
	}
	return nil
}

func (s *Session) Data(r io.Reader) error {
	raw, err := io.ReadAll(io.LimitReader(r, 30<<20))
	if err != nil {
		return err
	}
	res := s.backend.Pipeline.Process(context.Background(), s.from, s.rcpts, s.remoteIP, s.helo, raw)
	if res.Err != nil && res.Action == domain.SpamReject {
		return &smtp.SMTPError{Code: 554, Message: res.Err.Error()}
	}
	if res.Err != nil {
		log.Printf("pipeline warning: %v", res.Err)
	}
	return nil
}

func (s *Session) Reset() {
	s.from = ""
	s.rcpts = nil
}

func (s *Session) Logout() error { return nil }

// ListenAndServe starts an SMTP server.
func ListenAndServe(addr string, be *Backend, domain string) error {
	s := smtp.NewServer(be)
	s.Addr = addr
	s.Domain = domain
	s.ReadTimeout = 5 * time.Minute
	s.WriteTimeout = 5 * time.Minute
	s.MaxMessageBytes = 30 << 20
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true
	log.Printf("smtp listening on %s (auth_required=%v)", addr, be.RequireAuth)
	return s.ListenAndServe()
}
