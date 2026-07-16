package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"

	imapclient "github.com/emersion/go-imap/client"

	"github.com/Baddysays/wernanmail/server/internal/antispam"
	"github.com/Baddysays/wernanmail/server/internal/antivirus"
	"github.com/Baddysays/wernanmail/server/internal/api"
	"github.com/Baddysays/wernanmail/server/internal/config"
	"github.com/Baddysays/wernanmail/server/internal/domain"
	"github.com/Baddysays/wernanmail/server/internal/imapd"
	"github.com/Baddysays/wernanmail/server/internal/pipeline"
	"github.com/Baddysays/wernanmail/server/internal/queue"
	"github.com/Baddysays/wernanmail/server/internal/session"
	"github.com/Baddysays/wernanmail/server/internal/smtpd"
	"github.com/Baddysays/wernanmail/server/internal/store/sqlite"
	"github.com/Baddysays/wernanmail/server/internal/worker"
)

// TestSMTPInboundQueueWorkerIMAPAPI proves the core mail path:
// SMTP DATA → pipeline enqueue → worker AppendMessage → IMAP FETCH → API list.
func TestSMTPInboundQueueWorkerIMAPAPI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	st, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const (
		domainName = "example.test"
		localPart  = "user"
		password   = "test-pass-9xK2"
		addr       = localPart + "@" + domainName
	)
	hash, err := sqlite.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	d := &domain.Domain{Name: domainName, Enabled: true}
	if err := st.UpsertDomain(ctx, d); err != nil {
		t.Fatal(err)
	}
	mb := &domain.Mailbox{
		DomainID: d.ID, LocalPart: localPart, PasswordHash: hash,
		DisplayName: "Test User", Enabled: true,
	}
	if err := st.UpsertMailbox(ctx, mb); err != nil {
		t.Fatal(err)
	}

	qs := &queue.Service{Store: st}
	spam := antispam.New(nil, 100, 50, nil) // no DNS/RBL — stable offline CI
	pipe := &pipeline.Inbound{Store: st, Queue: qs, Spam: spam}
	pipe.SetPolicy(antivirus.Noop{}, 25<<20)

	smtpPort := freePort(t)
	imapPort := freePort(t)

	go func() {
		be := &smtpd.Backend{
			Store: st, Pipeline: pipe, Queue: qs,
			RequireAuth: false, Hostname: "mail." + domainName,
		}
		_ = smtpd.Listen(smtpd.ListenOpts{
			Addr:              fmt.Sprintf("127.0.0.1:%d", smtpPort),
			Backend:           be,
			Domain:            "mail." + domainName,
			AllowInsecureAuth: true,
		})
	}()
	go func() {
		_ = imapd.Listen(imapd.ListenOpts{
			Addr:              fmt.Sprintf("127.0.0.1:%d", imapPort),
			Backend:           &imapd.Backend{Store: st},
			AllowInsecureAuth: true,
		})
	}()
	go func() {
		(&worker.Runner{
			Store: st, Queue: st,
			PollEvery: 50 * time.Millisecond,
			WorkerID:  "integ-1",
			Hostname:  "mail." + domainName,
		}).Run(ctx)
	}()

	waitTCP(t, ctx, fmt.Sprintf("127.0.0.1:%d", smtpPort))
	waitTCP(t, ctx, fmt.Sprintf("127.0.0.1:%d", imapPort))

	subject := "integ-smoke-" + time.Now().UTC().Format("150405.000")
	raw := []byte(strings.Join([]string{
		"From: sender@elsewhere.test",
		"To: " + addr,
		"Subject: " + subject,
		"Message-ID: <" + subject + "@elsewhere.test>",
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"hello from integration smoke",
		"",
	}, "\r\n"))

	if err := smtp.SendMail(
		fmt.Sprintf("127.0.0.1:%d", smtpPort),
		nil,
		"sender@elsewhere.test",
		[]string{addr},
		raw,
	); err != nil {
		t.Fatalf("smtp send: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var delivered []domain.Message
	for time.Now().Before(deadline) {
		msgs, err := st.ListMessages(ctx, mb.ID, domain.FolderInbox, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range msgs {
			if m.Subject == subject {
				delivered = append(delivered, m)
			}
		}
		if len(delivered) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(delivered) == 0 {
		t.Fatal("message never appeared in store INBOX after SMTP+worker")
	}

	// IMAP LOGIN + SELECT + message count
	ic, err := imapclient.Dial(fmt.Sprintf("127.0.0.1:%d", imapPort))
	if err != nil {
		t.Fatalf("imap dial: %v", err)
	}
	defer ic.Logout()
	if err := ic.Login(addr, password); err != nil {
		t.Fatalf("imap login: %v", err)
	}
	mboxStatus, err := ic.Select("INBOX", true)
	if err != nil {
		t.Fatalf("imap select: %v", err)
	}
	if mboxStatus.Messages < 1 {
		t.Fatalf("imap INBOX empty, want >=1")
	}

	// API: login over IMAP (tls=false) and list messages
	sessStore := session.NewStoreWithSecret(time.Hour, "integ-session-secret")
	apiH := &api.Handler{
		Cfg: config.Config{
			CORSOrigins:   []string{"http://localhost"},
			CookieSecure:  false,
			SessionCookie: "wernan_sid",
		},
		Store: sessStore,
	}
	srv := httptest.NewServer(api.NewRouter(apiH))
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	tlsFalse := false
	loginBody, _ := json.Marshal(map[string]any{
		"imapHost": "127.0.0.1",
		"imapPort": imapPort,
		"smtpHost": "127.0.0.1",
		"smtpPort": smtpPort,
		"username": addr,
		"password": password,
		"tls":      &tlsFalse,
	})
	resp, err := httpClient.Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("api login: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("api login status %d: %s", resp.StatusCode, body)
	}

	listResp, err := httpClient.Get(srv.URL + "/api/messages?folder=INBOX")
	if err != nil {
		t.Fatalf("api messages: %v", err)
	}
	listBody, _ := io.ReadAll(listResp.Body)
	listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("api messages status %d: %s", listResp.StatusCode, listBody)
	}
	if !bytes.Contains(listBody, []byte(subject)) {
		t.Fatalf("api message list missing subject %q: %s", subject, listBody)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func waitTCP(t *testing.T, ctx context.Context, addr string) {
	t.Helper()
	for {
		d := net.Dialer{Timeout: 200 * time.Millisecond}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("wait for %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
