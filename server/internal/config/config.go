package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds process-level settings (not mailbox credentials).
type Config struct {
	Addr            string
	CORSOrigins     []string
	CookieSecure    bool
	SessionCookie   string
	SessionTTLHours int
	SessionSecret   string
	MasterPassword  string
	DefaultIMAPHost string
	DefaultIMAPPort int
	DefaultSMTPHost string
	DefaultSMTPPort int
	DefaultTLS      bool
	MTAStsMode      string
	MTAStsMX        string
	MTAStsMaxAge    int
}

func Load() Config {
	addr := getenv("ADDR", "")
	if addr == "" {
		if p := getenv("PORT", ""); p != "" {
			if p[0] == ':' {
				addr = p
			} else {
				addr = ":" + p
			}
		} else {
			addr = ":8080"
		}
	}
	imapHost := getenv("IMAP_HOST", getenv("MAIL_HOSTNAME", "localhost"))
	smtpHost := getenv("SMTP_HOST", imapHost)
	return Config{
		Addr:            addr,
		CORSOrigins:     splitCSV(getenv("CORS_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")),
		CookieSecure:    getenvBool("COOKIE_SECURE", true),
		SessionCookie:   getenv("SESSION_COOKIE", "wernan_sid"),
		SessionTTLHours: getenvInt("SESSION_TTL_HOURS", 24),
		SessionSecret:   getenvSecret("SESSION_SECRET", ""),
		MasterPassword:  getenvSecret("MAIL_MASTER_PASSWORD", ""),
		DefaultIMAPHost: imapHost,
		DefaultIMAPPort: getenvInt("IMAP_PORT", 993),
		DefaultSMTPHost: smtpHost,
		DefaultSMTPPort: getenvInt("SMTP_PORT", 465),
		DefaultTLS:      getenvBool("MAIL_TLS", true),
		MTAStsMode:      getenv("MTA_STS_MODE", "testing"),
		MTAStsMX:        getenv("MTA_STS_MX", imapHost),
		MTAStsMaxAge:    getenvInt("MTA_STS_MAX_AGE", 86400),
	}
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getenvSecret(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	if path := strings.TrimSpace(os.Getenv(key + "_FILE")); path != "" {
		if value, err := os.ReadFile(path); err == nil {
			if v := strings.TrimSpace(string(value)); v != "" {
				return v
			}
		}
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getenvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
