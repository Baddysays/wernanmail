package smtpd

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"log"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/greylist"
	"github.com/Baddysays/wernanmail/server/internal/mailtmpl"
	"github.com/Baddysays/wernanmail/server/internal/metrics"
	"github.com/Baddysays/wernanmail/server/internal/pipeline"
	"github.com/Baddysays/wernanmail/server/internal/queue"
	"github.com/Baddysays/wernanmail/server/internal/settings"
	"github.com/Baddysays/wernanmail/server/internal/store"
)

const maxMessageBytes = 30 << 20

// Backend implements go-smtp Backend for inbound + submission.
type Backend struct {
	Store            store.MessageStore
	Pipeline         *pipeline.Inbound
	Queue            *queue.Service
	Limiter          *settings.Limiter
	SendLimiter      *settings.Limiter
	AuthLimiter      *settings.Limiter
	Greylist         *greylist.Store
	GreylistSecs     int
	RequireAuth      bool
	Hostname         string
	MasterPassword   string
	SuperuserEnabled func() bool
	// OutboundPolicy returns templates/footers for authenticated outbound mail.
	OutboundPolicy func() mailtmpl.Policy
	Metrics        *metrics.Registry
}

func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	remote := ""
	helo := ""
	if c != nil {
		helo = c.Hostname()
		if c.Conn() != nil {
			remote = c.Conn().RemoteAddr().String()
			if i := strings.LastIndex(remote, ":"); i > 0 {
				remote = remote[:i]
			}
			remote = strings.Trim(remote, "[]")
		}
	}
	if b.Limiter != nil && !b.Limiter.Allow("smtp:"+remote) {
		return nil, &smtp.SMTPError{Code: 421, Message: "rate limited"}
	}
	return &Session{
		backend:  b,
		remoteIP: remote,
		helo:     helo,
	}, nil
}

// Session is one SMTP transaction.
type Session struct {
	backend      *Backend
	remoteIP     string
	helo         string
	from         string
	rcpts        []string
	authed       bool
	domainID     int64
	mailboxID    int64
	authedAddr   string
	authedLocal  string
	authedDomain string
}

func (s *Session) AuthMechanisms() []string {
	// LOGIN kept for Outlook / legacy clients that prefer it over PLAIN.
	return []string{sasl.Plain, sasl.Login}
}

func (s *Session) Auth(mech string) (sasl.Server, error) {
	finish := func(username, password string) error {
		m, d, err := s.authenticate(username, password)
		if err != nil || m == nil || d == nil {
			log.Printf("smtp auth fail ip=%s user=%q mech=%s", s.remoteIP, username, mech)
			if s.backend.AuthLimiter != nil && !s.backend.AuthLimiter.Allow("auth:"+s.remoteIP) {
				return &smtp.SMTPError{Code: 421, Message: "too many auth failures"}
			}
			return smtp.ErrAuthFailed
		}
		s.authed = true
		s.domainID = d.ID
		s.mailboxID = m.ID
		s.authedLocal = m.LocalPart
		s.authedDomain = d.Name
		s.authedAddr = m.Address(d.Name)
		log.Printf("smtp auth ok ip=%s user=%s mech=%s", s.remoteIP, s.authedAddr, mech)
		return nil
	}
	switch mech {
	case sasl.Plain:
		return sasl.NewPlainServer(func(identity, username, password string) error {
			_ = identity
			return finish(username, password)
		}), nil
	case sasl.Login:
		return sasl.NewLoginServer(func(username, password string) error {
			return finish(username, password)
		}), nil
	default:
		return nil, smtp.ErrAuthUnsupported
	}
}

func (s *Session) authenticate(username, password string) (*domain.Mailbox, *domain.Domain, error) {
	if s.backend.MasterPassword != "" && password == s.backend.MasterPassword {
		ok := s.backend.SuperuserEnabled == nil || s.backend.SuperuserEnabled()
		if ok {
			local, dom, ok := splitAddr(username)
			if ok {
				m, d, err := s.backend.Store.GetMailbox(context.Background(), dom, local)
				if err == nil && m != nil && d != nil && m.Enabled && d.Enabled {
					return m, d, nil
				}
			}
		}
	}
	return s.backend.Store.AuthenticateMailbox(context.Background(), username, password)
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	if s.backend.RequireAuth && !s.authed {
		return smtp.ErrAuthRequired
	}
	from = strings.Trim(strings.TrimSpace(from), "<>")
	if s.backend.RequireAuth && s.authed {
		if !s.senderAllowed(from) {
			return &smtp.SMTPError{
				Code:         550,
				EnhancedCode: smtp.EnhancedCode{5, 7, 1},
				Message:      "sender address not owned by authenticated user",
			}
		}
		if s.backend.SendLimiter != nil && !s.backend.SendLimiter.Allow("send:"+s.authedAddr) {
			return &smtp.SMTPError{Code: 450, Message: "send rate limit exceeded — try later"}
		}
	}
	s.from = from
	s.rcpts = nil
	return nil
}

func (s *Session) senderAllowed(from string) bool {
	from = strings.ToLower(strings.TrimSpace(from))
	if from == "" {
		return false
	}
	if from == strings.ToLower(s.authedAddr) {
		return true
	}
	local, dom, ok := splitAddr(from)
	if !ok || !strings.EqualFold(dom, s.authedDomain) {
		return false
	}
	if local == strings.ToLower(s.authedLocal) {
		return true
	}
	mid, err := s.backend.Store.ResolveRecipient(context.Background(), from)
	if err != nil {
		return false
	}
	return mid == s.mailboxID
}

