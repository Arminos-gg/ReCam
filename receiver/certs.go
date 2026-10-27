package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Safari only allows camera access on a secure page, so the receiver serves HTTPS
// with a certificate from its own local CA. The CA is created once and kept, so the
// phone only has to trust it once; the server certificate is re-issued whenever the
// PC's addresses change or it nears expiry.

const (
	caCertFile     = "ca.pem"
	caKeyFile      = "ca-key.pem"
	serverCertFile = "server.pem"
	serverKeyFile  = "server-key.pem"

	// Apple rejects TLS certificates valid for more than 825 days, and browsers cap
	// publicly trusted ones at 398, so stay under both.
	serverValidity = 397 * 24 * time.Hour
	renewBefore    = 30 * 24 * time.Hour
)

type certs struct {
	tls   tls.Certificate
	caPEM []byte
}

func loadOrCreateCerts(dir string, ips []net.IP) (*certs, error) {
	caCert, caKey, caPEM, err := loadOrCreateCA(dir)
	if err != nil {
		return nil, fmt.Errorf("local CA: %w", err)
	}

	certPath := filepath.Join(dir, serverCertFile)
	keyPath := filepath.Join(dir, serverKeyFile)
	if pair, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil && serverCertUsable(pair, caCert, ips) {
		return &certs{tls: pair, caPEM: caPEM}, nil
	}

	certPEM, keyPEM, err := issueServerCert(caCert, caKey, ips)
	if err != nil {
		return nil, fmt.Errorf("server certificate: %w", err)
	}
	if err := writeFiles(map[string][]byte{certPath: certPEM, keyPath: keyPEM}); err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &certs{tls: pair, caPEM: caPEM}, nil
}

func loadOrCreateCA(dir string) (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	if certErr == nil && keyErr == nil {
		cert, key, err := parsePair(certPEM, keyPEM)
		if err == nil && time.Now().Before(cert.NotAfter) {
			return cert, key, certPEM, nil
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "ReCam Local CA (" + host + ")", Organization: []string{"ReCam"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM, err = encodeKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	// a new CA invalidates any server certificate signed by the old one
	_ = os.Remove(filepath.Join(dir, serverCertFile))
	if err := writeFiles(map[string][]byte{certPath: certPEM, keyPath: keyPEM}); err != nil {
		return nil, nil, nil, err
	}
	return cert, key, certPEM, nil
}

func issueServerCert(ca *x509.Certificate, caKey *ecdsa.PrivateKey, ips []net.IP) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	host, _ := os.Hostname()
	dns := []string{"localhost"}
	if host != "" {
		dns = append(dns, host, host+".local")
	}
	tmpl := &x509.Certificate{
		SerialNumber: randomSerial(),
		Subject:      pkix.Name{CommonName: "ReCam receiver", Organization: []string{"ReCam"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(serverValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  append([]net.IP{net.IPv4(127, 0, 0, 1)}, ips...),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM, nil
}

// serverCertUsable reports whether a stored certificate was signed by the current
// CA, is not about to expire and still covers every address of this PC.
func serverCertUsable(pair tls.Certificate, ca *x509.Certificate, ips []net.IP) bool {
	if len(pair.Certificate) == 0 {
		return false
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || cert.CheckSignatureFrom(ca) != nil {
		return false
	}
	if time.Until(cert.NotAfter) < renewBefore {
		return false
	}
	for _, ip := range ips {
		if !slices.ContainsFunc(cert.IPAddresses, ip.Equal) {
			return false
		}
	}
	return true
}

func parsePair(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, nil, errors.New("bad PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

func randomSerial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	return n
}

func writeFiles(files map[string][]byte) error {
	for path, data := range files {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
