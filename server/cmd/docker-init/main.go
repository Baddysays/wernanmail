package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "show-admin-password" {
		value, err := os.ReadFile("/run/secrets/admin_password")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("admin password: %s\n", strings.TrimSpace(string(value)))
		return
	}

	if err := os.MkdirAll("/run/secrets", 0o700); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll("/run/tls", 0o700); err != nil {
		log.Fatal(err)
	}

	createdPassword, err := ensureSecret("/run/secrets/admin_password", 24)
	if err != nil {
		log.Fatal(err)
	}
	if _, err = ensureSecret("/run/secrets/session_secret", 48); err != nil {
		log.Fatal(err)
	}
	if _, err = ensureSecret("/run/secrets/master_password", 32); err != nil {
		log.Fatal(err)
	}
	if err = ensureCertificate(strings.TrimSpace(os.Getenv("MAIL_HOSTNAME"))); err != nil {
		log.Fatal(err)
	}

	if createdPassword != "" {
		fmt.Println("Generated persistent Docker secrets and bootstrap TLS certificate.")
		fmt.Println("Retrieve the admin password with: docker compose run --rm init /app/docker-init show-admin-password")
	} else {
		fmt.Println("Docker secrets and TLS certificate already exist.")
	}
}

func ensureSecret(path string, bytes int) (string, error) {
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		_ = os.Chown(path, 10001, 10001)
		return "", nil
	}
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(value+"\n"), 0o400); err != nil {
		return "", err
	}
	return value, os.Chown(path, 10001, 10001)
}

func ensureCertificate(host string) error {
	certPath := "/run/tls/fullchain.pem"
	keyPath := "/run/tls/privkey.pem"
	if cert, certErr := os.Stat(certPath); certErr == nil && cert.Size() > 0 {
		if key, keyErr := os.Stat(keyPath); keyErr == nil && key.Size() > 0 {
			_ = os.Chown(certPath, 10001, 10001)
			_ = os.Chown(keyPath, 10001, 10001)
			return nil
		}
	}
	if host == "" {
		host = "localhost"
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return err
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Wernanmail"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host, "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err = writePEM(certPath, "CERTIFICATE", der); err != nil {
		return err
	}
	if err = writePEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)); err != nil {
		return err
	}
	if err = os.Chown(certPath, 10001, 10001); err != nil {
		return err
	}
	return os.Chown(keyPath, 10001, 10001)
}

func writePEM(path, typ string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o400)
	if err != nil {
		return err
	}
	if err = pem.Encode(f, &pem.Block{Type: typ, Bytes: data}); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Clean(path))
}
