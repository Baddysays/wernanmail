package mailcfg

import (
	"crypto/tls"
	"log"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Config for Phase 2 mail daemons.
type Config struct {
	DataDir        string
	SMTPAddr       string
	SubmitAddr     string
	IMAPAddr       string
	AdminAddr      string
	Hostname       string
	EHLOHost       string // outbound EHLO; should match PTR when possible
	AdminUser      string
	AdminPassHash  []byte // bcrypt hash; never store plaintext after Load
	RelayHost      string
	ClamAddr       string
	TLSCertFile    string
	TLSKeyFile     string
	AdminCORS      []string
	SessionSecret  string // 32+ bytes preferred; used to encrypt webmail session secrets
	MasterPassword string // IMAP/SMTP master password for admin impersonation
	WebmailURL     string // public webmail base URL for "open as user"
}

func Load() Config {
	host := getenv("MAIL_HOSTNAME", "localhost")
	ehlo := getenv("MAIL_EHLO", host)
	if err := validateFQDN(ehlo); err != nil {
		log.Printf("mailcfg: MAIL_EHLO %q looks invalid: %v (continuing)", ehlo, err)
	}
	pass := getenvSecret("ADMIN_PASSWORD", "changeme")
	hashEnv := getenv("ADMIN_PASSWORD_HASH", "")
	var hash []byte
	if hashEnv != "" {
		hash = []byte(hashEnv)
	} else {
		h, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal("mailcfg: bcrypt admin password: ", err)
		}
		hash = h
		if pass == "changeme" {
			log.Printf("mailcfg: WARNING using default ADMIN_PASSWORD — set ADMIN_PASSWORD or ADMIN_PASSWORD_HASH")
		}
	}
	secret := getenvSecret("SESSION_SECRET", "")
	if secret == "" {
		// Derive a stable-enough secret from data dir + admin hash so restarts keep sessions
		// decryptable within one deployment without requiring extra env in MVP.
		secret = getenv("DATA_DIR", "./data") + string(hash)
		log.Printf("mailcfg: SESSION_SECRET unset — derived ephemeral key (set SESSION_SECRET in production)")
	}
	return Config{
		DataDir:        getenv("DATA_DIR", "./data"),
		SMTPAddr:       getenv("SMTP_ADDR", ":2525"),
		SubmitAddr:     getenv("SUBMIT_ADDR", ":2587"),
		IMAPAddr:       getenv("IMAP_ADDR", ":2143"),
		AdminAddr:      getenv("ADMIN_ADDR", ":8090"),
		Hostname:       host,
		EHLOHost:       ehlo,
		AdminUser:      getenv("ADMIN_USER", "admin"),
		AdminPassHash:  hash,
		RelayHost:      getenv("RELAY_HOST", ""),
		ClamAddr:       getenv("CLAMAV_ADDR", ""),
		TLSCertFile:    getenv("MAIL_TLS_CERT", ""),
		TLSKeyFile:     getenv("MAIL_TLS_KEY", ""),
		AdminCORS:      splitCSV(getenv("ADMIN_CORS_ORIGINS", "http://localhost:5174,http://127.0.0.1:5174")),
		SessionSecret:  secret,
		MasterPassword: getenvSecret("MAIL_MASTER_PASSWORD", ""),
		WebmailURL:     getenv("WEBMAIL_URL", ""),
	}
}

// LoadTLSConfig returns nil when cert/key are not configured (dev mode).
func (c Config) LoadTLSConfig() (*tls.Config, error) {
	if c.TLSCertFile == "" || c.TLSKeyFile == "" {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func (c Config) CheckAdminPassword(user, pass string) bool {
	if user != c.AdminUser {
		// Still run bcrypt to reduce timing leak on username.
		_ = bcrypt.CompareHashAndPassword(c.AdminPassHash, []byte(pass))
		return false
	}
	return bcrypt.CompareHashAndPassword(c.AdminPassHash, []byte(pass)) == nil
}

func validateFQDN(host string) error {
	host = strings.TrimSpace(host)
	if host == "" || strings.Contains(host, "://") || strings.ContainsAny(host, " /\\") {
		return errInvalidHost
	}
	return nil
}

var errInvalidHost = errString("invalid hostname")

type errString string

func (e errString) Error() string { return string(e) }

func getenv(k, fb string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fb
}

func getenvSecret(k, fb string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	if path := strings.TrimSpace(os.Getenv(k + "_FILE")); path != "" {
		if value, err := os.ReadFile(path); err == nil {
			if v := strings.TrimSpace(string(value)); v != "" {
				return v
			}
		}
	}
	return fb
}

func GetenvInt(k string, fb int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return fb
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fb
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
