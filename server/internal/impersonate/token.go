package impersonate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TokenPayload is a short-lived admin → webmail handoff.
type TokenPayload struct {
	Username  string `json:"u"`
	ExpiresAt int64  `json:"e"`
	Actor     string `json:"a"`
}

// Issue signs a token valid for ttl.
func Issue(secret, username, actor string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("impersonate secret unset")
	}
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	p := TokenPayload{
		Username:  strings.TrimSpace(username),
		ExpiresAt: time.Now().UTC().Add(ttl).Unix(),
		Actor:     actor,
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	sig := sign(secret, body)
	return body + "." + sig, nil
}

// Parse verifies and returns the payload.
func Parse(secret, token string) (*TokenPayload, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("impersonate secret unset")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("bad token")
	}
	body, sig := parts[0], parts[1]
	if !hmac.Equal([]byte(sign(secret, body)), []byte(sig)) {
		return nil, errors.New("bad signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}
	var p TokenPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Username == "" || time.Now().UTC().Unix() > p.ExpiresAt {
		return nil, fmt.Errorf("token expired")
	}
	return &p, nil
}

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
