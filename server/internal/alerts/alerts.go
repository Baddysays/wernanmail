package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/Baddysays/wernanmail/server/internal/outbound"
	"github.com/Baddysays/wernanmail/server/internal/settings"
)

// Issue is one alertable condition.
type Issue struct {
	Key    string
	Title  string
	Detail string
}

// Snapshot is the health input for Evaluate.
type Snapshot struct {
	StackMode    string
	StackMissing []string
	QueuePending int
	QueueDead    int
	QueueErr     string
	Host         string
}

// Config is loaded from admin settings (operator-entered, never hardcoded).
type Config struct {
	Enabled        bool
	Emails         []string
	TelegramToken  string
	TelegramChatID string
	WebhookURL     string
	Cooldown        time.Duration
	PendingWarn    int
	From           string // envelope/from for email alerts
	EHLO           string
	RelayHost      string
}

func ConfigFromSettings(sm *settings.Manager, from, ehlo, relay string) Config {
	emails := splitList(sm.Get(settings.KeyAlertsEmail))
	cool := sm.GetInt(settings.KeyAlertsCooldownMinutes, 60)
	if cool < 1 {
		cool = 1
	}
	pending := sm.GetInt(settings.KeyAlertsPendingWarn, 50)
	if pending < 1 {
		pending = 50
	}
	return Config{
		Enabled:        sm.GetBool(settings.KeyAlertsEnabled, false),
		Emails:         emails,
		TelegramToken:  strings.TrimSpace(sm.Get(settings.KeyAlertsTelegramBotToken)),
		TelegramChatID: strings.TrimSpace(sm.Get(settings.KeyAlertsTelegramChatID)),
		WebhookURL:     strings.TrimSpace(sm.Get(settings.KeyAlertsWebhookURL)),
		Cooldown:        time.Duration(cool) * time.Minute,
		PendingWarn:    pending,
		From:           strings.TrimSpace(from),
		EHLO:           strings.TrimSpace(ehlo),
		RelayHost:      strings.TrimSpace(relay),
	}
}

func (c Config) HasChannel() bool {
	return len(c.Emails) > 0 || (c.TelegramToken != "" && c.TelegramChatID != "") || c.WebhookURL != ""
}

// CollectIssues builds the alert list from snapshot + thresholds.
func CollectIssues(s Snapshot, pendingWarn int) []Issue {
	var out []Issue
	if s.QueueErr != "" {
		out = append(out, Issue{Key: "queue.error", Title: "Queue store error", Detail: s.QueueErr})
	}
	if s.QueueDead > 0 {
		out = append(out, Issue{
			Key: "queue.dead", Title: "Dead queue jobs",
			Detail: fmt.Sprintf("%d dead job(s) in the outbound queue", s.QueueDead),
		})
	}
	if pendingWarn > 0 && s.QueuePending >= pendingWarn {
		out = append(out, Issue{
			Key: "queue.pending", Title: "Queue backlog",
			Detail: fmt.Sprintf("%d pending job(s) (warn ≥ %d)", s.QueuePending, pendingWarn),
		})
	}
	if s.StackMode == "proc" && len(s.StackMissing) > 0 {
		out = append(out, Issue{
			Key: "stack.missing", Title: "Mail processes missing",
			Detail: "not running: " + strings.Join(s.StackMissing, ", "),
		})
	}
	return out
}

// Watcher polls health and notifies configured channels with per-key cooldown.
type Watcher struct {
	mu       sync.Mutex
	lastSent map[string]time.Time
	HTTP     *http.Client
}

func NewWatcher() *Watcher {
	return &Watcher{
		lastSent: map[string]time.Time{},
		HTTP:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (w *Watcher) Notify(ctx context.Context, cfg Config, issues []Issue) []error {
	if !cfg.Enabled || !cfg.HasChannel() || len(issues) == 0 {
		return nil
	}
	var errs []error
	now := time.Now()
	for _, issue := range issues {
		if !w.allow(issue.Key, cfg.Cooldown, now) {
			continue
		}
		if err := w.dispatch(ctx, cfg, issue); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", issue.Key, err))
			continue
		}
		w.mark(issue.Key, now)
	}
	return errs
}

// NotifyTest sends one test message ignoring cooldown.
func (w *Watcher) NotifyTest(ctx context.Context, cfg Config) error {
	if !cfg.HasChannel() {
		return fmt.Errorf("no alert channel configured (email, Telegram, or webhook)")
	}
	issue := Issue{
		Key:    "test",
		Title:  "Wernanmail alert test",
		Detail: "This is a test notification from admin settings. If you received it, alerts work.",
	}
	return w.dispatch(ctx, cfg, issue)
}

func (w *Watcher) allow(key string, cool time.Duration, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cool <= 0 {
		cool = time.Hour
	}
	if t, ok := w.lastSent[key]; ok && now.Sub(t) < cool {
		return false
	}
	return true
}

func (w *Watcher) mark(key string, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastSent[key] = now
}

func (w *Watcher) dispatch(ctx context.Context, cfg Config, issue Issue) error {
	var errs []string
	body := formatPlain(cfg, issue)
	if len(cfg.Emails) > 0 {
		if err := sendEmail(ctx, cfg, issue, body); err != nil {
			errs = append(errs, "email: "+err.Error())
		}
	}
	if cfg.TelegramToken != "" && cfg.TelegramChatID != "" {
		if err := w.sendTelegram(ctx, cfg, body); err != nil {
			errs = append(errs, "telegram: "+err.Error())
		}
	}
	if cfg.WebhookURL != "" {
		if err := w.sendWebhook(ctx, cfg, issue, body); err != nil {
			errs = append(errs, "webhook: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func formatPlain(cfg Config, issue Issue) string {
	host := cfg.EHLO
	if host == "" {
		host = "wernanmail"
	}
	return fmt.Sprintf("[%s] %s\n\n%s\n\nHost: %s\nKey: %s\nTime: %s UTC",
		host, issue.Title, issue.Detail, host, issue.Key, time.Now().UTC().Format(time.RFC3339))
}

func sendEmail(ctx context.Context, cfg Config, issue Issue, body string) error {
	from := cfg.From
	if from == "" {
		host := cfg.EHLO
		if host == "" {
			host = "localhost"
		}
		from = "alerts@" + host
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("bad from address: %w", err)
	}
	subject := fmt.Sprintf("[wernanmail] %s", issue.Title)
	var msg bytes.Buffer
	fmt.Fprintf(&msg, "From: %s\r\n", from)
	fmt.Fprintf(&msg, "To: %s\r\n", strings.Join(cfg.Emails, ", "))
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	msg.WriteString(body)
	msg.WriteString("\r\n")
	t := &outbound.SMTPTransporter{
		RelayHost: cfg.RelayHost,
		EHLOHost:  cfg.EHLO,
		Timeout:   20 * time.Second,
	}
	return t.Send(ctx, from, cfg.Emails, msg.Bytes())
}

func (w *Watcher) sendTelegram(ctx context.Context, cfg Config, text string) error {
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.TelegramToken)
	payload, _ := json.Marshal(map[string]any{
		"chat_id":                  cfg.TelegramChatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (w *Watcher) sendWebhook(ctx context.Context, cfg Config, issue Issue, text string) error {
	payload, _ := json.Marshal(map[string]any{
		"source":  "wernanmail",
		"key":     issue.Key,
		"title":   issue.Title,
		"detail":  issue.Detail,
		"text":    text,
		"host":    cfg.EHLO,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook HTTP %d", resp.StatusCode)
	}
	return nil
}

func splitList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		if _, err := mail.ParseAddress(p); err != nil {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
