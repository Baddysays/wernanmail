package dnsauth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"strings"

	"github.com/emersion/go-msgauth/dkim"
)

// Checker performs SPF/DKIM checks (best-effort).
type Checker struct{}

// CheckSPF returns pass|fail|softfail|none|error.
func (c *Checker) CheckSPF(ctx context.Context, from, ip string) string {
	_ = ctx
	at := strings.LastIndex(from, "@")
	if at < 0 {
		return "none"
	}
	domain := from[at+1:]
	txts, err := net.LookupTXT(domain)
	if err != nil {
		return "error"
	}
	var spf string
	for _, t := range txts {
		if strings.HasPrefix(strings.ToLower(t), "v=spf1") {
			spf = t
			break
		}
	}
	if spf == "" {
		return "none"
	}
	lower := strings.ToLower(spf)
	if strings.Contains(lower, "-all") && ip != "" {
		// Minimal: if explicit ip4: matches, pass; else softfail/fail heuristic
		if strings.Contains(lower, "ip4:"+ip) {
			return "pass"
		}
		if strings.Contains(lower, " +mx") || strings.Contains(lower, " mx") {
			return "softfail"
		}
		return "fail"
	}
	if strings.Contains(lower, "~all") {
		return "softfail"
	}
	return "none"
}

// CheckDKIM verifies DKIM signatures on raw message.
func (c *Checker) CheckDKIM(ctx context.Context, raw []byte) string {
	_ = ctx
	verifications, err := dkim.Verify(bytes.NewReader(raw))
	if err != nil || len(verifications) == 0 {
		return "none"
	}
	for _, v := range verifications {
		if v.Err != nil {
			return "fail"
		}
	}
	return "pass"
}

// KeyPair holds generated DKIM keys.
type KeyPair struct {
	PrivatePEM string
	PublicDNS  string // p=BASE64
	Selector   string
}

// GenerateDKIM creates an RSA-2048 key for the domain.
func GenerateDKIM(selector string) (*KeyPair, error) {
	if selector == "" {
		selector = "wernan"
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	priv := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	return &KeyPair{
		PrivatePEM: string(priv),
		PublicDNS:  "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pub),
		Selector:   selector,
	}, nil
}

// SignDKIM signs raw with domain private key.
func SignDKIM(raw []byte, domain, selector, privatePEM string) ([]byte, error) {
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return nil, fmt.Errorf("invalid dkim private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	opts := &dkim.SignOptions{
		Domain:   domain,
		Selector: selector,
		Signer:   key,
		Hash:     crypto.SHA256,
	}
	var buf bytes.Buffer
	if err := dkim.Sign(&buf, bytes.NewReader(raw), opts); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
