package mailcfg

import (
	"os"
	"strconv"
	"strings"
)

// Config for Phase 2 mail daemons.
type Config struct {
	DataDir       string
	SMTPAddr      string
	SubmitAddr    string
	IMAPAddr      string
	AdminAddr     string
	Hostname      string
	AdminUser     string
	AdminPassword string
	RelayHost     string
	ClamAddr      string
}

func Load() Config {
	return Config{
		DataDir:       getenv("DATA_DIR", "./data"),
		SMTPAddr:      getenv("SMTP_ADDR", ":2525"),
		SubmitAddr:    getenv("SUBMIT_ADDR", ":2587"),
		IMAPAddr:      getenv("IMAP_ADDR", ":2143"),
		AdminAddr:     getenv("ADMIN_ADDR", ":8090"),
		Hostname:      getenv("MAIL_HOSTNAME", "localhost"),
		AdminUser:     getenv("ADMIN_USER", "admin"),
		AdminPassword: getenv("ADMIN_PASSWORD", "changeme"),
		RelayHost:     getenv("RELAY_HOST", ""),
		ClamAddr:      getenv("CLAMAV_ADDR", ""),
	}
}

func getenv(k, fb string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
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
