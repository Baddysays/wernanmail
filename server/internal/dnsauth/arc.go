package dnsauth

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-msgauth/authres"
	"github.com/masa23/mmauth"
	"github.com/masa23/mmauth/arc"
)

var arcSignHeaders = []string{
	"from", "to", "cc", "subject", "date", "message-id",
	"mime-version", "content-type", "reply-to", "dkim-signature",
}

// CheckARC verifies an existing ARC chain on raw. Returns pass|fail|none|temperror|permerror.
func (c *Checker) CheckARC(raw []byte) string {
	m := mmauth.NewMMAuth()
	m.AddBodyHash(mmauth.BodyCanonicalizationAndAlgorithm{
		Body:      mmauth.CanonicalizationRelaxed,
		Algorithm: crypto.SHA256,
	})
	if _, err := m.Write(normalizeCRLF(raw)); err != nil {
		return "none"
	}
	if err := m.Close(); err != nil {
		return "none"
	}
	if m.AuthenticationHeaders == nil || m.AuthenticationHeaders.ARCSignatures == nil {
		return "none"
	}
	if m.AuthenticationHeaders.ARCSignatures.GetMaxInstance() == 0 {
		return "none"
	}
	m.Verify()
	switch m.AuthenticationHeaders.ARCSignatures.GetVerifyResult() {
	case arc.VerifyStatusPass:
		return "pass"
	case arc.VerifyStatusFail:
		return "fail"
	case arc.VerifyStatusTempErr:
		return "temperror"
	case arc.VerifyStatusPermErr:
		return "permerror"
	case arc.VerifyStatusNeutral:
		return "neutral"
	default:
		return "none"
	}
}

// FormatAuthResults builds an Authentication-Results header value (no field name).
func FormatAuthResults(authServID, spf, dkim, arcRes string) string {
	if strings.TrimSpace(authServID) == "" {
		authServID = "localhost"
	}
	results := []authres.Result{
		&authres.SPFResult{Value: authres.ResultValue(normalizeAuthResult(spf))},
		&authres.DKIMResult{Value: authres.ResultValue(normalizeAuthResult(dkim))},
		&authres.GenericResult{Method: "arc", Value: authres.ResultValue(normalizeAuthResult(arcRes))},
	}
	return authres.Format(authServID, results)
}

// PrependAuthResults adds Authentication-Results at the top of the message.
func PrependAuthResults(raw []byte, authServID, spf, dkim, arcRes string) []byte {
	line := "Authentication-Results: " + FormatAuthResults(authServID, spf, dkim, arcRes) + "\r\n"
	return append([]byte(line), normalizeCRLF(raw)...)
}

// SealARC adds an ARC set (AAR/AMS/AS) using the domain DKIM RSA key.
// Call after DKIM signing. authServID is typically MAIL_HOSTNAME.
func SealARC(raw []byte, domain, selector, privatePEM, authServID string, aarResults []string) ([]byte, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	selector = strings.TrimSpace(selector)
	if domain == "" || selector == "" || strings.TrimSpace(privatePEM) == "" {
		return raw, fmt.Errorf("arc: missing domain, selector, or key")
	}
	if strings.TrimSpace(authServID) == "" {
		authServID = domain
	}
	key, err := parseRSAPrivateKey(privatePEM)
	if err != nil {
		return raw, err
	}

	msg := normalizeCRLF(raw)
	m := mmauth.NewMMAuth()
	m.AddBodyHash(mmauth.BodyCanonicalizationAndAlgorithm{
		Body:      mmauth.CanonicalizationRelaxed,
		Algorithm: crypto.SHA256,
	})
	if _, err := m.Write(msg); err != nil {
		return raw, fmt.Errorf("arc parse: %w", err)
	}
	if err := m.Close(); err != nil {
		return raw, fmt.Errorf("arc parse: %w", err)
	}
	if m.AuthenticationHeaders == nil {
		return raw, fmt.Errorf("arc: no auth headers parsed")
	}
	ah := m.AuthenticationHeaders.ARCSignatures
	if ah == nil {
		empty := arc.Signatures{}
		ah = &empty
		m.AuthenticationHeaders.ARCSignatures = ah
	}
	m.Verify()
	if ah.GetARCChainValidation() == arc.ChainValidationResultFail {
		return raw, fmt.Errorf("arc: existing chain cv=fail, not sealing")
	}

	bodyHash := m.GetBodyHash(mmauth.BodyCanonicalizationAndAlgorithm{
		Body:      mmauth.CanonicalizationRelaxed,
		Algorithm: crypto.SHA256,
	})
	if bodyHash == "" {
		return raw, fmt.Errorf("arc: empty body hash")
	}

	instance := ah.GetMaxInstance() + 1
	ams := arc.ARCMessageSignature{
		InstanceNumber:   instance,
		Algorithm:        arc.SignatureAlgorithmRSA_SHA256,
		Domain:           domain,
		Selector:         selector,
		Canonicalization: "relaxed/relaxed",
		BodyHash:         bodyHash,
		Timestamp:        time.Now().Unix(),
	}
	if err := ams.Sign(mmauth.ExtractHeadersDKIM(m.Headers, arcSignHeaders), key); err != nil {
		return raw, fmt.Errorf("arc ams: %w", err)
	}

	if len(aarResults) == 0 {
		aarResults = []string{
			"dkim=pass header.d=" + domain,
			"spf=none",
		}
	}
	aar := arc.ARCAuthenticationResults{
		InstanceNumber: instance,
		AuthServId:     authServID,
		Results:        aarResults,
	}

	cv := arc.ChainValidationResult(ah.GetVerifyResult())
	if ah.GetMaxInstance() == 0 {
		cv = arc.ChainValidationResultNone
	}
	seal := arc.ARCSeal{
		InstanceNumber:  instance,
		Algorithm:       arc.SignatureAlgorithmRSA_SHA256,
		Domain:          domain,
		Selector:        selector,
		ChainValidation: cv,
		Timestamp:       time.Now().Unix(),
	}
	sealHeaders := ah.GetARCHeaders()
	sealHeaders = append(sealHeaders, "ARC-Authentication-Results: "+aar.String()+"\r\n")
	sealHeaders = append(sealHeaders, "ARC-Message-Signature: "+ams.String()+"\r\n")
	if err := seal.Sign(sealHeaders, key); err != nil {
		return raw, fmt.Errorf("arc seal: %w", err)
	}

	var out bytes.Buffer
	out.WriteString("ARC-Seal: " + seal.String() + "\r\n")
	out.WriteString("ARC-Message-Signature: " + ams.String() + "\r\n")
	out.WriteString("ARC-Authentication-Results: " + aar.String() + "\r\n")
	out.Write(msg)
	return out.Bytes(), nil
}

func parseRSAPrivateKey(privatePEM string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func normalizeCRLF(raw []byte) []byte {
	s := string(raw)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	return []byte(s)
}

func normalizeAuthResult(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "pass", "fail", "softfail", "neutral", "none", "temperror", "permerror", "policy":
		return v
	case "error":
		return "temperror"
	default:
		if v == "" {
			return "none"
		}
		return v
	}
}
