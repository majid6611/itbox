package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// tlsDir holds a self-signed cert+key per module that needs one — see
// Manifest.NeedsTLS. Lives under confDir (the same host directory bind-
// mounted into both this container and the nginx one) so nginx can read
// the files nginx.go's own generated server block references.
func (m *Manager) tlsDir(moduleID string) string {
	return filepath.Join(m.confDir, "tls", moduleID)
}

// ensureSelfSignedCert returns a cert+key file pair for hostname,
// generating a fresh 10-year self-signed one if none exists yet or the
// existing one is for a different hostname (e.g. the admin changed the
// base domain before reinstalling). Self-signed rather than a real CA
// cert since there's no ACME account or verified domain to issue one
// against yet — whoever opens a call sees a one-time browser warning to
// click through, same tradeoff every self-signed deployment has, until a
// real cert is swapped in later.
func (m *Manager) ensureSelfSignedCert(moduleID, hostname string) (certPath, keyPath string, err error) {
	dir := m.tlsDir(moduleID)
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	if certMatchesHostname(certPath, hostname) {
		return certPath, keyPath, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create tls dir: %w", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate tls key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate tls serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostname},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(hostname); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{hostname}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("create tls cert: %w", err)
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return "", "", fmt.Errorf("write tls cert: %w", err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return "", "", fmt.Errorf("encode tls cert: %w", err)
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("write tls key: %w", err)
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return "", "", fmt.Errorf("encode tls key: %w", err)
	}

	return certPath, keyPath, nil
}

// certMatchesHostname reports whether certPath already holds a still-valid
// certificate for exactly hostname — false (triggering regeneration) for
// anything missing, unparseable, or for a different hostname.
func certMatchesHostname(certPath, hostname string) bool {
	pair, err := tls.LoadX509KeyPair(certPath, filepath.Join(filepath.Dir(certPath), "key.pem"))
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return false
	}
	return cert.VerifyHostname(hostname) == nil && time.Now().Before(cert.NotAfter)
}