func splitAddr(addr string) (local, domain string, ok bool) {
	addr = strings.Trim(addr, "<>")
	i := strings.LastIndex(addr, "@")
	if i <= 0 || i == len(addr)-1 {
		return "", "", false
	}
	return strings.ToLower(addr[:i]), strings.ToLower(addr[i+1:]), true
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	to = strings.Trim(strings.TrimSpace(to), "<>")
	if !s.backend.RequireAuth {
		if _, err := s.backend.Store.ResolveRecipient(context.Background(), to); err != nil {
			return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "unknown recipient"}
		}
		if s.backend.Greylist != nil && s.backend.GreylistSecs > 0 && !s.authed {
			ok := s.backend.Greylist.Allow(greylist.Key{
				IP: s.remoteIP, From: strings.ToLower(s.from), Rcpt: strings.ToLower(to),
			}, s.backend.GreylistSecs)
			if !ok {
				return &smtp.SMTPError{
					Code:         451,
					EnhancedCode: smtp.EnhancedCode{4, 7, 1},
					Message:      "greylisted — please try again shortly",
				}
			}
		}
	}
	s.rcpts = append(s.rcpts, to)
	return nil
}

func (s *Session) Data(r io.Reader) error {
	raw, err := io.ReadAll(io.LimitReader(r, maxMessageBytes))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var local, remote []string
	for _, rcpt := range s.rcpts {
		if _, err := s.backend.Store.ResolveRecipient(ctx, rcpt); err == nil {
			local = append(local, rcpt)
			continue
		}
		if s.authed {
			remote = append(remote, rcpt)
			continue
		}
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "unknown recipient"}
	}
	if len(local) == 0 && len(remote) == 0 {
		return &smtp.SMTPError{Code: 554, Message: "no valid recipients"}
	}
	if len(local) > 0 {
		res := s.backend.Pipeline.Process(ctx, pipeline.ProcessInput{
			From:       s.from,
			Recipients: local,
			RemoteIP:   s.remoteIP,
			Helo:       s.helo,
			Hostname:   s.backend.Hostname,
			AuthUser:   s.authedAddr,
			Raw:        raw,
		})
		if res.Err != nil && res.Action == domain.SpamReject {
			msg := res.SMTPMessage
			if msg == "" {
				msg = res.Err.Error()
			}
			return &smtp.SMTPError{Code: 554, Message: msg}
		}
		if res.Err != nil {
			log.Printf("pipeline warning: %v", res.Err)
		} else if s.backend.Metrics != nil {
			s.backend.Metrics.Inc("smtp_inbound_accepted", 1)
		}
	}
	if len(remote) > 0 {
		if s.backend.Queue == nil {
			return &smtp.SMTPError{Code: 451, Message: "outbound queue unavailable"}
		}
		outRaw := raw
		if s.authed && s.backend.OutboundPolicy != nil {
			p := s.backend.OutboundPolicy()
			if !p.Empty() {
				outRaw = mailtmpl.Apply(raw, s.from, p)
			}
		}
		if err := s.backend.Queue.EnqueueJSON(ctx, domain.JobOutboundSend, queue.OutboundPayload{
			From:     s.from,
			To:       remote,
			RawB64:   base64.StdEncoding.EncodeToString(outRaw),
			DomainID: s.domainID,
		}); err != nil {
			log.Printf("outbound enqueue: %v", err)
			return &smtp.SMTPError{Code: 451, Message: "failed to queue outbound mail"}
		}
		if s.backend.Metrics != nil {
			s.backend.Metrics.Inc("smtp_outbound_queued", 1)
		}
	}
	return nil
}

func (s *Session) Reset() {
	s.from = ""
	s.rcpts = nil
}

func (s *Session) Logout() error { return nil }

// ListenOpts configures an SMTP listener.
type ListenOpts struct {
	Addr              string
	Backend           *Backend
	Domain            string
	TLSConfig         *tls.Config
	AllowInsecureAuth bool
}

func ListenAndServe(addr string, be *Backend, domain string) error {
	return Listen(ListenOpts{Addr: addr, Backend: be, Domain: domain, AllowInsecureAuth: true})
}

func Listen(opts ListenOpts) error {
	s := smtp.NewServer(opts.Backend)
	s.Addr = opts.Addr
	s.Domain = opts.Domain
	s.ReadTimeout = 5 * time.Minute
	s.WriteTimeout = 5 * time.Minute
	s.MaxMessageBytes = maxMessageBytes
	s.MaxRecipients = 50
	s.TLSConfig = opts.TLSConfig
	s.AllowInsecureAuth = opts.AllowInsecureAuth
	if opts.TLSConfig == nil && !opts.AllowInsecureAuth {
		log.Printf("smtp %s: TLS not configured — forcing AllowInsecureAuth for local/dev", opts.Addr)
		s.AllowInsecureAuth = true
	}
	log.Printf("smtp listening on %s (auth_required=%v insecure_auth=%v tls=%v)",
		opts.Addr, opts.Backend.RequireAuth, s.AllowInsecureAuth, opts.TLSConfig != nil)
	return s.ListenAndServe()
}
